#!/usr/bin/env python3
"""Proposal v5: you choose, reap stays quiet until asked. Read-only: nothing is ever run."""
import curses, json, os, textwrap
from reap_proto import C, DATA, DEAD, ROWS, WHY, DIM, HL, put, window
from reap_proto2 import EXTRA, HEALTH, PRICE, grad, rc, setup, spark
from reap_proto3 import RAMP, can, dead, groups, key, last, text

R = json.load(open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "rate.json")))
SHIFT_UP = {curses.KEY_SR, 337, ord("K")}
SHIFT_DOWN = {curses.KEY_SF, 336, ord("J")}
PEEK = ord("s")
PEEK_MS = 550  # longer than the keyboard's repeat delay, or a held key flickers


FONT = {"a": "█▀█|█▀█|▀ ▀", "b": "█▀▄|█▀▄|▀▀ ", "c": "█▀▀|█  |▀▀▀", "d": "█▀▄|█ █|▀▀ ", "e": "█▀▀|█▀▀|▀▀▀", "f": "█▀▀|█▀▀|▀  ", "g": "█▀▀|█ █|▀▀▀",
        "h": "█ █|█▀█|▀ ▀", "i": "█|█|▀", "j": "  █|█ █|▀▀▀", "k": "█ █|█▀▄|▀ ▀", "l": "█  |█  |▀▀▀", "m": "█▀▄▀█|█ ▀ █|▀   ▀", "n": "█▄ █|█ ▀█|▀  ▀", "o": "█▀█|█ █|▀▀▀",
        "p": "█▀█|█▀▀|▀  ", "q": "█▀█|█ █|▀▀█", "r": "█▀█|█▀▄|▀ ▀", "s": "█▀▀|▀▀█|▀▀▀", "t": "▀█▀| █ | ▀ ", "u": "█ █|█ █|▀▀▀", "v": "█ █|█ █| ▀ ", "w": "█   █|█ █ █|▀▀▀▀▀",
        "x": "█ █|▄▀▄|▀ ▀", "y": "█ █|▀█▀| ▀ ", "z": "▀▀█|▄▀ |▀▀▀", "0": "█▀█|█ █|▀▀▀", "1": "▄█ | █ |▀▀▀", "2": "▀▀█|█▀▀|▀▀▀", "3": "▀▀█| ▀█|▀▀▀", "4": "█ █|▀▀█|  ▀",
        "5": "█▀▀|▀▀█|▀▀▀", "6": "█▀▀|█▀█|▀▀▀", "7": "▀▀█|  █|  ▀", "8": "█▀█|█▀█|▀▀▀", "9": "█▀█|▀▀█|▀▀▀", "-": "  |▀▀|  ", "_": "  |  |▀▀", ".": " | |▀"}


def banner(word):
    rows = ["", "", ""]
    for ch in word.lower():
        g = FONT.get(ch, "   |   |   ").split("|")
        for n in range(3):
            rows[n] += g[n] + " "
    return rows


def banner_lines(name, width):
    """The name in big letters, wrapped at its hyphens. None when even that does not fit."""
    lines, cur = [], ""
    for part in name.replace("-", "-\n").split("\n"):
        if cur and len(banner(cur + part)[0]) > width:
            lines.append(cur)
            cur = ""
        cur += part
    lines.append(cur)
    if len(lines) > 3 or any(len(banner(l)[0]) > width for l in lines):
        return None
    return lines


def money(tok):
    v = tok * PRICE * R["rate"]
    if tok <= 0:
        return "-"
    if v < 0.01:
        return f"<{R['sym']}0,01"
    return f"{R['sym']}{v:.2f}".replace(".", ",")


def fmt(tok):
    return f"~{tok:,} tok {money(tok)}".replace(",", ".", 1) if tok >= 1000 else f"~{tok} tok {money(tok)}"


