// Package report joins inventory and usage evidence into verdicts and
// renders the result as ANSI text, JSON, or Markdown.
package report

import (
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/thousandflowers/skillreaper/internal/clock"
	"github.com/thousandflowers/skillreaper/internal/cost"
	"github.com/thousandflowers/skillreaper/internal/platform"
	"github.com/thousandflowers/skillreaper/internal/scan"
	"github.com/thousandflowers/skillreaper/internal/usage"
)

// Opts tunes report generation.
type Opts struct {
	MinSessions  int
	GraceDays    int    // items installed this recently → REVIEW(grace)
	MinTokens    int    // items below this token weight → KEEP(tiny)
	Model        string // model ID for tokenizer-specific estimates; empty → default ratio
	PricePerMTok float64
	Cutoff       time.Time // start of the evidence window
	WindowDays   int
	KeepSet      map[string]bool // items manually marked as keep
	// EvidenceBlind holds platform IDs whose session transcripts could not
	// be parsed (e.g. OpenCode's SQLite store, or a platform with no session
	// files). REAP/MUTE candidates from these platforms are held at REVIEW,
	// because absence of evidence is not evidence of absence.
	EvidenceBlind map[string]bool
	// History answers "did this ever fire, in any session ever recorded",
	// including ones whose transcripts have since been deleted. It is what
	// separates a rarely-used item from an abandoned one, a distinction the
	// window alone cannot make. Nil disables the check and restores the
	// previous behaviour exactly.
	History func(platformID, category, name string) (uses int, last time.Time, ok bool)
	// PerPlatform holds each platform's own stats. An item is judged only on the
	// evidence of its own platform: a use in one tool says nothing about the copy
	// installed in another. Nil judges everything against the merged stats.
	PerPlatform map[string]*usage.Stats
	// BlindReason says, per evidence-blind platform, why it has no evidence.
	BlindReason map[string]string
	// RetentionDays is Claude Code's transcript retention, 0 when unknown.
	RetentionDays int
	// ClaudeMDLines holds the non-comment lines of every detected CLAUDE.md.
	// A skill whose name appears here is held at KEEP(claude-md-ref).
	ClaudeMDLines []string
	// MuteThreshold / MuteMinTokens drive the MUTE verdict for skills: a used
	// skill heavier than MuteMinTokens that fires in fewer than MuteThreshold
	// of sessions is a MUTE candidate. MuteThreshold == 0 disables MUTE.
	MuteThreshold float64
	MuteMinTokens int
	// PriceBasis says what PricePerMTok rests on; empty when it was asked for.
	PriceBasis string
	// PrunedCopies maps paths to the manifest id of a copy in quarantine.
	PrunedCopies map[string]string
	// PlatformNames maps platform ids to display names, for machine readers.
	PlatformNames map[string]string
	// Muted holds the paths of the items whose description reap has stripped and
	// that are still without one.
	Muted map[string]bool
	// MuteLost holds the paths of items reap muted that have a description again.
	MuteLost map[string]bool
	// PruneLost holds the plugin items in quarantine, by PruneLostKey.
	PruneLost map[string]bool
}

// PruneLostKey says which item a quarantined plugin item is, wherever a new version
// of the plugin puts it.
func PruneLostKey(platform, category, source, name string) string {
	return platform + "\x00" + category + "\x00" + source + "\x00" + name
}

// Row is one inventory item with its evidence and verdict.
type Row struct {
	scan.Item
	Uses     int
	LastUsed time.Time
	Verdict  string
	Reason   string // why this verdict was assigned
	Tokens   int    // estimated per-session weight
	Kept     bool   // user manually marked this as keep
	// WeightUnknown marks a row whose per-session cost could not be measured,
	// as distinct from one measured at zero. An MCP server's cost is its tool
	// schemas and a hook's is its injected output: the host builds both at
	// runtime, neither is on disk in a form we can read, and no transcript
	// record declares them. Tokens stays 0 for these, and without this flag a
	// reader takes that 0 for "free" instead of "never measured".
	WeightUnknown bool
	ErrorCount    int       // invocations that errored in the window
	LastAttempt   time.Time // most recent attempt (success or error)
	// EvidenceNote says why a platform has no usage evidence, for a no-transcript
	// row. One text for every surface.
	EvidenceNote string `json:",omitempty"`
	// PrunedCopyID is the manifest id of a copy of this item in quarantine: it was
	// pruned and is live again, reinstalled by hand or by an update.
	PrunedCopyID string `json:",omitempty"`
	// MuteLost marks a row reap muted whose description has come back, by a plugin
	// update or an edit. It is a normal row with its full weight, and can be muted
	// again; reap never does that by itself.
	MuteLost bool `json:",omitempty"`
	// PruneLost marks a plugin item reap pruned that an update brought back. It is
	// a normal row, and can be pruned again; reap never does that by itself.
	PruneLost bool `json:",omitempty"`
	// HistoricalLast is when the durable record last saw it fire, for an item
	// with no use in this window but one before it.
	HistoricalLast time.Time `json:",omitzero"`
}

