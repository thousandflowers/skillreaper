#!/usr/bin/env python3
"""Proposal v5: you choose, reap stays quiet until asked. Read-only: nothing is ever run."""
import curses, datetime, os, textwrap
from reap_proto import C, DATA, DEAD, ROWS, WHY, DIM, HL, load_json, positive, put, tint, too_small, visible, why, window
from reap_proto2 import EXTRA, HEALTH, PRICE, grad, rc, setup, spark
from reap_proto3 import RAMP, can, dead, groups, is_plugin, key, last, own, read_only, text, why_not
from terminal import ENTER, NON_LATIN, SHIFT_ENTER, apply_key, is_apply, is_light, read_key


USD = {"cur": "USD", "sym": "US$", "rate": 1.0}  # a fallback must read as foreign where $ is local
NUMBER_STYLE = {"decimal": ",", "group": ".", "digits": 2}  # what a rate.json from before these fields meant
CARD_MIN = (11, 58)  # rows, columns: the action row and the keys must fit


def load_rate():
    """No rate, or an unusable one, means USD: a wrong amount is worse than a foreign one."""
    raw = load_json("rate.json", USD)
    usable = isinstance(raw, dict) and positive(raw.get("rate")) and isinstance(raw.get("sym"), str)
    return {**NUMBER_STYLE, **(raw if usable else USD)}


R = load_rate()
_plan_file = load_json("plan.json", {})
PLAN = visible(_plan_file.get("billingType")) if isinstance(_plan_file, dict) else ""
SUBSCRIBER = "subscription" in PLAN
SHIFT_UP = {curses.KEY_SR, 337, ord("K")}
SHIFT_DOWN = {curses.KEY_SF, 336, ord("J")}
PEEK = ord("s")
PEEK_MS = 550  # longer than the keyboard's repeat delay, or a held key flickers


FONT = {"a": "█▀█|█▀█|▀ ▀", "b": "█▀▄|█▀▄|▀▀ ", "c": "█▀▀|█  |▀▀▀", "d": "█▀▄|█ █|▀▀ ", "e": "█▀▀|█▀▀|▀▀▀", "f": "█▀▀|█▀▀|▀  ", "g": "█▀▀|█ █|▀▀▀",
        "h": "█ █|█▀█|▀ ▀", "i": "█|█|▀", "j": "  █|█ █|▀▀▀", "k": "█ █|█▀▄|▀ ▀", "l": "█  |█  |▀▀▀", "m": "█▀▄▀█|█ ▀ █|▀   ▀", "n": "█▄ █|█ ▀█|▀  ▀", "o": "█▀█|█ █|▀▀▀",
        "p": "█▀█|█▀▀|▀  ", "q": "█▀█|█ █|▀▀█", "r": "█▀█|█▀▄|▀ ▀", "s": "█▀▀|▀▀█|▀▀▀", "t": "▀█▀| █ | ▀ ", "u": "█ █|█ █|▀▀▀", "v": "█ █|█ █| ▀ ", "w": "█   █|█ █ █|▀▀▀▀▀",
        "x": "█ █|▄▀▄|▀ ▀", "y": "█ █|▀█▀| ▀ ", "z": "▀▀█|▄▀ |▀▀▀", "0": "█▀█|█ █|▀▀▀", "1": "▄█ | █ |▀▀▀", "2": "▀▀█|█▀▀|▀▀▀", "3": "▀▀█| ▀█|▀▀▀", "4": "█ █|▀▀█|  ▀",
        "5": "█▀▀|▀▀█|▀▀▀", "6": "█▀▀|█▀█|▀▀▀", "7": "▀▀█|  █|  ▀", "8": "█▀█|█▀█|▀▀▀", "9": "█▀█|▀▀█|▀▀▀", "-": "  |▀▀|  ", "_": "  |  |▀▀", ".": " | |▀", ",": " | |█"}


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


DAYS_IN_MONTH = 30


