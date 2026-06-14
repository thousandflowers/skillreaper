package report

import "time"

// Verdict labels for inventory rows. Severity descends:
// REAP > MUTE > REVIEW > KEEP > INFO.
const (
	VerdictKeep   = "KEEP"
	VerdictMute   = "MUTE" // keep installed, but strip the injected description
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
	// ReasonNoEvidence marks an item whose platform keeps transcripts we
	// cannot parse yet (e.g. OpenCode's SQLite store): there is no usage
	// signal, so it is held at REVIEW rather than REAP.
	ReasonNoEvidence = "no-transcript"
	// ReasonHeavyRare marks a MUTE: the skill is used, but its injected
	// description is heavy and it fires in too few sessions to justify the
	// per-session token cost.
	ReasonHeavyRare = "heavy-rare"
	// ReasonBroken marks a REAP for a skill that was invoked but only ever
	// errored in the window — concrete evidence it is dead weight.
	ReasonBroken = "broken"
	// ReasonClaudeMDRef holds an item at KEEP because its name is referenced
	// in a CLAUDE.md file, so the user clearly relies on it.
	ReasonClaudeMDRef = "claude-md-ref"
)

// VerdictOpts configures the smart verdict logic.
type VerdictOpts struct {
	MinSessions int
	GraceDays   int
	MinTokens   int
	WindowDays  int
	Cutoff      time.Time

	// ErrorCount is how many times this item was invoked but errored in the
	// window. With uses == 0 it yields REAP(broken) instead of REAP(unused).
	ErrorCount int
	// Mutable marks an item whose description can be stripped (skills only).
	Mutable bool
	// MuteThreshold is the per-session firing rate below which a heavy,
	// still-used item becomes a MUTE candidate (0 disables MUTE).
	MuteThreshold float64
	// MuteMinTokens is the minimum injected token weight for a MUTE candidate.
	MuteMinTokens int
}

// Verdict decides whether an item is safe to reap. Returns (verdict, reason).
//
// Logic (in order):
//  1. Used → KEEP(used), or MUTE(heavy-rare) when heavy and rarely fired
//  2. Invoked but only errored → REAP(broken)
//  3. Tiny weight → KEEP(tiny)
//  4. Installed within grace period → REVIEW(grace)
//  5. No sessions → REVIEW(needs-data)
//  6. Proportional minSessions if installed mid-window
//  7. Not enough sessions → REVIEW(needs-data)
//  8. Unused with enough evidence → REAP(unused)
func Verdict(uses, sessions, tokens int, installedAt time.Time, opts VerdictOpts) (string, string) {
	// 1. Used — keep items that have been invoked, unless the item is heavy
	//    and fires in too few sessions to justify the per-session weight.
	if uses > 0 {
		if opts.Mutable && opts.MuteThreshold > 0 && opts.MuteMinTokens > 0 &&
			tokens >= opts.MuteMinTokens && sessions > 0 &&
			float64(uses) < opts.MuteThreshold*float64(sessions) {
			return VerdictMute, ReasonHeavyRare
		}
		return VerdictKeep, ReasonUsed
	}

	// 2. Broken-cold — invoked but only ever errored. The error is concrete
	//    evidence the item is dead weight, independent of session volume.
	if opts.ErrorCount > 0 {
		return VerdictReap, ReasonBroken
	}

	// 3. Tiny weight — negligible cost, not worth pruning
	//    Only applies when we actually measured the weight (tokens > 0).
	//    Items with 0 tokens have unknown weight (e.g. MCP servers).
	if tokens > 0 && tokens < opts.MinTokens {
		return VerdictKeep, ReasonTiny
	}

	// 4. Grace period — installed too recently to make a judgment.
	//    The last opts.GraceDays of the window are the grace zone.
	windowEnd := opts.Cutoff.AddDate(0, 0, opts.WindowDays)
	graceCutoff := windowEnd.AddDate(0, 0, -opts.GraceDays)
	if !installedAt.IsZero() && installedAt.After(graceCutoff) {
		return VerdictReview, ReasonGrace
	}

	// 5. No sessions at all → no evidence
	if sessions == 0 {
		return VerdictReview, ReasonNeedsData
	}

	// 6. Proportional threshold
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

	// 7. Not enough sessions for this item's age
	if sessions < effectiveMinSessions {
		return VerdictReview, ReasonNeedsData
	}

	// 8. Unused with sufficient evidence → safe to reap
	return VerdictReap, ReasonUnused
}
