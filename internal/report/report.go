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
	PricePerMTok float64
	Cutoff       time.Time
	KeepSet      map[string]bool // items manually marked as keep
}

// Row is one inventory item with its evidence and verdict.
type Row struct {
	scan.Item
	Uses     int
	LastUsed time.Time
	Verdict  string
	Tokens   int       // estimated per-session weight
	Kept     bool      // user manually marked this as keep
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
	DeadToolChars        int // from init-based tool-declaration tracking
	SessionsPerMonth     int
	MoneyPerMonth        float64
	Warnings             []scan.Warning
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
			row.Verdict = Verdict(row.Uses, st.Sessions, opts.MinSessions, it.InstalledAt, opts.Cutoff)
			if opts.KeepSet != nil && opts.KeepSet[itemKey(it)] {
				row.Verdict = VerdictKeep
				row.Kept = true
			}
		default:
			row.Verdict = VerdictInfo
		}
		if row.Verdict == VerdictReap {
			r.DeadCount++
			r.DeadTokensPerSession += row.Tokens
		}
		r.Rows = append(r.Rows, row)
	}

	r.MoneyPerMonth = cost.MoneyPerMonth(r.DeadTokensPerSession, r.SessionsPerMonth, opts.PricePerMTok)
	sortRows(r.Rows)
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

var categoryOrder = map[scan.Category]int{
	scan.CatSkill: 0, scan.CatMCP: 1, scan.CatAgent: 2, scan.CatHook: 3, scan.CatProse: 4,
}

var verdictOrder = map[string]int{
	VerdictReap: 0, VerdictReview: 1, VerdictKeep: 2, VerdictInfo: 3,
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
		if a.Tokens != b.Tokens {
			return a.Tokens > b.Tokens
		}
		return a.Name < b.Name
	})
}
