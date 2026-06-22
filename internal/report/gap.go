package report

import (
	"encoding/json"
	"fmt"
	"io"

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

// utilColor maps a utilization percent to an ANSI color: low is bad (red),
// mid is yellow, high is good (green).
func utilColor(pct int) string {
	switch {
	case pct < 10:
		return cRed
	case pct < 50:
		return cYell
	default:
		return cGreen
	}
}

// utilBar renders a 10-segment bar filled proportionally to pct.
func utilBar(pct int) string {
	filled := pct / 10
	if filled < 0 {
		filled = 0
	}
	if filled > 10 {
		filled = 10
	}
	out := make([]rune, 0, 10)
	for i := 0; i < filled; i++ {
		out = append(out, '▰')
	}
	for i := filled; i < 10; i++ {
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
func renderGapLine(w io.Writer, r *Report, color bool) {
	g := r.Gap
	if g == nil || g.Loaded == 0 {
		return
	}
	paint := painter(color)
	_, utilStr := utilPct(g.Fired, g.Loaded, r.Sessions)
	fmt.Fprintf(w, "\n  %s %s  —  %d/%d items fired · ~%d/%d tok touched (%dd)\n",
		paint(cBCyan, "⟡"),
		paint(cBold, "utilization "+utilStr),
		g.Fired, g.Loaded, g.FiredTok, g.LoadedTok, r.WindowDays)
}

// RenderGap writes the dedicated loaded-vs-fired breakdown. The bar shows
// per-category utilization; counts and token weight sit side by side. The
// whole row is tinted by its utilization so alignment is unaffected by ANSI.
func RenderGap(w io.Writer, r *Report, color bool) {
	paint := painter(color)
	g := r.Gap

	fmt.Fprintf(w, "\n  %s\n\n",
		paint(cBold, fmt.Sprintf("⟡ loaded vs fired — last %d days · %d sessions", r.WindowDays, r.Sessions)))

	if g == nil || g.Loaded == 0 {
		fmt.Fprintf(w, "  %s\n\n", paint(cDim, "no inventory found."))
		return
	}

	fmt.Fprintf(w, "  %-9s %7s %6s %6s   %-10s   %s\n",
		"CATEGORY", "LOADED", "FIRED", "UTIL", "", "TOKENS")

	for _, gc := range g.PerCat {
		if gc.Loaded == 0 {
			continue
		}
		writeGapRow(w, gapLabel(gc.Category), gc, gc.Category == scan.CatMCP, r.Sessions, paint)
	}

	fmt.Fprintf(w, "  %s\n", paint(cDim, "─────────────────────────────────────────────────────────"))
	total := GapCat{Loaded: g.Loaded, Fired: g.Fired, LoadedTok: g.LoadedTok, FiredTok: g.FiredTok}
	writeGapRow(w, "total", total, false, r.Sessions, paint)
	fmt.Fprintln(w)

	renderMuteSavings(w, r, paint)
	renderBrokenSkills(w, r, paint)
	renderPayloadQuality(w, r, paint)
}

// renderMuteSavings shows the per-session tokens recoverable by muting heavy,
// rarely-fired skills.
func renderMuteSavings(w io.Writer, r *Report, paint func(code, s string) string) {
	if r.MuteCount == 0 {
		return
	}
	fmt.Fprintf(w, "  %s %s\n",
		paint(cBYell, "⟡ mute"),
		paint(cDim, fmt.Sprintf("%d heavy low-use skills · ~%d tok/session recoverable via `reap mute`",
			r.MuteCount, r.MuteTokensPerSession)))
}

// renderBrokenSkills lists skills that were invoked but only ever errored,
// with their error counts — distinct from never-invoked cold skills.
func renderBrokenSkills(w io.Writer, r *Report, paint func(code, s string) string) {
	var broken []Row
	for _, row := range r.Rows {
		if row.Reason == ReasonBroken {
			broken = append(broken, row)
		}
	}
	if len(broken) == 0 {
		return
	}
	fmt.Fprintf(w, "\n  %s\n", paint(cBRed, "Broken skills (invoked, only errored):"))
	for _, row := range broken {
		fmt.Fprintf(w, "    %-44s %s\n",
			truncate(row.Name, 44), paint(cDim, fmt.Sprintf("%d errors", row.ErrorCount)))
	}
}

// writeGapRow renders one aligned row, tinted by utilization. mcp marks the
// token columns as unknown ("?").
func writeGapRow(w io.Writer, label string, gc GapCat, mcp bool, sessions int, paint func(code, s string) string) {
	pct, utilStr := utilPct(gc.Fired, gc.Loaded, sessions)
	bar := utilBar(pct)
	tok := "      ? →     ?"
	if !mcp {
		tok = fmt.Sprintf("~%5d → %5d", gc.LoadedTok, gc.FiredTok)
	}
	line := fmt.Sprintf("  %-9s %7d %6d %6s   %-10s   %s",
		label, gc.Loaded, gc.Fired, utilStr, bar, tok)
	fmt.Fprintln(w, paint(utilColor(pct), line))
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
