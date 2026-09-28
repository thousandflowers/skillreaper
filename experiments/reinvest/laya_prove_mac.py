#!/usr/bin/env python3
"""
laya_prove_mac.py
Le 4 prove di Laya sui TUOI dati, in automatico.

  T1 reazione     dopo una skill eri contento?
  T2 pertinenza   questa skill serviva per questa richiesta?
  T3 doppioni     queste due skill installate fanno lo stesso lavoro?
  T4 suggerimenti quali skill di skills.sh e GitHub ti servirebbero?

Laya e le alternative senza Laya (parole chiave, embedding, classifica di skills.sh)
danno le loro risposte. Claude fa da giudice e decide qual è la risposta giusta.
Alla fine: chi è d'accordo più spesso con il giudice.

Uso normale, zero domande:
  .venv/bin/python laya_prove_mac.py

Opzionale: controlla il giudice su N casi a caso, rispondendo tu s/n:
  .venv/bin/python laya_prove_mac.py --controllo 10

Tutto resta in ./prove_laya/ . Contiene pezzi dei tuoi prompt: non pubblicarla.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import random
import re
import shutil
import subprocess
import sys
import textwrap
import urllib.error
import urllib.parse
import urllib.request
import warnings
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timedelta, timezone
from pathlib import Path

warnings.filterwarnings("ignore")

HOME = Path.home()
PROJECTS = HOME / ".claude" / "projects"
OUT = Path("prove_laya")
CACHE = HOME / ".cache" / "laya-prove"
SKILLS_SH = "https://skills.sh"
RAW_GUESSES = ["skills/{s}/SKILL.md", "{s}/SKILL.md", ".claude/skills/{s}/SKILL.md", ".agents/skills/{s}/SKILL.md",
               "skill/{s}/SKILL.md", "SKILL.md"]
GITHUB_CATALOGS = [
    "https://github.com/anthropics/skills",
    "https://github.com/alirezarezvani/claude-skills",
    "https://github.com/ComposioHQ/awesome-claude-skills",
]
EMB_MODEL = "sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2"
JUDGE_MODEL = "sonnet"  # più veloce del modello predefinito; cambialo con --modello-giudice
SEED = 7

NOISE_PREFIX = ("<task-notification", "<bash-input", "<bash-stdout", "<bash-stderr", "<local-command",
                "Caveat:", "<command-message", "<user-prompt-submit-hook")
CMD_RE = re.compile(r"<command-name>\s*/?([^<\s]+)\s*</command-name>")
ARGS_RE = re.compile(r"<command-args>(.*?)</command-args>", re.S)
TAG_RE = re.compile(r"<system-reminder>.*?</system-reminder>", re.S)

NEG_WORDS = ["no", "non", "sbagli", "terribil", "pessim", "rifai", "rifallo", "intendo", "arrabb", "rotto",
             "errori", "annulla", "peggior", "wrong", "not", "don't", "broke", "terrible", "revert", "worse"]

Q_REACT = ("After an AI assistant finished a task, the user wrote this message. How does the user feel about the result?",
           "satisfied with the result: approves, thanks, or moves on to the next step",
           "unhappy with the result: complains, corrects, rejects or asks to redo it")
Q_REL = ("Would this skill be useful to carry out the user's request?",
         "yes, the skill directly helps with this request",
         "no, the skill is about something else")
Q_DUP = ("Do skill A and skill B do the same job, so that one could replace the other?",
         "yes, they do the same job",
         "no, they do different jobs")
Q_SUG = ("Would this skill be useful for this user, given the work they do?",
         "yes, it fits the work this user does",
         "no, it is for work this user does not do")


# ============================================================================
# Utilità
# ============================================================================

def clean(t: str) -> str:
    return re.sub(r"\s+", " ", TAG_RE.sub("", t or "")).strip()


def cut(t: str, n: int) -> str:
    t = clean(t)
    return t if len(t) <= n else t[: n - 1] + "…"


def canon(name: str) -> str:
    return name.strip().lstrip("/").split(":")[-1].lower()


def save(name: str, data):
    OUT.mkdir(exist_ok=True)
    (OUT / name).write_text(json.dumps(data, ensure_ascii=False, indent=1), encoding="utf-8")


def load(name: str, default=None):
    p = OUT / name
    return json.loads(p.read_text(encoding="utf-8")) if p.exists() else default


# ============================================================================
# Skill
# ============================================================================

def parse_frontmatter(text: str) -> dict:
    lines = text.splitlines()
    if not lines or lines[0].strip() != "---":
        return {}
    out, key, buf = {}, None, []
    for line in lines[1:]:
        if line.strip() == "---":
            break
        m = re.match(r"^([A-Za-z_-]+):\s*(.*)$", line)
        if m and not line[:1].isspace():
            if key:
                out[key] = " ".join(buf).strip()
            key, val = m.group(1), m.group(2).strip()
            buf = [] if val in (">", "|", ">-", "|-", ">+", "|+", "") else [val.strip("\"'")]
        elif key and line[:1].isspace():  # solo le righe rientrate continuano il valore
            buf.append(line.strip().strip("\"'"))
    if key:
        out[key] = " ".join(buf).strip()
    return out


def read_skills(roots: list[Path], pattern: str = "**/SKILL.md") -> list[dict]:
    """Ritorna tutte le copie trovate (anche doppie), con hash del contenuto."""
    found = []
    for root in roots:
        if not root.exists():
            continue
        for md in root.glob(pattern):
            try:
                text = md.read_text(encoding="utf-8", errors="replace")
            except OSError:
                continue
            fm = parse_frontmatter(text)
            name = (fm.get("name") or md.parent.name).split()[0]
            desc = clean(fm.get("description", ""))
            if not desc:
                continue
            found.append({"name": name, "desc": desc, "path": str(md),
                          "hash": hashlib.sha1(text.encode()).hexdigest()})
    return found


def installed_roots() -> list[Path]:
    """~/.claude/skills più i plugin davvero installati (non le vecchie versioni in cache)."""
    roots = [HOME / ".claude" / "skills"]
    plugins = HOME / ".claude" / "plugins"
    reg = plugins / "installed_plugins.json"
    paths = []
    if reg.exists():
        try:
            data = json.loads(reg.read_text(encoding="utf-8")).get("plugins", {})
            for v in data.values():
                for entry in (v if isinstance(v, list) else [v]):
                    if isinstance(entry, dict) and entry.get("installPath"):
                        paths.append(Path(entry["installPath"]))
        except (OSError, ValueError):
            pass
    if not paths and (plugins / "cache").exists():
        # senza registro: per ogni plugin tieni solo la versione più recente
        for plugin_dir in (plugins / "cache").glob("*/*"):
            versions = [d for d in plugin_dir.iterdir() if d.is_dir()]
            if versions:
                paths.append(max(versions, key=lambda d: d.stat().st_mtime))
    out, seen = [], set()
    for p in roots + [p / "skills" for p in paths if (p / "skills").exists()]:
        key = p.resolve() if p.exists() else p
        if key not in seen:  # lo stesso plugin può comparire più volte nel registro (es. per più progetti)
            seen.add(key)
            out.append(p)
    return out


def read_installed() -> list[dict]:
    """Solo le skill che Claude Code carica davvero: <cartella skills>/<nome>/SKILL.md.
    (I repo dei plugin contengono spesso copie per altri agenti, es. .cursor/ o .agents/, da non contare.)"""
    return read_skills(installed_roots(), "*/SKILL.md")


def unique_by_name(skills: list[dict]) -> list[dict]:
    seen, out = set(), []
    for s in skills:
        k = canon(s["name"])
        if k not in seen:
            seen.add(k)
            out.append(s)
    return out


# ============================================================================
# Trascrizioni
# ============================================================================

def blocks(e: dict) -> list[dict]:
    c = (e.get("message") or {}).get("content")
    if isinstance(c, str):
        return [{"type": "text", "text": c}]
    return [b for b in c if isinstance(b, dict)] if isinstance(c, list) else []


def user_text(e: dict) -> str | None:
    if e.get("type") != "user" or e.get("isMeta") or e.get("isSidechain"):
        return None
    parts = []
    for b in blocks(e):
        if b.get("type") == "tool_result":
            return None
        if b.get("type") == "text":
            parts.append(b.get("text", ""))
    raw = "\n".join(parts).strip()
    return raw or None


def is_noise(raw: str) -> bool:
    return raw.lstrip().startswith(NOISE_PREFIX)


TOOL_DIR = Path(__file__).resolve().parent
LIVE_MINUTES = 10  # una sessione scritta negli ultimi minuti è ancora aperta: non è lavoro concluso


def is_automated(entries: list[dict]) -> bool:
    """Sessione non scritta da una persona: lanciata con `claude -p` / SDK (entrypoint "sdk-*"),
    oppure aperta nella cartella di questo strumento (il lavoro sullo strumento non è lavoro dell'utente)."""
    first = next((e for e in entries if e.get("type") == "user" and not e.get("isMeta")), None)
    if first is None:
        return True
    if str(first.get("entrypoint", "")).startswith("sdk"):
        return True
    cwd = first.get("cwd")
    return bool(cwd) and Path(cwd).resolve().is_relative_to(TOOL_DIR)


def read_sessions(days: int):
    since = datetime.now(timezone.utc) - timedelta(days=days)
    live_since = datetime.now(timezone.utc) - timedelta(minutes=LIVE_MINUTES)
    for path in sorted(PROJECTS.rglob("*.jsonl")):
        if "subagents" in path.parts:  # trascrizioni dei subagent: nessun prompt dell'utente
            continue
        try:
            mtime = datetime.fromtimestamp(path.stat().st_mtime, tz=timezone.utc)
            if mtime < since or mtime > live_since:  # troppo vecchia, o ancora in corso (es. chi lancia lo script)
                continue
            entries = []
            with path.open(encoding="utf-8", errors="replace") as f:
                for line in f:
                    try:
                        entries.append(json.loads(line))
                    except json.JSONDecodeError:
                        pass
            if is_automated(entries):
                continue
            yield path, entries
        except OSError:
            continue


def extract(days: int, installed_names: set[str]):
    """Ritorna (invocazioni, prompt dell'utente)."""
    invs, prompts, seen_inv, seen_prompt = [], [], set(), set()
    for path, entries in read_sessions(days):
        last_req, last_asst, open_inv = "", "", []

        def close(reply: str):
            for inv in open_inv:
                inv["reply"] = reply
                inv["claude"] = last_asst
            open_inv.clear()

        for e in entries:
            raw = user_text(e)
            if raw is not None:
                if raw.startswith("[Request interrupted"):
                    for inv in open_inv:
                        inv["interrupted"] = True
                    close("")
                    continue
                cmd = CMD_RE.search(raw)
                if cmd:
                    name = cmd.group(1)
                    if canon(name) in installed_names:
                        close("")
                        args = ARGS_RE.search(raw)
                        req = clean(args.group(1)) if args and clean(args.group(1)) else last_req
                        key = (e.get("timestamp"), name)
                        if key not in seen_inv:
                            seen_inv.add(key)
                            inv = {"skill": name, "request": req, "ts": e.get("timestamp", ""),
                                   "interrupted": False, "reply": "", "claude": ""}
                            invs.append(inv)
                            open_inv.append(inv)
                    continue
                if is_noise(raw):
                    continue
                text = clean(raw)
                close(text)
                last_req = text
                if len(text) >= 20 and text not in seen_prompt:
                    seen_prompt.add(text)
                    prompts.append(text)
                continue
            if e.get("type") == "assistant":
                for b in blocks(e):
                    if b.get("type") == "tool_use" and b.get("name") == "Skill":
                        inp = b.get("input") or {}
                        name = inp.get("skill") or inp.get("command") or inp.get("name") or "?"
                        key = (e.get("timestamp"), name)
                        if key in seen_inv:
                            continue
                        seen_inv.add(key)
                        inv = {"skill": name, "request": last_req, "ts": e.get("timestamp", ""),
                               "interrupted": False, "reply": "", "claude": ""}
                        invs.append(inv)
                        open_inv.append(inv)
                    elif b.get("type") == "text" and b.get("text", "").strip():
                        last_asst = clean(b["text"])
        close("")
    return invs, prompts


# ============================================================================
# Modelli
# ============================================================================

class Laya:
    def __init__(self):
        try:
            from laya import Router
        except ImportError:
            sys.exit("Manca Laya: uv pip install --python .venv/bin/python laya")
        print("Carico Laya…", flush=True)
        self.r = Router(max_loaded=2)

    @staticmethod
    def _q(q, flip):
        instr, yes, no = q
        crit = {"A": no, "B": yes} if flip else {"A": yes, "B": no}
        return {"x": {"type": "choice", "instructions": instr, "criteria": crit}}

    def p_yes(self, states: list[dict], q, models: list[str]) -> list[float]:
        """Probabilità del 'sì', media su modelli e sui due ordini delle opzioni."""
        totals = [0.0] * len(states)
        for m in models:
            for flip in (False, True):
                reqs = [{"state": s, "questions": self._q(q, flip), "model": m} for s in states]
                try:
                    res = self.r.predict_batch(reqs, batch_size=16)
                except Exception:  # noqa: BLE001
                    res = [self.r.predict(s, self._q(q, flip), model=m) for s in states]
                for i, rr in enumerate(res):
                    pr = rr["answers"]["x"]["probabilities"]
                    totals[i] += pr["B"] if flip else pr["A"]
        n = 2 * len(models)
        return [t / n for t in totals]


class Emb:
    def __init__(self):
        try:
            from sentence_transformers import SentenceTransformer
        except ImportError:
            sys.exit("Manca sentence-transformers: uv pip install --python .venv/bin/python sentence-transformers")
        print("Carico il modello di embedding…", flush=True)
        self.m = SentenceTransformer(EMB_MODEL)

    def enc(self, texts: list[str]):
        return self.m.encode(texts, normalize_embeddings=True, batch_size=64, show_progress_bar=False)


# ============================================================================
# Comandi
# ============================================================================

def cmd_profilo(args):
    installed = unique_by_name(read_installed())
    _, prompts = extract(args.days, {canon(s["name"]) for s in installed})
    if not prompts:
        sys.exit("Non ho trovato prompt nelle trascrizioni.")
    random.Random(SEED).shuffle(prompts)
    digest = "\n".join("- " + cut(p, 160) for p in prompts[:250])
    OUT.mkdir(exist_ok=True)
    (OUT / "digest_prompt.txt").write_text(digest, encoding="utf-8")
    instr = ("Below are messages a developer sent to an AI coding assistant. First write a profile of the work they do, "
             "in English, one paragraph, at most 100 words: languages, platforms, kinds of projects, recurring tasks, "
             "tools. Do not name people or projects. Then, on a new line starting with 'QUERIES:', write 15 short "
             "search queries (1-3 words each, comma separated) to find agent skills useful for this person on a skills "
             "marketplace. Output only these two parts.")
    claude = shutil.which("claude")
    if not claude:
        print(f"Non trovo il comando 'claude'. Scrivi a mano {OUT/'profilo.md'} (inglese, max 100 parole) "
              f"e {OUT/'ricerche.txt'} (una ricerca per riga).")
        return
    print(f"Claude legge {min(250, len(prompts))} tuoi messaggi e scrive il profilo…", flush=True)
    r = subprocess.run([claude, "-p", instr], input=digest, capture_output=True, text=True, timeout=300)
    text = r.stdout.strip()
    if r.returncode != 0 or not text:
        print("Claude non ha risposto:", r.stderr[:300])
        print(f"Scrivi a mano {OUT/'profilo.md'} (inglese, max 100 parole) e {OUT/'ricerche.txt'}.")
        return
    profile, _, queries = text.partition("QUERIES:")
    queries = [q.strip(" .-*") for q in queries.replace("\n", ",").split(",") if q.strip(" .-*")]
    (OUT / "profilo.md").write_text(profile.strip() + "\n", encoding="utf-8")
    (OUT / "ricerche.txt").write_text("\n".join(queries) + "\n", encoding="utf-8")
    print("\n" + textwrap.fill(profile.strip(), 80) + "\n")
    print("Ricerche per skills.sh: " + ", ".join(queries) + "\n")
    print(f"Salvati in {OUT/'profilo.md'} e {OUT/'ricerche.txt'} (una ricerca per riga).")
    print("Se qualcosa è sbagliato, correggilo lì e rilancia con --rifai.")


def http_json(url: str, tries: int = 4):
    """GET con nuovi tentativi se il sito dice 'troppe richieste' (429) o ha problemi (5xx)."""
    import time
    import urllib.error
    for k in range(tries):
        req = urllib.request.Request(url, headers={"User-Agent": "laya-prove/1.0"})
        try:
            with urllib.request.urlopen(req, timeout=20) as r:
                return json.loads(r.read().decode("utf-8"))
        except urllib.error.HTTPError as e:
            if e.code in (429, 500, 502, 503, 504) and k < tries - 1:
                wait = int(e.headers.get("Retry-After") or 0) or 2 ** (k + 1)
                time.sleep(min(wait, 8))
                continue
            raise


def fetch_skillssh(queries: list[str], per_query: int, skip_names: set[str] = frozenset(),
                   with_desc: bool = True) -> list[dict]:
    """Cerca su skills.sh e scarica la descrizione di ogni skill trovata (con cache)."""
    found = {}
    cdir = CACHE / "skillssh"
    cdir.mkdir(parents=True, exist_ok=True)
    for q in queries:
        qf = cdir / f"search_{per_query}_{re.sub(r'[^A-Za-z0-9]+', '_', q.lower())}.json"
        if qf.exists() and datetime.now().timestamp() - qf.stat().st_mtime < 86400:  # ricerche in cache per un giorno
            data = json.loads(qf.read_text(encoding="utf-8"))
        else:
            try:
                data = http_json(f"{SKILLS_SH}/api/search?" + urllib.parse.urlencode({"q": q, "limit": per_query}))
                qf.write_text(json.dumps(data), encoding="utf-8")
            except Exception as exc:  # noqa: BLE001
                print(f"  ricerca '{q}' fallita: {exc}")
                continue
        for rank, sk in enumerate(data.get("skills", [])):
            it = found.setdefault(sk["id"], {"id": sk["id"], "source": sk.get("source", ""), "slug": sk.get("skillId", ""),
                                             "name": sk.get("name", sk.get("skillId", "")),
                                             "installs": int(sk.get("installs") or 0), "query": q, "hits": {}})
            it["hits"][q] = rank  # posizione nei risultati di ogni ricerca che l'ha trovata
    before = len(found)
    found = {k: v for k, v in found.items() if canon(v["name"]) not in skip_names}
    if not with_desc:  # solo i risultati di ricerca: chi li usa scarica poi il SKILL.md delle poche che legge
        return [{**it, "desc": "", "path": f"skills.sh/{it['id']}"} for it in found.values()]
    print(f"skills.sh: {before} skill trovate con {len(queries)} ricerche, {before - len(found)} già installate. "
          f"Scarico {len(found)} descrizioni (quelle già scaricate restano in cache)…", flush=True)
    import threading
    per_repo = {}
    for it in found.values():
        per_repo[it["source"]] = per_repo.get(it["source"], 0) + 1
    sh_gate = threading.Semaphore(3)  # skills.sh limita le richieste: poche alla volta
    sh_blocked = threading.Event()  # al primo 429 smetto di chiedere a skills.sh per questo giro
    stats = {"github": 0, "skills.sh": 0, "cache": 0}

    def from_github(item) -> str | None:
        """Come fa il CLI ufficiale: legge SKILL.md da raw.githubusercontent.com (CDN, niente limiti)."""
        src = item["source"]
        if src.count("/") != 1 or "." in src.split("/")[0]:
            return None  # non è un repo GitHub (es. smithery.ai)
        for g in RAW_GUESSES:
            if g == "SKILL.md" and per_repo.get(src, 0) > 1:
                continue
            url = f"https://raw.githubusercontent.com/{src}/HEAD/" + g.format(s=urllib.parse.quote(item["slug"]))
            try:
                req = urllib.request.Request(url, headers={"User-Agent": "laya-prove/1.0"})
                with urllib.request.urlopen(req, timeout=10) as r:
                    md = r.read().decode("utf-8", "replace")
                fm = parse_frontmatter(md)
                if fm.get("description") and (g != "SKILL.md" or canon(fm.get("name", "")) == canon(item["slug"])):
                    return md
            except Exception:  # noqa: BLE001
                continue
        return None

    def from_skillssh(item) -> str | None:
        if sh_blocked.is_set():
            return None
        owner, _, repo = item["source"].partition("/")
        url = f"{SKILLS_SH}/api/download/{urllib.parse.quote(owner)}/{urllib.parse.quote(repo)}/{urllib.parse.quote(item['slug'])}"
        with sh_gate:
            try:
                data = http_json(url, tries=1)
            except urllib.error.HTTPError as e:
                if e.code == 429 and not sh_blocked.is_set():
                    sh_blocked.set()
                    print("  skills.sh risponde 429: per questo giro solo GitHub", flush=True)
                return "" if e.code == 404 else None  # 404 = non esiste: lo ricordo; altri errori: riprovo più tardi
            except Exception:  # noqa: BLE001
                return None
        return next((x["contents"] for x in data.get("files", []) if x.get("path", "").endswith("SKILL.md")), "")

    def desc(item):
        f = cdir / (re.sub(r"[^A-Za-z0-9]+", "_", item["id"]) + ".json")
        if f.exists():
            cached = json.loads(f.read_text(encoding="utf-8"))
            if cached.get("desc") or not cached.get("miss") or datetime.now().timestamp() - f.stat().st_mtime < 86400:
                stats["cache"] += 1
                return cached
        md = from_github(item)
        if md:
            stats["github"] += 1
        else:
            md = from_skillssh(item)
            if md:
                stats["skills.sh"] += 1
        if md is None:  # non trovata ora: non riprovo per un giorno (ogni tentativo costa secondi)
            f.write_text(json.dumps({"desc": "", "miss": True}), encoding="utf-8")
            return None
        out = {"desc": clean(parse_frontmatter(md).get("description", ""))}
        f.write_text(json.dumps(out), encoding="utf-8")
        return out

    items = list(found.values())
    descs = []
    with ThreadPoolExecutor(max_workers=16) as ex:
        for k, d in enumerate(ex.map(desc, items), 1):
            descs.append(d)
            if k % 25 == 0 or k == len(items):
                print(f"  {k}/{len(items)}", flush=True)
    print(f"skills.sh: {sum(1 for d in descs if d and d['desc'])} descrizioni su {len(items)} "
          f"(da GitHub {stats['github']}, da skills.sh {stats['skills.sh']}, già in cache {stats['cache']})")
    out = []
    for it, d in zip(items, descs):
        if d and d["desc"]:
            it["desc"] = d["desc"]
            it["path"] = f"skills.sh/{it['id']}"
            out.append(it)
    return out


def fetch_github(urls: list[str]) -> list[Path]:
    """Clona (una volta sola, in cache) i cataloghi di skill su GitHub."""
    CACHE.mkdir(parents=True, exist_ok=True)
    dirs = []
    for url in urls:
        d = CACHE / "github" / re.sub(r"[^A-Za-z0-9]+", "_", url.split("github.com/")[-1])
        if not d.exists():
            print(f"Scarico {url} …", flush=True)
            d.parent.mkdir(parents=True, exist_ok=True)
            r = subprocess.run(["git", "clone", "-q", "--depth", "1", url, str(d)], capture_output=True, text=True)
            if r.returncode != 0:
                print(f"  non riesco: {r.stderr.strip()[:200]}")
                continue
        dirs.append(d)
    return dirs


def read_local_catalog(dirs: list[Path]) -> list[dict]:
    out = []
    for s in read_skills(dirs):
        s.update({"installs": 0, "id": s["path"]})
        out.append(s)
    return out


def cmd_prepara(args):
    rng = random.Random(SEED)
    all_installed = read_installed()
    installed = unique_by_name(all_installed)
    names = {canon(s["name"]) for s in installed}
    invs, prompts = extract(args.days, names)
    print(f"Skill installate: {len(installed)} (copie trovate: {len(all_installed)})")
    print(f"Invocazioni: {len(invs)}, prompt tuoi: {len(prompts)}")

    laya, emb = Laya(), Emb()
    info = {"creato": datetime.now().isoformat(timespec="minutes"), "giorni": args.days, "versione": 3}

    # ---------------- T1 reazione ----------------
    interrupted = [i for i in invs if i["interrupted"]]
    t1 = [i for i in invs if i["reply"] and not i["interrupted"]]
    if t1:
        p = laya.p_yes([{"user_message": cut(i["reply"], 500)} for i in t1], Q_REACT, ["multilingual"])
        items = []
        for k, (i, pp) in enumerate(zip(t1, p)):
            low = i["reply"].lower()
            kw = 0 if any(re.search(rf"\b{re.escape(w)}", low) for w in NEG_WORDS) else 1
            items.append({"id": f"t1-{k}", "skill": i["skill"], "request": cut(i["request"], 220),
                          "claude": cut(i["claude"], 260), "reply": cut(i["reply"], 260),
                          "laya": pp, "keywords": kw})
        save("t1.json", items)
    info["t1"] = {"items": len(t1), "interrotte_escluse": len(interrupted)}
    print(f"T1 pronta: {len(t1)} casi")

    # ---------------- T2 pertinenza ----------------
    skill_txt = [f"{s['name']}: {cut(s['desc'], 300)}" for s in installed]
    sv = emb.enc(skill_txt)
    sample = prompts[:]
    rng.shuffle(sample)
    sample = sample[: args.prompts]
    pairs = []
    if sample:
        pv = emb.enc([cut(p, 400) for p in sample])
        sims = pv @ sv.T
        for i, prompt in enumerate(sample):
            for j in sims[i].argsort()[::-1][:3]:
                pairs.append({"prompt": cut(prompt, 400), "skill": installed[j]["name"],
                              "desc": cut(installed[j]["desc"], 300), "emb": float(sims[i, j])})
        p = laya.p_yes([{"user_request": x["prompt"], "skill": f"{x['skill']}: {x['desc']}"} for x in pairs],
                       Q_REL, ["english"])
        for x, pp in zip(pairs, p):
            x["laya"] = pp
        for k, x in enumerate(pairs):
            x["id"] = f"t2-{k}"
        save("t2.json", pairs)
        info["t2"] = {"coppie": len(pairs)}
        print(f"T2 pronta: {len(pairs)} coppie richiesta/skill")

    # ---------------- T3 doppioni ----------------
    by_hash = {}
    for s in all_installed:
        by_hash.setdefault(s["hash"], []).append(s["path"])
    exact = [v for v in by_hash.values() if len(v) > 1]
    sims = sv @ sv.T
    cand = []
    n = len(installed)
    for i in range(n):
        for j in range(i + 1, n):
            cand.append((float(sims[i, j]), i, j))
    cand.sort(reverse=True)
    cand = cand[: args.pairs]
    dpairs = [{"a": installed[i]["name"], "a_desc": cut(installed[i]["desc"], 260),
               "b": installed[j]["name"], "b_desc": cut(installed[j]["desc"], 260), "emb": s} for s, i, j in cand]
    p = laya.p_yes([{"skill_A": f"{d['a']}: {d['a_desc']}", "skill_B": f"{d['b']}: {d['b_desc']}"} for d in dpairs],
                   Q_DUP, ["english"])
    for d, pp in zip(dpairs, p):
        d["laya"] = pp
    for k, d in enumerate(dpairs):
        d["id"] = f"t3-{k}"
    save("t3.json", dpairs)
    info["t3"] = {"copie_identiche": exact, "coppie": len(dpairs)}
    print(f"T3 pronta: {len(exact)} gruppi di copie identiche, {len(dpairs)} coppie simili")

    # ---------------- T4 suggerimenti ----------------
    prof_path = OUT / "profilo.md"
    if not prof_path.exists():
        print("T4 saltata: prima esegui 'profilo'.")
    else:
        profile = cut(prof_path.read_text(encoding="utf-8"), 900)
        qfile = OUT / "ricerche.txt"
        queries = [q.strip() for q in qfile.read_text(encoding="utf-8").splitlines() if q.strip()] if qfile.exists() else []
        raw = fetch_skillssh(queries, args.per_query, names) if queries else []
        n_sh = len(raw)
        gh = [] if args.no_github else fetch_github(GITHUB_CATALOGS)
        raw += read_local_catalog(gh + [Path(d) for d in args.catalogo_dir])
        print(f"Fonti: {n_sh} da skills.sh, {len(raw) - n_sh} da GitHub e cartelle locali")
        catalog = [s for s in unique_by_name(raw) if canon(s["name"]) not in names]
        # togli copie con la stessa descrizione
        seen, cat = set(), []
        for s in catalog:
            k = s["desc"][:120].lower()
            if k not in seen:
                seen.add(k)
                cat.append(s)
        print(f"Catalogo: {len(cat)} skill non installate e con descrizione")
        if cat:
            cv = emb.enc([f"{s['name']}: {cut(s['desc'], 300)}" for s in cat])
            pv = emb.enc([profile])[0]
            cos = cv @ pv
            order = cos.argsort()[::-1]
            top_emb = [int(i) for i in order[:args.top]]
            pool = [int(i) for i in order[:args.pool]]
            p = laya.p_yes([{"user_profile": profile, "candidate_skill": f"{cat[i]['name']}: {cut(cat[i]['desc'], 300)}"}
                            for i in pool], Q_SUG, ["english", "multilingual"])
            lay = sorted(zip(pool, p), key=lambda x: -x[1])
            top_laya = [i for i, _ in lay[:args.top]]
            top_pop = sorted(range(len(cat)), key=lambda i: -cat[i]["installs"])[:args.top]
            if not any(cat[i]["installs"] for i in top_pop):
                top_pop = []
            merged = sorted(set(top_emb) | set(top_laya) | set(top_pop))
            rng.shuffle(merged)
            items = [{"id": f"t4-{k}", "skill": cat[i]["name"], "desc": cut(cat[i]["desc"], 300),
                      "in_laya": i in top_laya, "in_emb": i in top_emb, "in_pop": i in top_pop,
                      "installs": cat[i]["installs"], "fonte": cat[i]["path"]}
                     for k, i in enumerate(merged)]
            save("t4.json", items)
            info["t4"] = {"catalogo": len(cat), "pool_laya": len(pool), "da_etichettare": len(items)}
            print(f"T4 pronta: {len(items)} skill da controllare (le prime {args.top} di ogni metodo, unite)")
    save("info.json", info)
    print("Preparazione finita.")


# ---------------------------------------------------------------------------

B, R = "\033[1m", "\033[0m"


def show(title: str, fields: list[tuple[str, str]], question: str, k: int, n: int):
    print("\033[2J\033[H", end="")
    print(f"{B}{title}{R}   {k}/{n}\n")
    for label, text in fields:
        print(f"{B}{label}{R}")
        print(textwrap.fill(text or "(vuoto)", 80, initial_indent="  ", subsequent_indent="  "))
        print()
    print(f"{B}{question}{R}")


def ask() -> str | None:
    while True:
        a = input("  s = sì   n = no   ? = non so   q = esci  > ").strip().lower()
        if a in ("s", "n", "?"):
            return a
        if a == "q":
            return None


TESTS = {
    "t1": ("T1  Reazione dopo la skill", "Eri contento del risultato di questa skill?",
           lambda x: [("Skill", x["skill"]), ("Cosa avevi chiesto", x["request"]),
                      ("Ultima risposta di Claude", x["claude"]), ("Cosa hai scritto dopo", x["reply"])]),
    "t2": ("T2  Pertinenza", "Questa skill ti sarebbe servita per questa richiesta?",
           lambda x: [("La tua richiesta", x["prompt"]), ("Skill", f"{x['skill']}: {x['desc']}")]),
    "t3": ("T3  Doppioni", "Fanno lo stesso lavoro? (una potrebbe sostituire l'altra)",
           lambda x: [("Skill A", f"{x['a']}: {x['a_desc']}"), ("Skill B", f"{x['b']}: {x['b_desc']}")]),
    "t4": ("T4  Suggerimenti", "Ti sarebbe utile? La installeresti?",
           lambda x: [("Skill", x["skill"]), ("Cosa fa", x["desc"])]),
}


# ---------------------------------------------------------------------------
# Giudice: Claude decide la risposta giusta, in automatico
# ---------------------------------------------------------------------------

JUDGE = {
    "t1": ("Each item shows a skill an AI coding assistant used, what the user had asked, the assistant's last "
           "message, and the user's NEXT message. Was the user satisfied with the result of that skill? "
           "'s' = satisfied: approves, thanks, moves on, or asks for a next step that builds on the result "
           "(for example, after an audit, asking to fix the problems it found counts as satisfied). "
           "'n' = unhappy: corrects, complains, says it is wrong, rejects it or asks to redo it. "
           "'?' only if it is truly impossible to tell.",
           lambda x: f"skill: {x['skill']}\nuser had asked: {x['request']}\nassistant last message: {x['claude']}\n"
                     f"user next message: {x['reply']}"),
    "t2": ("Each item has a user's request to an AI coding assistant and an installed skill. "
           "'s' if the skill would genuinely help carry out that request, 'n' otherwise.",
           lambda x: f"request: {x['prompt']}\nskill: {x['skill']}: {x['desc']}"),
    "t3": ("Each item has two installed skills A and B. 's' if they do the same job, so one could replace the other. "
           "'n' if they do different jobs. The same kind of task for different languages or frameworks counts as "
           "different jobs.",
           lambda x: f"A: {x['a']}: {x['a_desc']}\nB: {x['b']}: {x['b_desc']}"),
    "t4": ("Below is the profile of a developer, then skills from a marketplace. 's' only if this developer would "
           "plausibly install and use the skill for the work described in the profile. 'n' if it is for other "
           "stacks or domains, or too generic to add value.",
           lambda x: f"{x['skill']}: {x['desc']}"),
}


def ask_claude(instr: str, items: list[dict], fmt) -> dict:
    claude = shutil.which("claude")
    if not claude:
        sys.exit("Non trovo il comando 'claude': serve per il giudizio automatico.")
    body = "\n\n".join(f"[{x['id']}]\n{fmt(x)}" for x in items)
    prompt = (instr + "\n\nReply with ONLY a JSON object mapping every item id to \"s\", \"n\" or \"?\", "
              "for example {\"t2-0\": \"s\", \"t2-1\": \"n\"}. No other text.")
    for _ in range(2):
        cmd = [claude, "-p", prompt] + (["--model", JUDGE_MODEL] if JUDGE_MODEL else [])
        r = subprocess.run(cmd, input=body, capture_output=True, text=True, timeout=900)
        out = r.stdout
        a, b = out.find("{"), out.rfind("}")
        if a >= 0 and b > a:
            try:
                data = json.loads(out[a:b + 1])
                return {k: str(v).strip().lower()[:1] for k, v in data.items() if str(v).strip()[:1].lower() in "sn?"}
            except ValueError:
                pass
    print("  il giudice non ha risposto in modo leggibile per un gruppo, lo salto")
    return {}


def cmd_giudice(args):
    verdicts = load("giudice.json", {})
    profile = (OUT / "profilo.md").read_text(encoding="utf-8").strip() if (OUT / "profilo.md").exists() else ""
    jobs = []
    for t, (instr, fmt) in JUDGE.items():
        items = [x for x in load(f"{t}.json", []) if x["id"] not in verdicts]
        if t == "t4":
            instr = instr + "\n\nDeveloper profile:\n" + profile
        for k in range(0, len(items), 60):
            jobs.append((instr, items[k:k + 60], fmt))
    if not jobs:
        return
    print(f"Claude ({JUDGE_MODEL or 'modello predefinito'}) giudica {sum(len(j[1]) for j in jobs)} casi "
          f"in {len(jobs)} gruppi, 4 alla volta…", flush=True)
    with ThreadPoolExecutor(max_workers=4) as ex:
        for k, res in enumerate(ex.map(lambda j: ask_claude(*j), jobs), 1):
            print(f"  gruppo {k}/{len(jobs)} fatto", flush=True)
            verdicts.update(res)
            save("giudice.json", verdicts)
    print("Giudizio finito.")


# ---------------------------------------------------------------------------
# Controllo a mano (opzionale)
# ---------------------------------------------------------------------------

def label_items(items: list[tuple[str, dict]], labels: dict) -> bool:
    for k, (t, x) in enumerate(items, 1):
        title, question, fields = TESTS[t]
        show(title, fields(x), question, k, len(items))
        a = ask()
        if a is None:
            save("etichette.json", labels)
            return False
        labels[x["id"]] = a
        save("etichette.json", labels)
    print("\033[2J\033[H", end="")
    return True


def cmd_controllo(args):
    """Poche domande a caso, per vedere se il giudice la pensa come te."""
    labels = load("etichette.json", {})
    verdicts = load("giudice.json", {})
    pool = [(t, x) for t in TESTS for x in load(f"{t}.json", [])
            if verdicts.get(x["id"]) in ("s", "n") and x["id"] not in labels]
    random.Random(SEED + 1).shuffle(pool)
    label_items(pool[: args.controllo], labels)


def cmd_etichetta(args):
    """Tutto a mano: rispondi tu a ogni caso invece del giudice."""
    labels = load("etichette.json", {})
    which = [args.prova] if args.prova else list(TESTS)
    todo = [(t, x) for t in which for x in load(f"{t}.json", []) if x["id"] not in labels]
    if label_items(todo, labels):
        print("Finito. Ora: .venv/bin/python laya_prove_mac.py risultati --giudice io")
    else:
        print("Salvato. Riprendi quando vuoi con 'etichetta'.")


# ---------------------------------------------------------------------------
# Risultati
# ---------------------------------------------------------------------------

def acc(preds, ys):
    return sum(p == y for p, y in zip(preds, ys)) / len(ys) if ys else None


def best_acc(scores, ys):
    if not ys:
        return None
    return max(acc([int(s >= t) for s in scores], ys) for t in sorted(set(scores)) + [9e9])


def pct(x):
    return "-" if x is None else f"{x:.0%}"


def verdict(laya, others):
    others = [o for o in others if o is not None]
    if laya is None or not others:
        return "-"
    d = laya - max(others)
    return "Laya meglio" if d >= .10 else ("pari" if d > -.10 else "Laya peggio")


def table(labels: dict) -> list[str]:
    rows = []

    def labelled(t):
        return [(x, 1 if labels[x["id"]] == "s" else 0) for x in load(f"{t}.json", [])
                if labels.get(x["id"]) in ("s", "n")]

    L = labelled("t1")
    if L:
        ys = [y for _, y in L]
        la = acc([int(x["laya"] >= .5) for x, _ in L], ys)
        kw = acc([x["keywords"] for x, _ in L], ys)
        maj = max(sum(ys), len(ys) - sum(ys)) / len(ys)
        rows.append(("T1 reazione", len(ys), pct(la), f"parole chiave {pct(kw)}", pct(maj), verdict(la, [kw, maj])))
    for t, name in (("t2", "T2 pertinenza"), ("t3", "T3 doppioni")):
        L = labelled(t)
        if L:
            ys = [y for _, y in L]
            la = acc([int(x["laya"] >= .5) for x, _ in L], ys)
            la_best = best_acc([x["laya"] for x, _ in L], ys)
            em = best_acc([x["emb"] for x, _ in L], ys)
            maj = max(sum(ys), len(ys) - sum(ys)) / len(ys)
            rows.append((name, len(ys), f"{pct(la)} (con soglia migliore {pct(la_best)})",
                         f"embedding {pct(em)} (con soglia migliore)", pct(maj), verdict(la, [em, maj])))
    L = labelled("t4")
    if L:
        def prec(key):
            v = [y for x, y in L if x.get(key)]
            return (sum(v), len(v), sum(v) / len(v) if v else None)
        (gl, nl, pl), (ge, ne, pe), (gp, npop, pp) = prec("in_laya"), prec("in_emb"), prec("in_pop")
        alt = f"embedding {ge}/{ne} ({pct(pe)})"
        if npop:
            alt += f"; più installate su skills.sh {gp}/{npop} ({pct(pp)})"
        rows.append(("T4 suggerimenti", len(L), f"{gl} utili su {nl} ({pct(pl)})", alt, "-", verdict(pl, [pe, pp])))
    out = ["| prova | casi | Laya | alternativa senza Laya | rispondere sempre uguale | verdetto |",
           "|---|---|---|---|---|---|"]
    return out + [f"| {' | '.join(str(c) for c in r)} |" for r in rows]


def cmd_risultati(args):
    info = load("info.json", {})
    judge = load("giudice.json", {})
    mine = load("etichette.json", {})
    use_me = getattr(args, "giudice", "claude") == "io"
    ref = mine if use_me else judge
    out = ["# Laya sui tuoi dati", "",
           f"Creato {info.get('creato', '?')}, ultimi {info.get('giorni', '?')} giorni.",
           f"Giudice: {'tu' if use_me else 'Claude'}.", ""]
    out += table(ref)
    out += ["", "La differenza deve essere di almeno 10 punti per contare.",
            "\"soglia migliore\" = soglia scelta guardando le risposte giuste: favorisce chi la usa.", ""]
    both = [k for k in mine if mine[k] in ("s", "n") and judge.get(k) in ("s", "n")]
    if both and not use_me:
        same = sum(mine[k] == judge[k] for k in both)
        out += [f"**Controllo:** tu e Claude siete d'accordo su {same} casi su {len(both)}.", ""]
    if info.get("t3", {}).get("copie_identiche"):
        groups = info["t3"]["copie_identiche"]
        out += [f"## Skill installate più volte: {len(groups)} (confronto esatto, senza Laya)", ""]
        out += ["- " + "  |  ".join(g) for g in groups[:30]]
        out.append("")
    if "t1" in info:
        out += [f"Invocazioni interrotte da te (segnale certo, senza Laya): {info['t1'].get('interrotte_escluse', 0)}", ""]
    t4 = load("t4.json", [])
    good = [x for x in t4 if ref.get(x["id"]) == "s"]
    if good:
        def who(x):
            return ", ".join(n for n, k in (("Laya", "in_laya"), ("embedding", "in_emb"), ("skills.sh", "in_pop")) if x.get(k))
        out += ["## Skill suggerite e giudicate utili", ""]
        out += [f"- **{x['skill']}** ({who(x)}) {x.get('fonte', '')}: {x['desc'][:140]}" for x in good]
        out.append("")
    text = "\n".join(out)
    (OUT / "risultati.md").write_text(text, encoding="utf-8")
    print(text)


def cmd_tutto(args):
    import time
    global JUDGE_MODEL
    JUDGE_MODEL = args.modello_giudice or None
    t0 = time.time()
    tempi = []
    if not (OUT / "profilo.md").exists():
        cmd_profilo(args)
        tempi.append(("profilo", time.time() - t0))
    if args.rifai or load("info.json", {}).get("versione") != 3:  # dati di una versione vecchia: rifai
        (OUT / "giudice.json").unlink(missing_ok=True)
        if (OUT / "etichette.json").exists():  # le risposte vecchie si riferiscono a casi che cambiano
            (OUT / "etichette.json").rename(OUT / f"etichette_vecchie_{datetime.now():%Y%m%d%H%M}.json")
        t = time.time()
        cmd_prepara(args)
        tempi.append(("preparazione (Laya, embedding, download)", time.time() - t))
    t = time.time()
    cmd_giudice(args)
    tempi.append(("giudizio di Claude", time.time() - t))
    if args.controllo:
        cmd_controllo(args)
    cmd_risultati(args)
    print(f"\nSalvato anche in {OUT / 'risultati.md'}")
    print("Tempi: " + ", ".join(f"{n} {int(sec // 60)}m{int(sec % 60):02d}s" for n, sec in tempi))


# ============================================================================

def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)

    def prep_args(p):
        p.add_argument("--days", type=int, default=60, help="quanti giorni di sessioni leggere")
        p.add_argument("--prompts", type=int, default=60, help="T2: prompt campionati (3 skill ciascuno)")
        p.add_argument("--pairs", type=int, default=60, help="T3: coppie di skill più simili da valutare")
        p.add_argument("--top", type=int, default=15, help="T4: quante skill propone ogni metodo")
        p.add_argument("--pool", type=int, default=100, help="T4: quante skill passano a Laya dopo il filtro")
        p.add_argument("--per-query", type=int, default=12, help="T4: risultati per ricerca su skills.sh")
        p.add_argument("--catalogo-dir", action="append", default=[], help="T4: cartella locale con altre skill")
        p.add_argument("--no-github", action="store_true", help="T4: usa solo skills.sh")

    prep_args(ap)
    ap.add_argument("--controllo", type=int, default=0, help="rispondi tu a N casi a caso per verificare il giudice")
    ap.add_argument("--rifai", action="store_true", help="rifai la preparazione da capo")
    ap.add_argument("--modello-giudice", default="sonnet", help="modello di Claude per il giudizio (vuoto = predefinito)")
    ap.set_defaults(cmd="tutto", giudice="claude")
    sub = ap.add_subparsers(dest="cmd")
    prep_args(sub.add_parser("profilo", help="solo il profilo"))
    prep_args(sub.add_parser("prepara", help="solo la preparazione"))
    sub.add_parser("giudice", help="solo il giudizio di Claude")
    p = sub.add_parser("controllo", help="rispondi tu a N casi a caso")
    p.add_argument("controllo", type=int, nargs="?", default=10)
    p = sub.add_parser("etichetta", help="rispondi tu a tutti i casi (modalità manuale)")
    p.add_argument("prova", nargs="?", choices=list(TESTS))
    p = sub.add_parser("risultati")
    p.add_argument("--giudice", choices=["claude", "io"], default="claude")
    args = ap.parse_args()
    {"tutto": cmd_tutto, "profilo": cmd_profilo, "prepara": cmd_prepara, "giudice": cmd_giudice,
     "controllo": cmd_controllo, "etichetta": cmd_etichetta, "risultati": cmd_risultati}[args.cmd or "tutto"](args)


if __name__ == "__main__":
    main()
