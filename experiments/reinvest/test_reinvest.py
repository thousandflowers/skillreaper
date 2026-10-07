"""Checks for the fixes to the README's known issues. Run: python3 -m unittest test_reinvest"""
from __future__ import annotations

import io
import json
import os
import tempfile
import time
import unittest
import urllib.error
from pathlib import Path
from unittest import mock

import reinvest
import sessions
import sources
import swaps


def entry(ts: str, text: str = "please fix the release workflow") -> dict:
    return {"type": "user", "timestamp": ts, "cwd": "/tmp", "entrypoint": "cli",
            "message": {"role": "user", "content": text}}


class SessionWindow(unittest.TestCase):
    """Known issue 7: counts moved because mtime moves when Claude Code appends metadata to old transcripts."""

    def test_window_uses_last_entry_not_mtime(self):
        with tempfile.TemporaryDirectory() as d:
            now = time.time()
            iso = lambda secs_ago: time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(now - secs_ago))
            old = Path(d, "p", "old.jsonl")
            old.parent.mkdir()
            old.write_text(json.dumps(entry(iso(86400))) + "\n" + json.dumps({"type": "atis-latch"}) + "\n")
            live = Path(d, "p", "live.jsonl")
            live.write_text(json.dumps(entry(iso(60))) + "\n")
            os.utime(old, (now, now))  # touched a minute ago, like the batched metadata appends
            with mock.patch.object(sessions, "PROJECTS", Path(d)):
                got = [p.stem for p, _ in sessions.read_transcripts(120)]
        self.assertEqual(got, ["old"])


