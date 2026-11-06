#!/usr/bin/env python3
"""What the menu knows about rows and choices, with no screen in it: which rows it lists, what a choice saves, how a choices map changes."""
import re
import collections, os, re
from reap_proto import DATA, ROWS, cut_mid
from reap_proto3 import can, dead, groups, is_plugin, key, own, read_only, why_not
from reap_proto7 import EXISTING_UNDO, PLAN, R, already_locked, existing, number, plural, reach, suggestion, suggestion_word


OWN_PLUGIN = "skillreaper"


OWN_SKILLS = {"skillreaper", "reap"}


def is_own(r):
    """reap never lists itself: a cleaner must not be able to clean itself away. Exact, so it hides nobody else's items."""
    if r["Source"].startswith("plugin:"):
        return r["Source"][7:].split("@")[0].casefold() == OWN_PLUGIN
    return r["Category"] == "skill" and r["Name"].casefold() in OWN_SKILLS


ROWS[:] = [r for r in ROWS if not is_own(r)]


BY_KEY = {key(r): r for r in ROWS}


NAME_COUNTS = collections.Counter(r["Name"] for r in ROWS)


def display_name(r):
    """A name, with its platform when two rows would otherwise read the same."""
    return f"{r['Name']} ({r.get('Platform') or 'unknown'})" if NAME_COUNTS.get(r["Name"], 0) > 1 else r["Name"]


CHARS_PER_TOKEN = 3.7  # internal/cost: a muted item keeps its name


LETTER = {"prune": "p", "mute": "m", "lock": "l", "delete": "d", "disable": "O", "unmute": "u", "unlock": "u", "restore": "u"}


LIGHTER = ("prune", "mute", "disable", "delete")


STRUCK = ("prune", "delete", "disable")  # out of the stack, so struck through; a muted item stays, so it fades
DONE_WORD = {"prune": "gone", "delete": "deleted", "disable": "off", "mute": "muted", "lock": "locked", "unmute": "undone", "unlock": "undone", "restore": "undone"}


PAST = {"prune": "pruned", "mute": "muted", "lock": "locked", "delete": "deleted", "unmute": "undone", "unlock": "undone", "restore": "undone", None: "cleared"}


PLUGIN_ID = re.compile(r"[A-Za-z0-9][\w.-]*(@[A-Za-z0-9][\w.-]*)?")  # what may be shown as a plugin, and later passed to a command


def saving(r, verb):
    """Tokens a choice really takes off every session. A muted item stays listed under its name."""
    if verb == "mute":
        kept = (len(r["Name"]) * 10 + int(CHARS_PER_TOKEN * 10) - 1) // int(CHARS_PER_TOKEN * 10)
        return max(0, r["Tokens"] - kept)
    return r["Tokens"] if verb in LIGHTER else 0


def state_box(r, plan):
    """The box says what you chose, with the letter of the key that sets it. A read-only row shows one only once locked."""
    p = plan.get(key(r))
    return f"[{LETTER[p]}]" if p else "   " if read_only(r) else "[ ]"


def muted_count(items, plan):
    """Being muted stays true until apply, whatever is chosen for it."""
    return sum(1 for r in items if r.get("Reason") == "muted")


def group_box(items, plan):
    """[-] when mixed, [ ] only when nothing is chosen. A row reap has no quarrel with and you left alone does not make it mixed."""
    marks = {state_box(r, plan) for r in items if key(r) in plan or (dead(r) and not read_only(r))} or {"[ ]"}
    return marks.pop() if len(marks) == 1 else "[-]"


def wasted(visible_row=lambda r: True):
    """The rows behind the headline: the report's own definition, so the menu and reap say one number. reap's own rows count there too."""
    return [r for r in DATA["Rows"] if r["Verdict"] == "REAP" and visible_row(r)]


def plugin_rows(source):
    """Every row of one plugin, wherever the list happens to show them."""
    return [r for r in ROWS if r["Source"] == source]


def in_use(r):
    """Chosen against the evidence: it fires, or reap itself would lock it."""
    return r["Uses"] > 0 or suggestion(r) == "keep"


def shared_folders(picks):
    """The folders a mute reaches beyond this tool: a linked skill's description lives there, for every tool using it."""
    return folders_said(shared_dir(r) for r, verb in picks if verb == "mute")


def shared_dir(r):
    """The folder this row's file really lives in, when other tools can be reading it too."""
    link = r.get("LinkTarget") or r.get("ViaLink")
    if link:
        return os.path.dirname(link)
    return os.path.dirname(os.path.dirname(r["Path"])) if r["Category"] == "skill" and not r["Removable"] and not is_plugin(r) else None


