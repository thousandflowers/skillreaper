"""
scambi.py — scelta interattiva degli scambi di reinvest_prova.py, ed esecuzione.

Ogni riga: sopra la skill (o le skill) mai usate da togliere, sotto una candidata da mettere.
  ←/→  altre candidate per questa riga      ↑/↓  riga precedente / successiva
  invio  accetta o annulla lo scambio        q    fine (poi chiede conferma prima di eseguire)

Togliere = spostare in ~/.claude/skills-archive/<data>/ (mai cancellare). Solo le skill personali in
~/.claude/skills/ si possono togliere una per una: quelle dentro un plugin no.
"""
from __future__ import annotations

import curses
import shutil
import subprocess
from datetime import datetime
from pathlib import Path

SKILLS_DIR = Path.home() / ".claude" / "skills"
ARCHIVE = Path.home() / ".claude" / "skills-archive"


def removable(skill: dict) -> bool:
    """Una skill personale (cartella in ~/.claude/skills), quindi archiviabile da sola."""
    return Path(skill.get("path", "")).parent.parent == SKILLS_DIR


def order_candidates(row_task: int, cost: int, passing: list[dict]) -> list[dict]:
    """Prima le candidate dello stesso buco, poi le altre; a parità, costo più vicino a quanto si toglie."""
    return sorted(passing, key=lambda c: (c["task"] != row_task, abs(c["cost"] - cost)))


# ---------------------------------------------------------------------------
# Interfaccia
# ---------------------------------------------------------------------------

def _draw(scr, rows: list[dict], cur: int):
    scr.erase()
    h, w = scr.getmaxyx()

    def put(y: int, x: int, text: str, attr=0):
        if 0 <= y < h - 1:
            scr.addnstr(y, x, text, max(0, w - x - 1), attr)

    put(0, 0, "Scambi consigliati   ↑↓ skill · ←→ candidate · invio accetta · q fine", curses.A_BOLD)
    y = 2
    for i, r in enumerate(rows):
        c = r["cands"][r["idx"]]
        sel = i == cur
        names = [n for n, _, _ in r["remove"]]
        out = " + ".join(names[:3]) + (f" + altre {len(names) - 3}" if len(names) > 3 else "")
        taken = any(o["ok"] and o is not r and o["cands"][o["idx"]]["name"] == c["name"] for o in rows)
        put(y, 0, f"{'▸' if sel else ' '} {'✓' if r['ok'] else ' '} togli  {out}  (~{r['out_cost']} token, mai usate)",
            curses.A_REVERSE if sel else 0)
        arrows = f"◂ {c['name']} ▸" if len(r["cands"]) > 1 else c["name"]
        put(y + 1, 4, f"metti {arrows}  ~{c['cost']} token ({c['kind']})  {r['idx'] + 1}/{len(r['cands'])}"
            + ("  [già scelta in un'altra riga]" if taken else ""), curses.A_BOLD if sel else 0)
        put(y + 2, 10, f"per: {c['gap']}")
        if sel:
            put(y + 3, 10, c["why"], curses.A_DIM)
            y += 1
        y += 4
    chosen = [r for r in rows if r["ok"]]
    add = sum(r["cands"][r["idx"]]["cost"] for r in chosen)
    rem = sum(r["out_cost"] for r in chosen)
    put(h - 2, 0, f"scelti {len(chosen)} · aggiungi ~{add} token · togli ~{rem} · netto {add - rem:+}", curses.A_BOLD)
    scr.refresh()


def handle_key(rows: list[dict], cur: int, k: int) -> tuple[int, bool]:
    """Un tasto → nuova riga corrente, e se l'elenco va chiuso. Separata da curses per poterla provare."""
    r = rows[cur]
    if k in (curses.KEY_DOWN, ord("j")):
        return min(cur + 1, len(rows) - 1), False
    if k in (curses.KEY_UP, ord("k")):
        return max(cur - 1, 0), False
    if k in (curses.KEY_RIGHT, ord("l")):
        r["idx"] = (r["idx"] + 1) % len(r["cands"])
    elif k in (curses.KEY_LEFT, ord("h")):
        r["idx"] = (r["idx"] - 1) % len(r["cands"])
    elif k in (10, 13, curses.KEY_ENTER):
        r["ok"] = not r["ok"]
        return (min(cur + 1, len(rows) - 1) if r["ok"] else cur), False
    elif k in (ord("q"), 27):
        return cur, True
    return cur, False


def accepted(rows: list[dict]) -> list[dict]:
    picked, seen = [], set()
    for r in rows:  # una candidata entra una volta sola anche se accettata in due righe
        name = r["cands"][r["idx"]]["name"]
        if r["ok"] and name not in seen:
            seen.add(name)
            picked.append(r)
    return picked


def choose(rows: list[dict]) -> list[dict]:
    """Apre l'elenco; ritorna le righe accettate (con la candidata scelta)."""
    def loop(scr):
        curses.curs_set(0)
        cur, done = 0, False
        while not done:
            _draw(scr, rows, cur)
            cur, done = handle_key(rows, cur, scr.getch())
    curses.wrapper(loop)
    return accepted(rows)


# ---------------------------------------------------------------------------
# Esecuzione
# ---------------------------------------------------------------------------

def install_cmd(c: dict) -> list[str] | None:
    if c["kind"] == "skill":
        src, slug = c.get("source", ""), c.get("slug", "")
        return ["npx", "-y", "skills", "add", src, "-s", slug, "-g", "-y"] if src and slug else None
    sv = c.get("server") or {}
    name = (sv.get("name") or c["name"]).split("/")[-1]
    for pk in sv.get("packages") or []:
        if pk.get("environmentVariables"):
            return None  # servono chiavi o impostazioni: va configurato a mano
        runner = {"npm": ["npx", "-y"], "pypi": ["uvx"]}.get(pk.get("registryType"))
        if runner and pk.get("identifier"):
            return ["claude", "mcp", "add", "-s", "user", name, "--", *runner, pk["identifier"]]
    for rm in sv.get("remotes") or []:
        transport = {"streamable-http": "http", "sse": "sse"}.get(rm.get("type"))
        if transport and rm.get("url"):
            return ["claude", "mcp", "add", "-s", "user", "--transport", transport, name, rm["url"]]
    return None


def execute(picked: list[dict], dry_run: bool) -> list[str]:
    """Installa le scelte e archivia le skill tolte. dry_run: solo il racconto, niente viene toccato."""
    stamp = ARCHIVE / f"reinvest-{datetime.now():%Y%m%d-%H%M%S}"
    report = []
    for r in picked:
        c = r["cands"][r["idx"]]
        cmd = install_cmd(c)
        if cmd is None:
            report.append(f"✗ {c['name']}: da installare a mano (servono impostazioni), niente tolto: {c.get('path', '')}")
            continue  # senza la nuova non tolgo le vecchie
        if dry_run:
            report.append("· " + " ".join(cmd))
        else:
            res = subprocess.run(cmd, capture_output=True, text=True)
            if res.returncode != 0:
                report.append(f"✗ {c['name']}: installazione fallita, niente tolto: {res.stderr.strip()[:200]}")
                continue
            report.append(f"✓ installata {c['name']}")
        for name, _cost, path in r["remove"]:
            src = Path(path).parent
            if dry_run:
                report.append(f"· archivia {src} → {stamp}/")
                continue
            stamp.mkdir(parents=True, exist_ok=True)
            shutil.move(str(src), str(stamp / src.name))  # sposta anche un link simbolico, senza toccare il bersaglio
            report.append(f"✓ archiviata {name} → {stamp / src.name}")
    return report