def sessions_per_month():
    """Sessions a month, or 0 when that cannot be known: the window's own count is not a month."""
    if "SessionsPerMonth" in DATA:
        return DATA["SessionsPerMonth"]
    days = DATA.get("WindowDays")
    return round(DATA.get("Sessions", 0) * DAYS_IN_MONTH / days) if days else 0


PER_MONTH = sessions_per_month()
PLATFORM_MONTH = DATA.get("PlatformSessionsPerMonth")


def sessions_of(r):
    """How many sessions a month load this row: its own platform's, when the report knows them per platform."""
    return PER_MONTH if PLATFORM_MONTH is None else PLATFORM_MONTH.get(r.get("Platform"), 0)


# Tokens times sessions behind MoneyPerMonth: what turns a row's own sessions into money.
_TOKEN_SESSIONS = sum(r["Tokens"] * sessions_of(r) for r in DATA["Rows"] if r["Verdict"] == "REAP")


def reach(r):
    """A row's tokens in the units money() prices: 1 unless its platform has more or fewer sessions than the average."""
    if PLATFORM_MONTH is None or not PRICE or not _TOKEN_SESSIONS:
        return 1
    return sessions_of(r) * DATA["MoneyPerMonth"] / _TOKEN_SESSIONS / PRICE


def number(v, digits=0):
    """A figure with the separators of the system's currency format."""
    swap = {",": R["group"], ".": R["decimal"]}
    return "".join(swap.get(c, c) for c in f"{v:,.{digits}f}")


def amount(v):
    """An amount of money: the symbol is never touched, only the number is formatted."""
    return f"{R['sym']}{number(v, R['digits'])}"


def money_figure(v):
    """One rule for every small amount, the big font's too: nothing is its symbol and 0, a fraction of a cent says less than a cent."""
    smallest = 10 ** -R["digits"]
    return f"{R['sym']}0" if v <= 0 else f"<{amount(smallest)}" if v < smallest else amount(v)


def money(tok):
    if not PRICE:
        return ""  # no price known: a figure here would be invented
    return money_figure(tok * PRICE * R["rate"])


def worth(text):
    """How money is worded. The figure is a floor; a flat plan's bill does not change, so there it is only a value."""
    return f"worth {text} at API prices" if SUBSCRIBER else f"at least {text}"


def big_money_words(words):
    """The same wording around a figure in the big font: (above it, beside it)."""
    return ("worth", f"at API prices, {words}") if SUBSCRIBER else ("at least", words)


def money_column():
    return "API PRICE/MONTH" if SUBSCRIBER else "AT LEAST/MONTH"


def tokens(tok):
    return number(tok)


def fmt(tok, priced=None):
    """Tokens and their money. priced is the same tokens weighed by the rows' own sessions, when that differs."""
    return f"~{tokens(tok)} tok {money(tok if priced is None else priced)}".rstrip()


def plural(n, word):
    return f"{n} {word}{'' if n == 1 else 's'}"


def suggestion(r):
    if r.get("Reason") == "pruned":
        return "pruned"
    if read_only(r):
        return "read-only"
    if r.get("ViaLink") or not (r["Removable"] or is_plugin(r)):
        return "-"  # a link or a shared folder: yours to choose, never reap's suggestion
    if r.get("Reason") == "muted":
        return "muted"
    if r["Verdict"] == "REAP" and own(r):
        return "prune"
    if r["Verdict"] in ("REAP", "MUTE") and r["Category"] in ("skill", "agent"):
        return "mute"
    if r["Verdict"] == "REAP":
        return "/plugin"
    return {"KEEP": "keep", "REVIEW": "not sure"}.get(r["Verdict"], "-")


SUGGEST_WORD = {"keep": "lock", "-": ""}
SUGGEST_LINE = {"muted": "already muted", "not sure": "reap is not sure", "-": "reap suggests nothing", "read-only": "read-only: reap only weighs it",
                "/plugin": "reap suggests turning its plugin off: o, or p for this one alone"}


def suggestion_word(r):
    """What reap suggests, in the words the list uses."""
    s = suggestion(r)
    return SUGGEST_WORD.get(s, s)


def suggestion_line(r):
    return SUGGEST_LINE.get(suggestion(r)) or f"reap suggests {suggestion_word(r)}"


