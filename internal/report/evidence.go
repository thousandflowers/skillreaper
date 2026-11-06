package report

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/thousandflowers/skillreaper/internal/clock"
	"github.com/thousandflowers/skillreaper/internal/platform"
	"github.com/thousandflowers/skillreaper/internal/scan"
	"github.com/thousandflowers/skillreaper/internal/usage"
)

// minExtrapolationDays is the shortest history a monthly figure is scaled from:
// one session today must not read as thirty a month.
const minExtrapolationDays = 3

// historyDays is how many days of a platform's sessions the counts really
// cover: the window, cut down by retention and by how old the oldest transcript
// is. Dividing by the window alone reads about 10x low with a 3-day retention.
func historyDays(st *usage.Stats, windowDays, retentionDays int) int {
	days := windowDays
	if retentionDays > 0 && retentionDays < days {
		days = retentionDays
	}
	if !st.OldestTranscript.IsZero() {
		age := int(math.Ceil(clock.Now().Sub(st.OldestTranscript).Hours() / 24))
		if age < days {
			days = max(age, min(days, minExtrapolationDays))
		}
	}
	return days
}

// sessionsPerMonth scales sessions up to thirty days and never floors an active
// user to 0: a low-frequency user (1 session in 31 days) still costs money.
func sessionsPerMonth(sessions, days int) int {
	if days <= 0 {
		return 0
	}
	n := int(math.Round(float64(sessions) * 30 / float64(days)))
	if n == 0 && sessions > 0 {
		return 1
	}
	return n
}

// foldKey is the form usage keys and item names are compared in. Transcripts can
// carry a different case from the folder a skill lives in.
func foldKey(s string) string { return strings.ToLower(s) }

// rawKeys is every name an item's firings may be recorded under, as written.
func rawKeys(it scan.Item) []string {
	return dedupe(append([]string{it.Name}, aliasKeys(it)...))
}

// evidenceKeys is rawKeys in the form a canonicalStats holds them.
func evidenceKeys(it scan.Item) []string {
	keys := rawKeys(it)
	if it.Category == scan.CatMCP {
		return keys
	}
	folded := make([]string, len(keys))
	for i, k := range keys {
		folded[i] = foldKey(k)
	}
	return dedupe(folded)
}

