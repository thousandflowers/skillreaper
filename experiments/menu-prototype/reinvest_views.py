#!/usr/bin/env -S python3 -B
"""Reinvest as a credit, the step after the receipt; alone, as a mockup: ./reinvest_views.py weight|topic. Read-only: writes nothing and sends nothing itself;
enter asks the browser to open the skill's page, which tells skills.sh that skill's id and your address.
What you removed is credit: nothing more to do. A new skill spends it, and shows which removed items earned it.
Work groups come from reinvest's last cached run; the new skills and their weights are placeholders.
Run it directly or with python3 -B: a statement in here cannot stop Python caching its own modules at startup."""
import sys

sys.dont_write_bytecode = True  # before the local imports below: read-only means no __pycache__ either

import argparse  # noqa: E402
import curses  # noqa: E402
import hashlib  # noqa: E402
import json  # noqa: E402
import math  # noqa: E402
import os  # noqa: E402
import queue  # noqa: E402
import re  # noqa: E402
import shutil  # noqa: E402
import subprocess  # noqa: E402
import tempfile  # noqa: E402
import threading  # noqa: E402
import time  # noqa: E402
import unicodedata  # noqa: E402
from collections import Counter  # noqa: E402
from pathlib import Path  # noqa: E402

import reap_proto10 as m  # noqa: E402
from reap_proto import DATA, PRUNED, cells, cut, pad, too_small  # noqa: E402
from terminal import ENTER, apply_key, enter, is_apply, launch, leave, read_key, without_mouse  # noqa: E402

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "reinvest"))
import sources  # noqa: E402  (its cache and its rules, already run on every candidate: one definition of each)
from sessions import canon  # noqa: E402

SKILLS_SH = "https://skills.sh"
_SEGMENT = r"[A-Za-z0-9][A-Za-z0-9._-]{0,99}"
SAFE_ID = re.compile(f"{_SEGMENT}/{_SEGMENT}/{_SEGMENT}")  # owner/repo/skill, ASCII as in Go's RE2: the form with a page
HOST_ID = re.compile(f"{_SEGMENT}/{_SEGMENT}")  # host/skill: a real result, it only has no page on skills.sh
VIEWS = ("weight", "topic")
MIN_SESSIONS = 3  # same rule as reinvest's find_gaps
MIN_DESCRIPTION = 40  # same rule as reinvest's candidates_for: under this it is not a SKILL.md worth a verdict
MAX_PROPOSALS = 20  # kinds of work; rows scroll, so nothing recurring is left out
MAX_TOPIC_MATCHES = 5  # the closest few: past these, overlap is not about the same work
MIN_COMBO = 2  # the point is a combination
COVERS = 0.7  # a combination under this share of the cost says it is only part of it
MAX_DOC_BYTES = 1 << 20
MAX_JSON_BYTES = 8 << 20
MAX_CONTROL_SHARE = 0.01  # more control characters than this is not text a rule can read
FRAME_MS = 25
STEP_MS = 700  # ponytail: staged pace, a real write takes minutes
SPINNER = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
STEPS_ONLINE = ("downloading", "security check", "installing")
STEPS_WRITTEN = ("reading your sessions", "writing it", "security check")
ROW_HEIGHT = 5
ROW_LINES = 4
LIST_TOP = 7
TASK_LINES = 3
MIN_SHARED_WORDS = 2  # ponytail: word overlap stands in for Claude judging what is similar
SCREEN_MIN = (LIST_TOP + ROW_LINES + 4, 40)  # rows, columns: one whole row, the notice, the action and the keys
OPENER_DIRS = ("/usr/bin", "/bin")  # never whatever comes first on PATH
OPENER_NAMES = ("open", "xdg-open")
REMOTE_VARS = ("SSH_CONNECTION", "SSH_TTY")
OPEN_TIMEOUT = 10
OPEN_REPEAT_S = 1.0  # a held enter is one press
NOT_CHECKED = "? not checked yet, read it first"
CHECKED_LATER = "✎ checked once it is written"
REGEX_ONLY = "regex only, Claude has not read it"
CLAUDE_PASSED = "✓ Claude read it, no risk reported"
STALE = "? checked {} days ago, read it first"
NO_CREDIT = "nothing can be taken: credit comes from removing items"
CREDIT_MEMORY = {"taken": frozenset()}  # esc goes back and what you took waits for the return, by the work it is for
ANSWER_KEYS = (curses.KEY_RESIZE, curses.KEY_MOUSE)  # the terminal talking, not the user


