package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/scan"
	"github.com/thousandflowers/skillreaper/internal/usage"
)

// A skill held because another tool's history could not be read is not one
// reap "has no record of": Claude Code's sessions were read, and may show
// uses. reap why said "no usage evidence" above "uses 2".
func TestWhySaysWhoseHistoryIsMissing(t *testing.T) {
	quiet := usage.NewStats(30)
	quiet.Sessions = 60
	r := Build([]scan.Item{{Category: scan.CatSkill, Name: "shared-one", Platform: "claude-code", Source: "personal", DescChars: 370}}, quiet, nil, Opts{
		MinSessions: 10, Cutoff: time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
		PerPlatform:        map[string]*usage.Stats{"claude-code": quiet},
		SkillFolderReaders: map[string][]string{"claude-code": {"opencode"}},
		PlatformNames:      map[string]string{"opencode": "OpenCode"},
		HistoryUnread:      map[string]string{"opencode": "reading its SQLite history needs the sqlite3 CLI, which was not found in PATH"},
	})
	row := r.Rows[0]
	if row.UnreadReader != "OpenCode" {
		t.Fatalf("the row does not name the tool: %+v", row)
	}
	for uses, want := range map[int]string{0: "no use in this tool's sessions", 2: "used here"} {
		row.Uses = uses
		var b bytes.Buffer
		RenderWhy(&b, BuildExplanation(row, 60, ExplainInput{}), false)
		said := b.String()
		if !strings.Contains(said, want) || !strings.Contains(said, "OpenCode") || strings.Contains(said, "no record of its use on this platform") || strings.Contains(said, "no usage evidence") {
			t.Errorf("%d uses:\n%s", uses, said)
		}
	}
}

// The advice to run reap keep is not given for an item that is kept already.
func TestWhyDoesNotAdviseAKeepThatIsThere(t *testing.T) {
	skill := Row{Item: scan.Item{Category: scan.CatSkill, Name: "review", Path: "/s/review/SKILL.md"}, Verdict: VerdictKeep, Reason: ReasonUserKeep, Kept: true}
	var b bytes.Buffer
	RenderWhy(&b, BuildExplanation(skill, 10, ExplainInput{ClaudeMDPlain: true}), false)
	if said := b.String(); strings.Contains(said, "reap keep") || !strings.Contains(said, "a plain word") {
		t.Errorf("kept already:\n%s", said)
	}
}

// A bulk mute leaves out what a mute by name refuses: an item whose mute
// would save nothing was rewritten and counted among the muted.
func TestAMuteThatSavesNothingIsNotEligible(t *testing.T) {
	saves := Row{Item: scan.Item{Category: scan.CatSkill, Name: "a", Path: "/s/a/SKILL.md", Removable: true}, Verdict: VerdictReap, Tokens: 40}
	nothing := Row{Item: scan.Item{Category: scan.CatSkill, Name: "a-name-as-long-as-its-words", Path: "/s/b/SKILL.md", Removable: true}, Verdict: VerdictReap, Tokens: 3}
	if !MuteEligible(saves) || MuteEligible(nothing) {
		t.Errorf("eligible: %v and %v", MuteEligible(saves), MuteEligible(nothing))
	}
}
