"""Swaps: mute skills you never use, install one that does work you actually do, for about the same tokens.

The picker is one list. Each entry shows the skills to mute above and a candidate below:
  up/down  previous / next swap      left/right  other candidates for this swap
  enter    accept / undo the swap    q           done (then asks before changing anything)

Muting goes through `reap mute` (reversible with `reap unmute`), so it works for plugin skills too.
"""
from __future__ import annotations

import curses
import subprocess

MAX_MUTED_PER_SWAP = 3  # an MCP server estimated at thousands of tokens must not mute half the install


def pair(cost: int, pool: list[dict]) -> list[dict]:
    """REAP skills whose weight adds up to about `cost`: the closest single or pair, else the heaviest few."""
    singles = [[x] for x in pool]
    pairs = [[a, b] for i, a in enumerate(pool) for b in pool[i + 1:] if a["tokens"] + b["tokens"] <= cost * 1.3]
    best = min(singles + pairs, key=lambda xs: (abs(sum(x["tokens"] for x in xs) - cost), len(xs)), default=[])
    if sum(x["tokens"] for x in best) >= cost * 0.7:
        return best
    heaviest = sorted(pool, key=lambda x: -x["tokens"])[:MAX_MUTED_PER_SWAP]
    return [x for i, x in enumerate(heaviest) if sum(y["tokens"] for y in heaviest[:i]) < cost]


def build(picks: list[dict], passing: list[dict], reap_pool: list[dict]) -> list[dict]:
    """One swap per pick; its candidates are every verified candidate, same gap first, closest weight next."""
    pool, swaps = list(reap_pool), []
    for pick in picks:
        mute = pair(pick["cost"], pool)
        if not mute:
            continue
        pool = [x for x in pool if x not in mute]
        out = sum(x["tokens"] for x in mute)
        cands = sorted(passing, key=lambda c: (c["gap"] != pick["gap"], abs(c["cost"] - out)))
        idx = next((i for i, c in enumerate(cands) if c["name"] == pick["name"]), 0)
        swaps.append({"mute": mute, "out": out, "cands": cands, "idx": idx, "ok": False})
    return swaps


def names(mute: list[dict]) -> str:
    shown = " + ".join(x["name"] for x in mute[:3])
    return shown + (f" + {len(mute) - 3} more" if len(mute) > 3 else "")


# ---------------------------------------------------------------------------
# Picker
# ---------------------------------------------------------------------------

def _draw(scr, swaps: list[dict], cur: int):
    scr.erase()
    h, w = scr.getmaxyx()

    def put(y: int, x: int, text: str, attr=0):
        if 0 <= y < h - 1:
            scr.addnstr(y, x, text, max(0, w - x - 1), attr)

    put(0, 2, "reinvest · swaps   ↑↓ swap · ←→ candidate · enter accept · q done", curses.A_BOLD)
    y = 2
    for i, s in enumerate(swaps):
        c, sel = s["cands"][s["idx"]], i == cur
        taken = any(o["ok"] and o is not s and o["cands"][o["idx"]]["name"] == c["name"] for o in swaps)
        put(y, 2, f"{'✓' if s['ok'] else ' '}  mute     {names(s['mute'])}   ~{s['out']} tok/session · never used",
            curses.A_REVERSE if sel else 0)
        pick = f"◂ {c['name']} ▸" if len(s["cands"]) > 1 else c["name"]
        put(y + 1, 5, f"install  {pick}   ~{c['cost']} tok/session · {c['kind']}   {s['idx'] + 1}/{len(s['cands'])}"
            + ("   (already picked above)" if taken else ""), curses.A_BOLD if sel else 0)
        put(y + 2, 14, f"for {c['gap']}")
        if sel:
            put(y + 3, 14, c["why"], curses.A_DIM)
            y += 1
        y += 4
    chosen = [s for s in swaps if s["ok"]]
    added = sum(s["cands"][s["idx"]]["cost"] for s in chosen)
    freed = sum(s["out"] for s in chosen)
    put(h - 2, 2, f"{len(chosen)} accepted · +{added} installed · −{freed} muted · net {added - freed:+} tok/session",
        curses.A_BOLD)
    scr.refresh()


