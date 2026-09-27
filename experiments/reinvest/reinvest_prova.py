#!/usr/bin/env python3
"""
reinvest_prova.py
Prototipo di "reap reinvest": dopo la pulizia, propone poche skill nuove che farebbero
il lavoro che fai più spesso o che ti costa di più. Zero domande: guarda solo le tue sessioni.

  0. Sessioni     solo quelle scritte da te (niente `claude -p`, subagent, sessione in corso)
  1. Tipi lavoro  UNA chiamata a Claude raggruppa le sessioni per tipo di lavoro (non per progetto) e scrive
                  per ognuno il compito da delegare + ricerche; si saltano i tipi rari e quelli coperti da
                  skill che usi già
  2. Candidati    skills.sh (solo GitHub se risponde 429) + cataloghi GitHub, confrontati con il compito
  3. Filtri       già installata, doppione di una che usi, sicurezza; la tua skill più vicina va a Claude
  4. Scelta       Claude legge le finaliste e sceglie al massimo 3 proposte, o nessuna
  5. Verifica     ogni proposta riletta per intero da sola: via se è per un altro progetto/agente/piattaforma,
                  a pagamento, pubblicità o consigli generici
  +  Gli altri     cosa è più installato su skills.sh nel tuo stack

Richiede laya_prove_mac.py nella stessa cartella (riusa lettura sessioni, download, embedding).
Uso:  .venv/bin/python reinvest_prova.py
Risultato in prove_laya/reinvest.md
"""
from __future__ import annotations

import argparse
import json
import os
import re
import statistics
import subprocess
import shutil
import sys
import tempfile
import time
import urllib.parse
import urllib.request
from collections import Counter, defaultdict
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

import laya_prove_mac as base
from laya_prove_mac import (OUT, CACHE, GITHUB_CATALOGS, RAW_GUESSES, SKILLS_SH, Emb, blocks, canon, clean, cut,
                            fetch_github, fetch_skillssh, http_json, is_noise, parse_frontmatter, read_installed,
                            read_local_catalog, read_sessions, unique_by_name, user_text, CMD_RE, NEG_WORDS)

MODEL = "sonnet"

MAX_CAND = 10  # candidate per compito che Claude legge per intero
MD_CHARS = 4000  # quanto SKILL.md legge Claude: i dettagli "scritta per un altro progetto" stanno spesso dopo l'intro
MD_VERIFY_CHARS = 15000  # la verifica di ogni proposta legge il SKILL.md quasi sempre intero
DUP_USED = 0.75  # sopra questa somiglianza con una skill che USI, la candidata si scarta senza chiedere

RISKY = [
    (r"curl[^\n|]{0,200}\|\s*(ba|z)?sh", "scarica ed esegue codice"),
    (r"wget[^\n|]{0,200}\|\s*(ba|z)?sh", "scarica ed esegue codice"),
    (r"ignore (all )?(previous|prior) instructions", "prova a scavalcare le istruzioni"),
    (r"base64\s+(-d|--decode)", "decodifica contenuti nascosti"),
    (r"(webhook|pastebin|ngrok|requestbin)", "possibile invio di dati all'esterno"),
    (r"(\.env|id_rsa|\.ssh/|keychain|api[_-]?key)[^\n]{0,80}(curl|post|upload|send|fetch)", "possibile invio di segreti"),
    (r"rm\s+-rf\s+(/|~)(\s|$)", "comando distruttivo"),
    (r"xattr\s+-c", "aggira la quarantena di macOS"),
]


def log(msg: str):
    print(msg, flush=True)


