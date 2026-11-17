package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// "codex:review" is a skill of that name in Claude Code, and it is review in
// Codex. With both there, which one was meant is not reap's to guess: it used
// to take the Codex one, --yes included.
func TestANameThatIsAlsoASelectorIsNotGuessed(t *testing.T) {
	home := deleteHome(t)
	own := putSkill(t, home, "codex:review")
	other := filepath.Join(home, ".codex", "skills", "review", "SKILL.md")
	mustWriteFile(t, other, "---\nname: review\ndescription: some words here\n---\nbody\n")
	backdated(t, other)

	code, out := reapRun("prune", "--yes", "codex:review")

	if code == 0 || !lexist(own) || !lexist(other) {
		t.Fatalf("prune codex:review with both there: %d\n%s", code, out)
	}
	for _, form := range []string{"claude-code:skill:codex:review", "codex:skill:review"} {
		if !strings.Contains(out, form) {
			t.Errorf("the answer does not name %s:\n%s", form, out)
		}
	}
	if code, out := reapRun("prune", "--yes", "claude-code:skill:codex:review"); code != 0 || lexist(own) || !lexist(other) {
		t.Errorf("the form it gave, pasted: %d\n%s", code, out)
	}
}

// The forms listed for a name several items share are pasted as printed.
func TestTheFormsListedForASharedNameAreOneWordEach(t *testing.T) {
	home := deleteHome(t)
	putSkill(t, home, "sp ace")
	other := filepath.Join(home, ".codex", "skills", "sp ace", "SKILL.md")
	mustWriteFile(t, other, "---\nname: sp ace\ndescription: some words here\n---\nbody\n")
	backdated(t, other)

	_, out := reapRun("prune", "sp ace")

	if !strings.Contains(out, "  "+scan.ShellArg("claude-code:skill:sp ace")+"  ") {
		t.Errorf("the form is not quoted:\n%s", out)
	}
}

// keepThenYes answers a question with yes, after putting the item on the keep
// list: another terminal ran `reap keep` while the question was open.
type keepThenYes struct {
	t    *testing.T
	name string
	done bool
}

func (k *keepThenYes) Read(p []byte) (int, error) {
	if k.done {
		return 0, io.EOF
	}
	k.done = true
	if code, out := reapRun("keep", k.name); code != 0 {
		k.t.Errorf("keep %s: %d %s", k.name, code, out)
	}
	return copy(p, "y\n"), nil
}

// The keep list is read again once the question is answered and the lock is
// held. `reap keep` had said "this item will be excluded", and the plan made
// before it went ahead all the same: prune, mute, and delete for good.
func TestAnItemKeptWhileTheQuestionWasOpenIsLeftAlone(t *testing.T) {
	for _, verb := range []string{"prune", "mute", "delete"} {
		t.Run(verb, func(t *testing.T) {
			home := deleteHome(t)
			file := filepath.Join(home, ".claude", "skills", "mine", "SKILL.md")
			before := readFile(t, file)
			var out, errOut bytes.Buffer

			code := run([]string{verb, "mine"}, &keepThenYes{t: t, name: "mine"}, &out, &errOut)

			if code == 0 || !lexist(file) || readFile(t, file) != before {
				t.Errorf("%s of an item kept meanwhile: %d\n%s%s", verb, code, out.String(), errOut.String())
			}
		})
	}
}

// A scan that could read nothing is not a clean stack, for a snapshot either:
// stored, `reap diff` then reported the whole stack as cleaned up.
func TestAFailedScanIsNotStoredAsASnapshot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".cfg"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	putSkill(t, home, "only")
	skills := filepath.Join(home, ".claude", "skills")
	if err := os.Chmod(skills, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(skills, 0o755) })
	if code, _ := reapRun("--json", "--no-banner"); code == 0 {
		t.Skip("this user reads the folder all the same")
	}

	for _, cmd := range [][]string{{"snapshot"}, {"share"}, {"gap"}, {"prune", "--yes"}} {
		if code, out := reapRun(cmd...); code == 0 || !strings.Contains(out, "A failed scan, not a clean stack.") {
			t.Errorf("reap %v on a folder that cannot be read: %d\n%s", cmd, code, firstN(out, 300))
		}
	}
	if _, out := reapRun("snapshot", "--list"); strings.TrimSpace(out) != "" {
		t.Errorf("a snapshot was stored: %s", out)
	}
}