def on_key(swaps: list[dict], cur: int, key: int) -> tuple[int, bool]:
    """One key press -> new cursor and whether the picker closes. Kept free of curses I/O so it can be tested."""
    s = swaps[cur]
    if key in (curses.KEY_DOWN, ord("j")):
        return min(cur + 1, len(swaps) - 1), False
    if key in (curses.KEY_UP, ord("k")):
        return max(cur - 1, 0), False
    if key in (curses.KEY_RIGHT, ord("l")):
        s["idx"] = (s["idx"] + 1) % len(s["cands"])
    elif key in (curses.KEY_LEFT, ord("h")):
        s["idx"] = (s["idx"] - 1) % len(s["cands"])
    elif key in (10, 13, curses.KEY_ENTER):
        s["ok"] = not s["ok"]
        return (min(cur + 1, len(swaps) - 1) if s["ok"] else cur), False
    elif key in (ord("q"), 27):
        return cur, True
    return cur, False


def accepted(swaps: list[dict]) -> list[dict]:
    """Accepted swaps; a candidate accepted twice is installed once."""
    out, seen = [], set()
    for s in swaps:
        name = s["cands"][s["idx"]]["name"]
        if s["ok"] and name not in seen:
            seen.add(name)
            out.append(s)
    return out


def pick(swaps: list[dict]) -> list[dict]:
    def loop(scr):
        curses.curs_set(0)
        cur, done = 0, False
        while not done:
            _draw(scr, swaps, cur)
            cur, done = on_key(swaps, cur, scr.getch())
    curses.wrapper(loop)
    return accepted(swaps)


# ---------------------------------------------------------------------------
# Apply
# ---------------------------------------------------------------------------

def install_command(c: dict) -> list[str] | None:
    """The command that installs a candidate, or None when it needs manual setup (keys, env vars)."""
    if c["kind"] == "skill":
        return ["npx", "-y", "skills", "add", c["source"], "-s", c["slug"], "-g", "-y"] \
            if c.get("source") and c.get("slug") else None
    server = c.get("server") or {}
    name = server.get("name", c["name"]).split("/")[-1]
    for pkg in server.get("packages") or []:
        if pkg.get("environmentVariables"):
            return None
        runner = {"npm": ["npx", "-y"], "pypi": ["uvx"]}.get(pkg.get("registryType"))
        if runner and pkg.get("identifier"):
            return ["claude", "mcp", "add", "-s", "user", name, "--", *runner, pkg["identifier"]]
    for remote in server.get("remotes") or []:
        transport = {"streamable-http": "http", "sse": "sse"}.get(remote.get("type"))
        if transport and remote.get("url"):
            return ["claude", "mcp", "add", "-s", "user", "--transport", transport, name, remote["url"]]
    return None


def apply(chosen: list[dict], dry_run: bool) -> list[str]:
    """Install each pick, then mute what it replaces. Nothing is muted if the install fails."""
    log = []
    for s in chosen:
        c = s["cands"][s["idx"]]
        steps = [install_command(c)] + [["reap", "mute", x["name"]] for x in s["mute"]]
        if steps[0] is None:
            log.append(f"  ✗  {c['name']}: needs manual setup, nothing muted · {c.get('id', '')}")
            continue
        for cmd in steps:
            if dry_run:
                log.append("  ·  " + " ".join(cmd))
                continue
            res = subprocess.run(cmd, capture_output=True, text=True)
            if res.returncode != 0:
                log.append(f"  ✗  {' '.join(cmd[:4])}: {res.stderr.strip()[:200]}")
                break  # a failed install leaves everything else untouched
            log.append(f"  ✓  {' '.join(cmd)}")
    return log
