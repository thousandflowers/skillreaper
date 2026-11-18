#!/usr/bin/env python3
"""Proposal v7: the receipt's feeling on the real list. Space clears, the number falls. Read-only."""
import curses, os, sys, textwrap
from reap_proto import DATA, DEAD, ROWS, DIM, HL, put, window
from reap_proto2 import HEALTH, PRICE, grad, rc, setup
from reap_proto3 import RAMP, can, dead, groups, key, strike
from reap_proto7 import R, details, fmt, last_label, money, possible, suggestion

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


PLATFORMS = ["all"] + sorted({r.get("Platform") or "?" for r in ROWS})
PER_MONTH = DATA["SessionsPerMonth"] if "SessionsPerMonth" in DATA else DATA.get("Sessions", 0)


def short(tok):
    return f"{tok / 1e6:.1f}M".replace(".", ",") if tok >= 1e6 else f"{tok / 1e3:.1f}k".replace(".", ",") if tok >= 1e3 else str(int(tok))


def best(r):
    return "remove" if can(r, "prune") else "mute" if can(r, "mute") else None


def run(w):
    mode, i, anchor, marking, opened, plan, msg, peek = 0, 0, None, False, set(), {}, "", False
    shown = 0.0
    sweep = {}
    unit, plat = "money", 0
    paint, rowmap = None, {}

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
            msg = "only Claude Code can disable this: /plugin"
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
        msg = f"{len(doable) - muted} removed · {muted} muted (inside plugins)" if muted and not single else "inside a plugin, so muted" if muted else f"{len(doable)} removed"
        if stuck:
            msg += f" · {stuck} only via /plugin"

    def act(items, ch):
        nonlocal msg
        verb = {"p": "remove", "m": "mute", "k": "keep", "u": None}[ch]
        n = 0
        for r in items:
            if verb is None:
                plan.pop(key(r), None)
                n += 1
            elif can(r, {"remove": "prune"}.get(verb, verb)):
                plan[key(r)] = verb
                sweep[key(r)] = 0
                n += 1
        msg = f"{n} {verb or 'cleared'}"

    while True:
        visible = lambda r: plat == 0 or (r.get("Platform") or "?") == PLATFORMS[plat]
        gs = [(n, sorted([r for r in its if visible(r)], key=lambda r: -r["Tokens"])) for n, its in groups(0 if mode == 0 else 2)]
        gs = [g for g in gs if g[1]]
        flat = []
        for gn, (name, its) in enumerate(gs):
            if mode == 0:
                flat.append(("g", gn, name, its))
            if mode == 1 or gn in opened:
                flat += [("i", gn, r, its) for r in its]
        i = max(0, min(i, len(flat) - 1))
        lo, hi = (min(anchor, i), max(anchor, i)) if anchor is not None else (i, i)
        chosen = []
        for f in flat[lo:hi + 1]:
            chosen += [f[2]] if f[0] == "i" else [r for r in f[3] if dead(r)]
        target = sum(r["Tokens"] for r in ROWS if plan.get(key(r)) in ("remove", "mute") and dead(r) and visible(r))
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
        if len(PLATFORMS) > 2:
            put(w, 6, 3, "showing: " + PLATFORMS[plat], DIM())
        n = wd - 34
        view = h - 12
        rowmap = {}
        top = window(i, view, len(flat))
        for row, (kd, g2, it, its) in enumerate(flat[top:top + view]):
            y = 7 + row
            rowmap[y] = top + row
            idx = top + row
            cur = idx == i
            inr = lo <= idx <= hi
            base = HL() if cur else (curses.A_REVERSE | curses.A_DIM if inr else 0)
            if kd == "g":
                doable = [r for r in its if dead(r) and best(r)]
                t = sum(r["Tokens"] for r in doable)
                on = sum(1 for r in doable if key(r) in plan)
                full = doable and on == len(doable)
                box = "[x]" if full else "[-]" if on else "[ ]" if doable else "   "
                label = f"{box} {'▾' if g2 in opened else '▸'} {it[:n - 26]}"
                pad = f"{label:<{n - 18}}"
                try:
                    w.addstr(y, 3, pad[:6] + (strike(pad[6:]) if full else pad[6:]), base or (DIM() if full else curses.A_BOLD))
                except curses.error:
                    pass
                grad(w, y, 3 + n - 18, 6, max(sum(1 for r in its if r["Uses"] > 0) / len(its), 0.03), HEALTH)
                put(w, y, 3 + n - 10, f"{str(on) + '/' if on else ''}{len(doable)}", base or DIM())
                put(w, y, 3 + n, f"{unit_fmt(t) if t else '':>26}", base or (curses.color_pair(62) if on else curses.A_BOLD))
            else:
                k = key(it)
                p = plan.get(k)
                gone = p in ("remove", "mute")
                box = "[x]" if gone else "[ ]" if best(it) else "   "
                put(w, y, 3, f"  {box}", base or (curses.color_pair(62) if gone else 0))
                name = f" {it['Name'][:n - 21]:<{n - 18}}"
                if gone:
                    sweep[k] = min(len(name), sweep.get(k, len(name)) + 7)
                    shownname = strike(name[:sweep[k]]) + name[sweep[k]:]
                else:
                    shownname = name
                try:
                    w.addstr(y, 8, shownname, base or (DIM() if gone else 0))
                except curses.error:
                    pass
                tag = (suggestion(it) if peek else (p if p in ("mute", "keep") else "")) or ""
                put(w, y, 3 + n - 10, f"{tag:<9}", base or (curses.color_pair(61) if peek or p == "mute" else curses.color_pair(62)))
                cost = unit_fmt(it["Tokens"]) if possible(it) and it["Tokens"] else ""
                put(w, y, 3 + n, f"{cost:>26}", base or (curses.color_pair(62) if gone else DIM()))
        cur_row = flat[i]
        if marking:
            hint = f"SELECTING {hi - lo + 1} rows · arrows to extend · space clears them · esc cancels"
        elif peek:
            hint = "reap suggests, while you hold s"
        elif cur_row[0] == "g":
            n_do = sum(1 for r in cur_row[3] if dead(r) and best(r))
            hint = f"space clears all {n_do} unused items in {cur_row[2]}" if n_do else f"nothing in {cur_row[2]} can be cleared here"
        else:
            hint = f"space clears {cur_row[2]['Name']}" if best(cur_row[2]) else f"{cur_row[2]['Name']}: only via /plugin"
        put(w, h - 4, 3, hint[: wd - 6], curses.A_BOLD | (curses.color_pair(61) if marking or peek else 0))
        put(w, h - 3, 3, msg[: wd - 6], curses.color_pair(62))
        put(w, h - 2, 3, "space clear · click a box, or drag · v select many · tab € or tokens · f tool · enter open · s reap · a done · q"[: wd - 6], DIM())
        w.refresh()
        moving = shown != target or any(sweep.get(key(r), 999) < 40 for r in ROWS if plan.get(key(r)) in ("remove", "mute"))
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
            elif idx is not None:
                if bs & (curses.BUTTON1_PRESSED | curses.BUTTON1_CLICKED):
                    i = idx
                    f = flat[idx]
                    items = [f[2]] if f[0] == "i" else [r for r in f[3] if dead(r) and best(r)]
                    paint = not all(key(r) in plan for r in items if best(r))
                    paint_row(idx, paint)
                    if bs & curses.BUTTON1_CLICKED:
                        paint = None
                elif paint is not None:
                    i = idx
                    paint_row(idx, paint)
            continue
        kd, gn, o, its = flat[i]
        if k == 9:
            unit = "tokens" if unit == "money" else "money"
            continue
        if k == ord("f"):
            plat, i, anchor, marking, opened = (plat + 1) % len(PLATFORMS), 0, None, False, set()
            shown = sum(r["Tokens"] for r in ROWS if plan.get(key(r)) in ("remove", "mute") and dead(r) and (plat == 0 or (r.get("Platform") or "?") == PLATFORMS[plat]))
            msg = "showing " + PLATFORMS[plat]
            continue
        if k in (curses.KEY_SR, 337, ord("K")):
            anchor = i if anchor is None else anchor
            i -= 1
        elif k in (curses.KEY_SF, 336, ord("J")):
            anchor = i if anchor is None else anchor
            i += 1
        elif k == ord("v"):
            marking = not marking
            anchor = i if marking else None
        elif k == curses.KEY_UP:
            anchor = anchor if marking else None
            i -= 1
        elif k == curses.KEY_DOWN:
            anchor = anchor if marking else None
            i += 1
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
                details(w, o, plan, act)
        elif k in (ord("p"), ord("m"), ord("k"), ord("u")):
            act(chosen, chr(k))
            anchor, marking = None, False
        elif k == ord("g"):
            mode, i, anchor, marking = 1 - mode, 0, None, False
        elif k == ord("a"):
            msg = receipt(w, plan)
        elif k == 27:
            anchor, marking = None, False


def receipt(w, plan):
    picks = [(r, plan[key(r)]) for r in ROWS if plan.get(key(r)) in ("remove", "mute")]
    tok = sum(r["Tokens"] for r, _ in picks)
    w.erase()
    h, wd = w.getmaxyx()
    pw = min(60, wd - 6)
    x = (wd - pw) // 2
    put(w, 1, x, f"{'SKILLREAPER':^{pw}}", curses.A_BOLD)
    put(w, 2, x, "┄" * pw, DIM())
    for n, (r, a) in enumerate(sorted(picks, key=lambda t: -t[0]["Tokens"])[: h - 16]):
        put(w, 3 + n, x, f"{a:<7} {r['Name'][:pw - 28]:<{pw - 28}}{fmt(r['Tokens']):>20}"[:pw], DIM())
    y = h - 11
    put(w, y, x, "┄" * pw, DIM())
    put(w, y + 1, x, f"{len(picks)} items · ~{tok:,} tokens lighter every session".replace(",", "."), curses.A_BOLD)
    big(w, y + 3, x, money(tok).replace("<", ""), 1.0)
    put(w, y + 4, x + 22, "saved every month", curses.color_pair(62) | curses.A_BOLD)
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
