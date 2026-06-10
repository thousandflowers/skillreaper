package report

import "time"

// Verdict labels for inventory rows.
const (
	VerdictKeep   = "KEEP"
	VerdictReap   = "REAP"
	VerdictReview = "REVIEW"
	VerdictInfo   = "-" // report-only categories (hooks, prose)
)

// Verdict decides whether an item is safe to reap based on usage
// evidence. Zero uses only earns REAP when the window holds enough
// sessions to be meaningful and the item predates the window.
func Verdict(uses, sessions, minSessions int, installedAt time.Time, cutoff time.Time) string {
	if uses > 0 {
		return VerdictKeep
	}
	if sessions < minSessions {
		return VerdictReview
	}
	if !installedAt.IsZero() && installedAt.After(cutoff) {
		return VerdictReview
	}
	return VerdictReap
}
