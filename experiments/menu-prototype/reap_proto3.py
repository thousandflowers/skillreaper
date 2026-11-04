#!/usr/bin/env python3
"""The proposal: Gauge's bar + Receipt's strike-out + Table's columns. Read-only: nothing is ever run."""
import curses, datetime, os, textwrap
from reap_proto import DATA, DEAD, ROWS, WHY, C, DIM, HL, put, window
from reap_proto2 import EXTRA, HEALTH, PRICE, grad, setup, spark, src

RAMP = list(reversed(HEALTH))
TODAY = datetime.datetime.now(datetime.timezone.utc)
MODES = ["by source", "by action", "flat"]
ACTION_NAMES = [("Your items you never use", lambda r: r["Verdict"] == "REAP" and r["Removable"]),
                ("Heavy skills you rarely fire", lambda r: r["Verdict"] == "MUTE"),
                ("Inside plugins", lambda r: r["Verdict"] == "REAP" and not r["Removable"]),
                ("Held back", lambda r: r["Verdict"] == "REVIEW"),
                ("Kept", lambda r: r["Verdict"] == "KEEP"),
                ("Muted", lambda r: r.get("Reason") == "muted")]
LABEL = {"prune": "prune", "mute": "mute", "keep": "keep"}


def dead(r):
    return r["Verdict"] in ("REAP", "MUTE")


def can(r, act):
    if act == "prune":
        return True
    if act == "mute":
        return r.get("Reason") != "muted" and r["Category"] in ("skill", "agent")
    return True


def key(r):
    return (r["Category"], r["Name"], r.get("Platform"))


def last(r):
    s = r.get("LastUsed") or ""
    if s.startswith("0001") or not s:
        return "never"
    try:
        d = (TODAY - datetime.datetime.fromisoformat(s.replace("Z", "+00:00"))).days
    except ValueError:
        return "?"
    return "today" if d == 0 else f"{d}d ago"


def groups(mode):
    if mode == 0:
        by = {}
        for r in ROWS:
            by.setdefault(src(r), []).append(r)
        out = [(n, its) for n, its in by.items()]
    elif mode == 1:
        out = [(n, [r for r in ROWS if t(r)]) for n, t in ACTION_NAMES]
    else:
        return [("everything", sorted(ROWS, key=lambda r: -r["Tokens"]))]
    out = [(n, sorted(i, key=lambda r: (-dead(r), -r["Tokens"]))) for n, i in out if i]
    return out if mode == 1 else sorted(out, key=lambda g: -sum(r["Tokens"] for r in g[1] if dead(r)))


def strike(s):
    return "".join(c + "̶" if c != " " else c for c in s)


def text(w, y, x, s, a, struck):
    """addnstr counts the combining marks, so cut first and write the whole string."""
    _, wd = w.getmaxyx()
    s = s[: max(0, wd - x - 1)]
    try:
        w.addstr(y, x, strike(s) if struck else s, a)
    except curses.error:
        pass


def details(w, r, plan, set_plan):
    while True:
        w.erase()
        h, wd = w.getmaxyx()
        rw = wd - 8
        put(w, 1, 3, r["Name"], curses.A_BOLD)
        put(w, 2, 3, f"{r['Category']} · {src(r)}", DIM())
        p = plan.get(key(r))
        put(w, 1, wd - 22, f"plan: {p or 'nothing'}", C(2) if p else DIM())
        put(w, 4, 3, "Costs you", DIM())
        put(w, 5, 3, f"{r['Tokens']} tok every session · ${r['Tokens'] * PRICE:.3f}/month")
        grad(w, 6, 3, 40, min(1, r["Tokens"] / 300), RAMP)
        put(w, 8, 3, "Use", DIM())
        put(w, 9, 3, f"{r['Uses']} uses in {DATA.get('WindowDays', 30)} days of {DATA.get('Sessions', 0)} sessions · last used {last(r)}")
        put(w, 10, 3, f"asked for by name {r.get('Requests', 0)} times · {r.get('ErrorCount', 0)} errors")
        vals = EXTRA["byday"].get(r["Name"])
        if vals:
            spark(w, 11, 3, vals, RAMP)
            put(w, 11, 35, "by day, last 30", DIM())
        put(w, 13, 3, "Why reap says this", DIM())
        put(w, 14, 3, f"{r['Verdict']} · {WHY.get(r.get('Reason'), r.get('Reason') or '')}")
        put(w, 16, 3, "What it does", DIM())
        for n, line in enumerate(textwrap.wrap(r.get("Description") or "no description", rw)[:5]):
            put(w, 17 + n, 3, line)
        tools = r.get("ToolSurface", 0)
        put(w, 23, 3, f"can use {'every tool' if tools == -1 else str(tools) + ' tools'} · {r.get('BodyChars', 0):,} chars when opened", DIM())
        put(w, 24, 3, (r.get("Path") or "")[:rw], DIM())
        put(w, h - 2, 3, "p prune · m mute · k keep · u undo · esc back · demo: nothing is run", DIM())
        w.refresh()
        k = w.getch()
        if k in (27, curses.KEY_LEFT, ord("q")):
            return
        if k in (ord("p"), ord("m"), ord("k"), ord("u")):
            set_plan([r], chr(k))


