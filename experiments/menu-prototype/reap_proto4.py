#!/usr/bin/env python3
"""Proposal v2: reap says what it would do per row, space accepts, savings on every row. Read-only."""
import curses, os
from reap_proto import DATA, DEAD, ROWS, C, DIM, HL, put, window
from reap_proto2 import HEALTH, PRICE, grad, rc, setup
from reap_proto3 import RAMP, MODES, dead, can, details, groups, key, last, summary, text


def suggestion(r):
    if r.get("Reason") == "muted":
        return "muted"
    if r["Verdict"] == "REAP" and can(r, "prune"):
        return "prune"
    if r["Verdict"] in ("REAP", "MUTE") and can(r, "mute"):
        return "mute"
    if r["Verdict"] == "REAP":
        return "/plugin"
    return {"KEEP": "keep", "REVIEW": "review"}.get(r["Verdict"], "-")


STYLE = {"prune": 1, "mute": 2, "/plugin": 3, "keep": 5, "review": 4}
ACTIONABLE = [r for r in ROWS if suggestion(r) in ("prune", "mute")]
BEST = sum(r["Tokens"] for r in ACTIONABLE)
COUNTS = {s: sum(1 for r in ROWS if suggestion(r) == s) for s in ("prune", "mute", "/plugin")}


def fmt(t):
    return f"~{t:,} tok ${t * PRICE:.2f}"


def bar(w, y, x, width, now, ghost):
    grad(w, y, x, width, now / max(1, DEAD), RAMP)
    a, b = int((now - ghost) / max(1, DEAD) * width), int(now / max(1, DEAD) * width)
    for i in range(max(0, a), max(a + (1 if ghost else 0), b)):
        put(w, y, x + i, "▒", curses.A_REVERSE)


