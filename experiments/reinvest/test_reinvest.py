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

    def test_block_scalar_counts_as_reap_reads_it(self):
        self.assertEqual(reinvest.reap_tokens("ab", "---\ndescription: >\n  long text\n---\n"), 1)


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