// MuteSaving is what muting a row takes off each session. A muted item stays
// installed and its name stays loaded, so the name's tokens are not saved.
func MuteSaving(row Row) int {
	kept := int(math.Ceil(float64(utf8.RuneCountInString(row.Name)) / nameCharsPerToken))
	return max(row.Tokens-kept, 0)
}

// nameCharsPerToken is the estimate for a name, the same one the menu uses.
const nameCharsPerToken = 3.7

// Report is the full result of a scan.
type Report struct {
	// MutedCount is how many installed items the user has muted.
	MutedCount  int
	GeneratedAt time.Time
	WindowDays  int
	Sessions    int
	// Measured is the provider's own token accounting for the window, read
	// from the transcripts rather than estimated from character counts. It
	// covers whole requests, so it can never be attributed to one item — it is
	// the ground truth the per-item estimate is checked against, and the only
	// view of how resident context actually bills. Zero when the transcripts
	// carry no usage block.
	Measured             usage.Measured
	MalformedLines       int
	UnreadableFiles      int
	TruncatedReads       int
	OversizedLines       int
	UnparsableLines      int
	Rows                 []Row
	DeadCount            int
	DeadTokensPerSession int
	// DeadUnknownWeight counts dead items excluded from DeadTokensPerSession
	// because their weight was never measurable. The total below is a floor,
	// not an estimate, whenever this is above zero.
	DeadUnknownWeight    int
	MuteCount            int // skills flagged MUTE (heavy + rarely fired)
	MuteTokensPerSession int // per-session tokens recoverable by muting them
	DeadToolChars        int // from init-based tool-declaration tracking
	SessionsPerMonth     int
	MoneyPerMonth        float64
	// Platforms maps the platform ids on the rows to their display names.
	Platforms map[string]string `json:",omitempty"`
	// PriceBasis says what the price rests on, when it was not asked for by hand.
	PriceBasis string `json:",omitempty"`
	// PricePerToken is the input price behind the money figures, in dollars per
	// token, whether or not anything is dead. Absent when no price is known.
	PricePerToken float64 `json:",omitempty"`
	// MonthlyBasisDays is how many days of history SessionsPerMonth is scaled
	// from. Below WindowDays, the monthly figures are an extrapolation.
	MonthlyBasisDays int
	// PlatformSessions is each platform's own session count, which is what an
	// item's tokens are multiplied by.
	PlatformSessions map[string]int `json:",omitempty"`
	// PlatformSessionsPerMonth is the same, scaled to a month: the figure to
	// multiply one row's tokens by.
	PlatformSessionsPerMonth map[string]int `json:",omitempty"`
	// ProjectPaths maps a project bucket to the real path its transcripts recorded.
	ProjectPaths map[string]string `json:",omitempty"`
	Warnings     []scan.Warning
	Gap          *Gap
	// SkillProjects maps a skill key to the projects that fired it (see
	// usage.Stats.SkillProjects). Powers the by-project view.
	SkillProjects map[string]map[string]int
	// MCPPayload holds the per-MCP-tool payload-quality scores (the second
	// utilization axis: signal vs noise per call). Powers the payload section
	// of the gap view. Nil/empty when there is no MCP result evidence.
	MCPPayload []PayloadRow
}

