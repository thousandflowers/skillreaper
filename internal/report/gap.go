package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// GapCat is the loaded-vs-fired breakdown for one category.
type GapCat struct {
	Category  scan.Category
	Loaded    int // inventory items whose description is injected
	Fired     int // items invoked at least once in the window
	LoadedTok int // sum of description tokens (0 for MCP — weight unknown)
	FiredTok  int // tokens of fired items (0 for MCP)
}

// Gap is the loaded-vs-fired snapshot for the window.
type Gap struct {
	PerCat    []GapCat // order: skill, mcp, agent
	Loaded    int
	Fired     int
	LoadedTok int
	FiredTok  int
}

// gapOrder fixes the category order shown in the gap view.
var gapOrder = []scan.Category{scan.CatSkill, scan.CatMCP, scan.CatAgent}

// utilPct returns the integer utilization percent and its display string.
// When there are no sessions there is no evidence, so it reports "n/a".
func utilPct(fired, loaded, sessions int) (int, string) {
	if sessions == 0 || loaded == 0 {
		return 0, "n/a"
	}
	pct := fired * 100 / loaded
	return pct, fmt.Sprintf("%d%%", pct)
}

// utilBar renders a 10-segment bar filled proportionally to pct.
//
// The band used to be carried a second way, by tinting the whole row red under
// 10%, yellow under 50% and green above — the single worst place in the tool
// for that, because low utilization is the finding this view exists to deliver
// and red/green is the pairing most readers lose. Monochrome, NO_COLOR and a
// pipe all reduced those three bands to one. The bar and the percent beside it
// already carry the magnitude in a form everyone receives, so the tint is
// gone rather than replaced.
//
// The bar itself had a matching flaw: filled = pct/10 rendered 0% and 9% as
// ten identical empty segments, collapsing "never fired once" and "fired, but
// rarely" — the distinction the whole report turns on. A value below one
// segment now shows a sliver instead, which is neither nothing nor a rounded
// up whole segment.
func utilBar(pct int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := pct / 10
	out := make([]rune, 0, 10)
	for i := 0; i < filled; i++ {
		out = append(out, '▰')
	}
	empty := 10 - filled
	if filled == 0 && pct > 0 {
		out = append(out, '▏')
		empty--
	}
	for i := 0; i < empty; i++ {
		out = append(out, '▱')
	}
	return string(out)
}

// gapLabel is the human label for a gap category.
func gapLabel(c scan.Category) string {
	switch c {
	case scan.CatSkill:
		return "skills"
	case scan.CatMCP:
		return "mcp"
	case scan.CatAgent:
		return "agents"
	default:
		return string(c)
	}
}

// renderGapLine appends a one-line utilization summary to the default report.
func renderGapLine(w io.Writer, r *Report, width int, color bool) {
	g := r.Gap
	if g == nil || g.Loaded == 0 {
		return
	}
	paint := painter(color)
	_, utilStr := utilPct(g.Fired, g.Loaded, r.Sessions)
	// The label is bold and the detail plain, so the ratio reads first. The
	// detail is truncated rather than allowed to run past the measure: this
	// line sits between the headline figures and the tables, and a fragment
	// wrapping there breaks the vertical rhythm the rest of the page relies on.
	label := "utilization " + utilStr
	detail := fmt.Sprintf("%d/%d items fired · ~%d/%d tok touched (%dd)",
		g.Fired, g.Loaded, g.FiredTok, g.LoadedTok, r.WindowDays)
	fmt.Fprintf(w, "\n  %s  %s\n",
		paint(cBold, label),
		truncate(detail, width-6-utf8.RuneCountInString(label)))
}

