#!/usr/bin/env python3
"""Five read-only layout prototypes for an interactive reap. Nothing is ever run."""
import curses, json, locale, os, sys, textwrap

HERE = os.path.dirname(os.path.abspath(__file__))
DATA = json.load(open(os.path.join(HERE, "data.json")))
ROWS = [r for r in DATA["Rows"] if r["Category"] in ("skill", "agent", "mcp")]
PRUNED = [e for e in json.load(open(os.path.join(HERE, "pruned.json"))).get("entries", []) if not e.get("restored")]

WHY = {
    "unused": "Never fired, with enough sessions to judge.",
    "broken": "Invoked, and it only ever errored.",
    "heavy-rare": "Used, but heavy and fired in too few sessions.",
    "tiny": "Weighs almost nothing, not worth removing.",
    "grace": "Installed too recently to judge.",
    "needs-data": "Not enough sessions yet.",
    "no-transcript": "This platform keeps no transcripts, so there is no evidence.",
    "rare": "Fired before this window, on a longer cadence.",
    "used": "Used in this window.",
    "user-keep": "You protected it with reap keep.",
    "claude-md-ref": "Named in your CLAUDE.md or rules.",
    "hook-loaded": "Its plugin injects it every session.",
    "requested": "You asked for it by name, and it never started.",
    "muted": "You muted it, so its uses prove nothing.",
}


def g(name, cmd, test, color):
    return {"name": name, "cmd": cmd, "items": sorted([r for r in ROWS if test(r)], key=lambda r: -r["Tokens"]), "color": color}


GROUPS = [
    g("Your items you never use", "reap prune", lambda r: r["Verdict"] == "REAP" and r["Removable"], 1),
    g("Heavy skills you rarely fire", "reap mute", lambda r: r["Verdict"] == "MUTE", 2),
    g("Inside plugins", "reap mute <name> or /plugin", lambda r: r["Verdict"] == "REAP" and not r["Removable"], 3),
    g("Held back", "reap why <name>", lambda r: r["Verdict"] == "REVIEW", 4),
    g("Kept", "reap why <name>", lambda r: r["Verdict"] == "KEEP", 5),
    g("Muted", "reap unmute <name>", lambda r: r.get("Reason") == "muted", 6),
]
FIRED = sum(1 for r in ROWS if r["Uses"] > 0)
DEAD = DATA.get("DeadTokensPerSession", 0)
MONEY = DATA.get("MoneyPerMonth", 0)
HEAD = f"skillreaper · {DATA.get('WindowDays', 30)} days · {DATA.get('Sessions', 0)} sessions"
SUB = f"~{DEAD:,} dead tok/session · ~${MONEY:.2f}/month · {FIRED} of {len(ROWS)} fired"


def tok(group):
    return sum(r["Tokens"] for r in group["items"])


def put(w, y, x, s, a=0):
    h, wd = w.getmaxyx()
    if 0 <= y < h and 0 <= x < wd:
        try:
            w.addnstr(y, x, s, max(0, wd - x - 1), a)
        except curses.error:
            pass


def setup():
    locale.setlocale(locale.LC_ALL, "")
    curses.use_default_colors()
    curses.curs_set(0)
    for i, c in enumerate([curses.COLOR_RED, curses.COLOR_YELLOW, curses.COLOR_MAGENTA, curses.COLOR_CYAN, curses.COLOR_GREEN, curses.COLOR_BLUE], 1):
        curses.init_pair(i, c, -1)
    curses.init_pair(7, curses.COLOR_BLACK, curses.COLOR_CYAN)
    curses.init_pair(8, curses.COLOR_WHITE, -1)


C = lambda n: curses.color_pair(n)
DIM = lambda: curses.A_DIM
HL = lambda: C(7) | curses.A_BOLD


def row_line(r, width):
    name = r["Name"][: max(10, width - 30)]
    return f"{name:<{max(10, width - 30)}} {r['Tokens']:>5} tok  {r['Uses']:>3} uses"