def run(w):
    mode, i, opened, plan, msg = 0, 0, set(), {}, "space accepts what reap says · demo: nothing is run"

    def set_plan(items, ch):
        nonlocal msg
        act = {"p": "prune", "m": "mute", "k": "keep", "u": None}[ch]
        n = sum(1 for r in items if act is None or can(r, act))
        for r in items:
            if act is None:
                plan.pop(key(r), None)
            elif can(r, act):
                plan[key(r)] = act
        msg = f"{n} marked {act}" if act else f"{n} undone"

    def accept(items):
        nonlocal msg
        go = [r for r in items if suggestion(r) in ("prune", "mute")]
        if not go:
            msg = "reap suggests nothing you can do here" + (" · only Claude Code can disable it (/plugin)" if any(suggestion(r) == "/plugin" for r in items) else "")
            return
        if all(key(r) in plan for r in go):
            for r in go:
                plan.pop(key(r), None)
            msg = f"{len(go)} unselected"
        else:
            for r in go:
                plan[key(r)] = suggestion(r)
            t = sum(r["Tokens"] for r in go)
            msg = f"{len(go)} selected · saves {fmt(t)}/mo"

    while True:
        gs = [(n, sorted(items, key=lambda r: (suggestion(r) not in ("prune", "mute"), -r["Tokens"]))) for n, items in groups(mode)]
        flat = []
        for gn, (name, items) in enumerate(gs):
            if mode != 2:
                flat.append(("g", gn, name, items))
            if mode == 2 or gn in opened:
                flat += [("i", gn, r, items) for r in items]
        i = max(0, min(i, len(flat) - 1))
        kind, gn, o, items = flat[i]
        saved = sum(r["Tokens"] for r in ROWS if plan.get(key(r)) in ("prune", "mute") and dead(r))
        now = max(0, DEAD - saved)
        hover = [r for r in (items if kind == "g" else [o]) if suggestion(r) in ("prune", "mute") and key(r) not in plan]
        ghost = sum(r["Tokens"] for r in hover)
        w.erase()
        h, wd = w.getmaxyx()
        put(w, 1, 2, "UNUSED CONTEXT", curses.A_BOLD)
        put(w, 1, 18, f"~{DEAD:,}" + (f" → ~{now:,}" if saved else "") + " tok/session")
        if saved:
            s = f"saving {fmt(saved)}/mo"
            put(w, 1, wd - len(s) - 3, s, C(5) | curses.A_BOLD)
        bar(w, 2, 2, wd - 6, now, ghost)
        put(w, 3, 2, f"reap suggests: {COUNTS['prune']} prune · {COUNTS['mute']} mute · {COUNTS['/plugin']} only via /plugin · doing all of it saves {fmt(BEST)}/mo", DIM())
        n = wd - 54
        put(w, 5, 2, f"    {'NAME  (' + MODES[mode] + ')':<{n}}{'USES':>5}{'  LAST':<10}{'REAP SAYS':<11}{'SAVES':>16}", curses.A_UNDERLINE)
        view = h - 9
        top = window(i, view, len(flat))
        for row, (kd, g2, it, its) in enumerate(flat[top:top + view]):
            y = 6 + row
            cur = top + row == i
            if kd == "g":
                go = [r for r in its if suggestion(r) in ("prune", "mute")]
                sel = sum(1 for r in go if key(r) in plan)
                t = sum(r["Tokens"] for r in go)
                base = HL() if cur else curses.A_BOLD
                box = "[x]" if go and sel == len(go) else "[-]" if sel else "[ ]" if go else "   "
                put(w, y, 1, ">" if cur else " ", base)
                put(w, y, 3, f"{box} {'▾' if g2 in opened else '▸'} {it[:n - 12]} ", base)
                c = len(its)
                put(w, y, 3 + n - 7, f"{len(go):>3} of {c:<4}", base)
                fired = sum(1 for r in its if r["Uses"] > 0)
                grad(w, y, 3 + n + 15, 8, max(fired / c, 0.03), HEALTH)
                put(w, y, 3 + n + 24, f"{fmt(t) if t else 'nothing':>16}", base)
            else:
                p = plan.get(key(it))
                out = p in ("prune", "mute")
                s = suggestion(it)
                color = HL() if cur else (C(5) if p == "keep" else C(1) if p == "prune" else C(2) if p == "mute" else 0)
                box = "[x]" if out else "[ ]" if s in ("prune", "mute") else "   "
                put(w, y, 1, ">" if cur else " ", color)
                put(w, y, 3, box, color)
                text(w, y, 7, f"  {it['Name'][:n - 5]:<{n - 4}}", color, out)
                put(w, y, 3 + n, f"{it['Uses']:>5}  {last(it):<8}", color if cur or out else DIM())
                put(w, y, 3 + n + 15, f"{(p if p else s):<9}", color if cur or p else C(STYLE.get(s, 8)))
                put(w, y, 3 + n + 26, f"{(fmt(it['Tokens']) if s in ('prune', 'mute') else '-'):>14}", color if cur or out else 0)
        if ghost:
            put(w, h - 3, 2, f"{'this group' if kind == 'g' else o['Name']}: selecting saves {fmt(ghost)}/mo   ", curses.A_BOLD)
            put(w, h - 3, 60, msg[: wd - 62], C(4))
        else:
            put(w, h - 3, 2, msg[: wd - 4], C(4))
        put(w, h - 2, 2, "↑↓ move · →← open/close · space select · enter details · p/m/k choose · g group · a apply · q quit"[: wd - 3], DIM())
        w.refresh()
        k = w.getch()
        if k == ord("q"):
            return
        if k == curses.KEY_UP:
            i -= 1
        elif k == curses.KEY_DOWN:
            i += 1
        elif k == curses.KEY_RIGHT and kind == "g":
            opened.add(gn)
        elif k == curses.KEY_LEFT:
            opened.discard(gn)
            i = next((x for x, f in enumerate(flat) if f[0] == "g" and f[1] == gn), i)
        elif k == ord(" "):
            accept(items if kind == "g" else [o])
        elif k in (10, 13):
            if kind == "g":
                opened ^= {gn}
            else:
                details(w, o, plan, set_plan)
        elif k in (ord("p"), ord("m"), ord("k"), ord("u")):
            set_plan([r for r in items if dead(r)] if kind == "g" and k != ord("u") else (items if kind == "g" else [o]), chr(k))
        elif k == ord("g"):
            mode, i, opened = (mode + 1) % 3, 0, set()
        elif k == ord("a"):
            msg = summary(w, plan)


def main(w):
    setup()
    w.keypad(True)
    run(w)


if __name__ == "__main__":
    os.environ.setdefault("ESCDELAY", "25")
    curses.wrapper(main)