// RenderGap writes the dedicated loaded-vs-fired breakdown.
//
// The table is laid out to the measured width like the main report, and the
// token pair is split into two labelled columns. It used to be one field,
// "~16394 →   331", under a single header reading TOKENS — an arrow the reader
// had to decode before either number meant anything, and a header that named
// neither of them.
func RenderGap(w io.Writer, r *Report, color bool) {
	paint := painter(color)
	width := termWidth(w)
	g := r.Gap

	fmt.Fprintf(w, "\n  %s\n", paint(cCyan,
		rule(fmt.Sprintf("loaded vs fired · last %d days · %d sessions", r.WindowDays, r.Sessions), width-2)))

	if g == nil || g.Loaded == 0 {
		fmt.Fprintf(w, "\n  %s\n\n", paint(cDim, "no inventory found."))
		return
	}

	var cats, loaded, fired, util, bars, tokLoaded, tokUsed []string
	add := func(label string, gc GapCat, mcp bool) {
		pct, utilStr := utilPct(gc.Fired, gc.Loaded, r.Sessions)
		cats = append(cats, label)
		loaded = append(loaded, humanNum(gc.Loaded))
		fired = append(fired, humanNum(gc.Fired))
		util = append(util, utilStr)
		bars = append(bars, utilBar(pct))
		if mcp {
			// An MCP server's weight is built by the host at runtime. Unknown
			// is not zero, and must not be rendered as a figure.
			tokLoaded = append(tokLoaded, "?")
			tokUsed = append(tokUsed, "?")
			return
		}
		tokLoaded = append(tokLoaded, "~"+humanNum(gc.LoadedTok))
		tokUsed = append(tokUsed, humanNum(gc.FiredTok))
	}
	for _, gc := range g.PerCat {
		if gc.Loaded == 0 {
			continue
		}
		add(gapLabel(gc.Category), gc, gc.Category == scan.CatMCP)
	}
	// The total belongs to the same table, so its digits line up under the
	// categories they sum. It is separated by a rule rather than by a second
	// table, which would align to its own contents and stop comparing.
	add("total", GapCat{Loaded: g.Loaded, Fired: g.Fired, LoadedTok: g.LoadedTok, FiredTok: g.FiredTok}, false)

	tbl := layout([]col{
		{head: "CATEGORY", flex: true, cells: cats},
		{head: "LOADED", right: true, cells: loaded},
		{head: "FIRED", right: true, cells: fired},
		{head: "UTIL", right: true, cells: util},
		{cells: bars, shed: 1},
		{head: "TOK LOADED", right: true, cells: tokLoaded},
		{head: "TOK USED", right: true, cells: tokUsed},
	}, width, 2)

	fmt.Fprintln(w)
	fmt.Fprintln(w, paint(cDim, tbl[0]))
	for _, l := range tbl[1 : len(tbl)-1] {
		fmt.Fprintln(w, l)
	}
	// Sized to the table rather than to the terminal: this rule divides the
	// categories from their total, and one running wider than the thing it
	// divides reads as a section break instead.
	tw := 0
	for _, l := range tbl {
		if n := utf8.RuneCountInString(l); n > tw {
			tw = n
		}
	}
	fmt.Fprintf(w, "  %s\n", paint(cDim, strings.Repeat("─", tw-2)))
	fmt.Fprintln(w, paint(cBold, tbl[len(tbl)-1]))

	renderMuteSavings(w, r, width, paint)
	renderBrokenSkills(w, r, width, paint)
	renderPayloadQuality(w, r, width, paint)
	fmt.Fprintln(w)
}

// renderMuteSavings shows the per-session tokens recoverable by muting heavy,
// rarely-fired skills. It carries the same glyph MUTE has in the main report,
// so the two views name the same verdict the same way.
func renderMuteSavings(w io.Writer, r *Report, width int, paint func(code, s string) string) {
	if r.MuteCount == 0 {
		return
	}
	fmt.Fprintln(w)
	for _, l := range wrap(fmt.Sprintf("MUTE — %d heavy low-use %s, ~%s tok/session recoverable with reap mute",
		r.MuteCount, plural(r.MuteCount, "skill"), humanNum(r.MuteTokensPerSession)),
		width, "  "+mark(VerdictMute)+"  ", "     ") {
		fmt.Fprintln(w, paint(cBold, l))
	}
}

