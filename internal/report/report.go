// Package report joins inventory and usage evidence into verdicts and
// renders the result as ANSI text, JSON, or Markdown.
package report

import (
	"sort"
	"strings"
	"time"

	"github.com/thousandflowers/skillreaper/internal/cost"
	"github.com/thousandflowers/skillreaper/internal/scan"
	"github.com/thousandflowers/skillreaper/internal/usage"
)

// Opts tunes report generation.
type Opts struct {
	MinSessions  int
	GraceDays    int // items installed this recently → REVIEW(grace)
	MinTokens    int // items below this token weight → KEEP(tiny)
	PricePerMTok float64
	Cutoff       time.Time // start of the evidence window
	WindowDays   int
	KeepSet      map[string]bool // items manually marked as keep
	// EvidenceBlind holds platform IDs whose session transcripts could not
	// be parsed (e.g. OpenCode's SQLite store, or a platform with no session
	// files). Zero-use items from these platforms are held at REVIEW instead
	// of REAP, because absence of evidence is not evidence of absence.
	EvidenceBlind map[string]bool
	// ClaudeMDLines holds the non-comment lines of every detected CLAUDE.md.
	// A skill whose name appears here is held at KEEP(claude-md-ref).
	ClaudeMDLines []string
	// MuteThreshold / MuteMinTokens drive the MUTE verdict for skills: a used
	// skill heavier than MuteMinTokens that fires in fewer than MuteThreshold
	// of sessions is a MUTE candidate. MuteThreshold == 0 disables MUTE.
	MuteThreshold float64
	MuteMinTokens int
}

// Row is one inventory item with its evidence and verdict.
type Row struct {
	scan.Item
	Uses        int
	LastUsed    time.Time
	Verdict     string
	Reason      string    // why this verdict was assigned
	Tokens      int       // estimated per-session weight
	Kept        bool      // user manually marked this as keep
	ErrorCount  int       // invocations that errored in the window
	LastAttempt time.Time // most recent attempt (success or error)
}

// Report is the full result of a scan.
type Report struct {
	GeneratedAt          time.Time
	WindowDays           int
	Sessions             int
	MalformedLines       int
	Rows                 []Row
	DeadCount            int
	DeadTokensPerSession int
	MuteCount            int // skills flagged MUTE (heavy + rarely fired)
	MuteTokensPerSession int // per-session tokens recoverable by muting them
	DeadToolChars        int // from init-based tool-declaration tracking
	SessionsPerMonth     int
	MoneyPerMonth        float64
	Warnings             []scan.Warning
	Gap                  *Gap
	// SkillProjects maps a skill key to the projects that fired it (see
	// usage.Stats.SkillProjects). Powers the by-project view.
	SkillProjects map[string]map[string]int
}

// Build joins inventory items with usage stats and computes verdicts
// and totals.
func Build(items []scan.Item, st *usage.Stats, warns []scan.Warning, opts Opts) *Report {
	r := &Report{
		GeneratedAt:    time.Now(),
		WindowDays:     st.WindowDays,
		Sessions:       st.Sessions,
		MalformedLines: st.MalformedLines,
		DeadToolChars:  st.DeadToolChars,
		Warnings:       warns,
	}
	if st.WindowDays > 0 {
		r.SessionsPerMonth = st.Sessions * 30 / st.WindowDays
	}

	itemKey := func(it scan.Item) string {
		return strings.ToLower(string(it.Category) + ":" + it.Name)
	}

	for _, it := range items {
		row := Row{Item: it, Tokens: cost.Tokens(it.DescChars)}
		switch it.Category {
		case scan.CatSkill, scan.CatAgent, scan.CatMCP:
			row.Uses, row.LastUsed = lookupUses(st, it)
			row.ErrorCount, row.LastAttempt = lookupErrors(st, it)
			vo := VerdictOpts{
				MinSessions: opts.MinSessions,
				GraceDays:   opts.GraceDays,
				MinTokens:   opts.MinTokens,
				WindowDays:  opts.WindowDays,
				Cutoff:      opts.Cutoff,
				ErrorCount:  row.ErrorCount,
			}
			// Only skills carry a strippable description, so MUTE applies to them.
			if it.Category == scan.CatSkill {
				vo.Mutable = true
				vo.MuteThreshold = opts.MuteThreshold
				vo.MuteMinTokens = opts.MuteMinTokens
			}
			row.Verdict, row.Reason = Verdict(row.Uses, st.Sessions, row.Tokens, it.InstalledAt, vo)
			if opts.KeepSet != nil && opts.KeepSet[itemKey(it)] {
				row.Verdict = VerdictKeep
				row.Reason = ReasonUserKeep
				row.Kept = true
			}
			// CLAUDE.md reference protection — same priority as the keep-list.
			// A skill named in a CLAUDE.md is one the user relies on, so never
			// REAP or MUTE it out from under them.
			if it.Category == scan.CatSkill &&
				(row.Verdict == VerdictReap || row.Verdict == VerdictMute) &&
				scan.ClaudeMDReferences(opts.ClaudeMDLines, it.Name) {
				row.Verdict = VerdictKeep
				row.Reason = ReasonClaudeMDRef
			}
			// Absence of evidence is not evidence of absence: an item from a
			// platform whose transcripts we could not parse (e.g. OpenCode's
			// SQLite store) has no usage signal, so a zero-use REAP here would
			// be unsafe. Hold it at REVIEW and let the user decide.
			if row.Verdict == VerdictReap && opts.EvidenceBlind[it.Platform] {
				row.Verdict = VerdictReview
				row.Reason = ReasonNoEvidence
			}
		default:
			row.Verdict = VerdictInfo
		}
		switch row.Verdict {
		case VerdictReap:
			r.DeadCount++
			r.DeadTokensPerSession += row.Tokens
		case VerdictMute:
			r.MuteCount++
			r.MuteTokensPerSession += row.Tokens
		}
		r.Rows = append(r.Rows, row)
	}

	r.MoneyPerMonth = cost.MoneyPerMonth(r.DeadTokensPerSession, r.SessionsPerMonth, opts.PricePerMTok)
	r.SkillProjects = st.SkillProjects
	sortRows(r.Rows)
	r.Gap = computeGap(r.Rows)
	return r
}

