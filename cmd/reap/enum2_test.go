package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/scan"
)

// With nothing to bring back, restore --all and unmute --all write nothing:
// pointed at any folder by mistake they left a reaped/ folder in it, and on a
// folder that cannot be written they failed after "Nothing to restore."
func TestNothingToUndoWritesNothing(t *testing.T) {
	home := deleteHome(t)
	reaped := filepath.Join(home, ".claude", "reaped")
	for _, args := range [][]string{{"restore", "--all", "--yes"}, {"unmute", "--all", "--yes"}} {
		var out, errOut bytes.Buffer
		if code := run(args, strings.NewReader(""), &out, &errOut); code != 0 {
			t.Errorf("%v: exit %d\n%s%s", args, code, out.String(), errOut.String())
		}
		if _, err := os.Stat(reaped); err == nil {
			t.Errorf("%v left %s behind", args, reaped)
		}
	}
}

// A skill that is a link into the folder other tools read too is not taken
// by a bulk mute (as a bulk prune leaves it), and a mute by name says whose
// file it edits: the plan named the skill only.
func TestAMuteOfASharedSkillIsNeverSilent(t *testing.T) {
	linked := report.Row{Item: scan.Item{Category: scan.CatSkill, Name: "shared", Path: "/h/.claude/skills/shared/SKILL.md", Removable: true, LinkTarget: "/h/.agents/skills/shared"}, Verdict: report.VerdictReap, Tokens: 40}
	if report.MuteEligible(linked) {
		t.Error("a bulk mute takes a skill linked into the shared folder")
	}
	deleteHome(t)
	var out, errOut bytes.Buffer
	run([]string{"mute", "claude-code:skill:shared", "--yes"}, strings.NewReader(""), &out, &errOut)
	if said := out.String(); !strings.Contains(said, "other tools") || !strings.Contains(said, ".agents") {
		t.Errorf("mute by name:\n%s%s", said, errOut.String())
	}
}
