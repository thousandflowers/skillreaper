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
// evidence. REAP requires at least one session analyzed as evidence
// (any session count above zero). Zero sessions or an item installed
// after the window cutoff produce REVIEW.
func Verdict(uses, sessions, _ int, installedAt time.Time, cutoff time.Time) string {
	if uses > 0 {
		return VerdictKeep
	}
	if sessions == 0 {
		return VerdictReview
	}
	if !installedAt.IsZero() && installedAt.After(cutoff) {
		return VerdictReview
	}
	return VerdictReap
}