def suggestion(r):
    if r.get("Reason") == "muted":
        return "muted"
    if r["Verdict"] == "REAP" and can(r, "prune"):
        return "prune"
    if r["Verdict"] in ("REAP", "MUTE") and can(r, "mute"):
        return "mute"
    if r["Verdict"] == "REAP":
        return "/plugin"
    return {"KEEP": "keep", "REVIEW": "not sure"}.get(r["Verdict"], "-")


last_label = last


def possible(r):
    return can(r, "prune") or can(r, "mute")


EXPLAIN = {"nothing": "leave it as it is", "lock": "locked: reap never suggests touching it", "mute": "keeps it, strips its description, reap unmute brings it back",
           "prune": "moved to quarantine, reap restore brings it back", "delete": "⚠ gone for good, it cannot be restored", "disable": "goes with its whole plugin: claude plugin disable"}
CHOICE_KEY = {"nothing": "u", "lock": "l", "mute": "m", "prune": "p", "delete": "d"}


def details(w, r, plan, act, siblings=None, on_end=None, on_start=None, on_apply=None, pending=None):
    """Same page as the list: big figure on top, bar under it, hint and keys at the bottom."""
    siblings = siblings or [r]
    at = next((n for n, x in enumerate(siblings) if x is r), 0)
    notice = ""
    focus_on, focus = None, 0
    while True:
        r = siblings[at]
        w.erase()
        h, wd = w.getmaxyx()
        # Every row is fixed: scrolling fast changes the words, nothing moves.
        short = r["Name"].split(":")[-1]
        weight = min(1, r["Tokens"] / 300)
        chosen = plan.get(key(r))
        selectable = can(r, "prune") or can(r, "mute")
        fit = short
        while len(banner(fit)[0]) > wd - 8:
            fit = fit[:-1]
        for row, text_row in enumerate(banner(fit)):
            # Colour is the weight. The fate is the shape: struck through, or faded.
            if chosen in ("prune", "delete", "disable") and row == 1:
                text_row = "━" * len(text_row.rstrip())
            put(w, 1 + row, 3, text_row, rc(RAMP, weight) | (curses.A_DIM if chosen == "mute" else curses.A_BOLD))
        put(w, 0, wd - 12, f"{at + 1} of {len(siblings)}", DIM())
        put(w, 4, 3, f"{r['Name'] if fit != short else short} · {r['Category']} · {r['Source'].split('@')[0]}"[: wd - 6], DIM())
        was_muted = r.get("Reason") == "muted"
        effective = "nothing" if chosen == "unmute" else chosen or ("mute" if was_muted else "nothing")
        options = [("nothing", True), ("lock", True), ("mute", r["Category"] in ("skill", "agent")), ("prune", True), ("delete", True)]
        labels = [o for o, _ in options]
        if focus_on is not r:
            focus_on, focus = r, labels.index(effective) if effective in labels else 0
        blocked = not options[focus][1]
        bx = 3
        for n_opt, (label, allowed) in enumerate(options):
            on = effective == label
            if label == "delete":
                put(w, 5, bx, "  ⚠ delete  ", rc(HEALTH, 0, True) | curses.A_BOLD if on else C(1) | curses.A_BOLD)
                bx += 2
            else:
                grey = curses.A_REVERSE | curses.A_DIM if n_opt == focus else DIM()
                put(w, 5, bx, f"  {label}  ", HL() if on else (0 if allowed else grey))
            bx += len(label) + 5
        if blocked:
            put(w, 6, 3, "mute is not available: an MCP server has no description to strip. Use prune or delete.", curses.color_pair(61) | curses.A_BOLD)
        elif chosen == "unmute":
            put(w, 6, 3, "brings its description back", DIM())
        else:
            put(w, 6, 3, EXPLAIN[effective], C(1) | curses.A_BOLD if effective == "delete" else DIM())
        put(w, 7, 3, f"~{r['Tokens']} tokens every session", curses.A_BOLD)
        put(w, 7, 34, f"{money(r['Tokens'])} a month", DIM())
        grad(w, 8, 3, wd - 7, max(weight, 0.01), RAMP)
        put(w, 10, 3, f"reap suggests {suggestion(r)}", curses.A_BOLD)
        put(w, 11, 3, WHY.get(r.get("Reason"), r.get("Reason") or ""), DIM())
        put(w, 13, 3, f"{r['Uses']} uses in {DATA.get('WindowDays', 30)} days · last used {last(r)}", curses.A_BOLD)
        put(w, 14, 3, f"asked for by name {r.get('Requests', 0)} times · {r.get('ErrorCount', 0)} errors", DIM())
        vals = EXTRA["byday"].get(r["Name"])
        if vals:
            spark(w, 15, 3, vals, RAMP)
        for n, line in enumerate(textwrap.wrap(r.get("Description") or "no description", wd - 8)[: max(0, h - 23)]):
            put(w, 17 + n, 3, line)
        put(w, h - 5, 3, (r.get("Path") or "")[: wd - 8], DIM())
        if notice:
            put(w, h - 3, 3, notice[: wd - 6], curses.color_pair(61) | curses.A_BOLD)
        notice = ""
        put(w, h - 2, 3, "←→ choose · n nothing · l lock · m mute · p prune · d delete · ↑↓ next skill · enter or esc back"[: wd - 6], DIM())
        count = pending() if pending else 0
        if count:
            label = f" a apply {count} change{'s' if count != 1 else ''} "
            put(w, h - 3, wd - len(label) - 4, label, HL())
        w.refresh()
        k = w.getch()
        if k in (27, ord("q"), 10, 13):
            return r
        if k == ord("a") and on_apply:
            on_apply()
            continue
        if k in (curses.KEY_LEFT, curses.KEY_RIGHT):
            focus = (focus + (1 if k == curses.KEY_RIGHT else -1)) % len(options)
            pick, ok = options[focus]
            if not ok:
                continue  # grey: you can stand on it and read why, nothing changes
            if was_muted and pick == "nothing":
                plan[key(r)] = "unmute"
            elif was_muted and pick == "mute":
                plan.pop(key(r), None)
            else:
                act([r], CHOICE_KEY[pick])
            if pick == "delete":
                curses.beep()
            continue
        if k == curses.KEY_UP:
            if at == 0 and on_start:
                earlier = on_start(siblings)
                if earlier:
                    siblings, at = earlier, len(earlier) - 1
                continue
            at = max(0, at - 1)
        elif k == curses.KEY_DOWN:
            if at == len(siblings) - 1 and on_end:
                following = on_end(siblings)
                if following:
                    siblings, at = following, 0
                continue
            at = min(len(siblings) - 1, at + 1)
        if k == ord(" "):
            if not (can(r, "prune") or can(r, "mute")):
                notice = f"{r['Name']} cannot be selected: disable it via /plugin in Claude Code"
                continue
            k = ord("u") if key(r) in plan else ord("p")
        if k == ord("n"):
            if was_muted:
                plan[key(r)] = "unmute"
            else:
                act([r], "u")
            focus_on = None
            continue
        if k == ord("m") and not options[2][1]:
            focus = 2
            continue
        if k in (ord("p"), ord("m"), ord("l"), ord("u"), ord("d")):
            act([r], chr(k))
            focus_on = None
            if k == ord("d"):
                curses.beep()