def folders_said(found):
    home = os.path.expanduser("~")
    return sorted({f"~{f[len(home):]}" if f.startswith(home + os.sep) else f for f in found if f})


def removed_for_others(picks):
    """The shared folders a prune or a delete really takes something out of. Through a link, only the link goes."""
    return folders_said(shared_dir(r) for r, verb in picks if verb in ("prune", "delete") and not (r.get("LinkTarget") or r.get("ViaLink")))


def update_warning(picks):
    """A plugin's update puts its files back. It only says so: the choice stays the user's."""
    n = sum(1 for r, verb in picks if verb in ("prune", "delete") and is_plugin(r))
    return f"⚠ {plural(n, 'plugin item')} may return with an update" if n else ""


def plugins_in(rows):
    """The plugins these rows belong to that o can name."""
    return sorted({r["Source"] for r in rows if plugin_target([r])})


def act_on(plan, sweep, applied, items, ch):
    """A key on rows or a group: every row that can take it, and it names those that cannot. Returns what to say."""
    if ch == "u":
        undoable = [r for r in items if key(r) not in applied and EXISTING_UNDO.get(existing(r))]
        for r in undoable:
            plan[key(r)] = EXISTING_UNDO[existing(r)]
            sweep[key(r)] = 0
        return f"{len(undoable)} undone" if undoable else "nothing to undo here: u takes back a mute or a lock"
    verb = {"p": "prune", "m": "mute", "l": "lock", "d": "delete", "n": None}[ch]
    n, refused = 0, []
    plan.refused = {}
    for r in [r for r in items if key(r) not in applied]:  # a card can still be showing a row that just went
        if verb is None or verb == existing(r):
            plan.pop(key(r), None)  # n clears; a verb it already is goes back to that, as the card's arrows do
            n += 1
        elif can(r, verb):
            plan[key(r)] = verb
            sweep[key(r)] = 0
            n += 1
        else:
            refused.append(r)
    msg = f"{n} {PAST[verb]}" if n or not refused else ""
    if verb == "delete" and n > 1:
        msg = f"⚠ {n} marked for delete: cannot be restored"
    if refused:
        plan.refused = {key(r): verb for r in refused}
        msg += f"{' · ' if msg else ''}{len(refused)} cannot be {PAST[verb]}: {refused[0]['Name'] + ': ' if len(refused) == 1 else ''}{why_not(refused[0], verb)}"
    return msg


def plugin_target(rows):
    """(source, id) of the one plugin these rows belong to, from any row or group of it; None otherwise."""
    sources = {r["Source"] for r in rows}
    only = next(iter(sources)) if len(sources) == 1 else ""
    return (only, only[7:]) if only.startswith("plugin:") and PLUGIN_ID.fullmatch(only[7:]) else None


def plugin_of(items):
    """The plugin behind a group, or None when the group is not one plugin with a valid id."""
    ids = {r["Source"][7:] for r in items if r["Source"].startswith("plugin:")}
    if len(ids) != 1 or not all(r["Source"].startswith("plugin:") for r in items):
        return None
    only = next(iter(ids))
    return only if PLUGIN_ID.fullmatch(only) else None


def best(r):
    """Reap's own choice, what space, a click and a all take. On an already muted row it has none to make."""
    if r.get("Reason") in ("muted", "pruned") or (already_locked(r) and suggestion(r) == "keep"):
        return None
    if suggestion(r) in SUGGEST_PLAN:
        return SUGGEST_PLAN[suggestion(r)]
    return "prune" if own(r) else "mute" if can(r, "mute") else "prune" if can(r, "prune") else None


SUGGEST_GROUPS = (("prune", "suggest to prune"), ("mute", "suggest to mute"), ("not sure", "not sure, you decide"), ("keep", "suggest to lock"), ("muted", "already muted"), ("pruned", "already pruned"))


SUGGEST_PLAN = {"prune": "prune", "mute": "mute", "keep": "lock"}


UNDECIDED = "not sure"  # where a row goes when reap has no group for it: every row stays reachable


def suggested_groups():
    known = {name for name, _ in SUGGEST_GROUPS}
    home = lambda r: suggestion(r) if suggestion(r) in known else UNDECIDED
    return [(label, [r for r in ROWS if home(r) == name]) for name, label in SUGGEST_GROUPS]


def click_choice(r):
    """A click takes reap's own choice, the same as space."""
    return best(r)