def clean_text(value):
    """Text from a file or a remote service, one line, with nothing that moves the cursor or reorders what is shown."""
    if not isinstance(value, str):
        return ""
    return "".join(c for c in " ".join(value.split()) if not unicodedata.category(c).startswith("C")).strip()


def wrap_cells(text, width):
    """Word wrap by cells: a double-width character takes two, and a word longer than the line is split."""
    if width <= 0:
        return []
    lines, line = [], ""
    for word in text.split():
        while cells(word) > width:
            part = cut(word, width) or word[0]
            if line:
                lines, line = lines + [line], ""
            lines, word = lines + [part], word[len(part):]
        joined = f"{line} {word}" if line else word
        if cells(joined) > width:
            lines, line = lines + [line], word
        else:
            line = joined
    return lines + [line] if line else lines


def read_regular(path, limit):
    """The file's bytes, or None when it is not a regular file of a sane size: a pipe would hang here for ever."""
    try:
        if not os.path.isfile(path) or os.path.getsize(path) > limit:
            return None
        with open(path, "rb") as f:
            return f.read(limit)
    except OSError:
        return None


def read_json(path):
    raw = read_regular(path, MAX_JSON_BYTES)
    if raw is None:
        return None
    try:
        return json.loads(raw.decode("utf-8"))
    except (ValueError, RecursionError):
        return None


def read_doc(path):
    """The document as text, or None when it cannot be read as one: the rules would see nothing and call it clean."""
    raw = read_regular(path, MAX_DOC_BYTES)
    if raw is None or b"\0" in raw:
        return None
    try:
        text = raw.decode("utf-8")
    except UnicodeDecodeError:
        return None
    control = sum(1 for c in text if unicodedata.category(c) == "Cc" and c not in "\t\n\r")
    return text if control <= len(text) * MAX_CONTROL_SHARE else None


def report_facts():
    """(names of reinvest's gaps, its session count per kind of work) from its report, or None without a usable one."""
    raw = read_regular(sources.CACHE / "report.md", MAX_JSON_BYTES)
    parts = re.split(r"^## ", raw.decode("utf-8", "replace"), flags=re.M) if raw else []
    section = {p.split("\n", 1)[0].strip(): p.split("\n", 1)[-1] for p in parts}
    if "Gaps" not in section or "Kinds of work" not in section:
        return None
    kinds = re.findall(r"^- (.+?): (\d+) sessions,", section["Kinds of work"], re.M)
    gaps = {name for name, _ in kinds if re.search(rf"^- {re.escape(name)}: ", section["Gaps"], re.M)}
    return gaps, {name: int(n) for name, n in kinds}


def clean_group(raw, facts):
    """One work group as proposals need it, or None when reinvest would not propose it: the same gate as find_gaps."""
    if not isinstance(raw, dict) or not raw.get("delegable"):
        return None
    name, task = clean_text(raw.get("name")), clean_text(raw.get("task"))
    ids = raw.get("session_ids")
    sessions = len(ids) if isinstance(ids, list) else 0
    if facts:
        if raw.get("name") not in facts[0]:
            return None
        sessions = facts[1].get(raw.get("name"), sessions)
    if not name or not task or sessions < MIN_SESSIONS:
        return None
    queries = raw.get("queries")
    return {"name": name, "task": task, "sessions": sessions,
            "queries": [q for q in sources.search_queries(raw) if q.strip()] if isinstance(queries, list) else []}


def report_verdicts():
    """(names Claude passed, {name: reason} for those it refused) in reinvest's last report."""
    raw = read_regular(sources.CACHE / "report.md", MAX_JSON_BYTES)
    parts = re.split(r"^## ", raw.decode("utf-8", "replace"), flags=re.M) if raw else []
    section = {p.split("\n", 1)[0].strip(): p.split("\n", 1)[-1] for p in parts}
    passed = set(re.findall(r"^- (.+?) \(", section.get("Verified candidates", ""), re.M))
    refused = dict(re.findall(r"^- (.+?): (.*)$", section.get("Rejected", ""), re.M))
    return passed - set(refused), refused


def denied_docs():
    """The hashes of documents a verifier flagged once: they stay flagged."""
    saved = read_json(sources.CACHE / "denied.json")
    return set(saved) if isinstance(saved, dict) else set()


