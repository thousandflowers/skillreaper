package report

import (
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// Explanation is the structured "why" behind one item's verdict.
type Explanation struct {
	Name         string `json:"name"` // canonical category:name
	Category     string `json:"category"`
	Verdict      string `json:"verdict"`
	Reason       string `json:"reason"`
	VerdictLabel string `json:"verdict_label"` // e.g. "REAP(unused)"
	Summary      string `json:"summary"`

	TokenWeight int `json:"token_weight"`
	DescChars   int `json:"desc_chars"`
	ListedChars int `json:"listed_chars,omitempty"`
	Sessions    int `json:"sessions"`
	Uses        int `json:"uses"`
	Errors      int `json:"errors"`

	LastSeen         string `json:"last_seen"`
	LastAttempt      string `json:"last_attempt,omitempty"`
	Installed        string `json:"installed"`
	InstalledAgeDays int    `json:"installed_age_days,omitempty"`
	GraceDays        int    `json:"grace_days"`
	GraceExpired     bool   `json:"grace_expired"`

	KeepList    bool   `json:"keep_list"`
	ClaudeMD    bool   `json:"claude_md_referenced"`
	ToolSurface string `json:"tool_surface"`

	// MUTE-specific (omitted from JSON when not applicable).
	FiringRate    float64 `json:"firing_rate,omitempty"`
	MuteThreshold float64 `json:"mute_threshold,omitempty"`
	Muted         bool    `json:"muted,omitempty"`

	Recommendation string `json:"recommendation"`
}

// ExplainInput carries the tuning and live state the explanation needs but
// that the Row does not itself hold.
type ExplainInput struct {
	MinSessions   int
	GraceDays     int
	MuteThreshold float64
	WindowDays    int
	Muted         bool // is this skill currently muted
	ClaudeMDRef   bool // referenced in a CLAUDE.md
	Now           time.Time
}

// MatchItems returns every inventory row matching arg, which is either a
// bare/plugin name ("graphify", "ecc:plan") or a "category:name" form whose
// prefix is a known category. Matching is by exact key or the bare suffix of a
// namespaced key (so "plan" matches "ecc:plan").
func MatchItems(r *Report, arg string) []Row {
	cat, name := splitCategory(arg)
	var whole, leaf []Row
	for _, row := range r.Rows {
		if cat != "" && string(row.Category) != cat {
			continue
		}
		switch i := strings.LastIndexByte(row.Name, ':'); {
		case row.Name == name, row.DiskName() == name: // as shown, or as it is on disk
			whole = append(whole, row)
		case i >= 0 && row.Name[i+1:] == name:
			leaf = append(leaf, row)
		}
	}
	if len(whole) > 0 { // one item's whole name is not also the tail of another's
		return whole
	}
	return leaf
}

func splitCategory(arg string) (cat, name string) {
	if i := strings.IndexByte(arg, ':'); i > 0 {
		switch arg[:i] {
		case string(scan.CatSkill), string(scan.CatAgent), string(scan.CatMCP),
			string(scan.CatHook), string(scan.CatProse):
			return arg[:i], arg[i+1:]
		}
	}
	return "", arg
}

// CanonicalName is the category:name key used as the explanation header and in
// follow-up command suggestions.
func CanonicalName(row Row) string {
	return string(row.Category) + ":" + row.Name
}

// BuildExplanation assembles the full explanation for one row.
func BuildExplanation(row Row, sessions int, in ExplainInput) Explanation {
	e := Explanation{
		Name:         CanonicalName(row),
		Category:     string(row.Category),
		Verdict:      row.Verdict,
		Reason:       row.Reason,
		VerdictLabel: verdictLabel(row),
		Summary:      summaryFor(row),
		TokenWeight:  row.Tokens,
		DescChars:    row.DescChars,
		ListedChars:  row.ListedChars,
		Sessions:     sessions,
		Uses:         row.Uses,
		Errors:       row.ErrorCount,
		LastSeen:     lastSeen(row),
		Installed:    dateOrUnknown(row.InstalledAt),
		GraceDays:    in.GraceDays,
		KeepList:     row.Kept,
		ClaudeMD:     in.ClaudeMDRef,
		ToolSurface:  permDisplay(row),
	}

	if !row.InstalledAt.IsZero() {
		age := in.daysSinceInstall(row)
		e.InstalledAgeDays = age
		e.GraceExpired = age >= in.GraceDays
	}
	if row.ErrorCount > 0 {
		e.LastAttempt = dateOrNever(row.LastAttempt)
	}
	if row.Verdict == VerdictMute {
		if sessions > 0 {
			e.FiringRate = float64(row.Uses) / float64(sessions)
		}
		e.MuteThreshold = in.MuteThreshold
		e.Muted = in.Muted
	}
	e.Recommendation = recommendationFor(row, sessions, in)
	return e
}

func verdictLabel(row Row) string {
	if row.Reason != "" && row.Verdict != VerdictInfo {
		return row.Verdict + "(" + row.Reason + ")"
	}
	return row.Verdict
}

func summaryFor(row Row) string {
	switch row.Reason {
	case ReasonUnused:
		return "zero uses in the evidence window"
	case ReasonBroken:
		return fmt.Sprintf("invoked but only ever errored (%d error(s)) — broken, not merely cold", row.ErrorCount)
	case ReasonUsed:
		return fmt.Sprintf("used %d time(s) in the window", row.Uses)
	case ReasonHeavyRare:
		return "used, but heavy and fired in too few sessions to justify the per-session weight"
	case ReasonTiny:
		return "token weight below the prune floor — negligible cost"
	case ReasonGrace:
		return "installed too recently — still inside the grace period"
	case ReasonNeedsData:
		return "not enough sessions yet to make a confident call"
	case ReasonNoEvidence:
		if row.EvidenceNote != "" {
			return "no usage evidence: " + row.EvidenceNote
		}
		return "no usage evidence for this platform"
	case ReasonUserKeep:
		return "manually protected via the keep-list"
	case ReasonClaudeMDRef:
		return "referenced in CLAUDE.md — protected from pruning"
	case ReasonMuted:
		return "you muted it, so it cannot fire on its own and its uses prove nothing"
	case ReasonHookLoaded:
		return "read into the session by a hook — in use without being invoked"
	case ReasonAgentLoaded:
		return "an agent names it under `skills:` and loads it when it starts — in use without being invoked"
	case ReasonMainAgent:
		return "settings.json runs the session itself as this agent — in use without being invoked"
	case ReasonSkillAgent:
		return "a skill names it under `agent:` and runs in it — in use whenever that skill is"
	case ReasonUsedElsewhere:
		return "another tool that loads this folder used it — Claude Code's sessions alone do not show it"
	default:
		if row.Reason == "" {
			return "measured and reported, not judged"
		}
		return row.Reason
	}
}

func recommendationFor(row Row, sessions int, in ExplainInput) string {
	if row.PruneLost {
		return "prune lost: you pruned it and a plugin update brought it back. run: reap prune " + scan.ShellArg(CanonicalName(row)) + " to take it again"
	}
	switch row.Verdict {
	case VerdictReap:
		if row.ViaLink != "" {
			return "unused, reached through a link to " + row.ViaLink + ". run: reap prune " + scan.ShellArg(CanonicalName(row)) + " takes the link and leaves that"
		}
		if row.Category == scan.CatSkill && !row.Removable && !IsPluginSource(row.Source) {
			return "unused, in a shared folder other tools read. run: reap prune " + scan.ShellArg(CanonicalName(row)) + " takes it from them too"
		}
		if !row.Removable && !IsPluginSource(row.Source) {
			return "unused, but reap cannot edit it. remove it by hand from " + row.Path
		}
		if !row.Removable {
			return "unused, inside a plugin. disable it via /plugin, or run: reap prune " + scan.ShellArg(CanonicalName(row)) + " (an update can bring it back)"
		}
		if row.Reason == ReasonBroken {
			return "broken — safe to prune. run: reap prune"
		}
		return "safe to prune. run: reap prune"
	case VerdictMute:
		return "run: reap mute " + scan.ShellArg(CanonicalName(row))
	case VerdictKeep:
		return "kept — nothing to do"
	case VerdictReview:
		switch row.Reason {
		case ReasonGrace:
			left := in.GraceDays
			if !row.InstalledAt.IsZero() { // to the day the verdict lets go: a date in the future pushes it out
				ends := row.InstalledAt.AddDate(0, 0, in.GraceDays)
				left = max(0, int(math.Ceil(ends.Sub(in.Now).Hours()/24)))
			}
			return fmt.Sprintf("wait — %d day(s) left in the %d-day grace period", left, in.GraceDays)
		case ReasonNoEvidence:
			return "judge it yourself: reap has no record of its use on this platform"
		case ReasonUsedElsewhere:
			return "used by another tool from this same folder, so reap will not condemn it. if you are sure, run: reap prune " + scan.ShellArg(CanonicalName(row))
		case ReasonRare:
			// More sessions change nothing here: it is held for a use on record.
			return "used before this window, so reap will not condemn it. if you are sure, run: reap prune " + scan.ShellArg(CanonicalName(row))
		default:
			if !row.HistoricalLast.IsZero() {
				// Enough sessions would only turn this into "held for a use on record".
				return "used before this window, so reap will not condemn it. if you are sure, run: reap prune " + scan.ShellArg(CanonicalName(row))
			}
			need := minSessionsFor(row.InstalledAt, in.Now.AddDate(0, 0, -in.WindowDays), in.MinSessions, in.WindowDays) - sessions
			if need < 1 {
				need = 1
			}
			return fmt.Sprintf("collect ~%d more session(s) until the verdict stabilizes", need)
		}
	default:
		return "report-only item — no verdict"
	}
}

func (in ExplainInput) daysSinceInstall(row Row) int {
	if row.InstalledAt.IsZero() {
		return 0
	}
	d := int(in.Now.Sub(row.InstalledAt).Hours() / 24)
	if d < 0 {
		return 0
	}
	return d
}

// lastSeen is the most that is known of when an item last fired. Uses with no
// date are not "never", and a use before the window has its date from history.
func lastSeen(row Row) string {
	switch {
	case !row.LastUsed.IsZero():
		return row.LastUsed.Format("2006-01-02")
	case row.Uses > 0:
		return "unknown (its events have no date)"
	case !row.HistoricalLast.IsZero():
		return row.HistoricalLast.Format("2006-01-02") + " (before this window)"
	}
	return "never"
}

func dateOrNever(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Format("2006-01-02")
}

func dateOrUnknown(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return t.Format("2006-01-02")
}

// RenderWhyJSON writes the explanation as indented JSON (never colored).
func RenderWhyJSON(w io.Writer, e Explanation) error {
	return WriteJSON(w, e)
}

// RenderWhy writes the human-readable explanation block. The verdict line is
// colored by label when color is enabled.
func RenderWhy(w io.Writer, e Explanation, color bool) {
	paint := painter(color)
	field := func(label, value string) {
		fmt.Fprintf(w, "%-13s%s\n", label, value)
	}

	fmt.Fprintf(w, "\n%s\n\n", paint(cBold, e.Name))
	field("verdict", paintVerdict(paint, e))
	field("reason", e.Summary)

	weight := "? (weight unknown)"
	switch {
	case e.Category == string(scan.CatMCP) && e.TokenWeight > 0:
		weight = fmt.Sprintf("~%d tok  (tool names + instructions, last session that loaded it)", e.TokenWeight)
	case e.Category == string(scan.CatMCP):
		weight = "? (never seen loading in a session, so never measured)"
	case e.ListedChars > 0 && e.ListedChars < e.DescChars:
		weight = fmt.Sprintf("~%d tok  (%d of the description's %d chars, as the last session listed it)", e.TokenWeight, e.ListedChars, e.DescChars)
	case e.DescChars > 0 || e.TokenWeight > 0:
		weight = fmt.Sprintf("~%d tok  (description: %d chars)", e.TokenWeight, e.DescChars)
	}
	field("token weight", weight)
	field("sessions", fmt.Sprintf("%d total in window", e.Sessions))
	field("uses", fmt.Sprintf("%d", e.Uses))

	if e.Errors > 0 {
		field("errors", fmt.Sprintf("%d", e.Errors))
		field("last attempt", e.LastAttempt)
	}
	field("last seen", e.LastSeen)

	if e.Installed != "unknown" {
		field("installed", fmt.Sprintf("%s (%d days ago)", e.Installed, e.InstalledAgeDays))
		grace := "within grace period"
		if e.GraceExpired {
			grace = "grace period expired"
		}
		field("", fmt.Sprintf("%s (grace: %d days)", grace, e.GraceDays))
	} else {
		field("installed", "unknown")
	}

	if e.Verdict == VerdictMute {
		field("uses/session", fmt.Sprintf("%.0f%% (below %.0f%% threshold)",
			e.FiringRate*100, e.MuteThreshold*100))
	}

	field("keep-list", yesNo(e.KeepList))
	field("claude-md", referencedStr(e.ClaudeMD))
	if e.Verdict == VerdictMute {
		field("muted", yesNo(e.Muted))
	}

	fmt.Fprintf(w, "→ %s\n", e.Recommendation)
}

// paintVerdict colors the verdict label like the main report: REAP red
// (bright when broken), MUTE/REVIEW yellow, KEEP green.
func paintVerdict(paint func(code, s string) string, e Explanation) string {
	switch e.Verdict {
	case VerdictReap:
		if e.Reason == ReasonBroken {
			return paint(cBRed, e.VerdictLabel)
		}
		return paint(cRed, e.VerdictLabel)
	case VerdictMute:
		return paint(cYell, e.VerdictLabel)
	case VerdictReview:
		return paint(cYell, e.VerdictLabel)
	case VerdictKeep:
		return paint(cGreen, e.VerdictLabel)
	default:
		return e.VerdictLabel
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func referencedStr(b bool) string {
	if b {
		return "referenced"
	}
	return "not referenced"
}
