package report

import (
	"bytes"
	"strings"
	"testing"

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
	if !strings.Contains(few, "collect ~6 more session(s)") {
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