// The second command of that error is pasted too, and for a name that begins
// with a dash it needs "--" before it and the folder's flag before that.
func TestTheUnmuteCommandInsideAnErrorWorksForANameWithADash(t *testing.T) {
	home := deleteHome(t)
	putSkill(t, home, "-dead")
	dir := filepath.Join(home, ".claude")
	reapRun("mute", "--yes", "--", "-dead")
	reapRun("prune", "--yes", "--", "-dead")

	_, out := reapRun("unmute", "--claude-dir", dir, "--", "-dead")

	if want := "then reap unmute --claude-dir " + scan.ShellArg(dir) + " -- -dead"; !strings.Contains(out, want) {
		t.Errorf("unmute of a skill in quarantine lacks %q:\n%s", want, out)
	}
}

// A selector in front of the name does not change what the item is.
func TestMuteOfAServerNamedBySelectorSaysWhatItIs(t *testing.T) {
	deleteHome(t)

	_, out := reapRun("mute", "mcp:srv")

	if !strings.Contains(out, "srv is an MCP server, and mute works on skills and agents") {
		t.Errorf("mute mcp:srv: %s", out)
	}
}

// A name that is one item's whole name is not also the tail of another's:
// reap why answers for that item, as prune does.
func TestWhyTakesAWholeNameBeforeANamespacedOne(t *testing.T) {
	home := deleteHome(t)
	putSkill(t, home, "codex:review")
	other := filepath.Join(home, ".codex", "skills", "review", "SKILL.md")
	mustWriteFile(t, other, "---\nname: review\ndescription: some words here\n---\nbody\n")
	backdated(t, other)

	for _, typed := range []string{"skill:review", "codex:review"} {
		if code, out := reapRun("why", typed); code != 0 {
			t.Errorf("why %s: %d %s", typed, code, out)
		}
	}
}

// --claude-json is checked by what reads it: version and the hook commands do not.
func TestAMistypedConfigPathDoesNotStopWhatNeverReadsIt(t *testing.T) {
	home := deleteHome(t)
	missing := filepath.Join(home, "nope.json")
	for _, cmd := range [][]string{{"version"}, {"install-hook", "--dry-run"}} {
		if code, out := reapRun(append(cmd, "--claude-json", missing)...); code != 0 {
			t.Errorf("reap %v --claude-json <missing>: %d %s", cmd, code, out)
		}
	}
}

// The answer to a misplaced reinvest is one line and what the flag check said,
// not a hundred lines of usage with the answer at the bottom.
func TestTheAnswerToAMisplacedReinvestIsNotBuriedInUsage(t *testing.T) {
	_, out := reapRun("--days", "5", "reinvest", "--max", "2")

	if n := strings.Count(out, "\n"); n > 2 {
		t.Errorf("%d lines:\n%s", n, firstN(out, 400))
	}
}

// A delete that fails on the way has deleted what it reached: "not done" alone
// reads as "nothing happened".
func TestADeleteThatFailsHalfwaySaysSo(t *testing.T) {
	home := deleteHome(t)
	skills := filepath.Join(home, ".claude", "skills")
	if err := os.Chmod(skills, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(skills, 0o755) })

	code, out := reapRun("delete", "--yes", "mine")

	if lexist(filepath.Join(skills, "mine", "SKILL.md")) {
		t.Skip("this user cannot be stopped by a read-only folder")
	}
	if code == 0 || !strings.Contains(out, "part of it is deleted already") {
		t.Errorf("delete in a read-only folder: %d\n%s", code, out)
	}
}