def tokens(text: str) -> int:
    return max(1, len(text) // 4)


# ---------------------------------------------------------------------------
# Sessioni con statistiche
# ---------------------------------------------------------------------------

def session_stats(days: int, installed_names: set[str]) -> list[dict]:
    out = []
    for path, entries in read_sessions(days):
        s = {"id": path.stem, "prompts": [], "tools": 0, "errors": 0, "fixes": 0, "interrupts": 0,
             "tokens": 0, "skills": set(), "ts": ""}
        for e in entries:
            if not s["ts"] and e.get("timestamp"):
                s["ts"] = e["timestamp"]
            raw = user_text(e)
            if raw is not None:
                if raw.startswith("[Request interrupted"):
                    s["interrupts"] += 1
                    continue
                m = CMD_RE.search(raw)
                if m:
                    if canon(m.group(1)) in installed_names:
                        s["skills"].add(canon(m.group(1)))
                    continue
                if is_noise(raw):
                    continue
                text = clean(raw)
                if len(text) >= 12:
                    s["prompts"].append(text)
                low = text.lower()
                if len(s["prompts"]) > 1 and any(re.search(rf"\b{re.escape(w)}", low) for w in NEG_WORDS):
                    s["fixes"] += 1
                continue
            if e.get("type") == "user":
                s["errors"] += sum(1 for b in blocks(e) if b.get("type") == "tool_result" and b.get("is_error"))
            elif e.get("type") == "assistant":
                u = (e.get("message") or {}).get("usage") or {}
                s["tokens"] += int(u.get("input_tokens") or 0) + int(u.get("output_tokens") or 0) \
                    + int(u.get("cache_creation_input_tokens") or 0)
                for b in blocks(e):
                    if b.get("type") == "tool_use":
                        s["tools"] += 1
                        if b.get("name") == "Skill":
                            inp = b.get("input") or {}
                            s["skills"].add(canon(inp.get("skill") or inp.get("command") or inp.get("name") or "?"))
        if s["prompts"]:
            out.append(s)
    return out


# ---------------------------------------------------------------------------
# Claude
# ---------------------------------------------------------------------------

# Chiamata pulita: senza hook, plugin, MCP, skill e CLAUDE.md dell'utente (28 s -> 3 s di avvio su questo Mac,
# e niente persona iniettata nella risposta); senza salvare la sessione, così non finisce tra quelle da analizzare.
GROUP_EFFORT = "medium"
GROUPS_TTL = 86400  # ponytail: un giorno, finché non decidiamo la durata giusta
THINK_TOKENS = "16000"  # per la scelta finale: senza, passano skill scritte per il progetto di qualcun altro
LEAN = ["--setting-sources", "", "--strict-mcp-config", "--tools", "", "--disable-slash-commands",
        "--no-session-persistence", "--system-prompt", "You do exactly what the request asks and reply only in the "
        "requested format."]


def claude_json(prompt: str, body: str, model: str | None = None, think: bool = True, effort: str | None = None):
    claude = shutil.which("claude")
    if not claude:
        sys.exit("Serve il comando 'claude'.")
    model = model or MODEL
    cmd = [claude, "-p", prompt, *LEAN] + (["--model", model] if model else []) + (["--effort", effort] if effort else [])
    # --setting-sources "" scarta anche l'impostazione del ragionamento esteso: lo decido qui, per chiamata
    env = {**os.environ, "MAX_THINKING_TOKENS": THINK_TOKENS if think else "0"}
    for _ in range(2):
        r = subprocess.run(cmd, input=body, capture_output=True, text=True, timeout=900, cwd=tempfile.gettempdir(),
                           env=env)
        out = r.stdout
        for a, b in (("[", "]"), ("{", "}")):
            i, j = out.find(a), out.rfind(b)
            if i >= 0 and j > i:
                try:  # i modelli veloci a volte lasciano una virgola finale: la tolgo
                    return json.loads(re.sub(r",\s*([\]}])", r"\1", out[i:j + 1]))
                except ValueError:
                    pass
    return None


# ---------------------------------------------------------------------------
# SKILL.md completo delle finaliste
# ---------------------------------------------------------------------------

def full_md(item: dict) -> str:
    p = item.get("path", "")
    if p and not p.startswith("skills.sh/") and Path(p).exists():
        return Path(p).read_text(encoding="utf-8", errors="replace")
    src, slug = item.get("source", ""), item.get("slug", "")
    if src.count("/") == 1 and "." not in src.split("/")[0]:
        for g in RAW_GUESSES:
            url = f"https://raw.githubusercontent.com/{src}/HEAD/" + g.format(s=urllib.parse.quote(slug))
            try:
                with urllib.request.urlopen(urllib.request.Request(url, headers={"User-Agent": "reinvest/1.0"}),
                                            timeout=10) as r:
                    md = r.read().decode("utf-8", "replace")
                if parse_frontmatter(md).get("description"):
                    return md
            except Exception:  # noqa: BLE001
                continue
    try:
        owner, _, repo = src.partition("/")
        data = http_json(f"{SKILLS_SH}/api/download/{owner}/{repo}/{urllib.parse.quote(slug)}", tries=2)
        return next((x["contents"] for x in data.get("files", []) if x.get("path", "").endswith("SKILL.md")), "")
    except Exception:  # noqa: BLE001
        return ""


VERIFY_PROMPT = (
    "You check ONE agent skill proposed to a developer who uses Claude Code, for the TASK below. Read the whole "
    "SKILL.md. Reject it if ANY of these is true: it is written for one specific other project or codebase (its "
    "own file names, commands, CI job names, internal tools); it is written for another AI agent (Codex, Cursor, "
    "OpenAI tools) or relies on skills/tools it does not include; it needs an account or a paid third-party "
    "service, or advertises a product or paid upgrade; it targets a different platform than the TASK; it is thin "
    "or generic advice an assistant would follow anyway; it does not actually do the TASK, or covers only a narrow special case of it (e.g. only invoices when the TASK is organizing any files); it does the same job as a skill the developer already uses or already has installed (judge by what the closest installed skills DO, not by similar names). Otherwise keep it. "
    "Reply with ONLY a JSON object: {\"keep\": true or false, \"reason\": \"one short sentence in ITALIAN "
    "on why\", \"use\": \"if kept: one short sentence in ITALIAN saying what the skill does in this work, "
    "addressed to the developer, no em-dash\"}.")


def words(text: str) -> set[str]:
    return {w for w in re.findall(r"[a-z]{4,}", text.lower())}


def closest_installed(md: str, installed: list[dict], n: int = 8) -> str:
    """Le installate con più parole in comune: il solo nome inganna (agent-sort ordina componenti, non file)."""
    mw = words(md[:3000])
    best = sorted(installed, key=lambda s: -len(mw & words(s["name"] + " " + s["desc"])))[:n]
    return "\n".join(f"- {s['name']}: {cut(s['desc'], 160)}" for s in best)


def verify(task: str, md: str, used_txt: str = "", installed: list[dict] | None = None) -> dict:
    near = closest_installed(md, installed or [])
    out = claude_json(VERIFY_PROMPT, f"TASK: {task}\n\nSkills the developer already uses: {used_txt or 'none'}\n\n"
                                     f"Installed skills closest to this one (read what they do, not just the name):\n"
                                     f"{near or 'none'}\n\nSKILL.md:\n{md[:MD_VERIFY_CHARS]}", effort="high")
    return out if isinstance(out, dict) and "keep" in out else {"keep": False, "reason": "verifica non riuscita"}


def risks(md: str) -> list[str]:
    low = md.lower()
    return sorted({why for pat, why in RISKY if re.search(pat, low)})


# ---------------------------------------------------------------------------
# Tipi di lavoro: Claude raggruppa le sessioni per TIPO di lavoro, non per progetto
# ---------------------------------------------------------------------------
# ponytail: gli embedding raggruppano per progetto o per frase ("voglio che…"), mai per tipo di lavoro
# (provato: soglie 0,55-0,75, con e senza centratura). Una chiamata a Claude su tutte le sessioni lo fa
# e restituisce gli id, quindi il conteggio delle sessioni è esatto. Limite: ~200 sessioni per chiamata.

WORK_PROMPT = (
    "Each line is the start of one conversation a developer had with an AI coding assistant (often in Italian), "
    "with its id. Group the conversations by the KIND of work they ask for, NOT by project: conversations about "
    "different projects that ask for the same kind of work (e.g. releasing a new version) belong together; "
    "conversations about the same project asking for different kinds of work belong apart. A group that fits only one project is wrong: merge it into the general "
    "kind of work it is an instance of (fixing reported UI bugs, releasing, editing a website...). Each conversation goes "
    "in at most one group; leave out conversations that fit no recurring kind of work. For each group give: "
    "'name': 2-4 words in ITALIAN naming the kind of work, without project names; 'sessions': the ids; "
    "'delegable': true only if a reusable agent skill could take over this work (not chit-chat, one-off questions, "
    "generic 'continue'); 'task': one English sentence describing the work the way a reusable skill would "
    "(inputs, steps, output), general: no names of the user's OWN projects, but KEEP names of public platforms, "
    "stores, languages and tools the work targets (Raycast Store, Chrome Web Store, Homebrew, Figma, Swift); "
    "'queries': 5 short search queries (1-3 words) phrased like the NAME a skill for this work would have "
    "(e.g. 'file organizer', 'bug triage', 'release notes'), from specific to broad. Reply with ONLY a JSON array: "
    "[{\"name\": \"...\", \"sessions\": [0, 4], \"delegable\": true, \"task\": \"...\", \"queries\": [\"...\"]}].")


def session_text(s: dict) -> str:
    return cut(" | ".join(s["prompts"][:3]), 500)


def work_groups(sessions: list[dict], model: str | None = None) -> list[dict]:
    body = "\n".join(f"[{i}] {session_text(s)}" for i, s in enumerate(sessions))
    # ponytail: senza ragionamento esteso 235 s -> ~20 s; la qualità dei gruppi regge, quella della scelta finale no
    out = claude_json(WORK_PROMPT, body, model=model, think=False, effort=GROUP_EFFORT)
    groups, taken = [], set()
    for g in out if isinstance(out, list) else []:
        if not isinstance(g, dict):
            continue
        ids = sorted({int(i) for i in g.get("sessions", []) if str(i).isdigit() and int(i) < len(sessions)} - taken)
        taken |= set(ids)
        if ids:
            groups.append({**g, "sid": [sessions[i]["id"] for i in ids]})
    return groups


def week(ts: str) -> str:
    try:
        from datetime import datetime
        d = datetime.fromisoformat(ts.replace("Z", "+00:00"))
        y, w, _ = d.isocalendar()
        return f"{y}-{w:02d}"
    except ValueError:
        return "?"


def main():
    global MODEL
    ap = argparse.ArgumentParser()
    ap.add_argument("--days", type=int, default=120, help="giorni di sessioni (lungo, per vedere cosa si accumula)")
    ap.add_argument("--temi", type=int, default=6, help="quanti temi più pesanti trasformare in compiti")
    ap.add_argument("--max", type=int, default=3, help="proposte al massimo")
    ap.add_argument("--min-installs", type=int, default=20, help="skills.sh: installazioni minime per una candidata")
    ap.add_argument("--quota", type=float, default=0.1, help="quota dei token liberati da reinvestire")
    ap.add_argument("--modello", default="sonnet")
    ap.add_argument("--gruppi-modello", default="sonnet", help="modello che raggruppa le sessioni per tipo di lavoro")
    ap.add_argument("--catalogo", choices=["tutto", "skillssh"], default="skillssh",
                    help="tutto = skills.sh + cataloghi GitHub ordinati con embedding; skillssh = solo risultati di ricerca")
    ap.add_argument("--selezione", choices=["scelta", "verifica"], default="verifica",
                    help="scelta = Claude sceglie tra tutte poi verifica; verifica = ogni finalista verificata da sola")
    ap.add_argument("--gruppi-cache", default=str(OUT / "gruppi.json"),
                    help="file JSON dei gruppi: riusato per un giorno (stabilità), poi rifatto; '' = sempre nuovi")
    ap.add_argument("--rifai-gruppi", action="store_true", help="rifai i gruppi anche se la cache è recente")
    ap.add_argument("--max-cand", type=int, default=MAX_CAND, help="candidate per compito lette per intero")
    ap.add_argument("--tag", default="", help="suffisso dei file di uscita, per confrontare versioni")
    ap.add_argument("--min-sessioni", type=int, default=3, help="un tipo di lavoro con meno sessioni non diventa un compito")
    args = ap.parse_args()
    MODEL = args.modello or None
    OUT.mkdir(exist_ok=True)
    t0 = time.time()
    tlast = [t0]

    def lap(phase: str):
        now = time.time()
        log(f"  [{phase}: {now - tlast[0]:.0f}s]")
        tlast[0] = now

    # ---- 0. Installate, usate, token liberabili ----
    installed = unique_by_name(read_installed())
    names = {canon(s["name"]) for s in installed}
    sessions = session_stats(args.days, names)
    used = Counter(k for s in sessions for k in s["skills"])
    unused = [s for s in installed if canon(s["name"]) not in used]
    freed = sum(tokens(s["name"] + s["desc"]) for s in unused)
    budget = int(freed * args.quota)
    log(f"Skill installate {len(installed)}, usate {len([n for n in names if n in used])}, "
        f"mai usate {len(unused)}: ~{freed:,} token liberabili, budget per nuove skill ~{budget:,}")
    log(f"Sessioni lette: {len(sessions)}")

    emb = Emb() if args.catalogo == "tutto" else None
    lap("sessioni e modello")

    # ---- 1. Tipi di lavoro (Claude) + copertura ----
    by_id = {x["id"]: x for x in sessions}
    themes = []
    gcache = Path(args.gruppi_cache) if args.gruppi_cache else None
    fresh = gcache and gcache.exists() and time.time() - gcache.stat().st_mtime < GROUPS_TTL \
        and all("sid" in g for g in json.loads(gcache.read_text()))  # cache vecchia (con posizioni): rifalla
    if fresh and not args.rifai_gruppi:
        log(f"Gruppi dalla cache {gcache} (di meno di un giorno; --rifai-gruppi per rifarli)")
        wg = json.loads(gcache.read_text())
    else:
        log(f"Claude raggruppa {len(sessions)} sessioni per tipo di lavoro…")
        wg = work_groups(sessions, args.gruppi_modello)
        if gcache:
            gcache.write_text(json.dumps(wg, ensure_ascii=False))
    for g in wg:
        ss = [by_id[x] for x in g.get("sid", []) if x in by_id]
        if not ss:
            continue
        skills_here = Counter(k for x in ss for k in x["skills"])
        covered_by, cov_n = skills_here.most_common(1)[0] if skills_here else (None, 0)
        themes.append({"nome": str(g.get("name", "")), "delegabile": bool(g.get("delegable")),
                       "task": str(g.get("task", "")), "queries": g.get("queries") or [],
                       "sid": [x["id"] for x in ss], "n_sessioni": len(ss),
                       "settimane": len({week(x["ts"]) for x in ss}), "token": sum(x["tokens"] for x in ss),
                       "costo_mediano": int(statistics.median([x["tokens"] for x in ss])),
                       "attrito": sum(x["errors"] + 2 * x["fixes"] + 3 * x["interrupts"] for x in ss),
                       "coperto_da": covered_by if cov_n / len(ss) >= 0.5 else None,
                       "esempi": [cut(x["prompts"][0], 200) for x in ss[:6]]})
    for t in themes:
        t["punteggio"] = t["n_sessioni"] * (1 + t["attrito"] / max(1, t["n_sessioni"])) * (1 + t["costo_mediano"] / 200_000)
        t["stato"] = ("troppo raro" if t["n_sessioni"] < args.min_sessioni
                      else "coperto da " + t["coperto_da"] if t["coperto_da"]
                      else "non delegabile" if not t["delegabile"] or not t["task"] else "compito")
    covered = [t for t in themes if t["stato"].startswith("coperto")]
    open_themes = sorted([t for t in themes if t["stato"] == "compito"], key=lambda t: -t["punteggio"])
    tasks = [{"task": t["task"], "queries": t["queries"], "evidenza": t} for t in open_themes[: args.temi]]
    grouped = sum(t["n_sessioni"] for t in themes)
    area_report = ["| tipo di lavoro | sessioni | settimane diverse | token | stato |", "|---|---|---|---|---|"]
    area_report += [f"| {t['nome']} | {t['n_sessioni']} | {t['settimane']} | {t['token']:,} | {t['stato']} |"
                    for t in sorted(themes, key=lambda t: -t["n_sessioni"])]
    area_report.append(f"| (senza un tipo ricorrente) | {len(sessions) - grouped} | | | escluse |")
    log(f"Tipi di lavoro: {len(themes)} ({len(covered)} già coperti da skill che usi), compiti delegabili: {len(tasks)}")
    lap("tipi di lavoro (Claude)")

    # ---- 4. Candidati ----
    queries = sorted({q for t in tasks for q in t.get("queries", [])})
    extra = OUT / "ricerche.txt"
    stack_q = [q.strip() for q in extra.read_text().splitlines() if q.strip()] if extra.exists() else []
    raw = fetch_skillssh(queries + stack_q, 12, names)
    n_sh = len(raw)
    if args.catalogo == "tutto":
        raw += read_local_catalog(fetch_github(GITHUB_CATALOGS))
    seen, cat = set(), []
    for s in unique_by_name(raw):
        k = s["desc"][:120].lower()
        if canon(s["name"]) not in names and k not in seen:
            seen.add(k)
            cat.append(s)
    lap("catalogo")
    log(f"Catalogo: {len(cat)} skill non installate ({n_sh} da skills.sh)")
    cv = emb.enc([f"{s['name']}: {cut(s['desc'], 300)}" for s in cat]) if emb else None
    iv = emb.enc([f"{s['name']}: {cut(s['desc'], 300)}" for s in installed]) if emb and installed else None

    # ---- 5. Filtri ----
    finalists = {}
    for ti, t in enumerate(tasks):
        if emb:
            sims = cv @ emb.enc([t["task"]])[0]
            order = [(int(j), float(sims[j])) for j in sims.argsort()[::-1][:25]]
        else:  # solo skills.sh: le candidate sono i risultati delle ricerche di questo compito, per installazioni
            # ordine = migliore posizione nelle ricerche di QUESTO compito (le installazioni premiano skill fuori tema)
            mine = set(t.get("queries", []))
            best = {j: min(r for q, r in s.get("hits", {}).items() if q in mine)
                    for j, s in enumerate(cat) if mine & set(s.get("hits", {}))}
            order = sorted(((j, 0.0) for j in best), key=lambda x: (best[x[0]], -cat[x[0]].get("installs", 0)))[:25]
        cand = []
        for j, sim in order:
            s = cat[j]
            if len(s["desc"]) < 40:
                continue
            if s.get("path", "").startswith("skills.sh/") and s.get("installs", 0) < args.min_installs:
                continue  # troppo poco usata per fidarsi
            near, near_sim = None, 0.0
            if iv is not None:
                k = int((iv @ cv[j]).argmax())
                near, near_sim = installed[k], float(iv[k] @ cv[j])
            if near and near_sim >= DUP_USED and canon(near["name"]) in used:
                continue  # fa lo stesso lavoro di una skill che già usi
            # ponytail: gli embedding non distinguono i doppioni (0 su 10 presi a 0,85 contro il giudizio di Claude):
            # la skill installata più vicina va a Claude, che decide nella scelta finale
            cand.append({**s, "sim": sim, "vicina": near})
            if len(cand) == args.max_cand:
                break
        with ThreadPoolExecutor(6) as ex:
            mds = list(ex.map(full_md, cand))
        keep = []
        for c, md in zip(cand, mds):
            c["rischi"] = risks(md)
            c["md"] = md
            c["costo"] = tokens(c["name"] + c["desc"])
            if c["rischi"] or not md:
                continue
            keep.append(c)
        finalists[ti] = keep
    lap("filtri e SKILL.md")
    peers = sorted([s for s in cat if s.get("installs")], key=lambda s: -s["installs"])[:8]
    log(f"Finaliste: {sum(len(v) for v in finalists.values())} per {len(tasks)} compiti, "
        f"più {len(peers)} tra le più installate nel tuo stack")

    # ---- 6. Scelta ----
    used_txt = "; ".join(f"{s['name']}: {cut(s['desc'], 120)}" for s in installed if canon(s["name"]) in used)
    # anche le installate mai usate: riproporre il doppione di una skill appena tolta non serve (solo i nomi, ~3 KB)
    used_txt += "\nAll skills already installed (names): " + ", ".join(sorted(s["name"] for s in installed))
    parts = []
    for ti, t in enumerate(tasks):
        ev = t["evidenza"]
        parts.append(f"TASK {ti}: {t['task']}\nEvidence: {ev.get('n_sessioni')} sessions, median tokens "
                     f"{ev.get('costo_mediano')}, friction {ev.get('attrito')}")
        for ci, c in enumerate(finalists.get(ti, [])):
            near = c.get("vicina")
            near_txt = f"\n  Closest skill the developer ALREADY HAS installed: {near['name']}: {cut(near['desc'], 200)}" if near else ""
            parts.append(f"  CANDIDATE {ti}.{ci} {c['name']} (installs {c.get('installs', 0)}, context cost "
                         f"~{c['costo']} tokens){near_txt}\n  {cut(c['md'], MD_CHARS)}")
    choice = [] if args.selezione == "verifica" else claude_json(
        f"You pick agent skills for a developer. For each TASK choose the single best CANDIDATE only if it would do "
        f"that work clearly better than an assistant without it (concrete steps, not generic advice); otherwise "
        f"choose none. Reject skills written for one specific other project or codebase (their instructions mention "
        f"that project's own files, commands or features) skills that are thin or generic advice, and skills that do the same job as the closest skill the developer "
        f"already has installed (they already own that capability). Pick each skill at most once. Reject skills that need an account or a paid third-party service to work, skills that advertise a product "
        f"or a paid upgrade, skills for a different platform than the one the TASK names, and skills written for another AI agent (Codex, "
        f"Cursor, OpenAI tools) or that rely on other skills or tools they do not include. At most "
        f"{args.max} picks in total, fewer is better than weak picks. For each pick write 'why' in ITALIAN, one short "
        f"sentence saying what the skill does in their work (no diagnosis of the user, no em-dash). Reply with ONLY "
        f"a JSON array: [{{\"pick\": \"0.2\", \"why\": \"...\"}}]. Empty array if nothing is good.",
        "\n\n".join(parts)) or []

    if args.selezione == "scelta":
        lap("scelta (Claude)")

    # ---- Report ----
    lines = ["# Reinvestire il contesto liberato", "",
             f"Skill mai usate negli ultimi {args.days} giorni: {len(unused)}, circa {freed:,} token per sessione.",
             f"Budget per nuove skill ({int(args.quota * 100)}%): circa {budget:,} token.", ""]
    total = 0
    picks, picked = [], set()
    for c in choice if isinstance(choice, list) else []:
        if not isinstance(c, dict):
            continue
        pick, why = str(c.get("pick", "")), c.get("why", "")
        if "." in pick:
            ti, ci = (int(x) for x in pick.split(".")[:2])
            if ti < len(tasks) and ci < len(finalists.get(ti, [])):
                s = finalists[ti][ci]
                ev = tasks[ti]["evidenza"]
                if canon(s["name"]) not in picked:
                    picked.add(canon(s["name"]))
                    picks.append((s, why, f"compito ricorrente in {ev.get('n_sessioni')} sessioni", s["costo"],
                                  tasks[ti]["task"], ti))
    if args.selezione == "verifica":  # nessuna scelta di gruppo: ogni finalista entra in verifica, in ordine di compito
        for ti, t in enumerate(tasks):  # ogni coppia compito-skill: i doppioni si tolgono dopo la verifica
            for c in finalists.get(ti, []):
                if (ti, canon(c["name"])) not in picked:
                    picked.add((ti, canon(c["name"])))
                    picks.append((c, "", f"compito ricorrente in {t['evidenza'].get('n_sessioni')} sessioni",
                                  c["costo"], t["task"], ti))
    # ---- 7. Verifica: ogni proposta riletta per intero, da sola (la scelta ne vede 20 insieme e ne lascia passare) ----
    with ThreadPoolExecutor(min(8, max(1, len(picks)))) as ex:
        checks_ = list(ex.map(lambda pk: verify(pk[4], pk[0].get("md", ""), used_txt, installed), picks))
    rejected = list({pk[0]["name"]: (pk[0]["name"], v.get("reason", ""))
                     for pk, v in zip(picks, checks_) if not v.get("keep")}.values())  # una riga per skill
    picks = [pk if pk[1] else (pk[0], v.get("use") or v.get("reason", ""), *pk[2:])
             for pk, v in zip(picks, checks_) if v.get("keep")]
    if args.selezione == "verifica":  # una per compito, al massimo --max
        seen_t, seen_n, best = set(), set(), []
        for pk in picks:
            if pk[5] not in seen_t and canon(pk[0]["name"]) not in seen_n and len(best) < args.max:
                seen_t.add(pk[5])
                seen_n.add(canon(pk[0]["name"]))
                best.append(pk)
        picks = best
    log(f"Verifica: {len(picks)} tenute, {len(rejected)} scartate")
    lap("verifica (Claude)")
    lines += ["## Proposte", ""]
    if not picks:
        lines += ["Nessuna proposta abbastanza buona. Meglio niente che una skill inutile.", ""]
    for n, (s, why, origin, cost, *_rest) in enumerate(picks, 1):
        total += cost
        src = s.get("path", "")
        near = s.get("vicina")
        checks = [f"la tua più vicina: {near['name']}" if near else "nessuna tua simile",
                  "sicurezza ok", f"{s.get('installs', 0)} installazioni" if s.get("installs") else "da catalogo GitHub"]
        lines += [f"### {n}. {s['name']}  (~{cost} token)", "", why, "",
                  f"Perché ora: {origin}. Controlli: " + ", ".join(checks) + ".", f"Fonte: {src}", ""]
    if picks:
        lines += [f"Costo totale: ~{total:,} token su ~{budget:,} di budget.", ""]
    if rejected:
        lines += ["Scartate nella verifica finale: " + "; ".join(f"{n} ({r})" for n, r in rejected), ""]
    lines += ["## Dietro le quinte", ""] + area_report + ["",
              "Le più installate su skills.sh tra le trovate (solo informazione, non proposte): "
              + (", ".join(f"{p['name']} ({p['installs']:,})" for p in peers) or "nessuna") + ".", "",
              f"Tipi di lavoro trovati: {len(themes)}. Già coperti da skill che usi: "
              + (", ".join(sorted({t['coperto_da'] for t in covered})) or "nessuno") + ".", ""]
    for i, t in enumerate(tasks):
        ev = t["evidenza"]
        lines.append(f"- Compito {i}: {t['task']} ({ev.get('n_sessioni')} sessioni, attrito {ev.get('attrito')}; "
                     f"{len(finalists.get(i, []))} finaliste dopo i filtri)")
    lines += ["", f"Tempo: {int(time.time() - t0)} secondi.", ""]
    text = "\n".join(lines)
    (OUT / f"reinvest{args.tag}.md").write_text(text, encoding="utf-8")
    base.save(f"reinvest_debug{args.tag}.json", {"proposte": [pk[0]["name"] for pk in picks],
                                     "scartate": rejected, "secondi": int(time.time() - t0),
                                     "gruppi": [(t["nome"], t["n_sessioni"], t["stato"]) for t in themes],"temi": themes, "compiti": tasks,
                                     "finaliste": {k: [{**{x: c[x] for x in c if x not in ("md", "vicina")},
                                                        "vicina": (c.get("vicina") or {}).get("name")} for c in v]
                                                   for k, v in finalists.items()},
                                     "scelta": choice})
    print("\n" + text)


if __name__ == "__main__":
    main()
