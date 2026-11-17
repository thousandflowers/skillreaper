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
