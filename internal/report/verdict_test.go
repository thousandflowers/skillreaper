package report

import (
	"testing"
	"time"
)

// Issue #54. An item used less often than the retention period has no
// surviving transcript by the time the next run looks, so a window-only view
// cannot tell it from one installed and forgotten. Pruning it is the mistake
// that costs a user's trust permanently.
func TestVerdictRareIsNotDead(t *testing.T) {
	cutoff := time.Now().AddDate(0, 0, -30)
	base := VerdictOpts{
		MinSessions: 5,
		GraceDays:   14,
		MinTokens:   3,
		WindowDays:  30,
		Cutoff:      cutoff,
	}
	installed := cutoff.AddDate(0, 0, -60) // old enough to be past grace

	t.Run("no history is still reaped", func(t *testing.T) {
		v, r := Verdict(0, 20, 500, installed, base)
		if v != VerdictReap || r != ReasonUnused {
			t.Errorf("got %s(%s), want REAP(unused)", v, r)
		}
	})

	t.Run("history spares it", func(t *testing.T) {
		opts := base
		opts.HistoricalUses = 2
		opts.HistoricalLast = cutoff.AddDate(0, 0, -20)
		v, r := Verdict(0, 20, 500, installed, opts)
		if v != VerdictReview || r != ReasonRare {
			t.Errorf("got %s(%s), want REVIEW(rare)", v, r)
		}
	})

	t.Run("history never turns a keep into a reap", func(t *testing.T) {
		opts := base
		opts.HistoricalUses = 0
		// Used inside the window: the history check must not be reached at all.
		v, r := Verdict(3, 20, 500, installed, opts)
		if v != VerdictKeep || r != ReasonUsed {
			t.Errorf("got %s(%s), want KEEP(used)", v, r)
		}
	})

	t.Run("an item that only ever errored is still broken", func(t *testing.T) {
		opts := base
		opts.ErrorCount = 4
		opts.HistoricalUses = 9
		// Step 2 comes first: concrete evidence of failure outranks history.
		v, r := Verdict(0, 20, 500, installed, opts)
		if v != VerdictReap || r != ReasonBroken {
			t.Errorf("got %s(%s), want REAP(broken)", v, r)
		}
	})

	t.Run("too little evidence still asks for more", func(t *testing.T) {
		opts := base
		opts.HistoricalUses = 5
		// Two sessions against a minimum of five: needs-data comes first, and
		// rare must not paper over a window that cannot support any verdict.
		v, r := Verdict(0, 2, 500, installed, opts)
		if v != VerdictReview || r != ReasonNeedsData {
			t.Errorf("got %s(%s), want REVIEW(needs-data)", v, r)
		}
	})
}

// The direction of the change is the safety property: consulting history can
// only move an item out of REAP, never into it.
func TestHistoryOnlyEverSpares(t *testing.T) {
	cutoff := time.Now().AddDate(0, 0, -30)
	installed := cutoff.AddDate(0, 0, -60)
	base := VerdictOpts{MinSessions: 5, GraceDays: 14, MinTokens: 3, WindowDays: 30, Cutoff: cutoff}

	for _, uses := range []int{0, 1, 7} {
		for _, hist := range []int{0, 1, 50} {
			withOut := base
			withHist := base
			withHist.HistoricalUses = hist

			a, _ := Verdict(uses, 20, 500, installed, withOut)
			b, _ := Verdict(uses, 20, 500, installed, withHist)

			if a != VerdictReap && b == VerdictReap {
				t.Errorf("uses=%d hist=%d: history turned %s into REAP", uses, hist, a)
			}
		}
	}
}