def group_all(f):
    """What a row reaches for a choice that fits any row, like a lock or reap's own suggestion: everything in a group."""
    return [f[2]] if f[0] == "i" else list(f[3])


def row_items(f):
    """What a row acts on: the item, or a group's unused items it can act on; failing that the rest not worth locking; failing that all."""
    if f[0] == "i":
        return [f[2]]
    return [r for r in f[3] if suggestion(r) != "keep"] or list(f[3])


def grouped(mode):
    """g cycles: by what reap suggests, by where it comes from, one flat list."""
    return suggested_groups() if mode == 0 else groups(0) if mode == 1 else groups(2)


MODE_NAMES = ("by suggestion", "by plugin", "flat")


def consume_plan(plan, gone):
    """After the y: what the receipt removed leaves the list for good and keeps its saving; every choice is spent."""
    kept = {**gone, **{k: v for k, v in plan.items() if v in LIGHTER}}
    for k in list(plan):
        plan.pop(k, None)
    plan.take_ended()
    return kept


class Plan(dict):
    """Choices by row. Any write forgets what a remembered for that row, so a can never undo a later choice.
    Plugin off is all or nothing: a row leaving it ends it for every row of that plugin, so box and receipt agree."""

    def __init__(self):
        super().__init__()
        self.memory = {}
        self.version = 0
        self.ended = None
        self.refused = {}  # the rows the last key left out, and the verb they refused

    def _end_plugin_off(self, k, v):
        if v == "disable" or self.get(k) != "disable":
            return
        for other in [x for x, verb in self.items() if verb == "disable" and x[3] == k[3] and x != k]:
            self.memory.pop(other, None)
            super().pop(other)
        self.ended = k[3]

    def take_ended(self):
        """The plugin whose off a write just ended, once."""
        ended, self.ended = self.ended, None
        return ended

    def __setitem__(self, k, v):
        self._end_plugin_off(k, v)
        self.memory.pop(k, None)
        self.version += 1
        super().__setitem__(k, v)

    def pop(self, k, *default):
        self._end_plugin_off(k, None)
        self.memory.pop(k, None)
        self.version += 1
        return super().pop(k, *default)


def auto(plan, sweep, items):
    """Sets what reap suggests, over any choice in the box; pressed again, puts back what stood before that press.
    Where reap is not sure, the same choice a click takes, so a whole group can go at once. Nothing runs until you apply."""
    rows = {key(r): r for r in items}
    choices = {k: suggestion(r) != "muted" and click_choice(r) for k, r in rows.items()}
    picks = {k: verb for k, verb in choices.items() if verb}
    left = sum(1 for k, r in rows.items() if k not in picks and suggestion(r) not in ("muted", "read-only") and not already_locked(r))
    tail = f" · {left} left: nothing reap can do to them" if left else ""
    if not picks:
        return f"reap chose nothing{tail}"
    matches = lambda k: plan.get(k) == picks[k]
    if all(map(matches, picks)) and any(k in plan.memory for k in picks):
        changed = False
        for k in [k for k in picks if k in plan.memory]:
            was = plan.memory[k]
            changed = changed or was != plan.get(k)
            plan.pop(k, None)
            sweep.pop(k, None)
            if was:
                plan[k] = was
        return ("reap's choice undone" if changed else "nothing to undo") + tail
    changes = not all(map(matches, picks))
    # Already what reap suggests with nothing else changing: there is nothing to go back to, so a second a empties it.
    memory = {k: plan.get(k) if changes else None for k in picks}
    for k, verb in picks.items():
        plan.pop(k, None)
        plan[k] = verb
        sweep[k] = 0
    plan.memory.update(memory)
    verbs = list(picks.values())
    said = " · ".join(f"{verbs.count(v)} {v}" for v in SUGGEST_PLAN.values() if v in verbs)
    return f"reap chose: {said}{tail}"


ACTION_GROUPS = (("prune", ("prune",)), ("mute", ("mute",)), ("delete", ("delete",)), ("plugin off", ("disable",)),
                 ("lock", ("lock",)), ("undo", ("unmute", "unlock", "restore")))


def plan_summary(plan):
    """Every choice so far, added up by action in a fixed order."""
    verbs = list(plan.values())
    counts = ((label, sum(verbs.count(v) for v in members)) for label, members in ACTION_GROUPS)
    return " · ".join(f"{label} {n}" for label, n in counts if n)


def is_echo(msg):
    """A message that only repeats what the summary already shows; anything else is news and is shown instead."""
    return bool(re.fullmatch(r"(reap chose: )?\d+ \w+( · \d+ \w+)*", msg))