// Build joins inventory items with usage stats and computes verdicts
// and totals.
func Build(items []scan.Item, st *usage.Stats, warns []scan.Warning, opts Opts) *Report {
	r := &Report{
		GeneratedAt:     clock.Now(),
		Platforms:       opts.PlatformNames,
		PricePerToken:   opts.PricePerMTok / 1e6,
		PriceBasis:      opts.PriceBasis,
		WindowDays:      st.WindowDays,
		Sessions:        st.Sessions,
		Measured:        st.Measured,
		MalformedLines:  st.MalformedLines,
		UnreadableFiles: st.UnreadableFiles,
		TruncatedReads:  st.TruncatedReads,
		OversizedLines:  st.OversizedLines,
		UnparsableLines: st.UnparsableLines,
		DeadToolChars:   st.DeadToolChars,
		Warnings:        cleanWarnings(warns),
		ProjectPaths:    st.ProjectPaths,
	}

	items = append(items, synthesizeTranscriptOnlyMCP(items, st)...)
	evidenceFor := evidenceByPlatform(items, st, opts)
	tally := platformTally{dead: map[string]int{}, mute: map[string]int{}}

	itemKey := func(it scan.Item) string {
		return strings.ToLower(string(it.Category) + ":" + it.Name)
	}

	var lost []string
	for _, it := range items {
		if isSelf(it) {
			continue
		}
		ev := evidenceFor(it.Platform)
		row := Row{Item: cleanItem(it), Tokens: cost.TokensFor(opts.Model, it.DescChars), MuteLost: opts.MuteLost[it.Path], PruneLost: opts.PruneLost[PruneLostKey(it.Platform, string(it.Category), it.Source, it.Name)], PrunedCopyID: prunedCopy(opts.PrunedCopies, it)}
		row.WeightUnknown = it.Category == scan.CatHook
		if it.Category == scan.CatMCP {
			chars, ok := lookupMCPWeight(ev.raw, it)
			row.Tokens, row.WeightUnknown = cost.TokensFor(opts.Model, chars), !ok
		}
		// Claude Code's listing keeps only some descriptions; what it injected
		// is the weight. A skill it never listed keeps the SKILL.md estimate.
		// The listing is from the last session, so it can predate a mute: the
		// next injection cannot exceed what the file holds now.
		if it.Category == scan.CatSkill && it.Platform == string(platform.ClaudeCode) {
			if w, ok := ev.raw.SkillWeight[it.Name]; ok {
				row.Tokens = cost.TokensFor(opts.Model, min(w.Chars, it.DescChars))
			}
		}
		switch it.Category {
		case scan.CatSkill, scan.CatAgent, scan.CatMCP:
			row.Uses, row.LastUsed = lookupUses(ev.canon, it)
			// A server known only from transcript evidence has no config entry
			// to read, so absence of a call cannot be told apart from absence of
			// the server. It is reported, never judged: no verdict path, so it
			// can never be REAP'd or counted as dead weight.
			if it.Source == SourceTranscriptOnly {
				row.Verdict = VerdictInfo
				r.Rows = append(r.Rows, row)
				continue
			}
			row.ErrorCount, row.LastAttempt = lookupErrors(ev.canon, it)
			vo := VerdictOpts{
				MinSessions: opts.MinSessions,
				GraceDays:   opts.GraceDays,
				MinTokens:   opts.MinTokens,
				WindowDays:  opts.WindowDays,
				Cutoff:      opts.Cutoff,
				ErrorCount:  row.ErrorCount,
			}
			if opts.History != nil {
				vo.HistoricalUses, vo.HistoricalLast = lookupHistory(opts.History, it)
				if row.Uses == 0 && vo.HistoricalUses > 0 {
					row.HistoricalLast = vo.HistoricalLast
				}
			}
			// Only skills carry a strippable description, so MUTE applies to them.
			if it.Category == scan.CatSkill {
				vo.Mutable = true
				vo.MuteThreshold = opts.MuteThreshold
				vo.MuteMinTokens = opts.MuteMinTokens
			}
			row.Verdict, row.Reason = Verdict(row.Uses, ev.raw.Sessions, row.Tokens, it.InstalledAt, vo)
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
			if it.HookLoaded && (row.Verdict == VerdictReap || row.Verdict == VerdictMute) {
				row.Verdict = VerdictKeep
				row.Reason = ReasonHookLoaded
			}
			// Muting removed the description that makes a skill fire, so its
			// zero uses are the mute's doing and say nothing about the skill.
			if opts.Muted[it.Path] {
				row.Verdict = VerdictInfo
				row.Reason = ReasonMuted
				r.MutedCount++
			}
			// Absence of evidence is not evidence of absence: an item from a
			// platform whose transcripts we could not parse (e.g. OpenCode's
			// SQLite store) has no complete usage signal, so REAP or MUTE would
			// be unsafe. Hold it at REVIEW and let the user decide.
			// On a history that proves use but not disuse, a use seen reads as used.
			// MUTE needs a firing rate, which that history cannot give.
			if ev.raw.PositiveOnly && row.Verdict == VerdictMute {
				row.Verdict = VerdictKeep
				row.Reason = ReasonUsed
			}
			if opts.EvidenceBlind[it.Platform] && (row.Verdict == VerdictReap || row.Verdict == VerdictMute ||
				(row.Verdict == VerdictReview && row.Reason == ReasonNeedsData)) {
				row.Verdict = VerdictReview
				row.Reason = ReasonNoEvidence
				row.EvidenceNote = opts.BlindReason[it.Platform]
			}
		default:
			row.Verdict = VerdictInfo
		}
		switch row.Verdict {
		case VerdictReap:
			r.DeadCount++
			if row.WeightUnknown {
				// Counted, but its weight cannot join the total: adding 0 would
				// quietly understate the headline by whatever it really costs.
				r.DeadUnknownWeight++
			}
			r.DeadTokensPerSession += row.Tokens
			tally.dead[it.Platform] += row.Tokens
		case VerdictMute:
			r.MuteCount++
			r.MuteTokensPerSession += MuteSaving(row)
			tally.mute[it.Platform] += MuteSaving(row)
		}
		r.Rows = append(r.Rows, row)
		if row.PruneLost {
			lost = append(lost, row.Name)
		}
	}
	if len(lost) > 0 {
		r.Warnings = append(r.Warnings, scan.Warning{Advisory: true, Path: "prune lost", Msg: "a plugin update brought back " + strings.Join(lost, ", ") + ", which you pruned: reap prune <name> takes it again"})
	}

	applyMonthlyFigures(r, st, tally, opts, func(tokens, perMonth int) float64 {
		return cost.MoneyPerMonth(tokens, perMonth, opts.PricePerMTok)
	})
	r.SkillProjects = st.SkillProjects
	r.MCPPayload = computePayload(st)
	sortRows(r.Rows)
	r.Gap = computeGap(r.Rows)
	return r
}

