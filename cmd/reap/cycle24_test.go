package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/hook"
	"github.com/thousandflowers/skillreaper/internal/menu"
	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/scan"
)

// A bulk prune said a skill whose whole skills folder is a link is "pruned
// only by name", and by name the prune is refused: the note and --json now
// say what reap why says.
func TestABulkPruneDoesNotSendAFolderLinkToAPruneByName(t *testing.T) {
	linked := report.Row{Item: scan.Item{Category: scan.CatSkill, Name: "a3", Path: "/h/.claude/skills/a3/SKILL.md", FolderLink: true}, Verdict: report.VerdictReap}
	shared := report.Row{Item: scan.Item{Category: scan.CatSkill, Name: "sh", Path: "/h/.agents/skills/sh/SKILL.md"}, Verdict: report.VerdictReap}
	var b, errOut bytes.Buffer
	(pruneOutcome{notRemovable: []report.Row{linked, shared}}).writeJSON(&b, &errOut, false)
	notes := strings.Join(unremovableNotes([]report.Row{linked, shared}), "\n")

	for name, said := range map[string]string{"notes": notes, "json": b.String()} {
		if strings.Count(said, "pruned only by name") != 1 || !strings.Contains(said, "the whole skills folder is a link") || !strings.Contains(said, "remove by hand") {
			t.Errorf("%s:\n%s", name, said)
		}
	}
}

// A named mute that cannot be done says so before it asks: the answer to a
// yes was an error.
func TestANamedMuteThatCannotBeDoneIsRefusedBeforeTheQuestion(t *testing.T) {
	home := deleteHome(t)
	mustWriteFile(t, filepath.Join(home, ".claude/skills/nodesc/SKILL.md"), "---\nname: nodesc\n---\nbody\n")
	mustWriteFile(t, filepath.Join(home, ".claude/skills/byhand/SKILL.md"), "---\nname: byhand\ndescription: some words here\ndisable-model-invocation: true\n---\nbody\n")
	var first, firstErr bytes.Buffer
	if code := run([]string{"mute", "mine", "--yes"}, strings.NewReader(""), &first, &firstErr); code != 0 {
		t.Fatalf("mute mine: exit %d\n%s%s", code, first.String(), firstErr.String())
	}
	for name, want := range map[string]string{
		"nodesc": "no description to strip",
		"mine":   "already muted: mine",
		"byhand": "runs only by hand",
	} {
		var out, errOut bytes.Buffer
		code := run([]string{"mute", name}, strings.NewReader("y\n"), &out, &errOut)
		said := out.String() + errOut.String()
		if code != 1 || !strings.Contains(errOut.String(), want) || strings.Contains(said, "Mute "+name+"?") || strings.Contains(said, "terminal") {
			t.Errorf("mute %s, exit %d:\n%s", name, code, said)
		}
	}
}

// The first nudge has no last check to be "more than", and a later one names
// what rose, not a count that stood still.
func TestTheNudgeComparesOnlyWithACheckThatWasMade(t *testing.T) {
	spoke := hook.NudgeState{LastNudgeAt: time.Now().AddDate(0, 0, -30), LastReapCount: 5, LastMuteCount: 2}
	for _, c := range []struct {
		dead, mute int
		was        hook.NudgeState
		want       string
	}{
		{7, 0, hook.NudgeState{}, "skillreaper: 7 unused items to prune. Run `reap` to review."},
		{7, 3, hook.NudgeState{}, "skillreaper: 7 unused items to prune and 3 items to mute. Run `reap` to review."},
		{7, 2, spoke, "skillreaper: 7 unused items to prune, more than at the last check. Run `reap` to review."},
		{5, 3, spoke, "skillreaper: 3 items to mute, more than at the last check. Run `reap` to review."},
		{7, 3, spoke, "skillreaper: 7 unused items to prune and 3 items to mute, more than at the last check. Run `reap` to review."},
	} {
		if got := nudgeText(c.dead, c.mute, c.was); got != c.want {
			t.Errorf("nudgeText(%d, %d) = %q, want %q", c.dead, c.mute, got, c.want)
		}
	}
}