def all_groups():
    """The work groups of reinvest's last run: a damaged file or entry is dropped, never a crash."""
    saved = read_json(sources.CACHE / "groups.json")
    raw = saved.get("groups") if isinstance(saved, dict) else saved
    if not isinstance(raw, list):
        return []
    facts = report_facts()
    groups = [g for g in (clean_group(r, facts) for r in raw) if g]
    return sorted(groups, key=lambda g: -g["sessions"])


def load_groups():
    return all_groups()[:MAX_PROPOSALS]


def unused():
    rows = next(its for label, its in m.suggested_groups() if label == "suggest to prune")
    return [{**r, "Verb": "prune"} for r in sorted(rows, key=lambda r: -r["Tokens"])]


def by_weight(cost, pool):
    """Removed items that add up to about the skill's weight: as many as it takes, not one for one. Fewer than two is no combination."""
    picked, total = [], 0
    for r in pool:
        # No single item may cover it alone: the point is a combination.
        if 0 < r["Tokens"] <= cost * 0.6 and total + r["Tokens"] <= cost * 1.3:
            picked.append(r)
            total += r["Tokens"]
        if total >= cost * COVERS:
            break
    return picked if len(picked) >= MIN_COMBO else []


def words(text):
    return {w for w in re.findall(r"[a-z]{4,}", text.lower())}


def by_topic(task, pool, cost):
    """Removed items about the same work, adding up to about the skill's weight by the same rule as by_weight."""
    wanted = words(task)
    scored = [(len(wanted & words(f"{r['Name']} {r.get('Description') or ''}")), r) for r in pool]
    close = sorted((s for s in scored if s[0] >= MIN_SHARED_WORDS), key=lambda s: -s[0])
    return by_weight(cost, [r for _, r in close[:MAX_TOPIC_MATCHES]])


def same_name(name):
    """'File Organizer', 'file-organizer' and 'x:file_organizer' are one skill."""
    return re.sub(r"[^a-z0-9]", "", canon(name))


def clean_result(raw):
    """One search result from a registry nothing here controls, or None. The page opened must be the skill shown."""
    if not isinstance(raw, dict) or not isinstance(raw.get("id"), str) or not (SAFE_ID.fullmatch(raw["id"]) or HOST_ID.fullmatch(raw["id"])):
        return None
    name, installs = clean_text(raw.get("name")), raw.get("installs") or 0
    if type(installs) is not int or installs < 0:
        return None
    tail = re.sub(r"[^a-z0-9]", "", raw["id"].rsplit("/", 1)[-1].lower())
    if re.sub(r"[^a-z0-9]", "", name.lower()) != tail:
        return None
    return {"id": raw["id"], "name": name, "installs": installs}


def search_results(query):
    saved = read_json(sources._cache_file("search", query.lower(), ".json"))  # by exact query: c++ never reads c#'s
    raw = saved.get("skills") if isinstance(saved, dict) else None
    return [r for r in (clean_result(x) for x in raw) if r] if isinstance(raw, list) else []


def online_choices(group, installed):
    """What Claude read and passed for this work in reinvest's last run, best fit first. Nothing else is proposed:
    a skill it refused, or whose document is flagged or stale, is not shown."""
    saved = read_json(sources.CACHE / "verified.json")
    rows = [r for r in (saved if isinstance(saved, list) else []) if isinstance(r, dict) and r.get("gap") == group.get("name")
            and isinstance(r.get("id"), str) and isinstance(r.get("name"), str) and same_name(r["name"]) not in installed]
    out = []
    for r in sorted(rows, key=lambda r: (-int(r.get("fit") or 0), -int(r.get("installs") or 0))):
        if not security_check(r)[1]:
            out.append({"id": r["id"], "name": r["name"], "installs": int(r.get("installs") or 0), "cost": max(1, int(r.get("cost") or 1)),
                        "safety": f"{CLAUDE_PASSED} · fit {int(r.get('fit') or 0)}/5 · {sources.one_line(str(r.get('why') or ''))}"})
    return out