def window(i, h, n):
    return max(0, min(i - h // 2, n - h)) if n > h else 0


def confirm_demo(w, y, cmd):
    """Asks, never runs. The answer only changes the message."""
    put(w, y, 2, " " * 200)
    put(w, y, 2, f"{cmd}?  [y/N] ", curses.A_BOLD)
    w.refresh()
    k = w.getch()
    return f"demo: would run {cmd}" if k in (ord("y"), ord("Y")) else "demo: cancelled"


def command_for(group, names):
    verb = group["cmd"].split(" <")[0].split(" or ")[0]
    return f"{verb} {' '.join(names)}".strip() if names else verb


# 1 ─ Menu: groups first, one level down for the items.
def v1(w):
    gi, ii, inside, sel, msg = 0, 0, False, set(), "demo: nothing here touches your files"
    while True:
        w.erase()
        h, wd = w.getmaxyx()
        put(w, 1, 2, HEAD, curses.A_BOLD)
        put(w, 2, 2, SUB, DIM())
        if not inside:
            for n, gr in enumerate(GROUPS):
                a = HL() if n == gi else 0
                put(w, 4 + n, 2, f"{'>' if n == gi else ' '} {gr['name']:<32}{'~' + format(tok(gr), ',') + ' tok':>12}{len(gr['items']):>6} items", a)
            put(w, 4 + len(GROUPS), 2, f"  {'Pruned':<32}{'':>12}{len(PRUNED):>6} items")
            foot = f"Same as: {GROUPS[gi]['cmd']}"
            keys = "↑↓ move · enter open · q quit"
        else:
            gr = GROUPS[gi]
            put(w, 4, 2, f"{gr['name']} · {len(gr['items'])} items · ~{tok(gr):,} tok", C(gr["color"]) | curses.A_BOLD)
            view = h - 11
            top = window(ii, view, len(gr["items"]))
            for n, r in enumerate(gr["items"][top:top + view]):
                idx = top + n
                mark = "[x]" if r["Name"] in sel else "[ ]"
                put(w, 6 + n, 2, f"{mark} {row_line(r, wd - 10)}", HL() if idx == ii else 0)
            if gr["items"]:
                put(w, h - 5, 2, WHY.get(gr["items"][ii].get("Reason"), ""), DIM())
            foot = "Same as: " + command_for(gr, sorted(sel))
            keys = "↑↓ move · space select · enter do it · esc back · q quit"
        put(w, h - 4, 2, "─" * (wd - 5), DIM())
        put(w, h - 3, 2, foot, C(4))
        put(w, h - 2, 2, keys + "   " + msg, DIM())
        w.refresh()
        k = w.getch()
        items = GROUPS[gi]["items"]
        if k == ord("q"):
            return
        if k == curses.KEY_UP:
            if inside:
                ii = max(0, ii - 1)
            else:
                gi = max(0, gi - 1)
        elif k == curses.KEY_DOWN:
            if inside:
                ii = min(len(items) - 1, ii + 1)
            else:
                gi = min(len(GROUPS) - 1, gi + 1)
        elif k in (27, curses.KEY_LEFT) and inside:
            inside, sel = False, set()
        elif k == ord(" ") and inside and items:
            sel ^= {items[ii]["Name"]}
        elif k in (10, 13, curses.KEY_RIGHT):
            if not inside:
                inside, ii = True, 0
            else:
                msg = confirm_demo(w, h - 3, command_for(GROUPS[gi], sorted(sel)))


# 2 ─ Split: everything in one tree, details always visible on the right.
def v2(w):
    i, opened, sel, msg = 0, {0}, set(), "demo: nothing is run"
    while True:
        flat = []
        for gn, gr in enumerate(GROUPS):
            flat.append(("g", gn, gr))
            if gn in opened:
                flat += [("i", gn, r) for r in gr["items"]]
        i = min(i, len(flat) - 1)
        w.erase()
        h, wd = w.getmaxyx()
        left = min(46, wd // 2)
        put(w, 0, 1, f" {HEAD} ", curses.A_BOLD)
        put(w, 0, left + 2, SUB[:wd - left - 3], DIM())
        put(w, 1, 0, "─" * (wd - 1), DIM())
        view = h - 4
        top = window(i, view, len(flat))
        for n, (kind, gn, o) in enumerate(flat[top:top + view]):
            a = HL() if top + n == i else 0
            if kind == "g":
                put(w, 2 + n, 1, f"{'▾' if gn in opened else '▸'} {o['name'][:left - 16]:<{left - 16}}{'~' + format(tok(o), ','):>9}", a or C(o["color"]) | curses.A_BOLD)
            else:
                mark = "●" if o["Name"] in sel else " "
                put(w, 2 + n, 1, f"  {mark} {o['Name'][:left - 13]:<{left - 13}}{o['Tokens']:>6}", a)
        for y in range(2, h - 2):
            put(w, y, left + 1, "│", DIM())
        kind, gn, o = flat[i]
        x = left + 3
        rw = wd - x - 2
        if kind == "g":
            put(w, 3, x, o["name"], C(o["color"]) | curses.A_BOLD)
            put(w, 5, x, f"{len(o['items'])} items · ~{tok(o):,} tok/session")
            put(w, 7, x, "Heaviest", DIM())
            for n, r in enumerate(o["items"][:8]):
                bar = "█" * max(1, r["Tokens"] * 16 // max(1, o["items"][0]["Tokens"]))
                put(w, 8 + n, x, f"{bar:<17}{r['Name'][:rw - 18]}", C(o["color"]))
            put(w, h - 4, x, "Same as: " + o["cmd"], C(4))
        else:
            put(w, 3, x, o["Name"], curses.A_BOLD)
            put(w, 4, x, f"{o['Category']} · {o['Source'][:rw - 10]}", DIM())
            put(w, 6, x, f"{o['Tokens']} tok/session · {o['Uses']} uses")
            put(w, 8, x, "Why", DIM())
            put(w, 9, x, WHY.get(o.get("Reason"), o.get("Reason") or "")[:rw])
            put(w, 11, x, "What it does", DIM())
            desc = (o.get("Description") or "no description")
            for n, line in enumerate(textwrap.wrap(desc, rw)[:6]):
                put(w, 12 + n, x, line)
            put(w, h - 4, x, "Same as: " + command_for(GROUPS[gn], sorted(sel) or [o["Name"]]), C(4))
        put(w, h - 2, 1, "↑↓ move · ←→ close/open · space select · enter do it · q quit   " + msg, DIM())
        w.refresh()
        k = w.getch()
        if k == ord("q"):
            return
        if k == curses.KEY_UP:
            i = max(0, i - 1)
        elif k == curses.KEY_DOWN:
            i = min(len(flat) - 1, i + 1)
        elif k == curses.KEY_RIGHT and kind == "g":
            opened.add(gn)
        elif k == curses.KEY_LEFT:
            opened.discard(gn)
            i = next(n for n, f in enumerate(flat) if f[0] == "g" and f[1] == gn)
        elif k == ord(" ") and kind == "i":
            sel ^= {o["Name"]}
        elif k in (10, 13):
            if kind == "g":
                opened ^= {gn}
            else:
                msg = confirm_demo(w, h - 2, command_for(GROUPS[gn], sorted(sel) or [o["Name"]]))


# 3 ─ Dashboard: every group at once, in coloured panels.
def v3(w):
    focus, pos, sel, msg = 0, [0] * len(GROUPS), set(), "demo: nothing is run"
    while True:
        w.erase()
        h, wd = w.getmaxyx()
        put(w, 0, 1, "skillreaper", C(2) | curses.A_BOLD)
        put(w, 0, 14, f"~{DEAD:,} dead tok/session", curses.A_BOLD)
        put(w, 0, 40, f"~${MONEY:.2f}/month", C(2) | curses.A_BOLD)
        put(w, 0, 56, f"{FIRED}/{len(ROWS)} fired · {DATA.get('Sessions', 0)} sessions", DIM())
        cols = 3 if wd >= 110 else 2
        rows = (len(GROUPS) + cols - 1) // cols
        pw, ph = (wd - 1) // cols, max(5, (h - 3) // rows)
        for n, gr in enumerate(GROUPS):
            y, x = 1 + (n // cols) * ph, (n % cols) * pw
            col = C(gr["color"]) | (curses.A_BOLD if n == focus else curses.A_DIM)
            put(w, y, x, "┌" + "─" * (pw - 3) + "┐", col)
            for b in range(1, ph - 1):
                put(w, y + b, x, "│", col)
                put(w, y + b, x + pw - 2, "│", col)
            put(w, y + ph - 1, x, "└" + "─" * (pw - 3) + "┘", col)
            put(w, y, x + 2, f" {gr['name']} ", col)
            put(w, y + 1, x + 2, f"{len(gr['items'])} item{'s' if len(gr['items']) != 1 else ''} · ~{tok(gr):,} tok", DIM())
            view = ph - 4
            top = window(pos[n], view, len(gr["items"]))
            for m, r in enumerate(gr["items"][top:top + view]):
                idx = top + m
                bar = "▇" * max(1, r["Tokens"] * 6 // max(1, gr["items"][0]["Tokens"] or 1))
                name = ("*" if r["Name"] in sel else "") + r["Name"]
                line = f"{bar:<7}{name[:pw - 18]:<{pw - 18}}{r['Tokens']:>5}"
                put(w, y + 2 + m, x + 2, line, HL() if n == focus and idx == pos[n] else C(gr["color"]))
        cur = GROUPS[focus]
        put(w, h - 1, 1, f"tab panel · ↑↓ move · space select · enter do it · q quit · same as: {command_for(cur, sorted(sel))}   {msg}"[:wd - 2], DIM())
        w.refresh()
        k = w.getch()
        if k == ord("q"):
            return
        if k in (9, curses.KEY_RIGHT):
            focus = (focus + 1) % len(GROUPS)
        elif k in (curses.KEY_BTAB, curses.KEY_LEFT):
            focus = (focus - 1) % len(GROUPS)
        elif k == curses.KEY_UP:
            pos[focus] = max(0, pos[focus] - 1)
        elif k == curses.KEY_DOWN:
            pos[focus] = min(len(cur["items"]) - 1, pos[focus] + 1)
        elif k == ord(" ") and cur["items"]:
            sel ^= {cur["items"][pos[focus]]["Name"]}
        elif k in (10, 13):
            msg = confirm_demo(w, h - 1, command_for(cur, sorted(sel)))


# 4 ─ Table: one long table with tabs, sort and search, like htop.
def v4(w):
    tabs = ["All"] + [gr["name"].split(" you")[0].replace("Your items", "Yours").replace("Heavy skills", "Heavy") for gr in GROUPS]
    tab, i, sort, query, typing, sel, msg = 0, 0, 0, "", False, set(), "demo: nothing is run"
    keys = [("Tokens", lambda r: -r["Tokens"]), ("Uses", lambda r: -r["Uses"]), ("Name", lambda r: r["Name"])]
    while True:
        items = ROWS if tab == 0 else GROUPS[tab - 1]["items"]
        items = sorted([r for r in items if query.lower() in r["Name"].lower()], key=keys[sort][1])
        i = max(0, min(i, len(items) - 1))
        w.erase()
        h, wd = w.getmaxyx()
        put(w, 0, 1, HEAD + " · " + SUB, curses.A_BOLD)
        x = 1
        for n, t in enumerate(tabs):
            put(w, 2, x, f" {t} ", HL() if n == tab else DIM())
            x += len(t) + 3
        nw = max(20, wd - 40)
        put(w, 4, 1, f"   {'NAME':<{nw}}{'TOK':>6}{'USES':>6}  VERDICT", curses.A_UNDERLINE)
        view = h - 8
        top = window(i, view, len(items))
        colors = {"REAP": 1, "MUTE": 2, "REVIEW": 4, "KEEP": 5}
        for n, r in enumerate(items[top:top + view]):
            mark = "x" if r["Name"] in sel else " "
            line = f" {mark} {r['Name'][:nw - 1]:<{nw}}{r['Tokens']:>6}{r['Uses']:>6}  {r['Verdict']} {r.get('Reason') or ''}"
            put(w, 5 + n, 1, line, HL() if top + n == i else C(colors.get(r["Verdict"], 8)))
        put(w, h - 2, 1, f"/{query}{'_' if typing else ''}" if typing or query else f"{len(items)} items · sorted by {keys[sort][0]}", C(4))
        put(w, h - 1, 1, f"←→ tab · ↑↓ move · / search · s sort · space select · d prune · m mute · q quit   {msg}"[:wd - 2], DIM())
        w.refresh()
        k = w.getch()
        if typing:
            if k in (10, 13, 27):
                typing = False
            elif k in (curses.KEY_BACKSPACE, 127, 8):
                query = query[:-1]
            elif 32 <= k < 127:
                query += chr(k)
            continue
        if k == ord("q"):
            return
        if k == curses.KEY_LEFT:
            tab, i = (tab - 1) % len(tabs), 0
        elif k == curses.KEY_RIGHT:
            tab, i = (tab + 1) % len(tabs), 0
        elif k == curses.KEY_UP:
            i -= 1
        elif k == curses.KEY_DOWN:
            i += 1
        elif k == ord("/"):
            typing = True
        elif k == ord("s"):
            sort = (sort + 1) % len(keys)
        elif k == ord(" ") and items:
            sel ^= {items[i]["Name"]}
        elif k in (ord("d"), ord("m")) and items:
            verb = "reap prune" if k == ord("d") else "reap mute"
            msg = confirm_demo(w, h - 2, f"{verb} {' '.join(sorted(sel) or [items[i]['Name']])}")


# 5 ─ Guided: one decision at a time, heaviest first.
def v5(w):
    queue = [(gr, r) for gr in GROUPS[:4] for r in gr["items"]]
    queue.sort(key=lambda t: -t[1]["Tokens"])
    i, choice = 0, {}
    while True:
        w.erase()
        h, wd = w.getmaxyx()
        saved = sum(q[1]["Tokens"] for n, q in enumerate(queue) if choice.get(n) in ("prune", "mute"))
        if i >= len(queue):
            put(w, 2, 4, "That is every item.", curses.A_BOLD)
            done(w, queue, choice, saved)
            return
        gr, r = queue[i]
        pw = min(wd - 8, 70)
        done_bar = (i * pw) // max(1, len(queue))
        put(w, 1, 4, f"{i + 1} of {len(queue)}", DIM())
        put(w, 1, 4 + pw - 26, f"saved so far ~{saved:,} tok", C(5))
        put(w, 2, 4, "█" * done_bar + "░" * (pw - done_bar), C(gr["color"]))
        put(w, 5, 6, r["Name"], curses.A_BOLD)
        put(w, 6, 6, f"{r['Category']} · {r['Source'][:50]}", DIM())
        desc = r.get("Description") or "no description"
        tw = pw - 4
        for n, line in enumerate(textwrap.wrap(desc, tw)[:4]):
            put(w, 8 + n, 6, line)
        put(w, 13, 6, f"{r['Tokens']} tok every session", C(2) | curses.A_BOLD)
        put(w, 14, 6, f"{r['Uses']} uses in {DATA.get('WindowDays', 30)} days")
        put(w, 15, 6, WHY.get(r.get("Reason"), ""), C(gr["color"]))
        can_prune = r["Removable"]
        opts = ([("p", "prune it")] if can_prune else []) + [("m", "mute it"), ("k", "keep it"), ("n", "decide later")]
        x = 6
        for key, label in opts:
            put(w, 18, x, f" {key} ", HL())
            put(w, 18, x + 4, label)
            x += len(label) + 8
        if not can_prune:
            put(w, 20, 6, "Inside a plugin: it cannot be pruned, only muted or disabled via /plugin.", DIM())
        put(w, h - 2, 4, "← back · f finish · q quit · demo: nothing is run", DIM())
        w.refresh()
        k = w.getch()
        if k == ord("q"):
            return
        if k == curses.KEY_LEFT:
            i = max(0, i - 1)
        elif k == ord("f"):
            w.erase()
            done(w, queue, choice, saved)
            return
        elif k == ord("p") and can_prune:
            choice[i], i = "prune", i + 1
        elif k == ord("m"):
            choice[i], i = "mute", i + 1
        elif k == ord("k"):
            choice[i], i = "keep", i + 1
        elif k == ord("n"):
            i += 1


def done(w, queue, choice, saved):
    picks = [(queue[n][1]["Name"], c) for n, c in sorted(choice.items()) if c != "keep"]
    put(w, 4, 4, f"You chose {len(picks)} changes · ~{saved:,} tok/session", curses.A_BOLD)
    for n, (name, c) in enumerate(picks[:12]):
        put(w, 6 + n, 6, f"demo: would run reap {c} {name}", C(4))
    put(w, 20, 4, "Press any key to close.", DIM())
    w.refresh()
    w.getch()


VERSIONS = {"1": v1, "2": v2, "3": v3, "4": v4, "5": v5}


def main(w, fn):
    setup()
    w.keypad(True)
    fn(w)


if __name__ == "__main__":
    os.environ.setdefault("ESCDELAY", "25")
    curses.wrapper(main, VERSIONS[sys.argv[1]])