func dedupe(keys []string) []string {
	seen := make(map[string]bool, len(keys))
	out := keys[:0:0]
	for _, k := range keys {
		if k != "" && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// canonicalStats returns a copy of st with skill and agent keys folded, and with
// calls to servers whose own name holds the separator credited to them. mcpNames
// are the servers configured on st's platform.
func canonicalStats(st *usage.Stats, mcpNames []string) *usage.Stats {
	c := *st
	c.Uses = map[scan.Category]map[string]int{}
	c.Last = foldedLatest(st.Last)
	c.Errors = map[scan.Category]map[string]int{}
	c.LastAttempt = foldedLatest(st.LastAttempt)
	for cat, m := range st.Uses {
		c.Uses[cat] = foldedCounts(cat, m)
	}
	for cat, m := range st.Errors {
		c.Errors[cat] = foldedCounts(cat, m)
	}
	creditToolCalls(&c, st, mcpNames)
	return &c
}

func foldedCounts(cat scan.Category, m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, n := range m {
		if cat != scan.CatMCP {
			k = foldKey(k)
		}
		out[k] += n
	}
	return out
}

func foldedLatest(m map[scan.Category]map[string]time.Time) map[scan.Category]map[string]time.Time {
	out := make(map[scan.Category]map[string]time.Time, len(m))
	for cat, byKey := range m {
		dst := make(map[string]time.Time, len(byKey))
		for k, t := range byKey {
			if cat != scan.CatMCP {
				k = foldKey(k)
			}
			if t.After(dst[k]) {
				dst[k] = t
			}
		}
		out[cat] = dst
	}
	return out
}

// creditToolCalls settles which server each recorded tool call belongs to, the
// longest configured name winning. Under the default separator the parser has
// already credited the first segment, so that credit moves.
func creditToolCalls(c, st *usage.Stats, mcpNames []string) {
	if len(st.ToolCalls) == 0 {
		return
	}
	sep := st.ToolSep
	if sep == "" {
		sep = usage.ToolSepDefault
	}
	keys := make([]string, 0, len(mcpNames))
	for _, n := range mcpNames {
		keys = append(keys, usage.MCPServerKey(n))
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for tool, n := range st.ToolCalls {
		for _, key := range keys {
			if !strings.HasPrefix(tool, key+sep) {
				continue
			}
			first, _, _ := strings.Cut(tool, sep)
			if st.ToolSep == "" {
				if first == key {
					break // the parser's credit was right
				}
				c.Uses[scan.CatMCP][first] -= n
				if c.Uses[scan.CatMCP][first] <= 0 {
					delete(c.Uses[scan.CatMCP], first)
				}
			}
			c.Uses[scan.CatMCP][key] += n
			if t := st.ToolLast[tool]; t.After(c.Last[scan.CatMCP][key]) {
				c.Last[scan.CatMCP][key] = t
			}
			break
		}
	}
}

// platformEvidence is one platform's stats as Build reads them.
type platformEvidence struct {
	raw, canon *usage.Stats
}

// evidenceByPlatform prepares each platform's stats for lookup. With no
// per-platform stats given, everything is judged against st.
func evidenceByPlatform(items []scan.Item, st *usage.Stats, opts Opts) func(platformID string) platformEvidence {
	mcpNames := map[string][]string{}
	for _, it := range items {
		if it.Category == scan.CatMCP {
			mcpNames[it.Platform] = append(mcpNames[it.Platform], it.Name)
		}
	}
	var every []string
	for _, names := range mcpNames {
		every = append(every, names...)
	}
	merged := platformEvidence{st, canonicalStats(st, every)}
	cache := map[string]platformEvidence{}
	empty := usage.NewStats(st.WindowDays)
	return func(id string) platformEvidence {
		if opts.PerPlatform == nil || id == "" {
			return merged
		}
		if e, ok := cache[id]; ok {
			return e
		}
		raw := opts.PerPlatform[id]
		if raw == nil {
			raw = empty
		}
		e := platformEvidence{raw, canonicalStats(raw, mcpNames[id])}
		cache[id] = e
		return e
	}
}

// lookupHistory sums the durable record over every name the item can fire under.
func lookupHistory(history func(platformID, category, name string) (int, time.Time, bool), it scan.Item) (int, time.Time) {
	uses := 0
	var last time.Time
	for _, key := range rawKeys(it) {
		if n, t, ok := history(it.Platform, string(it.Category), key); ok {
			uses += n
			if t.After(last) {
				last = t
			}
		}
	}
	return uses, last
}

// platformTally accumulates what a report owes each platform.
type platformTally struct {
	dead, mute map[string]int
}

// applyMonthlyFigures sets the per-session and monthly totals. A token counts
// only in sessions of the platform that loads it, so the per-session figures are
// session-weighted averages, and money is summed platform by platform.
func applyMonthlyFigures(r *Report, st *usage.Stats, t platformTally, opts Opts, money func(tokens, sessionsPerMonth int) float64) {
	retentionFor := func(id string) int {
		if opts.PerPlatform == nil || id == string(platform.ClaudeCode) {
			return opts.RetentionDays
		}
		return 0
	}
	if opts.PerPlatform == nil {
		days := historyDays(st, st.WindowDays, opts.RetentionDays)
		r.SessionsPerMonth = sessionsPerMonth(st.Sessions, days)
		r.MoneyPerMonth = money(r.DeadTokensPerSession, r.SessionsPerMonth)
		r.MonthlyBasisDays = days
		return
	}
	r.PlatformSessions, r.PlatformSessionsPerMonth = map[string]int{}, map[string]int{}
	var deadWeighted, muteWeighted, totalSessions int
	r.DeadTokensPerSession, r.MuteTokensPerSession, r.SessionsPerMonth, r.MoneyPerMonth = 0, 0, 0, 0
	for id, ps := range opts.PerPlatform {
		r.PlatformSessions[id] = ps.Sessions
		if ps.Sessions == 0 {
			continue
		}
		days := historyDays(ps, st.WindowDays, retentionFor(id))
		perMonth := sessionsPerMonth(ps.Sessions, days)
		r.PlatformSessionsPerMonth[id] = perMonth
		r.SessionsPerMonth += perMonth
		r.MoneyPerMonth += money(t.dead[id], perMonth)
		if r.MonthlyBasisDays == 0 || days < r.MonthlyBasisDays {
			r.MonthlyBasisDays = days
		}
		deadWeighted += t.dead[id] * ps.Sessions
		muteWeighted += t.mute[id] * ps.Sessions
		totalSessions += ps.Sessions
	}
	if totalSessions > 0 {
		r.DeadTokensPerSession = int(math.Round(float64(deadWeighted) / float64(totalSessions)))
		r.MuteTokensPerSession = int(math.Round(float64(muteWeighted) / float64(totalSessions)))
	}
}
