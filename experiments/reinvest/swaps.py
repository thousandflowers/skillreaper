"""Swaps: mute skills you never use, install one that does work you actually do, for about the same tokens.

The picker is one list. Each entry shows the skills to mute above and a candidate below:
  up/down  previous / next swap      left/right  other candidates for this swap
  enter    accept / undo the swap    q           done (then asks before changing anything)

Muting goes through `reap mute` (reversible with `reap unmute`), so it works for plugin skills too.
"""
from __future__ import annotations

import curses
import os
import subprocess
from pathlib import Path

MAX_MUTED_PER_SWAP = 3
STEP_TIMEOUT = 120  # `skills add` on a repo that is gone clones forever, even with git prompts off
# Install for Claude Code only: without -a, `skills add` installs for every agent it knows (~20), and exits 0
# even when one of them fails.
SKILLS_ADD = ["npx", "-y", "skills", "add"]
SKILLS_FLAGS = ["-g", "-y", "-a", "claude-code"]


def pair(cost: int | None, pool: list[dict]) -> list[dict]:
    """REAP skills whose weight adds up to about `cost`: the closest single or pair, else the heaviest few.
    Nothing for an unknown weight: muting against a guess is how a 2800-token estimate muted 706 real tokens."""
    if cost is None:
        return []
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
        if not mute and pick["cost"] is not None:
            continue
        pool = [x for x in pool if x not in mute]
        out = sum(x["tokens"] for x in mute)
        cands = sorted(passing, key=lambda c: (c["gap"] != pick["gap"], -c["fit"], c["cost"] is None,
                                               abs((c["cost"] or 0) - out)))
        idx = next((i for i, c in enumerate(cands) if c["name"] == pick["name"]), 0)
        swaps.append({"mute": mute, "out": out, "cands": cands, "idx": idx, "ok": False})
    return swaps


def weight(cost: int | None) -> str:
    return "weight unknown until installed" if cost is None else f"~{cost} tok/session"


def mute_text(s: dict) -> str:
    if not s["mute"]:
        return "nothing · the weight of what it installs is unknown, so nothing is traded for it"
    return f"{names(s['mute'])}   ~{s['out']} tok/session · never used"


def names(mute: list[dict]) -> str:
    if not mute:
        return "nothing"
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
        put(y, 2, f"{'✓' if s['ok'] else ' '}  mute     {mute_text(s)}",
            curses.A_REVERSE if sel else 0)
        pick = f"◂ {c['name']} ▸" if len(s["cands"]) > 1 else c["name"]
        put(y + 1, 5, f"install  {pick}   {weight(c['cost'])} · {c['kind']}   {s['idx'] + 1}/{len(s['cands'])}"
            + ("   (already picked above)" if taken else ""), curses.A_BOLD if sel else 0)
        put(y + 2, 14, f"for {c['gap']}")
        if sel:
            put(y + 3, 14, c["why"], curses.A_DIM)
            y += 1
        y += 4
    chosen = [s for s in swaps if s["ok"]]
    costs = [s["cands"][s["idx"]]["cost"] for s in chosen]
    freed = sum(s["out"] for s in chosen)
    net = "unknown (an MCP weight is unknown)" if None in costs else f"{sum(costs) - freed:+} tok/session"
    put(h - 2, 2, f"{len(chosen)} accepted · −{freed} muted · net {net}", curses.A_BOLD)
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

def mcp_name(c: dict) -> str:
    return (c.get("server") or {}).get("name", c["name"]).split("/")[-1]


def install_command(c: dict) -> list[str] | None:
    """The command that installs a candidate, or None when it needs manual setup (keys, env vars)."""
    if c["kind"] == "skill":
        return [*SKILLS_ADD, c["source"], "-s", c["slug"], *SKILLS_FLAGS] if c.get("source") and c.get("slug") else None
    server, name = c.get("server") or {}, mcp_name(c)
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


def uninstall_command(c: dict) -> list[str]:
    if c["kind"] == "skill":
        return ["npx", "-y", "skills", "remove", c["slug"], *SKILLS_FLAGS]
    return ["claude", "mcp", "remove", "-s", "user", mcp_name(c)]


def installed(c: dict) -> bool:
    """`skills add` exits 0 when some target failed; the skill must actually be where Claude Code reads it."""
    if c["kind"] != "skill":
        return True  # `claude mcp add` exits 1 on failure
    return (Path.home() / ".claude" / "skills" / c["slug"] / "SKILL.md").is_file()


def run(cmd: list[str]) -> str | None:
    """Run one step; None on success, else why it failed."""
    try:
        res = subprocess.run(cmd, capture_output=True, text=True, timeout=STEP_TIMEOUT,
                             env={**os.environ, "GIT_TERMINAL_PROMPT": "0"}, stdin=subprocess.DEVNULL)
    except subprocess.TimeoutExpired:
        return f"no answer in {STEP_TIMEOUT} s"
    except OSError as e:
        return str(e)
    return None if res.returncode == 0 else (res.stderr.strip() or res.stdout.strip())[-200:] or f"exit {res.returncode}"


def apply_one(s: dict) -> list[str]:
    """Install, then mute. All or nothing: a failed mute unmutes what this swap muted and uninstalls."""
    c = s["cands"][s["idx"]]
    install = install_command(c)
    if install is None:
        return [f"  ✗  {c['name']}: needs manual setup, nothing muted · {c.get('id', '')}"]
    err = run(install) or (None if installed(c) else "installed, but not for Claude Code")
    if err:
        return [f"  ✗  {c['name']}: {err} · nothing muted"]
    log, muted = [f"  ✓  installed {c['name']}"], []
    for x in s["mute"]:
        err = run(["reap", "mute", x["name"]])
        if err is None:
            muted.append(x["name"])
            continue
        undone = [n for n in muted if run(["reap", "unmute", n]) is None]
        removed = run(uninstall_command(c)) is None
        left = [n for n in muted if n not in undone] + ([] if removed else [c["name"] + " (installed)"])
        return log + [f"  ✗  reap mute {x['name']}: {err}",
                      "  ↺  rolled back" + (f", but still changed: {', '.join(left)}" if left else ": nothing changed")]
    return log + [f"  ✓  muted {name} (reap unmute {name} undoes)" for name in muted]


def apply(chosen: list[dict], dry_run: bool) -> list[str]:
    log = []
    for s in chosen:
        if not dry_run:
            log += apply_one(s)
            continue
        c = s["cands"][s["idx"]]
        cmd = install_command(c)
        log += ([f"  ✗  {c['name']}: needs manual setup, nothing muted · {c.get('id', '')}"] if cmd is None else
                ["  ·  " + " ".join(cmd)] + [f"  ·  reap mute {x['name']}" for x in s["mute"]])
    return log
