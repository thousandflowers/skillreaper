#!/usr/bin/env python3
"""Proposal v8: the receipt's feeling on the real list. Space clears, the number falls. Read-only."""
import ctypes, curses, json, os, sys, textwrap
from reap_proto import C, DATA, DEAD, ROWS, DIM, HL, put, window
from reap_proto2 import HEALTH, PRICE, grad, rc, setup
from reap_proto3 import RAMP, can, dead, groups, key, strike
from reap_proto7 import R, banner, details, fmt, last_label, money, possible, suggestion

GLYPH = {"0": ("█▀█", "█ █", "▀▀▀"), "1": ("▄█ ", " █ ", "▀▀▀"), "2": ("▀▀█", "█▀▀", "▀▀▀"), "3": ("▀▀█", " ▀█", "▀▀▀"), "4": ("█ █", "▀▀█", "  ▀"),
         "5": ("█▀▀", "▀▀█", "▀▀▀"), "6": ("█▀▀", "█▀█", "▀▀▀"), "7": ("▀▀█", "  █", "  ▀"), "8": ("█▀█", "█▀█", "▀▀▀"), "9": ("█▀█", "▀▀█", "▀▀▀"),
         ",": ("  ", "  ", "▄ "), ".": ("  ", "  ", "▄ "), "k": ("█ ▄", "██ ", "▀ ▀"), "€": ("▄▀▀", "█▀ ", " ▀▀")}
RUN = list(reversed(HEALTH))


def big(w, y, x, s, ramp_at):
    cx = x
    for ch in s:
        g = GLYPH.get(ch, ("   ",) * 3)
        for row in range(3):
            put(w, y + row, cx, g[row], rc(RUN, ramp_at) | curses.A_BOLD)
        cx += len(g[0]) + 1
    return cx


# reap never lists itself: a cleaner must not be able to clean itself away.
ROWS[:] = [r for r in ROWS if not r["Name"].startswith("skillreaper") and "skillreaper" not in r["Source"]]
PLAN = json.load(open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "plan.json"))).get("billingType") or ""
def _shift_probe():
    """macOS reports the keyboard's modifiers to any process, with no permission. Terminal hides Shift+up/down otherwise."""
    try:
        q = ctypes.CDLL("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices")
        q.CGEventSourceFlagsState.restype = ctypes.c_uint64
        q.CGEventSourceFlagsState.argtypes = [ctypes.c_int]
        return lambda: bool(q.CGEventSourceFlagsState(0) & 0x20000)
    except (OSError, AttributeError):
        return lambda: False


SHIFT_HELD = _shift_probe()
PLATFORMS = ["all"] + sorted({r.get("Platform") or "?" for r in ROWS})
PER_MONTH = DATA["SessionsPerMonth"] if "SessionsPerMonth" in DATA else DATA.get("Sessions", 0)


def short(tok):
    return f"{tok / 1e6:.1f}M".replace(".", ",") if tok >= 1e6 else f"{tok / 1e3:.1f}k".replace(".", ",") if tok >= 1e3 else str(int(tok))


LETTER = {"prune": "p", "mute": "m", "lock": "l", "delete": "d", "disable": "O", "unmute": " "}


def letter(r, plan):
    """The box says the state, with the letter of the key that sets it."""
    p = plan.get(key(r))
    if p:
        return LETTER[p]
    return "m" if r.get("Reason") == "muted" else " "


def group_box(items, plan):
    pool = [r for r in items if dead(r)] or items
    marks = {letter(r, plan) for r in pool}
    marks = marks or {" "}
    if len(marks) == 1:
        return f"[{marks.pop()}]"
    return "[-]" if marks != {" "} else "[ ]"


def plugin_of(items):
    """The plugin behind a group, or None when the group is not one plugin."""
    ids = {r["Source"][7:] for r in items if r["Source"].startswith("plugin:")}
    return next(iter(ids)) if len(ids) == 1 and all(r["Source"].startswith("plugin:") for r in items) else None


def best(r):
    return "prune" if can(r, "prune") else "mute" if can(r, "mute") else None