// lookupUses matches an item to usage evidence. Skills invoked as
// slash commands are recorded without their plugin namespace, so a
// "plugin:skill" item also matches its bare suffix.
//
// Known trade-off (do not "fix" without reading this): if a personal skill
// and a plugin skill share the same suffix (e.g. "foo" and "ecc:foo"), both
// rows absorb the bare "foo" uses. The transcripts carry no namespace, so the
// bare suffix is the only available signal; attributing it to exactly one
// item is not possible. This inflates both rows symmetrically and only in the
// rare collision case.
func lookupUses(st *usage.Stats, it scan.Item) (int, time.Time) {
	uses := 0
	var last time.Time
	for _, key := range evidenceKeys(it) {
		uses += st.Uses[it.Category][key]
		if t := st.Last[it.Category][key]; t.After(last) {
			last = t
		}
	}
	return uses, last
}

// lookupMCPWeight is the server's latest injected weight in chars, under its
// own name or its plugin alias. False when no session recorded loading it.
func lookupMCPWeight(st *usage.Stats, it scan.Item) (int, bool) {
	var best usage.Injected
	for _, key := range rawKeys(it) {
		if w, ok := st.MCPWeight[key]; ok && w.At.After(best.At) {
			best = w
		}
	}
	return best.Chars, best.Chars > 0
}

// lookupErrors mirrors lookupUses for errored invocations, folding the same
// aliases into one total.
func lookupErrors(st *usage.Stats, it scan.Item) (int, time.Time) {
	errs := 0
	var last time.Time
	for _, key := range evidenceKeys(it) {
		errs += st.Errors[it.Category][key]
		if t := st.LastAttempt[it.Category][key]; t.After(last) {
			last = t
		}
	}
	return errs, last
}

// SourceTranscriptOnly marks an MCP server that fired in a transcript but has
// no entry in any config file skillreaper reads — a connector authorised
// through claude.ai, for instance. There is nothing on disk to inventory and
// nothing to prune, so these rows are informational.
const SourceTranscriptOnly = "transcript-only"

