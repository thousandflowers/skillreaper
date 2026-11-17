package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// REVIEW(rare) does not depend on how many sessions there are: the item fired
// before the window. Telling the reader to collect more sessions promises a
// change that never comes.
func TestWhyRareDoesNotAskForMoreSessions(t *testing.T) {
	row := Row{Item: scan.Item{Name: "rareskill", Category: scan.CatSkill}, Verdict: VerdictReview, Reason: ReasonRare}
	got := recommendationFor(row, 12, ExplainInput{MinSessions: 10})
	if strings.Contains(got, "more session") {
		t.Errorf("rare item is told to wait for sessions: %q", got)
	}
	if !strings.Contains(got, "before this window") || !strings.Contains(got, "reap prune skill:rareskill") {
		t.Errorf("rare item's hint does not say why it is held or how to overrule it: %q", got)
	}
	// Too few sessions is still the case where more of them change the verdict.
	few := recommendationFor(Row{Verdict: VerdictReview, Reason: ReasonNeedsData}, 4, ExplainInput{MinSessions: 10})
	if !strings.Contains(few, "collect ~6 more sessions") {
		t.Errorf("needs-data hint = %q", few)
	}
}

// An MCP server's weight is measured from transcripts, not from a description,
// and an unknown one must say why it is unknown.
func TestRenderWhyMCPWeightSource(t *testing.T) {
	for name, c := range map[string]struct {
		e    Explanation
		want string
	}{
		"measured": {Explanation{Name: "srv", Category: "mcp", TokenWeight: 18}, "~18 tok  (tool names + instructions, last session that loaded it)"},
		"unknown":  {Explanation{Name: "srv", Category: "mcp"}, "? (never seen loading in a session, so never measured)"},
		"skill":    {Explanation{Name: "s", Category: "skill", TokenWeight: 5, DescChars: 18}, "~5 tok  (description: 18 chars)"},
	} {
		var b bytes.Buffer
		RenderWhy(&b, c.e, false)
		if !strings.Contains(b.String(), c.want) {
			t.Errorf("%s: want %q in\n%s", name, c.want, b.String())
		}
	}
}

// The command a hint gives is pasted as it is: a name with a space is one word.
func TestWhyHintQuotesANameWithASpace(t *testing.T) {
	row := Row{Item: scan.Item{Name: "my skill", Category: scan.CatSkill}, Verdict: VerdictMute}
	if got := recommendationFor(row, 12, ExplainInput{}); got != "run: reap mute 'skill:my skill'" {
		t.Errorf("hint = %q", got)
	}
}

// Every label of `reap why` stands clear of its value: "uses per session" was
// wider than the column and ran into the figure ("uses per session2%").
func TestWhyLabelsStandClearOfTheirValues(t *testing.T) {
	var b bytes.Buffer
	RenderWhy(&b, Explanation{Name: "skill:alpha", Category: "skill", Verdict: VerdictMute, Installed: "unknown",
		FiringRate: 0.02, MuteThreshold: 0.2}, false)
	if !strings.Contains(b.String(), "\nuses/session 2% (below 20% threshold)\n") {
		t.Errorf("the rate line reads:\n%s", b.String())
	}
}

// The days left in the grace period are counted to the day the verdict itself
// uses: a file dated in the future stays in grace until that date plus the
// period, not for the period from today.
func TestWhyCountsGraceFromTheInstallDate(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	in := ExplainInput{GraceDays: 14, Now: now}
	for _, c := range []struct {
		installed time.Time
		want      string
	}{
		{now.AddDate(0, 0, 5), "wait — 19 days left in the 14-day grace period"},
		{now.AddDate(0, 0, -3), "wait — 11 days left in the 14-day grace period"},
		{now.AddDate(0, 0, -30), "wait — 0 days left in the 14-day grace period"},
		{time.Time{}, "wait — 14 days left in the 14-day grace period"},
	} {
		row := Row{Item: scan.Item{Name: "x", Category: scan.CatSkill, InstalledAt: c.installed}, Verdict: VerdictReview, Reason: ReasonGrace}
		if got := recommendationFor(row, 12, in); got != c.want {
			t.Errorf("installed %s: %q, want %q", c.installed.Format("2006-01-02"), got, c.want)
		}
	}
}

// The sessions still needed are counted against the minimum the verdict uses,
// which is scaled down for an item installed inside the window: the hint asked
// for 48 more where 8 turned the verdict.
func TestWhyCountsSessionsAgainstTheScaledMinimum(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	in := ExplainInput{MinSessions: 60, WindowDays: 30, Now: now}
	inside := Row{Item: scan.Item{Name: "x", Category: scan.CatSkill, InstalledAt: now.AddDate(0, 0, -20)}, Verdict: VerdictReview, Reason: ReasonNeedsData}
	if got := recommendationFor(inside, 12, in); !strings.Contains(got, "collect ~8 more sessions") {
		t.Errorf("installed 20 days ago: %q", got)
	}
	before := Row{Item: scan.Item{Name: "x", Category: scan.CatSkill, InstalledAt: now.AddDate(0, 0, -200)}, Verdict: VerdictReview, Reason: ReasonNeedsData}
	if got := recommendationFor(before, 12, in); !strings.Contains(got, "collect ~48 more sessions") {
		t.Errorf("installed before the window: %q", got)
	}
}

// Claude Code's listing can keep part of a description. The weight is what it
// kept, and the line says so: "~7 tok (description: 463 chars)" contradicts itself.
func TestWhySaysAWeightTakenFromTheListing(t *testing.T) {
	row := Row{Item: scan.Item{Category: scan.CatSkill, Name: "shared", DescChars: 463}, Tokens: 7, ListedChars: 26}
	var b bytes.Buffer

	RenderWhy(&b, BuildExplanation(row, 10, ExplainInput{}), false)

	if want := "~7 tok  (26 of the description's 463 chars, as the last session listed it)"; !strings.Contains(b.String(), want) {
		t.Errorf("why lacks %q:\n%s", want, b.String())
	}
}