last_label = last


def possible(r):
    return can(r, "prune") or can(r, "mute")


EXPLAIN = {"nothing": "leave it as it is", "lock": "locked: reap never suggests touching it", "mute": "keeps it, strips its description, reap unmute brings it back",
           "prune": "moved to quarantine, reap restore brings it back", "delete": "⚠ gone for good, it cannot be restored", "disable": "goes with its whole plugin: claude plugin disable"}
CHOICE_KEY = {"nothing": "u", "lock": "l", "mute": "m", "prune": "p", "delete": "d"}


EXISTING_UNDO = {"mute": "unmute", "lock": "unlock", "prune": "restore"}  # a state already on disk, and the verb that takes it off
UNDO_TEXT = {"unmute": "brings its description back", "unlock": "reap may suggest touching it again", "restore": "brings it back from quarantine"}


def already_locked(r):
    return r.get("Kept") is True or r.get("Reason") == "keep"


def existing(r):
    """What is already true of a row before any choice."""
    return {"muted": "mute", "pruned": "prune"}.get(r.get("Reason")) or ("lock" if already_locked(r) else None)


def legend(w, h, keys):
    """The keys, most needed first, on the two bottom rows. What does not fit is cut as a whole key, never mid-word."""
    wd = w.getmaxyx()[1]
    rows, line = [], ""
    for k in keys:
        joined = f"{line} · {k}" if line else k
        if len(joined) > wd - 6 and line:
            rows, line = rows + [line], k
        else:
            line = joined
    for n, text_row in enumerate((rows + [line])[:2]):
        put(w, h - 2 + n, 3, text_row, DIM())


_generated = DATA.get("GeneratedAt")


def data_age_days(today=None):
    """How old the report is, or None without a readable date."""
    try:
        made = datetime.datetime.fromisoformat(str(_generated).replace("Z", "+00:00")).date()
    except ValueError:
        return None
    return ((today or datetime.date.today()) - made).days


def extras_current():
    """The sparkline counts days up to its own run: a different day from the report's would contradict the numbers."""
    dates = EXTRA["dates"]
    try:
        made = datetime.datetime.fromisoformat(str(_generated).replace("Z", "+00:00")).date()
        return abs((made - datetime.date.fromisoformat(dates[-1])).days) <= 1
    except (ValueError, TypeError, IndexError):
        return True  # nothing to compare: nothing to contradict


def caveats():
    """What the numbers on screen should be read with."""
    age = data_age_days()
    return ([f"data from {age} days ago: ./snapshot.sh gathers it again"] if age and age > 1 else []) + DATA["Warnings"]


def fit_title(name, width):
    """The longest start of name the big font can draw in width. Cutting first keeps a huge name from costing seconds."""
    fit = name[:max(0, width)]
    while fit and len(banner(fit)[0]) > width:
        fit = fit[:-1]
    return fit


def card_title(w, r, fit, weight, fate):
    if any(c.lower() in FONT for c in fit):
        for row, text_row in enumerate(banner(fit)):
            # Colour is the weight. The fate is the shape: struck through, or faded.
            if fate in ("prune", "delete", "disable") and row == 1:
                text_row = "━" * len(text_row.rstrip())
            put(w, 1 + row, 3, text_row, (0 if is_light() else rc(RAMP, weight)) | (curses.A_DIM if fate == "mute" else curses.A_BOLD))
    else:
        put(w, 2, 3, r["Name"], rc(RAMP, weight) | curses.A_BOLD)  # the big font has no letters for this name


def card_explain(r, effective):
    note = EXPLAIN[effective]
    if effective == "mute" and is_plugin(r):
        return f"{note} · a plugin update can bring the description back"
    if effective in ("prune", "delete") and is_plugin(r):
        return f"{note} · a plugin update can bring it back"
    return note


def card_usage(r):
    seen = last(r)
    if seen == "never" and r.get("HistoricalLast"):
        seen = f"{last({'LastUsed': r['HistoricalLast']})}, before this window"
    elif seen == "never" and r.get("Reason") == "rare":
        seen = "not in this window"
    days = DATA.get("WindowDays")
    span = f"in {days} days" if days else "in this window"
    return f"{plural(r['Uses'], 'use')} {span} · last used {seen}"