// renderBrokenSkills lists skills that were invoked but only ever errored —
// distinct from never-invoked cold skills, and the loudest thing this view can
// tell you, so it is named by a word and the REAP glyph rather than by red.
func renderBrokenSkills(w io.Writer, r *Report, width int, paint func(code, s string) string) {
	var names, errs []string
	for _, row := range r.Rows {
		if row.Reason == ReasonBroken {
			names = append(names, row.Name)
			errs = append(errs, humanNum(row.ErrorCount))
		}
	}
	if len(names) == 0 {
		return
	}
	fmt.Fprintf(w, "\n  %s\n", paint(cBold, truncate(
		fmt.Sprintf("%s  BROKEN — %d %s invoked that only ever errored",
			mark(VerdictReap), len(names), plural(len(names), "skill")), width-2)))
	for _, l := range layout([]col{
		{head: "NAME", flex: true, cells: names},
		{head: "ERRORS", right: true, cells: errs},
	}, width, 5) {
		fmt.Fprintln(w, l)
	}
}

// RenderGapMarkdown writes the gap breakdown as a Markdown table.
func RenderGapMarkdown(w io.Writer, r *Report) {
	g := r.Gap
	fmt.Fprintf(w, "# loaded vs fired\n\n")
	fmt.Fprintf(w, "Window: last %d days · %d sessions\n\n", r.WindowDays, r.Sessions)
	if g == nil || g.Loaded == 0 {
		fmt.Fprintln(w, "_No inventory found._")
		return
	}
	fmt.Fprintln(w, "| Category | Loaded | Fired | Util | Loaded tok | Fired tok |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|")

	row := func(label string, gc GapCat, mcp bool) {
		_, utilStr := utilPct(gc.Fired, gc.Loaded, r.Sessions)
		lt, ft := fmt.Sprintf("~%d", gc.LoadedTok), fmt.Sprintf("~%d", gc.FiredTok)
		if mcp {
			lt, ft = "?", "?"
		}
		fmt.Fprintf(w, "| %s | %d | %d | %s | %s | %s |\n",
			label, gc.Loaded, gc.Fired, utilStr, lt, ft)
	}
	for _, gc := range g.PerCat {
		if gc.Loaded == 0 {
			continue
		}
		row(gapLabel(gc.Category), gc, gc.Category == scan.CatMCP)
	}
	row("total", GapCat{Loaded: g.Loaded, Fired: g.Fired, LoadedTok: g.LoadedTok, FiredTok: g.FiredTok}, false)
	renderPayloadMarkdown(w, r)
}

// RenderGapJSON writes only the Gap snapshot as indented JSON.
func RenderGapJSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	// Embed *Gap so its fields stay top-level (backwards compatible) and add the
	// payload-quality axis alongside it.
	out := struct {
		*Gap
		Payload []PayloadRow `json:"payload,omitempty"`
	}{Gap: r.Gap, Payload: r.MCPPayload}
	return enc.Encode(out)
}

// computeGap derives the loaded-vs-fired snapshot from joined rows.
// Only skill/agent/mcp participate. MCP token weight is unknown, so its
// token sums are left at zero and excluded from totals.
func computeGap(rows []Row) *Gap {
	idx := map[scan.Category]int{}
	g := &Gap{}
	for i, c := range gapOrder {
		g.PerCat = append(g.PerCat, GapCat{Category: c})
		idx[c] = i
	}
	for _, row := range rows {
		i, ok := idx[row.Category]
		if !ok {
			continue
		}
		gc := &g.PerCat[i]
		gc.Loaded++
		g.Loaded++
		fired := row.Uses > 0
		if fired {
			gc.Fired++
			g.Fired++
		}
		if row.Category == scan.CatMCP {
			continue // token weight unknown without running the server
		}
		gc.LoadedTok += row.Tokens
		g.LoadedTok += row.Tokens
		if fired {
			gc.FiredTok += row.Tokens
			g.FiredTok += row.Tokens
		}
	}
	return g
}