def group_stats(its, plan):
    """What a group row says: the rows you chose, out of all its rows, in both of its phrases."""
    doable = [r for r in its if best(r)]
    marked = [r for r in its if key(r) in plan]
    return {"doable": doable, "marked": marked, "full": bool(its) and all(plan.get(key(r)) in STRUCK for r in its),
            "faded": bool(its) and all(plan.get(key(r)) in LIGHTER for r in its),
            "box": group_box(its, plan), "muted": muted_count(its, plan), "plugin": plugin_of(its),
            "all_pruned": bool(doable) and all(plan.get(key(r)) == best(r) for r in doable), "unused_doable": sum(1 for r in its if dead(r) and best(r)),
            "saved": sum(saving(r, plan[key(r)]) for r in marked), "total": sum(r["Tokens"] for r in doable),
            "saved_priced": sum(saving(r, plan[key(r)]) * reach(r) for r in marked), "total_priced": sum(r["Tokens"] * reach(r) for r in doable),
            "deletes": sum(1 for r in marked if plan[key(r)] == "delete"), "unused": sum(1 for r in its if r["Uses"] == 0)}


ROUNDS_UP = 0.99995  # at one decimal in the unit below, this already prints as 1000,0


def short(v):
    """A compact figure, rounded before the unit is chosen so a value just under a million reads 1,0M."""
    if v >= 1e15:
        return f"{v:.1e}".replace(".", R["decimal"])
    for limit, unit in ((1e12, "T"), (1e9, "G"), (1e6, "M"), (1e3, "k")):
        if v >= limit * ROUNDS_UP:
            return f"{number(v / limit, 1)}{unit}"
    return str(int(v))


def short_count(n):
    return str(n) if n < 1000 else short(n)


def group_phrase(its, st):
    """A count that agrees with the rows under it: 'unused' means no uses, and a muted row's uses prove nothing."""
    total = len(its)
    if st["marked"]:
        return f"{short_count(len(st['marked']))} of {short_count(total)} chosen"
    if st["muted"] == total:
        return f"{short_count(total)} muted"
    if its and all(r.get("Reason") == "pruned" for r in its):
        return f"{short_count(total)} pruned"
    return f"{short_count(st['unused'])} of {short_count(total)} unused"


def peek_phrase(its):
    """Holding s on a group: what reap suggests inside it."""
    words = collections.Counter(suggestion_word(r) or "nothing" for r in its)
    return " · ".join(f"{n} {word}" for word, n in words.most_common(2))


def pointer_top(top, row_y, y_first, view, rows):
    """Dragging onto the first or last visible row scrolls one row; otherwise the list stays under the pointer."""
    if row_y == y_first and top > 0:
        return top - 1
    if row_y == y_first + view - 1 and top + view < rows:
        return top + 1
    return top


def weigh(pairs):
    """(tokens, the same weighed by each row's own sessions) for (row, tokens) pairs."""
    pairs = list(pairs)
    return sum(t for _, t in pairs), sum(t * reach(r) for r, t in pairs)


def selection_hint(items, rows, how, unit_fmt):
    return f"SELECTING {plural(rows, 'row')} · {plural(len(items), 'item')} · {unit_fmt(*weigh((r, r['Tokens']) for r in items))} · {how} to extend · space prunes them · esc cancels"


VERB_S = {"prune": "prunes", "mute": "mutes", "lock": "locks"}


def verbs_said(rows):
    """What space does to these rows, in one word when it is one thing."""
    return " or ".join(sorted({VERB_S[best(r)] for r in rows if best(r)})) or "does nothing to"


def cursor_hint(cur_row, stats, plan):
    if cur_row[0] == "g":
        if stats["all_pruned"]:
            hint = f"space clears {plural(len(stats['doable']), 'item')} in {cur_row[2]}"
        elif stats["doable"]:
            hint = f"space {verbs_said(stats['doable'])} {cur_row[2]}: {plural(len(stats['doable']), 'item')}"
        else:
            hint = f"reap has nothing to change in {cur_row[2]}: u undoes it"
        return hint + (" · o turns the whole plugin off" if any(is_plugin(r) for r in cur_row[3]) and plugin_target(cur_row[3]) else "")
    r = cur_row[2]
    if not best(r):
        return f"{r['Name']} · {why_not(r, 'prune')}"
    verb = "clears" if plan.get(key(r)) == best(r) else VERB_S[best(r)]
    return f"space {verb} {r['Name']}" + (" · o turns its plugin off" if is_plugin(r) else " · d deletes it for good" if can(r, "delete") else "")
