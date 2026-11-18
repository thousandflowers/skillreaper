#!/usr/bin/env python3
"""The menu's screens outside the list: the big font, the gap between two lists, the question, the receipt."""
import curses, time
from menu_logic import DONE_WORD, LIGHTER, best, display_name, in_use, removed_for_others, saving, shared_folders, update_warning, weigh
from reap_proto import C, DIM, HL, ROWS, cut, cut_mid, pad, put, too_small
from reap_proto2 import HEALTH, grad, rc
from reap_proto3 import key
from reap_proto7 import FONT, banner, big_money_words, fit_title, fmt, money, plural, reach, tokens, worth
from terminal import NON_LATIN, apply_key, is_apply, is_light, key_gap, read_key, without_mouse


GLYPH = {"0": ("█▀█", "█ █", "▀▀▀"), "1": ("▄█ ", " █ ", "▀▀▀"), "2": ("▀▀█", "█▀▀", "▀▀▀"), "3": ("▀▀█", " ▀█", "▀▀▀"), "4": ("█ █", "▀▀█", "  ▀"),
         "5": ("█▀▀", "▀▀█", "▀▀▀"), "6": ("█▀▀", "█▀█", "▀▀▀"), "7": ("▀▀█", "  █", "  ▀"), "8": ("█▀█", "█▀█", "▀▀▀"), "9": ("█▀█", "▀▀█", "▀▀▀"),
         "<": ("▄▀ ", "▀▄ ", "   "), ",": ("  ", "  ", "█ "), ".": ("  ", "  ", "▄ "), "k": ("█ ▄", "██ ", "▀ ▀"), "€": ("▄▀▀", "█▀ ", " ▀▀"), "$": ("▄█▄", "▀█▄", "▀█▀")}


RUN = list(reversed(HEALTH))


RECEIPT_MIN = (19, 40)  # the warnings, the question and at least two rows of what it is about


SECTION_MIN = (13, 30)


BANNER_ROWS = 16  # below this the next list's name is plain text


YES_GUARD_S = 0.55  # longer than the keyboard's repeat delay: a held y must not answer a question it never saw


REPEAT_GAP_S = 0.15  # a y this close to the last key is a repeat or a paste, not an answer


def big_glyph(ch):
    if ch in GLYPH:
        return GLYPH[ch]
    if ch.lower() in FONT:
        return tuple(FONT[ch.lower()].split("|"))
    return ("  ", ch + " ", "  ")  # a currency with no big glyph still shows


def big_width(s):
    return sum(len(big_glyph(ch)[0]) + 1 for ch in s)


def big(w, y, x, s, ramp_at):
    cx = x
    for ch in s:
        g = big_glyph(ch)
        for row in range(3):
            put(w, y + row, cx, g[row], (0 if is_light() else rc(RUN, ramp_at)) | curses.A_BOLD)
        cx += len(g[0]) + 1
    return cx


def draw_apply_label(w, h, wd, count, hidden=0):
    """hidden: chosen rows the platform filter does not show, which apply all the same."""
    if count:
        label = f" {apply_key()} apply {count} change{'s' if count != 1 else ''}{f' · {hidden} not shown' if hidden else ''} "
        put(w, h - 3, wd - len(label) - 4, label, HL())