def run(w):
    mode, i, anchor, marking, opened, plan, msg, peek = 0, 0, None, False, set(), {}, "", False
    shown = 0.0
    sweep = {}
    unit, plat = ("tokens" if "subscription" in PLAN else "money"), 0
    paint, rowmap = None, {}
    painted = set()

    def unit_fmt(tok):
        return money(tok) if unit == "money" else f"~{tok:,} tok".replace(",", ".")

    def paint_row(idx, on):
        f = flat[idx]
        items = [f[2]] if f[0] == "i" else [r for r in f[3] if dead(r)]
        for r in items:
            if not best(r):
                continue
            if on:
                if key(r) not in plan:
                    plan[key(r)] = best(r)
                    sweep[key(r)] = 0
            else:
                plan.pop(key(r), None)

    def toggle(items, single):
        nonlocal msg
        doable = [r for r in items if best(r)]
        stuck = len(items) - len(doable)
        if not doable:
            msg = "nothing to select here: only Claude Code can disable these, via /plugin" if items else "nothing unused here to select"
            return
        if all(key(r) in plan for r in doable):
            for r in doable:
                plan.pop(key(r), None)
                sweep.pop(key(r), None)
            msg = f"{len(doable)} back"
            return
        for r in doable:
            plan[key(r)] = best(r)
            sweep[key(r)] = 0
        muted = sum(1 for r in doable if best(r) == "mute")
        msg = f"{len(doable)} pruned"
        if stuck:
            msg += f" · {stuck} only via /plugin"

    def act(items, ch):
        nonlocal msg
        verb = {"p": "prune", "m": "mute", "l": "lock", "d": "delete", "u": None, "n": None}[ch]
        n = 0
        for r in items:
            if verb is None:
                plan.pop(key(r), None)
                n += 1
            elif can(r, {"prune": "prune"}.get(verb, verb)):
                plan[key(r)] = verb
                sweep[key(r)] = 0
                n += 1
        msg = f"{n} {verb or 'cleared'}"

    focus = None
    while True:
        visible = lambda r: plat == 0 or (r.get("Platform") or "?") == PLATFORMS[plat]
        gs = [(n, sorted([r for r in its if visible(r)], key=lambda r: (not best(r), -r["Tokens"]))) for n, its in groups(0 if mode == 0 else 2)]
        gs = [g for g in gs if g[1]]
        flat = []
        for gn, (name, its) in enumerate(gs):
            if mode == 0:
                flat.append(("g", gn, name, its))
            if mode == 1 or gn in opened:
                flat += [("i", gn, r, its) for r in its]
        if focus is not None:
            i = next((x for x, f in enumerate(flat) if f[0] == "i" and f[2] is focus), i)
            focus = None
        i = max(0, min(i, len(flat) - 1))
        lo, hi = (min(anchor, i), max(anchor, i)) if anchor is not None else (i, i)
        chosen = []
        for f in flat[lo:hi + 1]:
            # A group acts on its unused items; one with none acts on everything in it.
            chosen += [f[2]] if f[0] == "i" else ([r for r in f[3] if dead(r)] or list(f[3]))
        target = sum(r["Tokens"] for r in ROWS if plan.get(key(r)) in ("prune", "mute", "disable", "delete") and dead(r) and visible(r))
        shown += (target - shown) * 0.3
        if abs(target - shown) < 2:
            shown = target
        total = sum(r["Tokens"] for r in ROWS if dead(r) and visible(r))
        left = max(0, total - shown)
        w.erase()
        h, wd = w.getmaxyx()
        frac = left / max(1, total)
        if unit == "money":
            end = big(w, 1, 3, f"{R['sym']}{left * PRICE * R['rate']:.2f}".replace(".", ","), frac)
            put(w, 1, end + 2, "wasted a month", curses.A_BOLD)
            put(w, 2, end + 2, f"~{int(left):,} tokens re-sent every session".replace(",", "."), DIM())
            if shown:
                put(w, 3, end + 2, f"you saved {money(int(shown))} a month", curses.color_pair(62) | curses.A_BOLD)
        else:
            end = big(w, 1, 3, f"{int(left):,}".replace(",", "."), frac)
            put(w, 1, end + 2, "tokens wasted every session", curses.A_BOLD)
            put(w, 2, end + 2, f"~{short(left * PER_MONTH)} in a month of sessions · what eats your plan", DIM())
            if shown:
                put(w, 3, end + 2, f"you saved ~{int(shown):,} tokens per session".replace(",", "."), curses.color_pair(62) | curses.A_BOLD)
        tabs = ("€ money", "tokens")
        tx = wd - 22
        put(w, 0, tx, f" {tabs[0]} ", HL() if unit == "money" else DIM())
        put(w, 0, tx + 10, f" {tabs[1]} ", HL() if unit == "tokens" else DIM())
        grad(w, 5, 3, wd - 7, frac, RAMP)
        hdr_n = wd - 34
        put(w, 6, 3, f"{'NAME' + ('  (' + PLATFORMS[plat] + ')' if plat else ''):<{hdr_n - 18}}", DIM())
        put(w, 6, 3 + hdr_n - 18, "USED", DIM())
        put(w, 6, 3 + hdr_n - 10, "CAN GO", DIM())
        label = "TOKENS" if unit == "tokens" else "A MONTH"
        put(w, 6, 3 + hdr_n + 26 - len(label), label, DIM())
        n = wd - 34
        view = h - 12
        rowmap = {}
        top = window(i, view, len(flat))
        for row, (kd, g2, it, its) in enumerate(flat[top:top + view]):
            y = 7 + row
            rowmap[y] = top + row
            idx = top + row
            cur = idx == i
            put(w, y, 1, ">" if cur else " ", curses.A_BOLD)
            inr = lo <= idx <= hi
            base = HL() if cur or inr or idx in painted else 0
            if kd == "g":
                doable = [r for r in its if dead(r) and best(r)] or list(its)
                t = sum(r["Tokens"] for r in doable)
                on = sum(1 for r in doable if plan.get(key(r)) in ("prune", "mute", "disable", "delete"))
                full = doable and on == len(doable)
                box = group_box(its, plan)
                label = f"{box} {'▾' if g2 in opened else '▸'} {it[:n - 26]}"
                pad = f"{label:<{n - 18}}"
                try:
                    w.addstr(y, 3, pad[:3], base or (curses.color_pair(62) if on else DIM() if box not in ("[ ]",) else curses.A_BOLD) | curses.A_BOLD if on else base or (DIM() if box != "[ ]" else curses.A_BOLD))
                    w.addstr(y, 6, strike(pad[3:]) if full else pad[3:], base or (DIM() if full else curses.A_BOLD))
                except curses.error:
                    pass
                share = sum(1 for r in its if r["Uses"] > 0) / len(its)
                grad(w, y, 3 + n - 18, 3, max(share, 0.03), HEALTH)
                put(w, y, 3 + n - 15, f"{share * 100:>3.0f}%", base or DIM())
                put(w, y, 3 + n - 10, f"{str(on) + '/' if on else ''}{len(doable)}", base or DIM())
                put(w, y, 3 + n, f"{unit_fmt(t) if t else '':>26}", base or (curses.color_pair(62) if on else curses.A_BOLD))
            else:
                k = key(it)
                p = plan.get(k)
                gone = p in ("prune", "mute", "disable", "delete")
                mark = letter(it, plan)
                put(w, y, 3, f"  [{mark}]", base or (C(1) | curses.A_BOLD if p == "delete" else curses.color_pair(62) if p else DIM() if mark != " " else 0))
                name = f" {it['Name'][:n - 26]:<{n - 24}}"
                uses = it["Uses"]
                put(w, y, 3 + n - 18, f"{uses} use{'' if uses == 1 else 's'}"[:8], base or (0 if uses else DIM()))
                if gone:
                    sweep[k] = min(len(name), sweep.get(k, len(name)) + 7)
                    shownname = strike(name[:sweep[k]]) + name[sweep[k]:]
                else:
                    shownname = name
                try:
                    w.addstr(y, 8, shownname, base or (DIM() if gone or not best(it) else 0))
                except curses.error:
                    pass
                tag = (suggestion(it) if peek else ("⚠ delete" if p == "delete" else "")) or ""
                put(w, y, 3 + n - 10, f"{tag:<9}", base or (C(1) | curses.A_BOLD if p == "delete" and not peek else curses.color_pair(61) if peek or p == "mute" else curses.color_pair(62)))
                cost = unit_fmt(it["Tokens"]) if possible(it) and it["Tokens"] else ""
                put(w, y, 3 + n, f"{cost:>26}", base or (curses.color_pair(62) if gone else DIM()))
        cur_row = flat[i]
        if anchor is not None and not marking:
            hint = f"SELECTING {hi - lo + 1} rows · shift+↑↓ to extend · space prunes them · esc cancels"
        elif marking:
            hint = f"SELECTING {hi - lo + 1} rows · arrows to extend · space prunes them · esc cancels"
        elif peek:
            hint = "reap suggests, while you hold s"
        elif cur_row[0] == "g":
            n_do = sum(1 for r in cur_row[3] if dead(r) and best(r))
            hint = f"space prunes all {n_do} unused items in {cur_row[2]}" if n_do else f"space prunes {cur_row[2]}: {len(cur_row[3])} of yours, in use or kept"
            if plugin_of(cur_row[3]):
                hint += " · o turns the whole plugin off"
        else:
            hint = f"space prunes {cur_row[2]['Name']} · d deletes it for good"
        put(w, h - 4, 3, hint[: wd - 6], curses.A_BOLD | (curses.color_pair(61) if marking or peek else 0))
        put(w, h - 3, 3, msg[: wd - 28], curses.color_pair(62))
        count = sum(1 for r in ROWS if plan.get(key(r)) in ("prune", "mute", "disable", "delete", "lock", "unmute"))
        if count:
            label = f" a apply {count} change{'s' if count != 1 else ''} "
            put(w, h - 3, wd - len(label) - 4, label, HL())
        put(w, h - 2, 3, "space prune · n nothing · m mute · l lock · d delete · o plugin off · shift+↑↓ many · tab €/tok · enter open · s reap · q"[: wd - 6], DIM())
        w.refresh()
        moving = shown != target or any(f[0] == "i" and plan.get(key(f[2])) in ("prune", "mute", "disable", "delete") and sweep.get(key(f[2]), 999) < 40 for f in flat[top:top + view])
        w.timeout(25 if moving else (550 if peek else -1))
        k = w.getch()
        w.timeout(-1)
        if k == -1:
            if not moving:
                peek = False
            continue
        if k == ord("s"):
            peek = True
            continue
        peek = False
        if k == ord("q"):
            return
        if k == curses.KEY_MOUSE:
            try:
                _, mx, my, _, bs = curses.getmouse()
            except curses.error:
                continue
            idx = rowmap.get(my)
            if bs & curses.BUTTON1_RELEASED:
                paint = None
                painted.clear()
            elif idx is not None:
                if bs & (curses.BUTTON1_PRESSED | curses.BUTTON1_CLICKED):
                    i = idx
                    f = flat[idx]
                    items = [f[2]] if f[0] == "i" else [r for r in f[3] if dead(r) and best(r)]
                    paint = not all(key(r) in plan for r in items if best(r))
                    paint_row(idx, paint)
                    painted.add(idx)
                    if bs & curses.BUTTON1_CLICKED:
                        paint = None
                        painted.clear()
                elif paint is not None:
                    i = idx
                    paint_row(idx, paint)
                    painted.add(idx)
            continue
        kd, gn, o, its = flat[i]
        if k == ord("o") and kd == "g":
            plug = plugin_of(its)
            if not plug:
                msg = f"{o} is not a plugin"
                continue
            if all(plan.get(key(r)) == "disable" for r in its):
                for r in its:
                    plan.pop(key(r), None)
                msg = f"{o} stays enabled"
                continue
            used = [r for r in its if r["Uses"] > 0 or r["Verdict"] == "KEEP"]
            if used:
                names = ", ".join(r["Name"].split(":")[-1] for r in used[:3])
                put(w, h - 3, 3, " " * (wd - 6))
                put(w, h - 3, 3, f"this also turns off {len(used)} you use or keep ({names}). Disable {o}? [y/N] "[: wd - 6], curses.color_pair(61) | curses.A_BOLD)
                w.refresh()
                if w.getch() not in (ord("y"), ord("Y")):
                    msg = "cancelled"
                    continue
            for r in its:
                plan[key(r)] = "disable"
            msg = f"{o}: whole plugin will be disabled"
            continue
        if k == 9:
            unit = "tokens" if unit == "money" else "money"
            continue
        if k == ord("f"):
            plat, i, anchor, marking, opened = (plat + 1) % len(PLATFORMS), 0, None, False, set()
            shown = sum(r["Tokens"] for r in ROWS if plan.get(key(r)) in ("prune", "mute", "disable", "delete") and dead(r) and (plat == 0 or (r.get("Platform") or "?") == PLATFORMS[plat]))
            msg = "showing " + PLATFORMS[plat]
            continue
        if k in (curses.KEY_SR, 337, ord("K"), curses.KEY_SLEFT):
            anchor = i if anchor is None else anchor
            i -= 1
        elif k in (curses.KEY_SF, 336, ord("J"), curses.KEY_SRIGHT):
            anchor = i if anchor is None else anchor
            i += 1
        elif k == ord("v"):
            marking = not marking
            anchor = i if marking else None
        elif k in (curses.KEY_UP, curses.KEY_DOWN):
            if SHIFT_HELD():
                anchor = i if anchor is None else anchor
            elif not marking:
                anchor = None
            i += -1 if k == curses.KEY_UP else 1
        elif k == curses.KEY_RIGHT and kd == "g":
            opened.add(gn)
        elif k == curses.KEY_LEFT:
            opened.discard(gn)
            i, anchor, marking = next((x for x, f in enumerate(flat) if f[0] == "g" and f[1] == gn), i), None, False
        elif k == ord(" "):
            toggle(chosen, len(chosen) == 1)
            anchor, marking = None, False
        elif k in (10, 13):
            if kd == "g":
                opened ^= {gn}
            else:
                def on_end(current):
                    names = [g[1] for g in gs]
                    at = next((x for x, g in enumerate(names) if g is current), -1)
                    following = names[at + 1] if 0 <= at < len(names) - 1 else None
                    return section_end(w, gs[at][0] if at >= 0 else "", current, gs[at + 1][0] if following else None, following, plan, unit_fmt)

                def on_start(current):
                    names = [g[1] for g in gs]
                    at = next((x for x, g in enumerate(names) if g is current), -1)
                    earlier = names[at - 1] if at > 0 else None
                    return section_end(w, gs[at][0] if at >= 0 else "", current, gs[at - 1][0] if earlier else None, earlier, plan, unit_fmt, True)

                stopped = details(w, o, plan, act, its, on_end, on_start, lambda: receipt(w, plan, unit),
                                  lambda: sum(1 for r in ROWS if plan.get(key(r)) in ("prune", "mute", "disable", "delete", "lock", "unmute")))
                opened |= {x for x, g in enumerate(gs) if any(r is stopped for r in g[1])}
                focus = stopped
        elif k in (ord("p"), ord("m"), ord("l"), ord("u"), ord("n"), ord("d")):
            if k == ord("n"):
                # Clearing reaches everything in the rows, also what a group action skipped.
                chosen = [r for f in flat[lo:hi + 1] for r in ([f[2]] if f[0] == "i" else f[3])]
            act(chosen, chr(k))
            if k == ord("d"):
                curses.beep()
            anchor, marking = None, False
        elif k == ord("g"):
            mode, i, anchor, marking = 1 - mode, 0, None, False
        elif k == ord("a"):
            msg = receipt(w, plan, unit)
        elif k == 27:
            anchor, marking = None, False