def card_options(r):
    """The card's five, each with whether Go could carry it out on this row."""
    allowed = {"lock": can(r, "lock") or existing(r) == "lock", "mute": can(r, "mute") or existing(r) == "mute", "prune": can(r, "prune"), "delete": can(r, "delete")}
    return [("nothing", True), *allowed.items()]


def details(w, r, plan, act, siblings=None, on_end=None, on_start=None, on_apply=None, pending=None, on_auto=None, shift_held=lambda: False, on_quit=None, on_plugin=None):
    """Same page as the list: big figure on top, bar under it, hint and keys at the bottom. Returns the row it ended on, or None to leave the menu."""
    siblings = siblings or [r]
    at = next((n for n, x in enumerate(siblings) if x is r), 0)
    notice = ""
    focus_on, focus = None, 0
    while True:
        r = siblings[at]
        if too_small(w, CARD_MIN):
            if read_key(w) in (27, ord("q")):
                return r
            continue
        w.erase()
        h, wd = w.getmaxyx()
        # Every row is fixed: scrolling fast changes the words, nothing moves.
        short = r["Name"].split(":")[-1] or r["Name"]
        weight = min(1, r["Tokens"] / 300)
        chosen = plan.get(key(r))
        fit = fit_title(short, wd - 8)
        state = existing(r)
        effective = "nothing" if chosen in UNDO_TEXT else chosen or state or "nothing"
        card_title(w, r, fit, weight, effective)
        place = f"{at + 1} of {len(siblings)}"
        put(w, 0, wd - len(place) - 1, place, DIM())
        put(w, 4, 3, f"{r['Name'] if fit != short else short} · {r['Category']} · {r['Source'].split('@')[0]} · {r.get('Platform') or 'unknown'}"[: wd - 6], DIM())
        options = card_options(r)
        labels = [o for o, _ in options]
        if focus_on is not r:
            focus_on, focus = r, labels.index(effective) if effective in labels else 0
        blocked = not options[focus][1]
        bx = 3
        for n_opt, (label, allowed) in enumerate(options):
            on = effective == label
            if label == "delete":
                grey = curses.A_REVERSE | curses.A_DIM if n_opt == focus else DIM()
                put(w, 5, bx, "  ⚠ delete  ", grey if not allowed else rc(HEALTH, 0, True) | curses.A_BOLD if on else C(1) | curses.A_BOLD)
                bx += 2
            else:
                grey = curses.A_REVERSE | curses.A_DIM if n_opt == focus else DIM()
                put(w, 5, bx, f"  {label}  ", HL() if on else (0 if allowed else grey))
            bx += len(label) + 5
        if effective == "disable":
            put(w, 5, bx, "  plugin off  ", HL())
        if blocked:
            put(w, 6, 3, f"{labels[focus]} is not available: {why_not(r, labels[focus])}", curses.color_pair(61) | curses.A_BOLD)
        elif chosen in UNDO_TEXT:
            put(w, 6, 3, UNDO_TEXT[chosen], DIM())
        else:
            put(w, 6, 3, card_explain(r, effective), C(1) | curses.A_BOLD if effective == "delete" else DIM())

        def body(y, *args):
            """Rows above the path: when the window is short they give way, the action row and the keys never do."""
            if y < h - 5:
                put(w, y, *args)

        weight_text = "weight unknown" if r.get("WeightUnknown") else f"~{tokens(r['Tokens'])} tokens every session"
        body(7, 3, weight_text, curses.A_BOLD)
        if not r.get("WeightUnknown"):
            body(7, max(34, 5 + len(weight_text)), f"{worth(money(r['Tokens'] * reach(r)))} a month" if PRICE else "", DIM())
        if 8 < h - 5:
            grad(w, 8, 3, wd - 7, max(weight, 0.01), RAMP)
        body(10, 3, suggestion_line(r), curses.A_BOLD)
        body(11, 3, why(r), DIM())
        body(13, 3, card_usage(r), curses.A_BOLD)
        body(14, 3, plural(r.get("ErrorCount", 0), "error"), DIM())
        vals = EXTRA["byday"].get(r["Name"])
        if vals and any(vals) and 15 < h - 5 and extras_current():
            spark(w, 15, 3, vals, RAMP)
            put(w, 15, 4 + len(vals), f"by day, last {len(vals)}", DIM())
        room = max(0, h - 23)
        if room:
            # Only what the visible lines can hold is wrapped: a huge description must not slow every key.
            desc = r.get("Description", "").strip()[: wd * (room + 1)] or "no description"
            for n, line in enumerate(textwrap.wrap(desc, wd - 8)[: room]):
                put(w, 17 + n, 3, line)
        put(w, h - 5, 3, (r.get("Path") or "")[: wd - 8], DIM())
        ended = plan.take_ended() if hasattr(plan, "take_ended") else None
        notice = f"plugin off cancelled: it takes the whole plugin" if ended else notice
        if notice:
            put(w, h - 3, 3, notice[: wd - 6], curses.color_pair(61) | curses.A_BOLD)
        legend(w, h, ["enter or esc back", "←→ choose", "↑↓ next skill", "a auto", "n clear", "l lock", "m mute", "p prune", "d delete"] + (["o plugin off"] if is_plugin(r) else []) + ["q quit"])
        count = pending() if pending else 0
        if count:
            label = f" {apply_key()} apply {count} change{'s' if count != 1 else ''} "
            put(w, h - 3, wd - len(label) - 4, label, HL())
        w.refresh()
        k = read_key(w)
        if k in (curses.KEY_MOUSE, curses.KEY_RESIZE):
            continue  # the card has no use for the mouse: what it says stays
        notice = ""
        if k == NON_LATIN:
            notice = "keys are read as Latin letters: switch keyboard layout"
            continue
        if on_apply and is_apply(k, shift_held, bool(count)):
            notice = on_apply() or ""
            continue
        if k == ord("q") and on_quit:
            if on_quit():
                return None
            continue
        if k in (27, ord("q"), SHIFT_ENTER) or k in ENTER:
            return r
        if k == ord("o") and on_plugin:
            notice = on_plugin(r)
            focus_on = None
            continue
        if k == ord("a") and on_auto:
            notice = on_auto([r])
            focus_on = None
            continue
        if k in (curses.KEY_LEFT, curses.KEY_RIGHT):
            moved = min(len(options) - 1, max(0, focus + (1 if k == curses.KEY_RIGHT else -1)))  # no wrap: left of nothing is not delete
            if moved == focus:
                continue
            focus = moved
            pick, ok = options[focus]
            if not ok:
                continue  # grey: you can stand on it and read why, nothing changes
            if state and pick == "nothing":
                plan[key(r)] = EXISTING_UNDO[state]
            elif pick == state:
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
            # As in the list: space means what Go can do to it at once, even over another choice, and clears only that.
            verb = "p" if can(r, "prune") else "m" if can(r, "mute") else None
            if verb:
                act([r], "u" if chosen == {"p": "prune", "m": "mute"}[verb] else verb)
            focus_on = None
            continue
        if k == ord("n"):
            act([r], "u")  # n clears what is pending, here as in the list; undoing a mute or a lock is the choice row's nothing
            focus_on = None
            continue
        refused = next((n for n, (label, ok) in enumerate(options) if not ok and CHOICE_KEY[label] == chr(k)), None) if 0 <= k < 256 else None
        if refused is not None:
            focus = refused  # stand on the grey option: it says why
            continue
        if k in (ord("p"), ord("m"), ord("l"), ord("d")):
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
    tint(60, 174, curses.COLOR_RED)
    tint(61, 180, curses.COLOR_YELLOW)
    tint(62, 114, curses.COLOR_GREEN)
    w.keypad(True)
    run(w)


if __name__ == "__main__":
    os.environ.setdefault("ESCDELAY", "25")
    curses.wrapper(main)