def summary(w, plan):
    picks = [(r, plan[key(r)]) for r in ROWS if plan.get(key(r)) in ("prune", "mute", "delete")]
    w.erase()
    h, wd = w.getmaxyx()
    tok = sum(r["Tokens"] for r, _ in picks)
    put(w, 1, 3, f"{len(picks)} changes · {fmt(tok)} a month", curses.A_BOLD)
    y = 3
    for verb in ("prune", "mute", "delete"):
        names = [r["Name"] for r, a in picks if a == verb]
        if names:
            put(w, y, 3, f"{verb} · {len(names)}", curses.A_BOLD)
            for line in textwrap.wrap(", ".join(names), wd - 8)[:8]:
                y += 1
                put(w, y, 5, line, DIM())
            y += 2
    put(w, h - 3, 3, "Apply all of this?  [y/N]", curses.A_BOLD)
    w.refresh()
    return "demo: would apply" if w.getch() in (ord("y"), ord("Y")) else "demo: cancelled, your choices are still here"


def run(w):
    mode, i, anchor, opened, plan, msg, peek = 0, 0, None, set(), {}, "choose with p, m, k · demo: nothing is run", False

    def act(items, ch):
        nonlocal msg
        verb = {"p": "prune", "m": "mute", "l": "lock", "d": "delete", "u": None}[ch]
        ok, no = 0, 0
        for r in items:
            if verb is None:
                plan.pop(key(r), None)
                ok += 1
            elif can(r, {"prune": "prune"}.get(verb, verb)):
                plan[key(r)] = verb
                ok += 1
            else:
                no += 1
        msg = f"{ok} set to {verb}" if verb else f"{ok} cleared"
        if no:
            msg += f" · {no} cannot be {verb}d" + (", inside a plugin: try m" if verb == "prune" else ", only /plugin can")

    while True:
        gs = [(n, sorted(its, key=lambda r: -r["Tokens"])) for n, its in groups(0 if mode == 0 else 2)]
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
        saved = sum(r["Tokens"] for r in ROWS if plan.get(key(r)) in ("prune", "mute", "delete") and dead(r))
        now = max(0, DEAD - saved)
        ghost = sum(r["Tokens"] for r in chosen if possible(r) and key(r) not in plan)
        w.erase()
        h, wd = w.getmaxyx()
        put(w, 1, 2, "UNUSED CONTEXT", curses.A_BOLD)
        put(w, 1, 18, f"~{DEAD:,}" + (f" → ~{now:,}" if saved else "") + " tok/session")
        if saved:
            s = f"saving {fmt(saved)} a month"
            put(w, 1, wd - len(s) - 3, s, curses.color_pair(62) | curses.A_BOLD)
        grad(w, 2, 2, wd - 6, now / max(1, DEAD), RAMP)
        a, b = int((now - ghost) / max(1, DEAD) * (wd - 6)), int(now / max(1, DEAD) * (wd - 6))
        for c in range(max(0, a), b):
            put(w, 2, 2 + c, "▒", curses.A_REVERSE)
        n = wd - 58
        last_col = "REAP" if peek else "LAST"
        put(w, 4, 2, f"   {'NAME':<{n}}{'USED':>9}{'  ' + last_col:<11}{'YOURS':<9}{'SAVES':>22}", curses.A_UNDERLINE)
        view = h - 8
        top = window(i, view, len(flat))
        diffs = 0
        if peek:
            diffs = sum(1 for r in ROWS if plan.get(key(r)) and plan[key(r)] != suggestion(r))
        for row, (kd, g2, it, its) in enumerate(flat[top:top + view]):
            y = 5 + row
            idx = top + row
            inrange = lo <= idx <= hi
            cur = idx == i
            base = HL() if cur else (curses.A_REVERSE | curses.A_DIM if inrange else 0)
            put(w, y, 1, ">" if cur else ("+" if inrange else " "), base or 0)
            if kd == "g":
                t = sum(r["Tokens"] for r in its if dead(r) and possible(r))
                fired = sum(1 for r in its if r["Uses"] > 0)
                marked = sum(1 for r in its if key(r) in plan)
                put(w, y, 3, f"{'▾' if g2 in opened else '▸'} {it[:n - 18]:<{n - 16}}", base or curses.A_BOLD)
                put(w, y, 3 + n - 14, f"{str(marked) + ' of ' if marked else ''}{len(its)} items", base or DIM())
                grad(w, y, 3 + n + 2, 8, max(fired / len(its), 0.03), HEALTH)
                put(w, y, 3 + n + 29, f"{fmt(t):>22}", base or curses.A_BOLD)
            else:
                p = plan.get(key(it))
                out = p in ("prune", "mute", "delete")
                style = base or (DIM() if out else 0)
                text(w, y, 3, f"  {it['Name'][:n - 3]:<{n - 2}}", style, out)
                put(w, y, 3 + n, f"{it['Uses']:>7}  ", style)
                if peek:
                    s = suggestion(it)
                    differs = bool(p) and p != s
                    tint = curses.color_pair(61) if differs else curses.color_pair(60) if s in ("prune", "mute", "delete") else DIM()
                    put(w, y, 3 + n + 9, f"{s:<9}", base or tint)
                    put(w, y, 3 + n + 19, f"{p or '':<7}", base or (tint | curses.A_BOLD if differs else curses.A_BOLD))
                else:
                    put(w, y, 3 + n + 9, f"{last(it):<9}", base or DIM())
                    put(w, y, 3 + n + 19, f"{p or '':<7}", base or curses.A_BOLD)
                cost = fmt(it["Tokens"]) if possible(it) and it["Tokens"] else "-"
                put(w, y, 3 + n + 29, f"{cost:>22}", base or (curses.color_pair(62) if out else DIM()))
        if peek:
            sug = sum(1 for r in ROWS if suggestion(r) in ("prune", "mute", "delete"))
            put(w, h - 3, 2, f"reap would act on {sug} items · you differ on {diffs} · keep holding s to see", curses.color_pair(61) | curses.A_BOLD)
        else:
            head = f"{hi - lo + 1} rows · {len(chosen)} items · saves {fmt(ghost)}   " if anchor is not None else ""
            put(w, h - 3, 2, (head + msg)[: wd - 4], curses.color_pair(62))
        put(w, h - 2, 2, "↑↓ move · shift+↑↓ select many · →← open · enter card · p remove · m mute · l lock · u clear · hold s reap · a apply"[: wd - 3], DIM())
        w.refresh()
        w.timeout(PEEK_MS if peek else -1)
        k = w.getch()
        w.timeout(-1)
        if k == PEEK:
            peek = True
            continue
        if k == -1:
            peek = False
            continue
        peek = False
        if k == ord("q"):
            return
        kd, gn, o, its = flat[i]
        if k in SHIFT_UP or k in SHIFT_DOWN:
            anchor = i if anchor is None else anchor
            i += -1 if k in SHIFT_UP else 1
        elif k == curses.KEY_UP:
            anchor, i = None, i - 1
        elif k == curses.KEY_DOWN:
            anchor, i = None, i + 1
        elif k == curses.KEY_RIGHT and kd == "g":
            opened.add(gn)
        elif k == curses.KEY_LEFT:
            opened.discard(gn)
            i, anchor = next((x for x, f in enumerate(flat) if f[0] == "g" and f[1] == gn), i), None
        elif k in (10, 13):
            if kd == "g":
                opened ^= {gn}
            else:
                details(w, o, plan, act)
        elif k in (ord("p"), ord("m"), ord("l"), ord("u"), ord("d")):
            act(chosen, chr(k))
            anchor = None
        elif k == ord("g"):
            mode, i, anchor = 1 - mode, 0, None
        elif k == ord("a"):
            msg = summary(w, plan)
        elif k == 27:
            anchor = None
        elif k != curses.KEY_RESIZE:
            msg = f"key {k} ({curses.keyname(k).decode(errors='replace')}) does nothing here"


def main(w):
    setup()
    curses.init_pair(60, 174, -1)
    curses.init_pair(61, 180, -1)
    curses.init_pair(62, 114, -1)
    w.keypad(True)
    run(w)


if __name__ == "__main__":
    os.environ.setdefault("ESCDELAY", "25")
    curses.wrapper(main)
