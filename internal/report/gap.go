package report

import (
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
