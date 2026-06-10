package report

import "time"

// Verdict labels for inventory rows.
const (
	VerdictKeep   = "KEEP"
	VerdictReap   = "REAP"
	VerdictReview = "REVIEW"
	VerdictInfo   = "-" // report-only categories (hooks, prose)
)

// Reason strings appended after verdict when meaningful.
const (
	ReasonUsed      = "used"
	ReasonUserKeep  = "keep"
	ReasonTiny      = "tiny"
	ReasonGrace     = "grace"
	ReasonNeedsData = "needs-data"
	ReasonUnused    = "unused"
)

// VerdictOpts configures the smart verdict logic.
type VerdictOpts struct {
	MinSessions int
	GraceDays   int
	MinTokens   int
	WindowDays  int
	Cutoff      time.Time
}

// Verdict decides whether an item is safe to reap. Returns (verdict, reason).
//
// Logic (in order):
//  1. Used → KEEP(used)
//  2. Tiny weight → KEEP(tiny)
//  3. Installed within grace period → REVIEW(grace)
//  4. No sessions → REVIEW(needs-data)
//  5. Proportional minSessions if installed mid-window
//  6. Not enough sessions → REVIEW(needs-data)
//  7. Unused with enough evidence → REAP(unused)
func Verdict(uses, sessions, tokens int, installedAt time.Time, opts VerdictOpts) (string, string) {
	// 1. Used — keep items that have been invoked
	if uses > 0 {
		return VerdictKeep, ReasonUsed
	}

	// 2. Tiny weight — negligible cost, not worth pruning
	//    Only applies when we actually measured the weight (tokens > 0).
	//    Items with 0 tokens have unknown weight (e.g. MCP servers).
	if tokens > 0 && tokens < opts.MinTokens {
		return VerdictKeep, ReasonTiny
	}

	// 3. Grace period — installed too recently to make a judgment.
	//    The last opts.GraceDays of the window are the grace zone.
	windowEnd := opts.Cutoff.AddDate(0, 0, opts.WindowDays)
	graceCutoff := windowEnd.AddDate(0, 0, -opts.GraceDays)
	if !installedAt.IsZero() && installedAt.After(graceCutoff) {
		return VerdictReview, ReasonGrace
	}

	// 4. No sessions at all → no evidence
	if sessions == 0 {
		return VerdictReview, ReasonNeedsData
	}

	// 5. Proportional threshold
	//    If item was installed mid-window, scale minSessions down
	//    proportionally so newer items aren't unfairly REAP'd.
	effectiveMinSessions := opts.MinSessions
	if !installedAt.IsZero() {
		windowStart := opts.Cutoff
		daysSinceInstall := installedAt.Sub(windowStart).Hours() / 24
		if daysSinceInstall > 0 && daysSinceInstall < float64(opts.WindowDays) {
			ratio := daysSinceInstall / float64(opts.WindowDays)
			scaled := int(float64(opts.MinSessions) * ratio)
			if scaled < 1 {
				scaled = 1
			}
			if scaled < effectiveMinSessions {
				effectiveMinSessions = scaled
			}
		}
	}

	// 6. Not enough sessions for this item's age
	if sessions < effectiveMinSessions {
		return VerdictReview, ReasonNeedsData
	}

	// 7. Unused with sufficient evidence → safe to reap
	return VerdictReap, ReasonUnused
}