// lookupUses matches an item to usage evidence. Skills invoked as
// slash commands are recorded without their plugin namespace, so a
// "plugin:skill" item also matches its bare suffix.
func lookupUses(st *usage.Stats, it scan.Item) (int, time.Time) {
	uses := st.Uses[it.Category][it.Name]
	last := st.Last[it.Category][it.Name]
	if it.Category == scan.CatSkill {
		if i := strings.LastIndexByte(it.Name, ':'); i >= 0 {
			suffix := it.Name[i+1:]
			uses += st.Uses[it.Category][suffix]
			if t := st.Last[it.Category][suffix]; t.After(last) {
				last = t
			}
		}
	}
	return uses, last
}

// lookupErrors mirrors lookupUses for errored invocations, folding the bare
// slash-command alias of a namespaced skill into the same total.
func lookupErrors(st *usage.Stats, it scan.Item) (int, time.Time) {
	errs := st.Errors[it.Category][it.Name]
	last := st.LastAttempt[it.Category][it.Name]
	if it.Category == scan.CatSkill {
		if i := strings.LastIndexByte(it.Name, ':'); i >= 0 {
			suffix := it.Name[i+1:]
			errs += st.Errors[it.Category][suffix]
			if t := st.LastAttempt[it.Category][suffix]; t.After(last) {
				last = t
			}
		}
	}
	return errs, last
}

var categoryOrder = map[scan.Category]int{
	scan.CatSkill: 0, scan.CatMCP: 1, scan.CatAgent: 2, scan.CatHook: 3, scan.CatProse: 4,
}

var verdictOrder = map[string]int{
	VerdictReap: 0, VerdictMute: 1, VerdictReview: 2, VerdictKeep: 3, VerdictInfo: 4,
}

func sortRows(rows []Row) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if categoryOrder[a.Category] != categoryOrder[b.Category] {
			return categoryOrder[a.Category] < categoryOrder[b.Category]
		}
		if verdictOrder[a.Verdict] != verdictOrder[b.Verdict] {
			return verdictOrder[a.Verdict] < verdictOrder[b.Verdict]
		}
		// Broken REAP rows display before plain unused REAP rows.
		if ab, bb := a.Reason == ReasonBroken, b.Reason == ReasonBroken; ab != bb {
			return ab
		}
		// Wider permission surface first — those are the riskiest to leave unused.
		if ra, rb := surfaceRank(a.ToolSurface), surfaceRank(b.ToolSurface); ra != rb {
			return ra > rb
		}
		if a.Tokens != b.Tokens {
			return a.Tokens > b.Tokens
		}
		return a.Name < b.Name
	})
}

// surfaceRank orders permission surfaces for sorting: unrestricted
// (ToolSurfaceAll) ranks highest, then larger tool counts.
func surfaceRank(s int) int {
	if s == scan.ToolSurfaceAll {
		return 1 << 30
	}
	return s
}