def security_check(hit):
    """(text, is_bad) from reinvest's rules on the SKILL.md cached by its last run. No copy read, no claim made."""
    path = sources._cache_file("doc", hit["id"], ".md")
    doc = read_doc(path)
    if doc is None or len(sources.frontmatter(doc).get("description", "")) < MIN_DESCRIPTION:
        return NOT_CHECKED, True
    found = sources.risks(doc)
    if found:
        return "⚠ " + ", ".join(found), True
    age = time.time() - os.path.getmtime(path)
    if age >= sources.SEARCH_TTL:  # upstream may have changed since: past reinvest's own limit it counts as not checked
        return STALE.format(int(age // 86400)), True
    passed, refused = report_verdicts()
    reason = refused.get(hit.get("name"))
    flagged = reason is not None and (reason.startswith("security") or "too long" in reason)
    if flagged or hashlib.sha256(doc.encode("utf-8", "replace")).hexdigest() in denied_docs():
        return "⚠ Claude refused it", True
    return (CLAUDE_PASSED if hit.get("name") in passed else REGEX_ONLY), False


def known_names():
    """Everything the user has or chose to remove: the list's own rows hide reap itself and never hold what was pruned."""
    return {same_name(str(r.get("Name") or "")) for r in DATA["Rows"]} | {same_name(str(e.get("name") or "")) for e in PRUNED}


def find_opener():
    candidates = (os.path.join(d, n) for n in OPENER_NAMES for d in OPENER_DIRS)
    return next((p for p in candidates if os.access(p, os.X_OK)), None)


OPENER = find_opener()


def open_page(url):
    """Hands a skills.sh address to the browser and says what became of it. Its output never reaches the screen."""
    if any(os.environ.get(v) for v in REMOTE_VARS):
        return f"remote session, open it on your machine: {url}"
    if not OPENER:
        return f"no browser opener here: {url}"
    try:
        subprocess.run([OPENER, url], check=True, timeout=OPEN_TIMEOUT, stdin=subprocess.DEVNULL,
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        return f"opened {url}"
    except (OSError, subprocess.SubprocessError) as e:
        return f"could not open {url}: {e}"


def proposals(view, pool):
    pool, out, proposed = list(pool), [], frozenset()
    installed = known_names()
    for g in load_groups():
        hits = [h for h in online_choices(g, installed) if h["id"] not in proposed]
        proposed = proposed | {h["id"] for h in hits}
        # Every way for the same work: what Claude passed online, best first, and always one written from your sessions.
        for found in [*hits, None]:
            cost = found["cost"] if found else max(1, len(g["task"]) // 4)
            combo = by_weight(cost, pool) if view == "weight" else by_topic(g["task"], pool, cost)
            pool = [r for r in pool if r not in combo]
            out.append({"name": g["name"], "task": g["task"], "sessions": g["sessions"], "cost": cost, "combo": combo,
                        "skill": found["name"] if found else None, "id": found["id"] if found else None,
                        "page": f"{SKILLS_SH}/{found['id']}" if found and SAFE_ID.fullmatch(found["id"]) else None,
                        "installs": found["installs"] if found else 0,
                        "safety": found["safety"] if found else CHECKED_LATER})
    return out


def sheet(w):
    """The confirm screen's frame at the window's size right now: h, width, left edge, row of the separator."""
    h, wd = w.getmaxyx()
    pw = min(76, wd - 6)
    return h, pw, (wd - pw) // 2, h - 7


def sheet_top(w, x, pw):
    m.put(w, 1, x, f"{'REINVEST':^{pw}}", curses.A_BOLD)
    m.put(w, 2, x, "┄" * pw, m.DIM())


def plan_line(p):
    """What the row will do. An online skill is named by its id: the name is the registry's to choose, the id is checked."""
    return ("install", p["id"]) if p["id"] else ("write", f"a skill for {p['name']}")


def draw_capped(w, x, pw, top, room, lines):
    """Every line that fits under the top, and how many more there are when they do not."""
    shown = lines if len(lines) <= room else lines[: max(0, room - 1)]
    for n, (text, attr) in enumerate(shown):
        m.put(w, top + n, x, pad(text, pw), attr)
    if len(shown) < len(lines) and room > 0:
        m.put(w, top + len(shown), x, f"+{len(lines) - len(shown)} more"[:pw], m.DIM())


def draw_confirm(w, chosen, amount, left):
    """Returns False when the window cannot show the question: then y must not answer it."""
    if too_small(w, SCREEN_MIN, "too small to confirm: esc cancels"):
        return False
    w.erase()
    h, pw, x, y = sheet(w)
    sheet_top(w, x, pw)
    lines = [(f"{pad(f'{plan_line(p)[0]:<8}{plan_line(p)[1]}', max(0, pw - 16))} {amount(p['cost']):>15}", m.DIM()) for p in chosen]
    draw_capped(w, x, pw, 3, y - 3, lines)
    m.put(w, y, x, "┄" * pw, m.DIM())
    m.put(w, y + 1, x, cut(f"spends {amount(sum(p['cost'] for p in chosen))} · credit left {amount(left)}", pw), curses.A_BOLD)
    online = sum(1 for p in chosen if p["id"])
    if online:
        m.put(w, y + 2, x, cut(f"{online} by other people: enter on the list opens the page", pw), curses.color_pair(61) | curses.A_BOLD)
    m.put(w, y + 3, x, f"Add {len(chosen)}?  [y/n]", curses.A_BOLD)
    w.refresh()
    return True


def progress_timeline(chosen):
    """(row, step, share of the row done when the step starts, share the step is worth) for every step of every row."""
    plan = []
    for n, p in enumerate(chosen):
        steps = STEPS_ONLINE if p["id"] else STEPS_WRITTEN
        plan += [(n, step, s / len(steps), 1 / len(steps)) for s, step in enumerate(steps)]
    return plan


def done_cost(chosen, row, row_share):
    """The tokens reinvested so far: finished rows at their cost, plus the running row's part of its own."""
    return sum(p["cost"] for p in chosen[:row]) + (chosen[row]["cost"] * row_share if row < len(chosen) else 0)


def draw_progress(w, chosen, row, step, row_share, spin, amount):
    w.erase()
    _, pw, x, y = sheet(w)
    sheet_top(w, x, pw)
    room = max(1, y - 3)
    start = m.window(row, room, len(chosen))
    for slot, (n, p) in enumerate(list(enumerate(chosen))[start: start + room]):
        how, what = plan_line(p)
        if n < row:
            m.put(w, 3 + slot, x, pad(f"✓ {how:<8}{what}", pw), curses.color_pair(62) | curses.A_BOLD)
        elif n == row:
            suffix = f" · {step}…"
            head = f"{SPINNER[spin % len(SPINNER)]} {how:<8}"
            m.put(w, 3 + slot, x, pad(head + cut(what, max(0, pw - cells(head) - cells(suffix))) + suffix, pw), curses.A_BOLD)
        else:
            m.put(w, 3 + slot, x, pad(f"  {how:<8}{what}", pw), m.DIM())
    cost = done_cost(chosen, row, row_share)
    m.grad(w, y + 1, x, pw, cost / max(1, sum(p["cost"] for p in chosen)), m.HEALTH)  # progress ends green, as every progress bar does
    m.put(w, y + 2, x, pad(f"{amount(int(cost))} reinvested · esc stops, what is done stays", pw), m.DIM())
    w.refresh()


def run_steps(w, chosen, amount, flip):
    """Staged: one row at a time goes through its steps, the bar fills, the number counts up. Returns how many finished.
    Paced by the clock: only esc and tab mean something here, and no key can hurry it."""
    timeline = progress_timeline(chosen)
    started = time.monotonic()
    row = 0
    curses.flushinp()
    try:
        while True:
            elapsed = (time.monotonic() - started) * 1000
            index = int(elapsed // STEP_MS)
            if index >= len(timeline):
                return len(chosen)
            row, step, begun, worth = timeline[index]
            if not too_small(w, SCREEN_MIN, "esc stops, what is done stays"):
                draw_progress(w, chosen, row, step, begun + worth * (elapsed % STEP_MS) / STEP_MS, int(elapsed // FRAME_MS), amount)
            k = read_key(w, FRAME_MS)
            if k == 27:
                return row
            if k == 9:
                flip()
    except KeyboardInterrupt:  # ctrl-c stops like esc: what is done stays, and it says so
        return row


def draw_result(w, chosen, finished, amount):
    if too_small(w, SCREEN_MIN, "any key goes back"):
        return
    w.erase()
    _, pw, x, y = sheet(w)
    sheet_top(w, x, pw)
    lines = [(f"{'✓' if n < finished else ' '} {how:<8}{what}", curses.color_pair(62) | curses.A_BOLD if n < finished else m.DIM())
             for n, (how, what) in enumerate(map(plan_line, chosen))]
    draw_capped(w, x, pw, 3, y - 3, lines)
    cost = done_cost(chosen, finished, 0)
    m.grad(w, y + 1, x, pw, cost / max(1, sum(p["cost"] for p in chosen)), m.HEALTH)
    m.put(w, y + 2, x, pad(f"{amount(cost)} reinvested", pw), m.DIM())
    if finished:
        m.put(w, y + 3, x, cut(f"✓ {finished} of {len(chosen)} added (demo: nothing touched) · any key", pw), curses.color_pair(62) | curses.A_BOLD)
    else:
        m.put(w, y + 3, x, cut("stopped, nothing added · any key", pw), curses.A_BOLD)
    w.refresh()


def install_screen(w, chosen, amount, flip, left):
    """Same step as the receipt: what will happen, then an answer. Nothing runs without the y.
    Returns (what to say, how many of the chosen finished)."""
    if not m.yes_or_no(w, lambda: draw_confirm(w, chosen, amount, left), draw_confirm(w, chosen, amount, left)):
        return "demo: cancelled, still here", 0
    finished = run_steps(w, chosen, amount, flip)
    curses.flushinp()  # keys pressed during the run do not dismiss a result nobody saw
    while True:
        draw_result(w, chosen, finished, amount)
        k = read_key(w)
        if k == 9:
            flip()
        elif k not in ANSWER_KEYS:
            break
    return (f"demo: {finished} of {len(chosen)} added, nothing touched" if finished else "demo: stopped, nothing added"), finished


def short_names(pool):
    """The name to show for each removed item: short, unless two different items would read the same."""
    count = Counter(r["Name"].split(":")[-1] for r in pool)
    return {r["Name"]: r["Name"].split(":")[-1] if count[r["Name"].split(":")[-1]] == 1 else r["Name"] for r in pool}


def fit_names(names, width):
    """Whole names joined by +, then how many did not fit: the combination is the reward, none vanishes silently."""
    for keep in range(len(names), 0, -1):
        text = " + ".join(names[:keep]) + (f" + {len(names) - keep} more" if keep < len(names) else "")
        if cells(text) <= width:
            return text
    return cut(f"{names[0]} + {len(names) - 1} more" if len(names) > 1 else names[0], width)


def earned_line(combo, labels, width):
    """Which removed items earned this, in the words of what happened to them: muting is not pruning."""
    by_verb = {}
    for r in combo:
        by_verb.setdefault(m.GERUND.get(r.get("Verb"), "pruning"), []).append(labels[r["Name"]])
    room = max(0, width // max(1, len(by_verb)))
    return " · ".join(f"earned by {gerund}: {fit_names(names, max(0, room - len(gerund) - 12))}" for gerund, names in by_verb.items())


def draw_row(w, y, p, is_cursor, is_taken, is_added, amount, labels, left):
    wd = w.getmaxyx()[1]
    width = wd - 6
    base = m.HL() if is_cursor else 0
    on = is_taken or is_added
    installs = f"{p['installs']:,}".replace(",", ".")
    title = f"online · {p['skill']} · {installs} installs" if p["skill"] else "✎ written for you"
    box = " ✓  added ·" if is_added else "[✓]" if is_taken else "[ ]"
    uses = f"uses {amount(p['cost'])}"
    line = f"{box} {title} · for {p['name']}, {p['sessions']} sessions"
    m.put(w, y, 3, f"{pad(line, max(0, width - cells(uses) - 1))} {uses}", base or (curses.color_pair(62) if on else curses.A_BOLD))
    short_by = p["cost"] - left
    part = " (part of it)" if p["combo"] and sum(r["Tokens"] for r in p["combo"]) < p["cost"] * COVERS else ""
    earned = earned_line(p["combo"], labels, wd - 14 - len(part)) + part if p["combo"] else "from your credit"
    if short_by > 0 and not on:  # say it before the key is pressed, not after
        earned = f"not enough credit: {amount(short_by)} short"
    m.put(w, y + 1, 9, cut(earned, wd - 14), curses.color_pair(62) if on else m.DIM())
    for n, text in enumerate(wrap_cells(p["safety"], wd - 14)[:ROW_LINES - 2]):  # a reason is read whole, not cut off
        m.put(w, y + 2 + n, 9, text, m.DIM())


def list_layout(h, wd, task):
    """How many rows and task lines fit between the header and the notice: rows first, the list has to stay usable."""
    room = (h - 4) - LIST_TOP
    lines = wrap_cells(task[: wd * (TASK_LINES + 1)], wd - 10)[:TASK_LINES]  # this runs every frame: only what can show
    rows = max(1, (room - len(lines)) // ROW_HEIGHT)
    return rows, lines[: max(0, room - rows * ROW_HEIGHT)]


def draw_list(w, items, i, taken, added, amount, labels, left):
    h, wd = w.getmaxyx()
    rows, task = list_layout(h, wd, items[i]["task"])
    start = m.window(i, rows, len(items))
    for slot, n in enumerate(range(start, min(start + rows, len(items)))):
        draw_row(w, LIST_TOP + slot * ROW_HEIGHT, items[n], n == i, n in taken, n in added, amount, labels, left)
    for n, line in enumerate(task):
        m.put(w, LIST_TOP + rows * ROW_HEIGHT + n, 3, line)


def draw_header(w, added, frac, unit, left, priced_share, has_credit, top_of=""):
    wd = w.getmaxyx()[1]
    room = lambda at: max(0, wd - at - 4)  # noqa: E731
    if unit == "money":
        value = added * priced_share * m.PRICE * m.R["rate"]
        above, words = m.big_money_words("reinvested a month")
        text = m.money_figure(value)
        figure = text if m.big_width(text) <= wd - 5 - len(words) else f"{m.R['sym']}{m.short(value)}"
        m.put(w, 0, 3, above, m.DIM())
    else:
        words = "tokens reinvested every session"
        figure = m.tokens(added) if m.big_width(m.tokens(added)) <= wd - 5 - len(words) else m.short(added)
    end = m.big(w, 1, 3, figure, frac)
    m.put(w, 1, end + 2, words, curses.A_BOLD)
    m.put(w, 2, end + 2, cut(f"skills for the work you repeat{top_of} (demo)", room(end)), m.DIM())
    if has_credit:
        m.put(w, 3, end + 2, cut(f"credit left {left}, earned by what you removed", room(end)), curses.color_pair(62) | curses.A_BOLD)
        m.grad(w, 5, 3, wd - 7, frac, m.RAMP)
    else:
        m.put(w, 3, end + 2, cut("nothing removed yet: removing items earns credit", room(end)), curses.A_BOLD)
    tabs = (f" {m.R['sym']} money ", " tokens ")  # placed as the list places them: a longer symbol takes its room
    token_x = wd - 4 - len(tabs[1])
    m.put(w, 0, token_x - 1 - len(tabs[0]), tabs[0], m.HL() if unit == "money" else m.DIM())
    m.put(w, 0, token_x, tabs[1], m.HL() if unit == "tokens" else m.DIM())


def empty_screen(w):
    """Nothing qualifies yet, the state of anyone new: nothing to take, and nothing to fall over."""
    while True:
        if not too_small(w, SCREEN_MIN):
            w.erase()
            wd = w.getmaxyx()[1]
            m.put(w, 1, 3, cut("Nothing to propose yet", wd - 6), curses.A_BOLD)
            for n, line in enumerate(wrap_cells("reinvest has no finished run that shows work you repeat.", wd - 6)):
                m.put(w, 3 + n, 3, line, m.DIM())
            m.put(w, w.getmaxyx()[0] - 2, 3, cut("esc back", wd - 6), m.DIM())
            w.refresh()
        if read_key(w) in (27, ord("q")):
            return


def eased(shown, target):
    """The number on screen moving towards its target, by the list's own constants, so the two end alike."""
    step = shown + (target - shown) * m.EASE
    return float(target) if abs(target - step) < m.SNAP or step == shown else step


def on_screen(shown, target):
    return math.ceil(shown) if shown < target else math.floor(shown)


def picker(w, view, removed, unit):
    """A list with no use for the mouse, and its text can be selected."""
    with without_mouse():
        return _picker(w, view, removed, unit)


def _picker(w, view, removed, unit):
    """removed: what was removed, each row's Tokens being what its removal really saves, its Verb how. That is the credit."""
    credit = sum(r["Tokens"] for r in removed)
    priced_share = sum(r["Tokens"] * m.reach(r) for r in removed) / max(1, credit)  # priced as the receipt priced them
    items = proposals(view, removed)
    if not items:
        return empty_screen(w)
    labels = short_names(removed)
    # taken waits for the apply key; added is done and cannot be undone or redone
    fits, kept = [], 0
    for n, p in enumerate(items):
        if p["name"] in CREDIT_MEMORY["taken"] and kept + p["cost"] <= credit:
            fits, kept = fits + [n], kept + p["cost"]
    taken, added = frozenset(fits), frozenset()
    i, notice = 0, ""
    shown = 0.0  # same motion as the list: the number eases
    total = sum(r["Tokens"] for r in m.wasted())
    groups_found = len(all_groups())
    top_of = f": top {len(items)} of {groups_found}" if groups_found > len(items) else ""
    opened, pending, last_open = queue.SimpleQueue(), [], {}

    def amount(tok):
        if unit != "money":
            return f"~{m.tokens(tok)} tok"
        return m.money(tok * priced_share)

    def flip():
        nonlocal unit, notice
        if not m.PRICE:
            notice = "no price known: tokens only"
            return
        unit = "tokens" if unit == "money" else "money"

    def spent(chosen):
        return sum(items[n]["cost"] for n in chosen)

    while True:
        target = spent(taken | added)
        shown = eased(shown, target)
        pending = [t for t in pending if t.is_alive()]
        while not opened.empty():
            notice = opened.get()
        small = too_small(w, SCREEN_MIN)
        if not small:
            h, wd = w.getmaxyx()
            count = on_screen(shown, target)
            # The list's own bar, on its own scale, picking up where the removals left it: now it climbs.
            frac = (total - credit + shown) / max(1, total)
            w.erase()
            left = credit - spent(taken | added)
            draw_header(w, count, frac, unit, amount(credit - count), priced_share, credit > 0, top_of)
            draw_list(w, items, i, taken, added, amount, labels, left)
            stuck = not taken and all(items[n]["cost"] > left for n in range(len(items)) if n not in added)
            line = notice or (NO_CREDIT if stuck else "")
            if line:
                m.put(w, h - 4, 3, cut(line, wd - 6), curses.color_pair(61) | curses.A_BOLD)
            m.put(w, h - 2, 3, cut(f"↑↓ move · space take · enter open its page · tab {m.R['sym']}/tok · esc back", wd - 6), m.DIM())
            if taken:
                label = f" {apply_key()} add {len(taken)} "
                m.put(w, h - 3, wd - len(label) - 4, label, m.HL())
            w.refresh()
        k = read_key(w, FRAME_MS if shown != target or pending else -1)
        if k == -1 or k in ANSWER_KEYS:
            continue
        if small and k not in (27, ord("q")):
            continue
        notice = ""
        if k == curses.KEY_UP:
            i = max(0, i - 1)
        elif k == curses.KEY_DOWN:
            i = min(len(items) - 1, i + 1)
        elif k == 9:
            flip()
        elif is_apply(k, m.SHIFT_HELD, bool(taken)):
            if not taken:
                notice = "space takes a skill first" if credit - spent(added) >= min((items[n]["cost"] for n in range(len(items)) if n not in added), default=0) else NO_CREDIT
                continue
            chosen = sorted(taken)
            notice, finished = install_screen(w, [items[n] for n in chosen], amount, flip, credit - spent(taken | added))
            done = frozenset(chosen[:finished])
            added, taken = added | done, taken - done
        elif k in ENTER:
            url = items[i]["page"]
            if not url and items[i]["id"]:
                notice = f"{items[i]['id']} has no page on skills.sh"
                continue
            now, before = time.monotonic(), last_open.get(url, -OPEN_REPEAT_S)
            last_open = {**last_open, url: now}
            if not url:
                notice = "written from your sessions: it has no web page"
            elif now - before < OPEN_REPEAT_S:
                notice = f"opening {url}"
            else:
                # Off the input path: a slow opener must not freeze the keys, nor leave them to be read under another Shift.
                pending = pending + [threading.Thread(target=lambda u=url: opened.put(open_page(u)), daemon=True)]
                pending[-1].start()
                notice = f"opening {url}"
        elif k == ord(" "):
            after = taken ^ {i}
            need = spent(after | added) - credit
            if i in added:
                notice = "already added"
            elif need > 0:
                notice = NO_CREDIT if credit <= 0 else f"not enough credit: ~{m.tokens(need)} tok short"
            else:
                taken = frozenset(after)
        elif k in (27, ord("q")):
            CREDIT_MEMORY["taken"] = frozenset(items[n]["name"] for n in taken)
            return


def main(w, view):
    m.setup()
    m.tints()
    enter(w)  # the list's own terminal setup: one way to read keys, signals, mouse reports and leave
    try:
        # ponytail: staged as if every "suggest to prune" was removed; from the receipt the credit is what it removed
        picker(w, view, unused(), "tokens" if "subscription" in m.PLAN or not m.PRICE else "money")
    except (KeyboardInterrupt, EOFError):
        pass
    finally:
        leave()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Mockups of reinvest as a credit. Read-only.")
    parser.add_argument("view", nargs="?", default="weight", choices=VIEWS)
    parser.add_argument("--self-check", action="store_true", help=argparse.SUPPRESS)
    args = parser.parse_args()
    if args.self_check:
        from reinvest_check import self_check  # its own file: the rules' checks are not part of the screens
        self_check()
    else:
        os.environ.setdefault("ESCDELAY", "25")
        launch(main, args.view)