def summary(w, plan):
    picks = [(r, plan[key(r)]) for r in ROWS if key(r) in plan and plan[key(r)] != "keep"]
    w.erase()
    h, wd = w.getmaxyx()
    tok = sum(r["Tokens"] for r, _ in picks)
    put(w, 1, 3, f"{len(picks)} changes · ~{tok:,} tok/session · ${tok * PRICE:.2f}/month", curses.A_BOLD)
    pr = [r["Name"] for r, a in picks if a == "prune"]
    mu = [r["Name"] for r, a in picks if a == "mute"]
    y = 3
    for verb, names in (("prune", pr), ("mute", mu)):
        if names:
            put(w, y, 3, f"{verb} · {len(names)} items", curses.A_BOLD)
            for line in textwrap.wrap(", ".join(names), wd - 8)[:8]:
                y += 1
                put(w, y, 5, line, DIM())
            y += 2
    put(w, h - 3, 3, "Apply all of this?  [y/N]", curses.A_BOLD)
    w.refresh()
    k = w.getch()
    return f"demo: would run {len(pr)} prune and {len(mu)} mute" if k in (ord("y"), ord("Y")) else "demo: cancelled, your plan is still here"


def run(w):
    mode, i, opened, plan, msg = 0, 0, set(), {}, "demo: nothing is run"

    def set_plan(items, ch):
        nonlocal msg
        act = {"p": "prune", "m": "mute", "k": "keep", "u": None}[ch]
        done, refused = 0, 0
        for r in items:
            if act is None:
                done += plan.pop(key(r), None) is not None
            elif can(r, act):
                plan[key(r)] = act
                done += 1
            else:
                refused += 1
        what = {"prune": "marked to prune", "mute": "marked to mute", "keep": "marked to keep", None: "undone"}[act]
        msg = f"{done} {what}"
        if refused:
            msg += f" · {refused} cannot be {act}d" + (" (inside a plugin, try m)" if act == "prune" else " (only Claude Code can disable them)")

    while True:
        gs = groups(mode)
        flat = []
        for gn, (name, items) in enumerate(gs):
            if mode != 2:
                flat.append(("g", gn, name, items))
            if mode == 2 or gn in opened:
                flat += [("i", gn, r, items) for r in items]
        i = max(0, min(i, len(flat) - 1))
        saved = sum(r["Tokens"] for r in ROWS if plan.get(key(r)) in ("prune", "mute") and dead(r))
        now = max(0, DEAD - saved)
        w.erase()
        h, wd = w.getmaxyx()
        bw = wd - 6
        put(w, 1, 2, "UNUSED CONTEXT", curses.A_BOLD)
        put(w, 1, 18, f"~{DEAD:,}" + (f" → ~{now:,}" if saved else "") + " tok/session", 0)
        if saved:
            s = f"saving ~{saved:,} · ${saved * PRICE:.2f}/mo"
            put(w, 1, wd - len(s) - 3, s, C(5) | curses.A_BOLD)
        grad(w, 2, 2, bw, now / max(1, DEAD), RAMP)
        n = wd - 56
        put(w, 4, 2, f"   {'NAME  (' + MODES[mode] + ')':<{n}}{'USED':<10}{'ITEMS':>9}{'TOK':>7}{'/MO':>7}{'  LAST':<10}{'PLAN':<7}", curses.A_UNDERLINE)
        view = h - 8
        top = window(i, view, len(flat))
        for row, (kind, gn, o, items) in enumerate(flat[top:top + view]):
            y = 5 + row
            cur = top + row == i
            if kind == "g":
                d = [r for r in items if dead(r)]
                tok = sum(r["Tokens"] for r in d)
                fired = sum(1 for r in items if r["Uses"] > 0)
                plans = {plan.get(key(r)) for r in items} - {None}
                pl = (plans.pop() if len(plans) == 1 else "mixed") if plans else ""
                base = HL() if cur else curses.A_BOLD
                line = f"{'▾' if gn in opened else '▸'} {o[:n - 3]:<{n - 2}}"
                put(w, y, 1, ">" if cur else " ", base)
                put(w, y, 3, line, base)
                grad(w, y, 3 + n - 1 + 1, 8, max(fired / len(items), 0.03), HEALTH)
                put(w, y, 3 + n + 9, f"{len(d):>4}/{len(items):<4}{tok:>7,}{'$' + format(tok * PRICE, '.2f'):>7}{'':<10}{pl:<7}", base)
            else:
                p = plan.get(key(o))
                out = p in ("prune", "mute")
                color = HL() if cur else (C(1) if p == "prune" else C(2) if p == "mute" else C(5) if p == "keep" else (0 if dead(o) else DIM()))
                put(w, y, 1, ">" if cur else " ", color)
                text(w, y, 3, f"  {o['Name'][:n - 4]:<{n - 2}}", color, out)
                put(w, y, 3 + n + 1, f"{o['Uses']:>3} uses", color)
                text(w, y, 3 + n + 9, f"{'':<9}{o['Tokens']:>7}{'$' + format(o['Tokens'] * PRICE, '.2f'):>7}  {last(o):<8}", color, out)
                put(w, y, 3 + n + 9 + 33, f"{p or '':<7}", color)
        put(w, h - 3, 2, msg[: wd - 4], C(4))
        put(w, h - 2, 2, "↑↓ move · →← open/close · enter details · p prune · m mute · k keep · u undo · g group · a apply · q quit"[: wd - 3], DIM())
        w.refresh()
        k = w.getch()
        if not flat:
            return
        kind, gn, o, items = flat[i]
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
        elif k in (10, 13):
            if kind == "g":
                opened ^= {gn}
            else:
                details(w, o, plan, set_plan)
        elif k in (ord("p"), ord("m"), ord("k"), ord("u")):
            # On a group, only what reap judges unused: a tap must not take what you use.
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
