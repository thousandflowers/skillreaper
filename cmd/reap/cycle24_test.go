package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/hook"
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
	if said := skill.String(); !strings.Contains(said, "a plain word, which protects nothing") || !strings.Contains(said, "reap keep mine") {
		t.Errorf("why skill:mine:\n%s%s", said, errOut.String())
	}
}