// synthesizeTranscriptOnlyMCP returns a row for every MCP server the
// transcripts show firing that no scanner found on disk. Without this they were
// invisible: absent from the inventory, absent from the loaded count, and
// absent from the utilization denominator, while their tool schemas sat in
// every prompt.
//
// Matching mirrors lookupUses, alias included, so a plugin-shipped server
// already covered by a scanned row is not duplicated under its
// plugin_<plugin>_<server> transcript key.
func synthesizeTranscriptOnlyMCP(items []scan.Item, st *usage.Stats) []scan.Item {
	if st == nil {
		return nil
	}
	known := map[string]bool{}
	for _, it := range items {
		if it.Category != scan.CatMCP {
			continue
		}
		for _, key := range rawKeys(it) {
			known[key] = true
			// A server named a__b fires as mcp__a__b__tool, which the parser first
			// reads as a server "a". That is this row's use, not another server.
			if first, _, ok := strings.Cut(key, usage.ToolSepDefault); ok {
				known[first] = true
			}
		}
	}

	var out []scan.Item
	for key := range st.Uses[scan.CatMCP] {
		if key == "" || known[key] {
			continue
		}
		known[key] = true
		out = append(out, scan.Item{
			Category:  scan.CatMCP,
			Name:      key,
			Source:    SourceTranscriptOnly,
			Removable: false,
		})
	}
	// Map iteration is random; the report must not reorder between runs.
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// aliasKeys returns the extra usage keys an item's firings may be recorded
// under, because the name in the config is not always the name in a transcript.
//
//   - skill: slash commands drop the plugin namespace, so "ecc:plan" also
//     matches the bare "plan" (see the collision trade-off above).
//   - mcp: a plugin-shipped server is configured under its bare name
//     ("playwright"), but Claude Code exposes its tools as
//     "mcp__plugin_<plugin>_<server>__<tool>", which the parser records under
//     "plugin_<plugin>_<server>". Without this alias a plugin server that fired
//     looks unused and earns a REAP verdict.
//
// The mcp alias is built up from the plugin name rather than parsed back out of
// the recorded key on purpose: "plugin_ecc_sequential-thinking" cannot be split
// into plugin and server by scanning for a separator, because either half may
// itself contain one.
//
// Skills and agents also fire under a frontmatter name that differs from the
// folder or file name. An mcp key takes the form tool names carry, where any
// character outside [A-Za-z0-9_-] is "_".
func aliasKeys(it scan.Item) []string {
	switch it.Category {
	case scan.CatSkill, scan.CatAgent:
		var keys []string
		prefix, bare := "", it.Name
		if i := strings.LastIndexByte(it.Name, ':'); i >= 0 {
			prefix, bare = it.Name[:i+1], it.Name[i+1:]
			keys = append(keys, bare)
		}
		if fm := it.FrontmatterName; fm != "" {
			keys = append(keys, prefix+fm, fm)
		}
		return keys
	case scan.CatMCP:
		keys := []string{usage.MCPServerKey(it.Name)}
		if p, ok := sourcePluginName(it.Source); ok {
			keys = append(keys, "plugin_"+usage.MCPServerKey(p)+"_"+usage.MCPServerKey(it.Name))
		}
		return keys
	}
	return nil
}

// cleanItem makes an item's text safe to print. Path stays as it is on disk,
// because prune and mute need it. Everything printed takes the clean copy.
func cleanItem(it scan.Item) scan.Item {
	it.Name = scan.SafeText(it.Name)
	it.Source = scan.SafeText(it.Source)
	it.Description = scan.SafeText(it.Description)
	it.FrontmatterName = scan.SafeText(it.FrontmatterName)
	return it
}

func cleanWarnings(warns []scan.Warning) []scan.Warning {
	out := make([]scan.Warning, len(warns))
	for i, w := range warns {
		w.Path, w.Msg = scan.SafeText(w.Path), scan.SafeText(w.Msg)
		out[i] = w
	}
	return out
}

// sourcePluginName extracts "ecc" from a scan Source of "plugin:ecc@ecc".
// Reports false for any non-plugin source.
func sourcePluginName(source string) (string, bool) {
	const prefix = "plugin:"
	if !strings.HasPrefix(source, prefix) {
		return "", false
	}
	full, _, _ := strings.Cut(source[len(prefix):], "@")
	return full, full != ""
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

// selfName is this tool's own name as a skill, plugin or server.
const selfName = "skillreaper"

// isSelf reports an item that is this tool. It is never offered for removal, by
// any surface: the tool must not list itself as waste or take itself away.
func isSelf(it scan.Item) bool {
	lead, _, _ := strings.Cut(it.Name, ":")
	plugin, _, _ := strings.Cut(strings.TrimPrefix(it.Source, "plugin:"), "@")
	return lead == selfName || (IsPluginSource(it.Source) && plugin == selfName)
}

// prunedCopy finds a quarantined copy of an item. A skill is pruned as its folder.
func prunedCopy(copies map[string]string, it scan.Item) string {
	if id, ok := copies[it.Path]; ok {
		return id
	}
	return copies[filepath.Dir(it.Path)]
}

// MuteEligible says whether a bulk reap mute takes this row.
func MuteEligible(row Row) bool {
	return row.Path != "" &&
		row.Removable &&
		(row.Category == scan.CatSkill || row.Category == scan.CatAgent) &&
		(row.Verdict == VerdictReap || row.Verdict == VerdictMute)
}