def section_end(w, name, items, next_name, following, plan, unit_fmt, going_up=False, on_apply=None, shift_held=lambda: False):
    """Between two lists: what you just did, what comes next. Full screen, one key."""
    advance = curses.KEY_UP if going_up else curses.KEY_DOWN
    while True:
        if too_small(w, SECTION_MIN):
            if read_key(w) in (27, ord("q")):
                return None
            continue
        done = [r for r in items if key(r) in plan]
        tok, priced = weigh((r, saving(r, plan[key(r)])) for r in done)
        w.erase()
        h, wd = w.getmaxyx()
        y = max(2, h // 2 - 8)
        put(w, y, 3, name if going_up else f"{name} · done", DIM())
        put(w, y + 1, 3, f"{len(done)} of {len(items)} chosen" + (f" · {unit_fmt(tok, priced)}" if tok else "") if done else f"nothing chosen out of {len(items)}", curses.color_pair(62) | curses.A_BOLD)
        grad(w, y + 3, 3, wd - 7, len(done) / max(1, len(items)), HEALTH)
        if following:
            d = [r for r in following if best(r) and key(r) not in plan]
            put(w, y + 6, 3, "previous" if going_up else "next", DIM())
            title = fit_title(next_name, wd - 8)
            if h >= BANNER_ROWS:
                for row, text_row in enumerate(banner(title)):
                    put(w, y + 7 + row, 3, text_row, curses.color_pair(62) | curses.A_BOLD)
                if title != next_name or not any(c.lower() in FONT for c in title):
                    put(w, y + 10, 3, next_name, DIM())
                summary_y = y + 11
            else:
                put(w, y + 7, 3, next_name, curses.color_pair(62) | curses.A_BOLD)
                summary_y = y + 8
            put(w, summary_y, 3, f"{plural(len(following), 'item')} · {len(d)} left to choose · {unit_fmt(*weigh((r, r['Tokens']) for r in d))}", curses.A_BOLD)
            put(w, h - 2, 3, ("↑ or enter go back to it" if going_up else "↓ or enter continue") + " · esc stay here", DIM())
        else:
            put(w, y + 6, 3, "This is the first list." if going_up else "That was the last list.", curses.A_BOLD)
            put(w, h - 2, 3, f"esc back, then {apply_key()} to apply", DIM())
        draw_apply_label(w, h, wd, len(plan))
        w.refresh()
        k = read_key(w)
        if k in (curses.KEY_RESIZE, curses.KEY_MOUSE):
            continue
        if on_apply and is_apply(k, shift_held, bool(plan)):
            on_apply()
            continue
        if following and (k == advance or k in (10, 13)):
            return following
        if k in (27, curses.KEY_UP, curses.KEY_DOWN, curses.KEY_LEFT, ord("q"), 10, 13):
            return None


def yes_or_no(w, redraw=None, allowed=True):
    """Waits for an answer: arrows, clicks and any other key leave the screen as it is.
    A y counts only as a fresh key: not typed ahead, pasted, still held from the question before, or under a window too small to show the question."""
    asked_at = time.monotonic()
    curses.flushinp()
    while True:
        k = read_key(w)
        if k == curses.KEY_RESIZE and redraw:
            allowed = redraw() is not False
        elif k == NON_LATIN:
            put(w, w.getmaxyx()[0] - 1, 3, "y and n are read as Latin letters: switch keyboard layout", curses.color_pair(61) | curses.A_BOLD)
            w.refresh()
        elif k == ord("y"):
            if allowed and time.monotonic() - asked_at >= YES_GUARD_S and key_gap() >= REPEAT_GAP_S:
                return True
        elif k in (ord("n"), 27):
            return False


def receipt_rows(plan):
    """Every change the label counts, what cannot be undone first, so it survives a short window."""
    picks = [(r, plan[key(r)]) for r in ROWS if key(r) in plan]
    return sorted(picks, key=lambda t: (t[1] != "delete", t[1] != "disable", -t[0]["Tokens"]))


def receipt_line(r, verb, pw):
    if verb == "disable":
        return f"{'disable':<9}{cut_mid(r['Source'][7:], pw - 9)}"
    kept = saving(r, verb)
    worth = "?" if r.get("WeightUnknown") else fmt(kept, kept * reach(r)) if kept else ""
    amount_w = max(len(worth), min(20, pw // 3))
    name_w = pw - 9 - amount_w
    tag = {"delete": "⚠ delete", "unmute": "undo", "unlock": "undo", "restore": "undo"}.get(verb, verb)
    return f"{tag:<9}{pad(cut_mid(display_name(r), name_w), name_w)}{worth:>{amount_w}}"


def receipt(w, plan, unit, on_applied=lambda: None, on_reinvest=None):
    """on_reinvest(picks) is the next step, when the answer to its offer is yes."""
    with without_mouse():
        return _receipt(w, plan, unit, on_applied, on_reinvest)


def using_warning(picks):
    """Chosen against the evidence. It only says so: the choice stays the user's."""
    n = sum(1 for r, c in picks if c in LIGHTER and in_use(r))
    return f"⚠ {n} of these {'is' if n == 1 else 'are'} in use" if n else ""


def _receipt(w, plan, unit, on_applied, on_reinvest):
    picks = receipt_rows(plan)
    if not picks:
        return "nothing to apply"
    tok, priced = weigh((r, saving(r, c)) for r, c in picks)
    linked = shared_folders(picks)
    taken = removed_for_others(picks)
    warnings = [line for line in (f"⚠ also muted for other tools using {', '.join(linked)}" if linked else "",
                                  f"⚠ also gone for other tools using {', '.join(taken)}" if taken else "", update_warning(picks)) if line]
    using = using_warning(picks)
    back = sum(1 for _, c in picks if c in ("unmute", "restore"))
    seen = set()
    lines = [t for t in picks if t[1] != "disable" or not (t[0]["Source"] in seen or seen.add(t[0]["Source"]))]
    gone = sum(1 for _, c in picks if c == "delete")

    def draw():
        """Returns False when the window cannot show the question: then y must not answer it."""
        if too_small(w, RECEIPT_MIN, "too small to confirm: esc cancels"):
            return False
        w.erase()
        h, wd = w.getmaxyx()
        pw = min(60, wd - 6)
        x = (wd - pw) // 2
        put(w, 1, x, f"{'SKILLREAPER':^{pw}}", curses.A_BOLD)
        put(w, 2, x, "┄" * pw, DIM())
        room = h - 14 - max(2, len(warnings) + (1 if gone else 0))
        shown_lines = lines[:room] if len(lines) <= room else lines[: room - 1]
        for n, (r, a) in enumerate(shown_lines):
            attr = C(1) | curses.A_BOLD if a == "delete" else curses.color_pair(61) | curses.A_BOLD if a == "disable" else DIM()
            put(w, 3 + n, x, receipt_line(r, a, pw), attr)
        if len(shown_lines) < len(lines):
            put(w, 3 + len(shown_lines), x, f"… and {len(lines) - len(shown_lines)} more", DIM())
        y = h - 11
        put(w, y, x, "┄" * pw, DIM())
        if gone:
            warn = f" ⚠ {gone} deleted for good: {'it' if gone == 1 else 'they'} cannot be restored "
            put(w, y - 1, x, pad(warn, pw), rc(HEALTH, 0, True) | curses.A_BOLD)
        for n, line in enumerate(warnings):
            put(w, y - (2 if gone else 1) - n, x, cut(line, pw), curses.color_pair(61) | curses.A_BOLD)
        summary = plural(len(picks), "item") + (f" · ~{tokens(tok)} tokens lighter every session" if tok else "") + (f" · {back} comes back heavier" if back == 1 else f" · {back} come back heavier" if back else "")
        put(w, y + 1, x, cut(summary, pw), curses.A_BOLD)
        if tok:
            above, beside = big_money_words("saved every month") if unit == "money" else ("", "tokens saved every session")
            put(w, y + 2, x, above, DIM())
            end = big(w, y + 3, x, money(priced) if unit == "money" else tokens(tok), 0.0)  # green: what you get back, not what you waste
            put(w, y + 4, end + 2, beside, curses.color_pair(62) | curses.A_BOLD)
        if using:
            put(w, y + 6, x, cut(using, pw), curses.color_pair(61) | curses.A_BOLD)
        put(w, y + 7, x, "Do it?  [y/n]", curses.A_BOLD)
        w.refresh()
        return True

    if gone:
        curses.beep()
    if not yes_or_no(w, draw, draw()):
        return "demo: cancelled, still here"
    on_applied()
    verbs = [c for _, c in picks]
    done = " · ".join(f"{verbs.count(v)} {word}" for v, word in DONE_WORD.items() if v in verbs)
    amount_text = worth(money(priced)) if unit == "money" else f"~{tokens(tok)} tokens"
    if not tok:
        return f"✓ {done} (demo: nothing touched)"

    def draw_offer():
        if draw() is False:
            return False
        h, wd = w.getmaxyx()
        pw = min(60, wd - 6)
        x = (wd - pw) // 2
        y = h - 11
        put(w, y + 7, x, pad(f"✓ {done} (demo: nothing touched)", pw), curses.color_pair(62) | curses.A_BOLD)
        put(w, y + 8, x, f"Reinvest {amount_text}?  [y/n]"[:pw], curses.A_BOLD)
        w.refresh()
        return True

    if yes_or_no(w, draw_offer, draw_offer()) and on_reinvest:
        on_reinvest(picks)
    return f"✓ {done} (demo: nothing touched)"
