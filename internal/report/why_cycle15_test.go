package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// An item with a use on record is held for it once there are enough sessions.
// Below that number, asking for more sessions asks for nothing.
func TestWhyDoesNotAskForSessionsThatChangeNothing(t *testing.T) {
	row := Row{
		Item:    scan.Item{Category: scan.CatSkill, Name: "used-one"},
		Verdict: VerdictReview, Reason: ReasonNeedsData,
		HistoricalLast: time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC),
	}
	var b bytes.Buffer

	RenderWhy(&b, BuildExplanation(row, 9, ExplainInput{MinSessions: 10, WindowDays: 30, Now: time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)}), false)

	if said := b.String(); !strings.Contains(said, "used before this window, so reap will not condemn it") || strings.Contains(said, "more session") {
		t.Errorf("why at 9 sessions of 10, for an item used before the window:\n%s", said)
	}
}

// A backslash in a name is written so that it stays a backslash: left alone it
// un-escaped the "|" after it, and the cell split in two.
func TestMarkdownTextEscapesABackslash(t *testing.T) {
	if got, want := mdText(`a\|b <i> [l](u)`), `a\\\|b &lt;i> \[l](u)`; got != want {
		t.Errorf("mdText = %s, want %s", got, want)
	}
}

// The headline's three numbers add up: never fired is what reap condemns plus
// what it holds back. A muted skill that fired in the window was never among
// the never-fired, and is not taken off them.
func TestHeadlineAddsUpWithAMutedSkillThatFired(t *testing.T) {
	r := &Report{
		DeadCount: 1, MutedCount: 1,
		Gap: &Gap{Loaded: 4, Fired: 1},
		Rows: []Row{
			{Item: scan.Item{Category: scan.CatSkill, Name: "dead"}, Verdict: VerdictReap},
			{Item: scan.Item{Category: scan.CatSkill, Name: "muted-and-used"}, Verdict: VerdictInfo, Reason: ReasonMuted, Uses: 2},
			{Item: scan.Item{Category: scan.CatSkill, Name: "held-1"}, Verdict: VerdictReview},
			{Item: scan.Item{Category: scan.CatSkill, Name: "held-2"}, Verdict: VerdictKeep},
		},
	}

	h := NewHeadline(r)

	if h.NeverFired != 3 || h.Reaped+h.HeldBack != h.NeverFired {
		t.Errorf("never fired %d, REAP %d, held back %d", h.NeverFired, h.Reaped, h.HeldBack)
	}
}

// An item reap only reports (a hook, a prose file) has no reason: its line
// says so, where it stood empty.
func TestWhyOfAReportOnlyItemHasNoEmptyReason(t *testing.T) {
	if got := summaryFor(Row{Item: scan.Item{Category: scan.CatHook, Name: "SessionStart#0"}, Verdict: VerdictInfo}); got == "" {
		t.Error("the reason line is empty")
	}
}