class ReapTokens(unittest.TestCase):
    """Known issue 6: weights now match reap's formula (checked against all 301 installed skills)."""

    def test_formula(self):
        doc = '---\nname: x\ndescription: "Sorts files"\n---\nbody'
        self.assertEqual(reinvest.reap_tokens("file-organizer", doc), -(-len("file-organizerSorts files") * 10 // 37))

    def test_manual_only_skill_weighs_nothing(self):
        doc = "---\ndescription: a long description\ndisable-model-invocation: true\n---\n"
        self.assertEqual(reinvest.reap_tokens("ab", doc), 0)

    def test_block_scalar_counts_as_reap_reads_it(self):
        # "ab" + "long text\n" = 12 bytes -> 4 tokens: the whole folded description, as reap now reads it
        self.assertEqual(reinvest.reap_tokens("ab", "---\ndescription: >\n  long text\n---\n"), 4)


class Verification(unittest.TestCase):
    """Known issues 4 and 5."""

    def test_kept_only_if_every_run_keeps(self):
        self.assertFalse(reinvest.combine([{"keep": True, "fit": 4}, {"keep": False, "reason": "no"}])["keep"])
        self.assertEqual(reinvest.combine([{"keep": True, "fit": 4}, {"keep": True, "fit": 2}])["fit"], 2)

    def test_best_fit_not_first(self):
        passing = [{"gap": "g", "name": "first", "fit": 2, "cost": 50},
                   {"gap": "g", "name": "better", "fit": 5, "cost": 90}]
        self.assertEqual([c["name"] for c in reinvest.best_per_gap(passing, 3)], ["better"])

    def test_verdicts_cached(self):
        cand = {"task": "t", "id": "a/b/c", "doc": "d", "name": "c", "kind": "skill", "cost": 10}
        state = {"installed": [], "used": []}
        with tempfile.TemporaryDirectory() as d, mock.patch.object(sources, "CACHE", Path(d)), \
                mock.patch("llm.verify", return_value={"keep": True, "fit": 3, "use": "u"}) as v:
            reinvest.verify_all([cand], state)
            reinvest.verify_all([cand], state)
        self.assertEqual(v.call_count, reinvest.VERIFY_RUNS)


class McpChecks(unittest.TestCase):
    """Known issues 2 and 3."""

    def test_command_runners_rejected(self):
        for doc in ("tools: run_command, list_apps", "It can execute commands or AppleScript", "an applescript runner"):
            self.assertTrue(sources.risks(doc, mcp=True), doc)
        self.assertFalse(sources.risks("tools: get_file, list_frames", mcp=True))

    def test_unknown_weight_mutes_nothing(self):
        mcp = {"gap": "g", "name": "srv", "fit": 4, "cost": None}
        built = swaps.build([mcp], [mcp], [{"name": "old", "tokens": 300}])
        self.assertEqual(built[0]["mute"], [])
        self.assertEqual(swaps.pair(None, [{"name": "old", "tokens": 300}]), [])


class Apply(unittest.TestCase):
    """Known issue 1: a failure mid-swap leaves nothing half-done."""

    def swap(self):
        c = {"kind": "skill", "name": "new", "source": "o/r", "slug": "new"}
        return {"cands": [c], "idx": 0, "mute": [{"name": "a"}, {"name": "b"}]}

    def test_failed_mute_rolls_back(self):
        calls = []
        def fake(cmd):
            calls.append(cmd)
            return "no skill or agent found: b" if cmd[:3] == ["reap", "mute", "b"] else None
        with mock.patch.object(swaps, "run", fake), mock.patch.object(swaps, "installed", return_value=True):
            log = swaps.apply_one(self.swap())
        self.assertIn(["reap", "unmute", "a"], calls)
        self.assertEqual(calls[-1][:5], ["npx", "-y", "skills", "remove", "new"])
        self.assertTrue(log[-1].endswith("nothing changed"))

    def test_install_not_for_claude_code_mutes_nothing(self):
        calls = []
        with mock.patch.object(swaps, "run", lambda cmd: calls.append(cmd)), \
                mock.patch.object(swaps, "installed", return_value=False):
            swaps.apply_one(self.swap())
        self.assertEqual(len(calls), 1)

    def test_skills_add_targets_claude_code_only(self):
        self.assertIn("claude-code", swaps.install_command(self.swap()["cands"][0]))


class WeighLater(unittest.TestCase):
    """An MCP swap mutes nothing at install; once reap has weighed the server, the next run offers the mute."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.patch = mock.patch.object(swaps, "WEIGH_LATER", Path(self.tmp.name, "weigh_later.json"))
        self.patch.start()

    def tearDown(self):
        self.patch.stop()
        self.tmp.cleanup()

    def mcp_swap(self):
        c = {"kind": "mcp", "name": "io.github.x/docs", "gap": "g", "why": "w",
             "server": {"name": "io.github.x/docs", "remotes": [{"type": "streamable-http", "url": "https://x"}]}}
        return {"cands": [c], "idx": 0, "mute": []}

    def test_installed_mcp_is_remembered_then_offered_once_weighed(self):
        with mock.patch.object(swaps, "run", return_value=None):
            swaps.apply_one(self.mcp_swap())
        pool = [{"name": "old", "tokens": 120}]
        self.assertEqual(swaps.settle({"docs": None}, pool), [])  # not weighed yet: nothing to offer, kept
        settled = swaps.settle({"docs": 118}, pool)
        self.assertEqual([x["name"] for x in settled[0]["mute"]], ["old"])
        self.assertTrue(settled[0]["cands"][0]["installed"])

    def test_uninstalled_server_is_forgotten(self):
        with mock.patch.object(swaps, "run", return_value=None):
            swaps.apply_one(self.mcp_swap())
        self.assertEqual(swaps.settle({}, [{"name": "old", "tokens": 120}]), [])
        self.assertEqual(swaps.settle({"docs": 118}, [{"name": "old", "tokens": 120}]), [])

    def test_accepting_mutes_without_installing_then_forgets(self):
        with mock.patch.object(swaps, "run", return_value=None):
            swaps.apply_one(self.mcp_swap())
        s = swaps.settle({"docs": 118}, [{"name": "old", "tokens": 120}])[0]
        calls = []
        with mock.patch.object(swaps, "run", lambda cmd: calls.append(cmd)):
            swaps.apply_one(s)
        self.assertEqual(calls, [["reap", "mute", "old"]])
        self.assertEqual(swaps.settle({"docs": 118}, [{"name": "old", "tokens": 120}]), [])

    def test_failed_mute_of_settled_swap_keeps_server(self):
        with mock.patch.object(swaps, "run", return_value=None):
            swaps.apply_one(self.mcp_swap())
        s = swaps.settle({"docs": 300}, [{"name": "a", "tokens": 150}, {"name": "b", "tokens": 150}])[0]
        calls = []
        def fake(cmd):
            calls.append(cmd)
            return "boom" if cmd == ["reap", "mute", "b"] else None
        with mock.patch.object(swaps, "run", fake):
            swaps.apply_one(s)
        self.assertNotIn("remove", [w for cmd in calls for w in cmd])
        self.assertEqual(len(swaps.settle({"docs": 300}, [{"name": "a", "tokens": 150}])), 1)


class ReapHint(unittest.TestCase):
    """reap's text report points at the proposals reinvest leaves in swaps.json."""

    def test_cache_is_where_reap_reads(self):
        if os.uname().sysname == "Darwin":
            self.assertEqual(sources.CACHE, Path.home() / "Library" / "Caches" / "skillreaper" / "reinvest")

    def test_proposals_saved_then_applied_ones_dropped(self):
        with tempfile.TemporaryDirectory() as d, mock.patch.object(swaps, "SWAPS", Path(d, "swaps.json")):
            mk = lambda n: {"cands": [{"name": n}], "idx": 0, "mute": [{"name": "old-" + n}]}
            a, b = mk("a"), mk("b")
            swaps.save_proposals([a, b])
            saved = json.loads(swaps.SWAPS.read_text())
            self.assertEqual([x["install"] for x in saved["swaps"]], ["a", "b"])
            self.assertIn("reinvest.py", saved["command"])
            self.assertTrue(saved["at"])
            swaps.drop_proposals([a])
            self.assertEqual([x["install"] for x in json.loads(swaps.SWAPS.read_text())["swaps"]], ["b"])


class IncrementalGroups(unittest.TestCase):
    """Groups are rebuilt from scratch weekly; in between only new sessions are placed, so proposals stay put."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.patch = mock.patch.object(sources, "CACHE", Path(self.tmp.name))
        self.patch.start()
        self.calls = []

    def tearDown(self):
        self.patch.stop()
        self.tmp.cleanup()

    def sess(self, *ids):
        return [{"id": i, "prompts": [f"prompt {i}"]} for i in ids]

    def fake(self, answers):
        def ask(prompt, body, **kw):
            self.calls.append(prompt)
            return answers.pop(0)
        return mock.patch.object(reinvest.llm, "ask_json", ask)

    def full(self):
        return [{"name": "releasing", "task": "t", "sessions": [0, 1]}]

    def test_first_run_groups_everything(self):
        with self.fake([self.full()]):
            groups = reinvest.load_groups(self.sess("a", "b"), regroup=False)
        self.assertEqual(groups[0]["session_ids"], ["a", "b"])
        self.assertEqual(self.calls, [reinvest.llm.GROUP_PROMPT])

    def test_new_session_is_placed_not_regrouped(self):
        with self.fake([self.full(), {"assign": {"0": 0}, "new": []}]):
            reinvest.load_groups(self.sess("a", "b"), regroup=False)
            groups = reinvest.load_groups(self.sess("a", "b", "c"), regroup=False)
        self.assertEqual(groups[0]["session_ids"], ["a", "b", "c"])
        self.assertEqual(self.calls, [reinvest.llm.GROUP_PROMPT, reinvest.llm.ASSIGN_PROMPT])

    def test_new_kind_of_work_becomes_a_group(self):
        with self.fake([self.full(), {"assign": {}, "new": [{"name": "blender", "task": "t", "sessions": [0, 1]}]}]):
            reinvest.load_groups(self.sess("a", "b"), regroup=False)
            groups = reinvest.load_groups(self.sess("a", "b", "c", "d"), regroup=False)
        self.assertEqual([g["session_ids"] for g in groups], [["a", "b"], ["c", "d"]])

    def test_nothing_new_asks_nothing(self):
        with self.fake([self.full()]):
            reinvest.load_groups(self.sess("a", "b"), regroup=False)
            reinvest.load_groups(self.sess("a", "b"), regroup=False)
        self.assertEqual(len(self.calls), 1)

    def test_left_out_session_not_sent_again(self):
        with self.fake([self.full(), {"assign": {}, "new": []}]):
            reinvest.load_groups(self.sess("a", "b"), regroup=False)
            reinvest.load_groups(self.sess("a", "b", "chitchat"), regroup=False)
            reinvest.load_groups(self.sess("a", "b", "chitchat"), regroup=False)
        self.assertEqual(len(self.calls), 2)

    def test_failed_call_retries_next_run(self):
        with self.fake([self.full(), None, {"assign": {"0": 0}, "new": []}]):
            reinvest.load_groups(self.sess("a", "b"), regroup=False)
            reinvest.load_groups(self.sess("a", "b", "c"), regroup=False)
            groups = reinvest.load_groups(self.sess("a", "b", "c"), regroup=False)
        self.assertEqual(groups[0]["session_ids"], ["a", "b", "c"])

    def test_week_old_or_old_format_regroups(self):
        Path(self.tmp.name, "groups.json").write_text(json.dumps([{"name": "x", "session_ids": ["a"]}]))
        with self.fake([self.full()]):
            reinvest.load_groups(self.sess("a", "b"), regroup=False)
        saved = json.loads(Path(self.tmp.name, "groups.json").read_text())
        saved["at"] -= reinvest.REGROUP_EVERY + 1
        Path(self.tmp.name, "groups.json").write_text(json.dumps(saved))
        with self.fake([self.full()]):
            reinvest.load_groups(self.sess("a", "b"), regroup=False)
        self.assertEqual(self.calls, [reinvest.llm.GROUP_PROMPT, reinvest.llm.GROUP_PROMPT])

    def test_regroup_flag_forces_it(self):
        with self.fake([self.full(), self.full()]):
            reinvest.load_groups(self.sess("a", "b"), regroup=False)
            reinvest.load_groups(self.sess("a", "b"), regroup=True)
        self.assertEqual(self.calls, [reinvest.llm.GROUP_PROMPT] * 2)


class AskJson(unittest.TestCase):
    """An object answer holding a list was cut down to the inner list, so every placement looked like no answer."""

    def run_with(self, stdout):
        res = mock.Mock(stdout=stdout)
        with mock.patch.object(reinvest.llm.subprocess, "run", return_value=res), \
                mock.patch.object(reinvest.llm.shutil, "which", return_value="claude"):
            return reinvest.llm.ask_json("p", "b")

    def test_outer_object_wins(self):
        self.assertEqual(self.run_with('ok: {"assign": {"0": 1}, "new": [{"name": "x"}]}'),
                         {"assign": {"0": 1}, "new": [{"name": "x"}]})

    def test_outer_list_wins(self):
        self.assertEqual(self.run_with('[{"name": "x", "sessions": [1]}]'), [{"name": "x", "sessions": [1]}])


class SkillsShRateLimit(unittest.TestCase):
    """Known issue 8: a 429 waits and retries instead of disabling skills.sh for the rest of the run."""

    def test_429_then_ok(self):
        err = urllib.error.HTTPError("u", 429, "slow down", {"Retry-After": "1"}, io.BytesIO())
        with mock.patch.object(sources, "http_get", side_effect=[err, b'{"ok": 1}']), \
                mock.patch.object(sources.time, "sleep") as sleep:
            self.assertEqual(sources._skillssh_json("u"), {"ok": 1})
        sleep.assert_called_once_with(1)


if __name__ == "__main__":
    unittest.main()


class WrittenSkills(unittest.TestCase):
    """A skill written from the user's own sessions, the default instead of searching online."""

    gap = {"name": "releasing", "task": "cut a release", "ids": ["a", "b"], "sessions": 2}
    sessions = [{"id": "a", "prompts": ["release it", "do not push without asking"]},
                {"id": "b", "prompts": ["ship v2"]}]
    answer = {"name": "ship-release", "description": "Cuts a release: bumps, tags, checks CI. Use when asked to "
                                                     "release or ship.", "body": "## Steps\n" + "1. step\n" * 60}

    def test_frontmatter_is_built_and_read_back(self):
        import llm
        with mock.patch("llm.ask_json", return_value=self.answer):
            slug, doc = llm.write_skill(self.gap, [["release it"]], [])
        self.assertEqual(slug, "ship-release")
        self.assertEqual(sources.frontmatter(doc)["description"], self.answer["description"])
        self.assertEqual(reinvest.reap_tokens(slug, doc),
                         -(-len((slug + self.answer["description"]).encode()) * 10 // 37))

    def test_unusable_answer_rejected(self):
        import llm
        for bad in ({**self.answer, "name": "Ship Release!"}, {**self.answer, "description": "short"},
                    {**self.answer, "body": "too thin"}, ["not", "an", "object"]):
            with mock.patch("llm.ask_json", return_value=bad):
                self.assertIsNone(llm.write_skill(self.gap, [["x"]], []), bad)

    def test_written_once_until_the_group_changes(self):
        with tempfile.TemporaryDirectory() as d, mock.patch.object(sources, "CACHE", Path(d)), \
                mock.patch("llm.ask_json", return_value=self.answer) as ask:
            first = reinvest.created_for([self.gap], self.sessions, [])
            reinvest.created_for([self.gap], self.sessions, [])
            self.assertEqual(ask.call_count, 1)
            reinvest.created_for([{**self.gap, "ids": ["a", "b", "c"]}], self.sessions, [])
            self.assertEqual(ask.call_count, 2)
        self.assertTrue(first[0]["created"])
        self.assertEqual(first[0]["gap"], "releasing (2 sessions)")

    def test_failed_write_not_cached(self):
        with tempfile.TemporaryDirectory() as d, mock.patch.object(sources, "CACHE", Path(d)), \
                mock.patch("llm.ask_json", side_effect=[None, self.answer]) as ask:
            self.assertEqual(reinvest.created_for([self.gap], self.sessions, []), [])
            self.assertEqual(len(reinvest.created_for([self.gap], self.sessions, [])), 1)
        self.assertEqual(ask.call_count, 2)

    def test_verifier_told_it_is_the_users_own(self):
        cand = {"task": "t", "id": "new:x", "doc": "d", "name": "x", "kind": "skill", "cost": 10, "created": True}
        with tempfile.TemporaryDirectory() as d, mock.patch.object(sources, "CACHE", Path(d)), \
                mock.patch("llm.verify", return_value={"keep": True, "fit": 4}) as v:
            reinvest.verify_all([cand], {"installed": [], "used": []})
        self.assertTrue(all(c.kwargs["own"] for c in v.call_args_list))


class PickerWrittenSkills(unittest.TestCase):
    """Step 2: a written skill shows as one, can be read first, and can be taken without muting anything."""

    new = {"gap": "g", "name": "ship-release", "slug": "ship-release", "fit": 4, "cost": 80, "created": True,
           "kind": "skill",
           "doc": "---\nname: ship-release\n---\nbody"}

    def test_offered_even_with_nothing_to_mute(self):
        built = swaps.build([self.new], [self.new], [])
        self.assertEqual(len(built), 1)
        self.assertEqual(built[0]["mute"], [])
        self.assertEqual(swaps.verb(self.new), "create")
        self.assertIn("written from your sessions", swaps.kind_text(self.new))
        self.assertIn("no unused skill", swaps.mute_text(built[0]))

    def test_m_keeps_the_old_skills_then_mutes_them_again(self):
        old = {"name": "old", "tokens": 80}
        built = swaps.build([self.new], [self.new], [old])
        swaps.on_key(built, 0, ord("m"))
        self.assertEqual((built[0]["mute"], built[0]["out"]), ([], 0))
        self.assertIn("m to mute old again", swaps.mute_text(built[0]))
        self.assertEqual(len(swaps.apply(built, dry_run=True)), 1)  # the write, no `reap mute` line
        swaps.on_key(built, 0, ord("m"))
        self.assertEqual((built[0]["mute"], built[0]["out"]), ([old], 80))

    def test_preview_pipes_the_whole_doc_to_the_pager(self):
        with mock.patch.dict(os.environ, {"PAGER": "less -R"}), mock.patch("subprocess.run") as run:
            swaps.preview(self.new)
        self.assertEqual(run.call_args.args[0], ["less", "-R"])
        self.assertIn("body", run.call_args.kwargs["input"])


class CreateWritten(unittest.TestCase):
    """Step 3: a written skill lives in ~/.claude/skills-reinvest and is linked into ~/.claude/skills."""

    def swap(self, mute=()):
        c = {"kind": "skill", "name": "ship-release", "slug": "ship-release", "created": True,
             "doc": "---\nname: ship-release\n---\nbody\n"}
        return {"cands": [c], "idx": 0, "mute": [{"name": n} for n in mute]}

    def home(self):
        d = tempfile.TemporaryDirectory()
        self.addCleanup(d.cleanup)
        patcher = mock.patch.dict(os.environ, {"HOME": d.name})
        patcher.start()
        self.addCleanup(patcher.stop)
        return Path(d.name)

    def test_written_then_linked_then_muted(self):
        home = self.home()
        with mock.patch.object(swaps, "run", return_value=None) as run:
            log = swaps.apply_one(self.swap(["old"]))
        link = home / ".claude" / "skills" / "ship-release"
        self.assertTrue(link.is_symlink())
        self.assertEqual(link.resolve(), (home / ".claude" / "skills-reinvest" / "ship-release").resolve())
        self.assertEqual((link / "SKILL.md").read_text(), "---\nname: ship-release\n---\nbody\n")
        self.assertEqual([c.args[0] for c in run.call_args_list], [["reap", "mute", "old"]])
        self.assertIn("created ship-release", log[0])

    def test_existing_skill_of_that_name_is_never_replaced(self):
        home = self.home()
        mine = home / ".claude" / "skills" / "ship-release"
        mine.mkdir(parents=True)
        (mine / "SKILL.md").write_text("the user's own")
        with mock.patch.object(swaps, "run") as run:
            log = swaps.apply_one(self.swap(["old"]))
        self.assertEqual((mine / "SKILL.md").read_text(), "the user's own")
        self.assertFalse((home / ".claude" / "skills-reinvest").exists())
        run.assert_not_called()
        self.assertIn("already exists", log[0])

    def test_failed_mute_removes_what_was_written(self):
        home = self.home()
        fake = lambda cmd: "boom" if cmd[:3] == ["reap", "mute", "b"] else None
        with mock.patch.object(swaps, "run", fake):
            log = swaps.apply_one(self.swap(["a", "b"]))
        self.assertFalse((home / ".claude" / "skills" / "ship-release").is_symlink())
        self.assertFalse((home / ".claude" / "skills-reinvest" / "ship-release").exists())
        self.assertTrue(log[-1].endswith("nothing changed"))

    def test_dry_run_writes_nothing(self):
        home = self.home()
        log = swaps.apply([{**self.swap(["old"]), "ok": True}], dry_run=True)
        self.assertFalse((home / ".claude").exists())
        self.assertIn("skills-reinvest/ship-release/SKILL.md", log[0])
        self.assertEqual(log[1], "  ·  reap mute old")
