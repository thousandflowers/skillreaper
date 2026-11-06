#!/usr/bin/env python3
"""Proposal v8: the receipt's feeling on the real list. Space clears, the number falls. Read-only."""
import collections, curses, os, re, sys, time
from reap_proto import C, DATA, DEAD, ROWS, DIM, HL, cut_mid, load_json, pad, put, tint, too_small, visible, window
from reap_proto2 import HEALTH, PRICE, grad, rc, setup
from reap_proto3 import RAMP, can, dead, groups, is_plugin, key, read_only, strike, why_not
from reap_proto7 import EXISTING_UNDO, FONT, PER_MONTH, R, already_locked, amount, banner, big_money_words, caveats, details, existing, fit_title, fmt, last_label, legend, money, money_column, money_figure, number, plural, possible, reach, sessions_of, suggestion, suggestion_word, tokens, worth
from menu_logic import is_echo, plan_summary, act_on, plugins_in, removed_for_others, update_warning, BY_KEY, DONE_WORD, LETTER, LIGHTER, MODE_NAMES, NAME_COUNTS, OWN_PLUGIN, OWN_SKILLS, PAST, PLAN, PLUGIN_ID, STRUCK, SUGGEST_GROUPS, SUGGEST_PLAN, UNDECIDED, auto, best, click_choice, consume_plan, cursor_hint, display_name, group_all, group_box, group_phrase, group_stats, grouped, in_use, is_own, muted_count, peek_phrase, plugin_of, plugin_rows, plugin_target, pointer_top, row_items, saving, selection_hint, shared_folders, short, short_count, state_box, suggested_groups, wasted, weigh, Plan
from menu_receipt import GLYPH, RUN, big, big_glyph, big_width, draw_apply_label, using_warning, receipt, receipt_line, receipt_rows, section_end, yes_or_no
from terminal import ENTER, NON_LATIN, S_HELD, SHIFT_ENTER, SHIFT_HELD, apply_key, enter, is_apply, is_light, key_gap, last_mouse, launch, leave, read_key, without_mouse

LIST_MIN = (14, 58)  # rows, columns: two rows of list and an opened group's name column
AMOUNT_COLUMN = 14  # the amount's room on a narrow window; a wide one keeps more air
WIDE_SLACK = 12
SELECT_KEYS = (curses.KEY_SR, curses.KEY_SF, ord("K"), ord("J"))
EASE = 0.3  # the share of the way the big number moves each frame
SNAP = 2  # this close, it lands
WHEEL_ROWS = 3
ARROW_COLUMNS = (6, 7, 8)  # around a group's triangle: a click here opens it
TINTS = {60: (174, 124, curses.COLOR_RED), 61: (180, 130, curses.COLOR_YELLOW), 62: (114, 28, curses.COLOR_GREEN)}  # on dark, on light, basic
GERUND = {"prune": "pruning", "delete": "deleting", "mute": "muting", "disable": "turning off"}
LATIN_HINT = "keys are read as Latin letters: switch keyboard layout"


PLATFORMS = [None] + sorted({r.get("Platform") or "" for r in ROWS})  # None is the filter's own "all", whatever the data calls itself


def platform_label(p):
    return "all" if p is None else "unknown" if not p else f'"{p}"' if p == "all" else p


USES_WIDTH = 8


def uses_field(n):
    """The word goes before the figure does."""
    words = f"{short_count(n)} use{'' if n == 1 else 's'}"
    return f"{words if len(words) <= USES_WIDTH else short_count(n):<{USES_WIDTH}}"


def box_colour(letter_box, active):
    """One colour per letter, on a group as on a row: delete is red everywhere."""
    if letter_box == "[d]":
        return C(1) | curses.A_BOLD
    return curses.color_pair(62) if active else 0


def tints():
    for pair_n, (dark, light, basic) in TINTS.items():
        tint(pair_n, light if is_light() else dark, basic)


def reinvest_step(w, picks, unit):
    sys.modules.setdefault("reap_proto10", sys.modules[__name__])  # run as a script, this module is __main__: not a second copy
    from reinvest_views import picker  # here, because reinvest_views imports this module
    picker(w, "weight", [{**r, "Tokens": saving(r, c), "Verb": c} for r, c in picks if saving(r, c) > 0], unit)