// reap why speaks of CLAUDE.md for a skill: a server of a name that is in it
// read "claude-md referenced" under REAP, safe to prune. And a skill whose
// name is there as a sentence's word is told so.
func TestWhyReadsCLAUDEMDForSkillsAndSaysAPlainWord(t *testing.T) {
	home := deleteHome(t)
	mustWriteFile(t, filepath.Join(home, ".claude", "CLAUDE.md"), "- **srv** — the server\nalways mine the logs first\n")
	var server, skill, errOut bytes.Buffer
	run([]string{"why", "mcp:srv"}, strings.NewReader(""), &server, &errOut)
	if said := server.String(); strings.Contains(said, "claude-md") || !strings.Contains(said, "srv") {
		t.Errorf("why mcp:srv:\n%s%s", said, errOut.String())
	}
	run([]string{"why", "skill:mine"}, strings.NewReader(""), &skill, &errOut)
	if said := skill.String(); !strings.Contains(said, "a plain word, which protects nothing") || !strings.Contains(said, "reap keep skill:mine") {
		t.Errorf("why skill:mine:\n%s%s", said, errOut.String())
	}
}

// An agent is one file, and the menu's mute writes it anew: the delete that
// followed was refused as changed by someone else. Through a link too.
func TestMenuDeleteOfAnAgentAfterItsOwnMute(t *testing.T) {
	home := deleteHome(t)
	if err := os.Symlink("ag.md", filepath.Join(home, ".claude", "agents", "aglink.md")); err != nil {
		t.Skip(err)
	}
	rows := reportRows(t)
	applier := &menuApplier{dir: filepath.Join(home, ".claude"), seen: stampsOf(rows)}

	if out := applier.Apply([]menu.Pick{{Row: rowOf(t, rows, "claude-code", "agent", "aglink"), Verb: menu.Mute}}); out[0].Err != nil {
		t.Fatalf("mute through the link: %v", out[0].Err)
	}
	if out := applier.Apply([]menu.Pick{{Row: rowOf(t, rows, "claude-code", "agent", "ag"), Verb: menu.Delete}}); out[0].Err != nil {
		t.Errorf("delete of the agent the menu muted: %v", out[0].Err)
	}
}

// The undo printed for one mute undoes that mute. Two tools had a skill of one
// name, both muted one after the other: "reap unmute dup" brought both back.
func TestTheUndoOfAMuteNamesTheCopyThatWasMuted(t *testing.T) {
	home := deleteHome(t)
	var first, second, errOut bytes.Buffer
	if code := run([]string{"mute", "claude-code:skill:dup", "--yes"}, strings.NewReader(""), &first, &errOut); code != 0 {
		t.Fatalf("exit %d: %s%s", code, first.String(), errOut.String())
	}
	if code := run([]string{"mute", "codex:skill:dup", "--yes"}, strings.NewReader(""), &second, &errOut); code != 0 {
		t.Fatalf("exit %d: %s%s", code, second.String(), errOut.String())
	}
	want := "Undo: reap unmute " + filepath.Join(home, ".codex", "skills", "dup", "SKILL.md")
	if said := second.String(); !strings.Contains(said, want) || strings.Contains(said, "Undo: reap unmute dup\n") {
		t.Errorf("the undo of the second mute:\n%s", said)
	}
	var one bytes.Buffer
	run([]string{"mute", "copied", "--yes"}, strings.NewReader(""), &one, &errOut)
	if said := one.String(); !strings.Contains(said, "Undo: reap unmute copied\n") {
		t.Errorf("a name only one item has is undone by name:\n%s", said)
	}
}

// A named mute that would save nothing is refused like the others: a skill
// whose description is empty was rewritten for "~0 tok/session saved".
func TestANamedMuteThatSavesNothingIsRefused(t *testing.T) {
	home := deleteHome(t)
	path := filepath.Join(home, ".claude/skills/emp/SKILL.md")
	mustWriteFile(t, path, "---\nname: emp\ndescription: \"\"\n---\nbody\n")
	var out, errOut bytes.Buffer
	code := run([]string{"mute", "emp", "--yes"}, strings.NewReader(""), &out, &errOut)
	if b, _ := os.ReadFile(path); code != 1 || !strings.Contains(errOut.String(), "save nothing") || !strings.Contains(string(b), "description") {
		t.Errorf("exit %d:\n%s%s\nfile: %q", code, out.String(), errOut.String(), b)
	}
}