def section_end(w, name, items, next_name, following, plan, unit_fmt, going_up=False):
    """Between two lists: what you just did, what comes next. Full screen, one key."""
    done = [r for r in items if plan.get(key(r)) in ("prune", "mute", "disable", "delete")]
    tok = sum(r["Tokens"] for r in done)
    w.erase()
    h, wd = w.getmaxyx()
    y = max(2, h // 2 - 8)
    put(w, y, 3, name if going_up else f"{name} · done", DIM())
    put(w, y + 1, 3, f"{len(done)} of {len(items)} cleared · {unit_fmt(tok)} saved" if done else f"nothing cleared out of {len(items)}", curses.color_pair(62) | curses.A_BOLD)
    grad(w, y + 3, 3, wd - 7, len(done) / max(1, len(items)), HEALTH)
    if following:
        d = [r for r in following if dead(r) and best(r)]
        put(w, y + 6, 3, "previous" if going_up else "next", DIM())
        title = next_name
        while len(banner(title)[0]) > wd - 8:
            title = title[:-1]
        for row, text_row in enumerate(banner(title)):
            put(w, y + 7 + row, 3, text_row, curses.color_pair(62) | curses.A_BOLD)
        put(w, y + 11, 3, f"{len(following)} items · {len(d)} you could clear · {unit_fmt(sum(r['Tokens'] for r in d))}", curses.A_BOLD)
        put(w, h - 2, 3, ("↑ or enter go back to it" if going_up else "↓ or enter continue") + " · esc stay here", DIM())
    else:
        put(w, y + 6, 3, "This is the first list." if going_up else "That was the last list.", curses.A_BOLD)
        put(w, h - 2, 3, "esc back, then a to finish", DIM())
    w.refresh()
    while True:
        k = w.getch()
        if k in ((curses.KEY_UP if going_up else curses.KEY_DOWN), 10, 13) and following:
            return following
        if k in (27, curses.KEY_UP, curses.KEY_DOWN, curses.KEY_LEFT, ord("q")) or not following:
            return None


def receipt(w, plan, unit):
    picks = [(r, plan[key(r)]) for r in ROWS if plan.get(key(r)) in ("prune", "mute", "disable", "delete")]
    tok = sum(r["Tokens"] for r, _ in picks)
    plugins = sorted({r["Source"][7:] for r, c in picks if c == "disable"})
    singles = [t for t in picks if t[1] != "disable"]
    w.erase()
    h, wd = w.getmaxyx()
    pw = min(60, wd - 6)
    x = (wd - pw) // 2
    put(w, 1, x, f"{'SKILLREAPER':^{pw}}", curses.A_BOLD)
    put(w, 2, x, "┄" * pw, DIM())
    for n, plug in enumerate(plugins[:4]):
        put(w, 3 + n, x, f"claude plugin disable {plug}"[:pw], curses.color_pair(61) | curses.A_BOLD)
    for n, (r, a) in enumerate(sorted(singles, key=lambda t: -t[0]["Tokens"])[: max(0, h - 16 - len(plugins))]):
        put(w, 3 + len(plugins) + n, x, f"{a:<7} {r['Name'][:pw - 28]:<{pw - 28}}{fmt(r['Tokens']):>20}"[:pw], DIM())
    y = h - 11
    put(w, y, x, "┄" * pw, DIM())
    gone = sum(1 for _, c in picks if c == "delete")
    if gone:
        curses.beep()
        warn = f" ⚠ {gone} deleted for good: they cannot be restored "
        put(w, y - 1, x, f"{warn:<{pw}}", rc(HEALTH, 0, True) | curses.A_BOLD)
    put(w, y + 1, x, f"{len(picks)} items · ~{tok:,} tokens lighter every session".replace(",", "."), curses.A_BOLD)
    end = big(w, y + 3, x, money(tok).replace("<", "") if unit == "money" else f"{tok:,}".replace(",", "."), 1.0)
    put(w, y + 4, end + 2, "saved every month" if unit == "money" else "tokens saved every session", curses.color_pair(62) | curses.A_BOLD)
    put(w, y + 7, x, "Do it?  [y/N]", curses.A_BOLD)
    w.refresh()
    return "demo: would apply" if w.getch() in (ord("y"), ord("Y")) else "demo: cancelled, still here"


def main(w):
    setup()
    curses.init_pair(60, 174, -1)
    curses.init_pair(61, 180, -1)
    curses.init_pair(62, 114, -1)
    w.keypad(True)
    curses.mousemask(curses.ALL_MOUSE_EVENTS | curses.REPORT_MOUSE_POSITION)
    curses.mouseinterval(0)
    sys.stdout.write("\033[?1002h")
    sys.stdout.flush()
    try:
        run(w)
    finally:
        sys.stdout.write("\033[?1002l")
        sys.stdout.flush()


if __name__ == "__main__":
    os.environ.setdefault("ESCDELAY", "25")
    curses.wrapper(main)