def row_tag(r, choice, refused=None):
    """The small word on a row: what is about to happen if that cannot be undone, else what is true of it."""
    if refused:
        return f"can't {refused}"
    if choice == "delete":
        return "⚠ delete"
    return {"unmute": "undo", "unlock": "undo", "restore": "undo"}.get(choice) or ("read-only" if read_only(r) else "mute lost" if r.get("MuteLost") else "prune lost" if r.get("PruneLost") else "muted" if r.get("Reason") == "muted" else "locked" if already_locked(r) else "")


AMBER_TAGS = ("can't mute", "can't prune", "can't delete", "muted", "locked", "mute lost", "prune lost", "undo", "pruned")
PAGE_KEYS = {curses.KEY_PPAGE: lambda i, view, _n: i - view, curses.KEY_NPAGE: lambda i, view, _n: i + view,
             curses.KEY_HOME: lambda *_: 0, curses.KEY_END: lambda _i, _view, n: n - 1}
LIST_Y = 7  # first row of the list


def run(w):
    mode, i, anchor, marking, opened, msg, peek = 0, 0, None, False, set(), "", False
    plan = Plan()
    applied = {}  # applied in this session, in memory only: out of the list, their saving still counted
    shown = 0.0
    sweep = {}
    unit, plat = ("tokens" if "subscription" in PLAN or not PRICE else "money"), 0
    notes = caveats()
    paint, rowmap, last_painted, held_top, repeat = None, {}, None, None, None
    painted = set()
    before_off = {}
    cached = {}
    focus = None

    def unit_fmt(tok, priced=None):
        if unit != "money":
            return f"~{tokens(tok)} tok"
        text = money(tok if priced is None else priced)  # each row rounds on its own: they add up only about
        return text if text.startswith("<") else f"~{text}"

    def forget_paint():
        nonlocal paint, last_painted
        paint, last_painted = None, None
        painted.clear()

    def paint_row(idx, on):
        for r in group_all(flat[idx]):
            choice = click_choice(r)
            if not choice or key(r) in applied:
                continue
            if on and plan.get(key(r)) != choice:
                plan[key(r)] = choice
                sweep[key(r)] = 0
            elif not on and plan.get(key(r)) == choice:
                plan.pop(key(r), None)

    def toggle(items):
        nonlocal msg
        plan.refused = {}
        doable = [r for r in items if best(r)]
        stuck = len(items) - len(doable)
        if not doable:
            if items and all(r.get("Reason") in ("muted", "pruned") for r in items):
                msg = "reap has nothing to change here: u undoes it"
            else:
                msg = f"nothing to select here: {why_not(items[0], 'prune')}" if items else "nothing unused here to select"
            return
        if all(plan.get(key(r)) == best(r) for r in doable):
            for r in doable:
                plan.pop(key(r), None)
                sweep.pop(key(r), None)
            msg = f"{len(doable)} back"
            return
        for r in doable:
            plan[key(r)] = best(r)
            sweep[key(r)] = 0
        said = collections.Counter(PAST[best(r)] for r in doable)
        msg = " · ".join(f"{n} {word}" for word, n in said.most_common())
        if stuck:
            msg += f" · {stuck} left: {why_not(next(r for r in items if not best(r)), 'prune')}"

    def act(items, ch):
        nonlocal msg
        msg = act_on(plan, sweep, applied, items, ch)

    def consume():
        nonlocal applied, anchor, marking
        applied = consume_plan(plan, applied)
        sweep.clear()
        before_off.clear()
        forget_paint()
        anchor, marking = None, False

    def apply_now():
        return receipt(w, plan, unit, consume, lambda picks: reinvest_step(w, picks, unit))

    def may_quit():
        """Choices not applied are lost with the window, so that takes an answer. y needs the question on screen."""
        if not plan:
            return True
        lost = plural(len(plan), "choice")

        def ask():
            nh, nwd = w.getmaxyx()
            put(w, nh - 3, 3, " " * max(0, nwd - 6))
            put(w, nh - 3, 3, f"Quit and lose {lost}? [y/n]", curses.color_pair(61) | curses.A_BOLD)
            w.refresh()

        ask()
        return yes_or_no(w, ask)

    def plugin_off(rows):
        """o on any row or group of one plugin: the question is about the plugin, counted from all of its rows."""
        nonlocal msg
        found = plugin_target(rows)
        if not found:
            several = plugins_in(rows)  # every plugin in a mixed group, each asked about on its own
            for source in several:
                plugin_off(plugin_rows(source))
            left = sum(1 for r in rows if r["Source"] not in several)
            off = sum(1 for source in several if all(plan.get(key(r)) == "disable" for r in plugin_rows(source)))
            msg = f"{off} of {plural(len(several), 'plugin')} will be disabled" if several else f"o cannot act here: {why_not(rows[0], 'disable')}" if rows else "o works on a plugin"
            msg += f" · {left} not in a plugin" if several and left else ""
            return msg
        source, plug = found
        members = [r for r in plugin_rows(source) if key(r) not in applied]
        if all(plan.get(key(r)) == "disable" for r in members):
            for r in members:
                was = before_off.get(source, {}).get(key(r))
                plan.pop(key(r), None)
                if was:
                    plan[key(r)] = was
            plan.take_ended()
            msg = f"{plug} stays enabled"
            return msg
        used = [r for r in members if r["Uses"] > 0 or r["Verdict"] in ("KEEP", "REVIEW") or r.get("Reason") == "muted"]
        if used:
            names = ", ".join(r["Name"].split(":")[-1] for r in used[:3])
            question = f"Disable {plug}? [y/N] all {plural(len(members), 'row')} go, {len(used)} you use, keep, muted or reap is unsure about: {names}"

            def ask():
                nh, nwd = w.getmaxyx()
                put(w, nh - 3, 3, " " * max(0, nwd - 6))
                put(w, nh - 3, 3, question[: max(0, nwd - 6)], curses.color_pair(61) | curses.A_BOLD)
                w.refresh()

            ask()
            if not yes_or_no(w, ask):
                msg = "cancelled"
                return msg
        before_off[source] = {key(r): plan.get(key(r)) for r in members}
        for r in members:
            plan[key(r)] = "disable"
        plan.take_ended()
        msg = f"{plug}: whole plugin will be disabled, {plural(len(members), 'row')}"
        return msg

    while True:
        visible_row = lambda r: plat == 0 or (r.get("Platform") or "") == PLATFORMS[plat]
        if cached.get("list") != (mode, plat, len(applied)):
            cached["list"] = (mode, plat, len(applied))
            still_open = {cached["gs"][n][0] for n in opened if n < len(cached.get("gs", ()))}  # groups renumber when one empties
            found = [(n, sorted([r for r in its if visible_row(r) and key(r) not in applied], key=lambda r: (not best(r), -r["Tokens"]))) for n, its in grouped(mode)]
            cached["gs"] = [g for g in found if g[1]]
            opened = {n for n, g in enumerate(cached["gs"]) if g[0] in still_open}
            waste = wasted(visible_row)
            cached["total"] = sum(r["Tokens"] for r in waste)
            cached["total_priced"] = sum(r["Tokens"] * reach(r) for r in waste)
            cached["total_month"] = sum(r["Tokens"] * sessions_of(r) for r in waste)
            cached["plan_version"] = None  # group numbers are remembered per list
        gs, total = cached["gs"], cached["total"]
        if cached.get("flat_key") != (mode, plat, frozenset(opened), len(applied)):
            cached["flat_key"] = (mode, plat, frozenset(opened), len(applied))
            cached["flat"] = []
            for gn, (name, its) in enumerate(gs):
                if mode < 2:
                    cached["flat"].append(("g", gn, name, its))
                if mode == 2 or gn in opened:
                    cached["flat"] += [("i", gn, r, its) for r in its]
        flat = cached["flat"]
        if cached.get("plan_version") != (plan.version, plat):
            cached["plan_version"] = (plan.version, plat)
            lighter_rows = [(BY_KEY[k], saving(BY_KEY[k], v)) for k, v in {**applied, **plan}.items() if k in BY_KEY and visible_row(BY_KEY[k])]
            cached["target"], cached["target_priced"] = weigh(lighter_rows)
            cached["target_month"] = sum(t * sessions_of(r) for r, t in lighter_rows)
            cached["stats"] = {}
        target = cached["target"]

        def stats_of(its):
            if id(its) not in cached["stats"]:
                cached["stats"][id(its)] = group_stats(its, plan)
            return cached["stats"][id(its)]


        if too_small(w, LIST_MIN, "q quits" if not plan else "q quits, once the window is large enough to ask about your choices"):
            if read_key(w) == ord("q") and not plan:
                return
            continue
        if plan.take_ended():
            msg = f"{msg} · plugin off cancelled: it takes the whole plugin".lstrip(" ·")
        if not flat:
            w.erase()
            h, wd = w.getmaxyx()
            put(w, 1, 3, f"nothing to review for {platform_label(PLATFORMS[plat])}" if plat else "nothing to review", curses.A_BOLD)
            put(w, 3, 3, msg, curses.color_pair(62) | curses.A_BOLD)  # what just emptied the list
            put(w, h - 2, 3, "f platform · g group by · q", DIM())
            w.refresh()
            k = read_key(w)
            if k == ord("q"):
                return
            if k == ord("f"):
                plat, i, anchor, marking, opened = (plat + 1) % len(PLATFORMS), 0, None, False, set()
            elif k == ord("g"):
                mode, i, anchor, marking, opened = (mode + 1) % 3, 0, None, False, set()
            continue

        if focus is not None:
            i = next((x for x, f in enumerate(flat) if f[0] == "i" and f[2] is focus), i)
            focus = None
        i = max(0, min(i, len(flat) - 1))
        lo, hi = (min(anchor, i), max(anchor, i)) if anchor is not None else (i, i)
        chosen = lambda: list({key(r): r for f in flat[lo:hi + 1] for r in row_items(f)}.values())
        step = shown + (target - shown) * EASE
        shown = target if abs(target - step) < SNAP or step == shown else step
        left = max(0, total - shown)
        eased = shown / target if target else 0  # the money and the month fall with the number
        saved_priced = cached["target_priced"] * eased
        left_month = max(0, cached["total_month"] - cached["target_month"] * eased)
        w.erase()
        h, wd = w.getmaxyx()
        frac = left / max(1, total)
        if not DATA.get("Sessions"):
            put(w, 1, 3, "no sessions read: nothing is measured yet", curses.A_BOLD)
            put(w, 2, 3, "the verdicts below have no evidence behind them", DIM())
        elif unit == "money":
            value = max(0, cached["total_priced"] - saved_priced) * PRICE * R["rate"]
            above, words = big_money_words("wasted a month")
            text = money_figure(value)
            figure = text if big_width(text) <= wd - 5 - len(words) else f"{R['sym']}{short(value)}"
            put(w, 0, 3, above, DIM())
            end = big(w, 1, 3, figure, frac)
            put(w, 1, end + 2, words, curses.A_BOLD)
            put(w, 2, end + 2, f"~{tokens(int(left))} tokens re-sent every session", DIM())
            if shown:
                put(w, 3, end + 2, f"you saved {worth(money(saved_priced))} a month", curses.color_pair(62) | curses.A_BOLD)
        else:
            words = "tokens wasted every session"
            figure = tokens(int(left)) if big_width(tokens(int(left))) <= wd - 5 - len(words) else short(left)
            end = big(w, 1, 3, figure, frac)
            put(w, 1, end + 2, words, curses.A_BOLD)
            if cached["total_month"]:
                put(w, 2, end + 2, f"~{short(left_month)} in a month of sessions" + (" · what eats your plan" if "subscription" in PLAN else ""), DIM())
            if shown:
                put(w, 3, end + 2, f"you saved ~{tokens(int(shown))} tokens per session", curses.color_pair(62) | curses.A_BOLD)
        tabs = (f" {R['sym']} money ", " tokens ")
        token_x = wd - 4 - len(tabs[1])
        put(w, 0, token_x - 1 - len(tabs[0]), tabs[0], HL() if unit == "money" else DIM())
        put(w, 0, token_x, tabs[1], HL() if unit == "tokens" else DIM())
        if notes:
            put(w, 4, 3, f"⚠ {notes[0]}"[: wd - 18] + (f" · +{len(notes) - 1} more" if len(notes) > 1 else ""), curses.color_pair(61))
        grad(w, 5, 3, wd - 7, frac, RAMP)
        n = wd - 8 - AMOUNT_COLUMN - min(WIDE_SLACK, max(0, wd - 88))
        amount_w = wd - 5 - (3 + n)
        put(w, 6, 3, pad(f"NAME · {MODE_NAMES[mode]}" + (f"  ({platform_label(PLATFORMS[plat])})" if plat else ""), n - 18), DIM())
        label = "TOKENS" if unit == "tokens" else money_column()
        put(w, 6, wd - 5 - len(label), label, DIM())
        view = h - 12
        rowmap = {}
        if held_top is not None and not held_top <= i < held_top + view:
            held_top = None
        top = max(0, min(held_top, len(flat) - view)) if held_top is not None else window(i, view, len(flat))
        animating = False
        for row, (kd, g2, it, its) in enumerate(flat[top:top + view]):
            y = LIST_Y + row
            rowmap[y] = top + row
            idx = top + row
            cur = idx == i
            put(w, y, 1, ">" if cur else " ", curses.A_BOLD)
            inr = lo <= idx <= hi
            base = HL() if cur or inr or idx in painted else 0
            if kd == "g":
                st = stats_of(its)
                on = bool(st["marked"])
                box, n_muted = st["box"], st["muted"]
                muted_note = f" · {n_muted} muted" if n_muted and n_muted != len(its) else ""
                delete_note = f" · ⚠ {st['deletes']} delete" if st["deletes"] else ""
                arrow = "▾" if g2 in opened else "▸"
                field = pad(f"{box} {arrow} {cut_mid(it, n - 24)}{muted_note}{delete_note}", n - 18)
                put(w, y, 3, field[:3], base or box_colour(box, on) or (DIM() if box != "[ ]" else curses.A_BOLD))
                put(w, y, 6, strike(field[3:]) if st["full"] else field[3:], base or (C(1) | curses.A_BOLD if st["deletes"] else DIM() if st["faded"] else curses.A_BOLD))
                phrase = peek_phrase(its) if peek else group_phrase(its, st)
                phrase_w = max(18, len(phrase))
                put(w, y, 3 + n - phrase_w, pad(phrase, phrase_w), base or (curses.color_pair(62) if on else DIM()))
                t, priced = (st["saved"], st["saved_priced"]) if on else (st["total"], st["total_priced"])
                put(w, y, 3 + n, f"{unit_fmt(t, priced) if t else '':>{amount_w}}", base or (curses.color_pair(62) if on else curses.A_BOLD))
            else:
                k = key(it)
                p = plan.get(k)
                gone = p in LIGHTER
                mark = state_box(it, plan)
                put(w, y, 3, f"  {mark}", base or (box_colour(mark, bool(p))))
                name = " " + pad(cut_mid(display_name(it), n - 26), n - 24)
                uses = it["Uses"]
                put(w, y, 3 + n - 18, uses_field(uses), base or (0 if uses else DIM()))
                if p in STRUCK:
                    sweep[k] = min(len(name), sweep.get(k, len(name)) + 7)
                    animating = animating or sweep[k] < len(name)
                    shownname = strike(name[:sweep[k]]) + name[sweep[k]:]
                else:
                    shownname = name
                put(w, y, 8, shownname, base or (DIM() if gone or not best(it) else 0))
                tag = suggestion_word(it) if peek else row_tag(it, p, plan.refused.get(key(it)))
                put(w, y, 3 + n - 10, f"{tag:<10}", base or (C(1) | curses.A_BOLD if p == "delete" and not peek else curses.color_pair(61) if peek or p == "mute" or tag in AMBER_TAGS else DIM() if tag == "read-only" else curses.color_pair(62)))
                weight = saving(it, p) if gone else it["Tokens"]
                cost = "?" if it.get("WeightUnknown") else unit_fmt(weight, weight * reach(it)) if it["Tokens"] else ""
                put(w, y, 3 + n, f"{cost:>{amount_w}}", base or (curses.color_pair(62) if gone else DIM()))
        cur_row = flat[i]
        if anchor is not None:
            hint = selection_hint(chosen(), hi - lo + 1, "arrows" if marking else "shift+↑↓", unit_fmt)
        elif peek:
            hint = "reap suggests, while you hold s"
        else:
            hint = cursor_hint(cur_row, stats_of(cur_row[3]) if cur_row[0] == "g" else None, plan)
        put(w, h - 4, 3, hint[: wd - 6], curses.A_BOLD | (curses.color_pair(61) if marking or peek else 0))
        status = plan_summary(plan) if not msg or is_echo(msg) else msg
        put(w, h - 3, 3, status[: wd - 28], C(1) | curses.A_BOLD if status.startswith("⚠") else curses.color_pair(62))
        draw_apply_label(w, h, wd, len(plan), sum(1 for k in plan if k in BY_KEY and not visible_row(BY_KEY[k])))
        legend(w, h, ["enter open", "space choose", "a auto", "p prune", "m mute", "l lock", "d delete", "n nothing", "u undo", "q quit", f"tab {R['sym']}/tok", "g group by", "f platform", "shift+↑↓ many", "o plugin off", "hold s reap", "v mark"])
        w.refresh()
        moving = shown != target or animating
        k = read_key(w, 25 if moving else (550 if peek else -1))
        if k == -1:
            if not moving and not (peek and S_HELD()):  # the system can say the key is still down, where key repeat cannot be trusted
                peek = False
            continue
        if k == curses.KEY_RESIZE:
            continue
        if k == ord("s"):
            peek = True
            continue
        peek = False
        if k == ord("q"):
            if may_quit():
                return
            msg = ""
            continue
        if k == NON_LATIN:
            msg = LATIN_HINT
            continue
        if k == curses.KEY_MOUSE:
            ev = last_mouse()
            idx = rowmap.get(ev.y)
            repeat = None
            if ev.kind == "release":
                forget_paint()
            elif ev.kind in ("wheel_up", "wheel_down"):
                # The wheel moves the view, and the cursor only as far as it must to stay in it. No state changes.
                held_top = max(0, min(top + (WHEEL_ROWS if ev.kind == "wheel_down" else -WHEEL_ROWS), max(0, len(flat) - view)))
                i, anchor, marking = max(held_top, min(i, held_top + view - 1)), None, False
            elif ev.kind == "press" and idx is not None and flat[idx][0] == "g" and ev.x in ARROW_COLUMNS:
                held_top, i, anchor, marking, msg = top, idx, None, False, ""
                opened ^= {flat[idx][1]}
            elif ev.kind == "press" and idx is not None:
                # The list holds still under the pointer, so a second click lands on the row the first one changed.
                held_top, i, anchor, marking, msg = top, idx, None, False, ""
                paint = not all(plan.get(key(r)) == click_choice(r) for r in group_all(flat[idx]) if click_choice(r))
                paint_row(idx, paint)
                painted.clear()
                painted.add(idx)
                last_painted = idx
            elif ev.kind == "drag" and paint is not None and idx is not None:
                # Reports land where the pointer is: every row between two of them counts. Group headers only by a press.
                for between in range(min(last_painted, idx), max(last_painted, idx) + 1):
                    if flat[between][0] == "i":
                        paint_row(between, paint)
                        painted.add(between)
                i, last_painted = idx, idx
                held_top = pointer_top(top, ev.y, LIST_Y, view, len(flat))
            continue
        forget_paint()
        kd, gn, o, its = flat[i]
        if k == ord("o"):
            repeat = None
            if anchor is not None:
                msg = "o takes one plugin: esc ends the selection first"
                continue
            plugin_off(its if kd == "g" else [o])
            continue
        if k == 9:
            if not PRICE:
                msg = "no price known: tokens only"
                continue
            unit = "tokens" if unit == "money" else "money"
            continue
        if k == ord("f"):
            plat, i, anchor, marking, opened = (plat + 1) % len(PLATFORMS), 0, None, False, set()
            shown = sum(saving(BY_KEY[key_], v) for key_, v in {**applied, **plan}.items() if key_ in BY_KEY and (plat == 0 or (BY_KEY[key_].get("Platform") or "") == PLATFORMS[plat]))
            msg = "showing " + platform_label(PLATFORMS[plat])
            continue
        again = repeat[1] if repeat and repeat[0] == k and anchor is None else None
        was_selecting = anchor is not None or again is not None
        repeat = None
        if k in SELECT_KEYS:
            anchor = i if anchor is None else anchor
            i += -1 if k in (curses.KEY_SR, ord("K")) else 1
        elif k == ord("v"):
            marking = not marking
            anchor = i if marking else None
        elif k in PAGE_KEYS:
            if SHIFT_HELD():
                anchor = i if anchor is None else anchor
            elif not marking:
                anchor = None
            i = PAGE_KEYS[k](i, view, len(flat))
            held_top = None
        elif k in (curses.KEY_UP, curses.KEY_DOWN):
            if SHIFT_HELD():
                anchor = i if anchor is None else anchor
            elif not marking:
                anchor = None
            i += -1 if k == curses.KEY_UP else 1
        elif k == curses.KEY_RIGHT and kd == "g":
            opened.add(gn)
            anchor, marking = None, False  # the rows under a selection just changed
        elif k == curses.KEY_LEFT:
            opened.discard(gn)
            i, anchor, marking = next((x for x, f in enumerate(flat) if f[0] == "g" and f[1] == gn), i), None, False
        elif k == ord(" "):
            items = again or list({key(r): r for f in flat[lo:hi + 1] for r in group_all(f)}.values())
            toggle(items)
            anchor, marking = None, False
            repeat = (k, items) if was_selecting else None
        elif is_apply(k, SHIFT_HELD, bool(plan)):
            # Terminal sends Shift+Enter as plain Enter, same as Shift+arrows: ask the system.
            msg = apply_now()
        elif k == SHIFT_ENTER or (k in ENTER and SHIFT_HELD()):
            msg = "nothing chosen yet: choose something, then shift+enter applies it"
        elif k in ENTER:
            if kd == "g":
                opened ^= {gn}
                anchor, marking = None, False
            else:
                def next_list(current, step):
                    at = next((x for x, g in enumerate(gs) if g[1] is current), -1)
                    other = at + step
                    neighbour = gs[other] if at >= 0 and 0 <= other < len(gs) else (None, None)
                    return section_end(w, gs[at][0] if at >= 0 else "", current, neighbour[0], neighbour[1], plan, unit_fmt, step < 0, apply_now, SHIFT_HELD)

                with without_mouse():
                    stopped = details(w, o, plan, act, its, lambda cur: next_list(cur, 1), lambda cur: next_list(cur, -1), apply_now,
                                      lambda: len(plan), lambda items: auto(plan, sweep, [r for r in items if key(r) not in applied]), SHIFT_HELD,
                                      may_quit, lambda r: plugin_off([r]))
                if stopped is None:
                    return
                msg = ""  # what the card did is on the row now: its last message may no longer be true
                opened |= {x for x, g in enumerate(gs) if any(r is stopped for r in g[1])}
                if stopped is not o:
                    anchor, marking = None, False
                focus, held_top = stopped, None
        elif k in (ord("p"), ord("m"), ord("l"), ord("n"), ord("d"), ord("u")):
            # Clearing reaches everything in the rows, also what a group action skipped.
            items = [r for f in flat[lo:hi + 1] for r in group_all(f)]
            act(list({key(r): r for r in items}.values()), chr(k))
            if k == ord("d"):
                curses.beep()
            anchor, marking = None, False
        elif k == ord("g"):
            mode, i, anchor, marking, opened, held_top = (mode + 1) % 3, 0, None, False, set(), None
            msg = MODE_NAMES[mode]
        elif k == ord("a"):
            items = again or list({key(r): r for f in flat[lo:hi + 1] for r in group_all(f)}.values())
            plan.refused = {}
            msg = auto(plan, sweep, items)
            anchor, marking = None, False
            repeat = (k, items) if was_selecting else None
        elif k == 27:
            anchor, marking = None, False


def main(w):
    setup()
    tints()
    enter(w)
    try:
        run(w)
    except (KeyboardInterrupt, EOFError):
        pass
    finally:
        leave()


if __name__ == "__main__":
    os.environ.setdefault("ESCDELAY", "25")
    launch(main)
