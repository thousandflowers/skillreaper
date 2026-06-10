package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// ANSI escape codes, applied only when color is enabled.
const (
	cReset = "\x1b[0m"
	cBold  = "\x1b[1m"
	cRed   = "\x1b[31m"
	cGreen = "\x1b[32m"
	cYell  = "\x1b[33m"
	cDim   = "\x1b[2m"
)

var sectionTitles = []struct {
	cat   scan.Category
	title string
}{
	{scan.CatSkill, "SKILLS (description injected every session)"},
	{scan.CatMCP, "MCP SERVERS (tool schemas injected; weight unknown without running them)"},
	{scan.CatAgent, "AGENTS (description injected every session)"},
	{scan.CatHook, "HOOKS (report-only: output cost varies per event)"},
	{scan.CatProse, "ALWAYS-LOADED PROSE (CLAUDE.md, rules)"},
}

// RenderJSON writes the report as indented JSON.
func RenderJSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// RenderText writes the human-readable report. color toggles ANSI codes.
func RenderText(w io.Writer, r *Report, color bool) {
	paint := func(code, s string) string {
		if !color {
			return s
		}
		return code + s + cReset
	}

	fmt.Fprintf(w, "\n%s\n\n", paint(cBold, "skillreaper — evidence-based pruning for your agent stack"))
	fmt.Fprintf(w, "Window: last %d days · %d sessions analyzed", r.WindowDays, r.Sessions)
	if r.MalformedLines > 0 {
		fmt.Fprintf(w, " · %d unreadable lines skipped", r.MalformedLines)
	}
	fmt.Fprintln(w)

	if r.Sessions == 0 {
		fmt.Fprintf(w, "\n%s\n", paint(cYell, "WARNING: no transcripts found in window — verdicts unavailable, inventory only."))
	}

	shock := fmt.Sprintf("%d items never used · ~%d dead tokens injected per session · ~$%.2f/month at your session rate",
		r.DeadCount, r.DeadTokensPerSession, r.MoneyPerMonth)
	fmt.Fprintf(w, "\n%s\n", paint(cBold+cRed, shock))
	if r.DeadToolChars > 0 {
		fmt.Fprintf(w, "  (init parser: ~%d chars of tool descriptions unused per session)\n",
			r.DeadToolChars)
	}

	for _, sec := range sectionTitles {
		rows := filterRows(r.Rows, sec.cat)
		if len(rows) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n%s\n", paint(cBold, sec.title))
		tw := newTable(w)
		tw.row("NAME", "PLATFORM", "SOURCE", "WEIGHT/SESSION", "USES", "LAST USED", "VERDICT")
		for _, row := range rows {
			weight := fmt.Sprintf("~%d tok", row.Tokens)
			if row.Category == scan.CatMCP || row.Category == scan.CatHook {
				weight = "?"
			}
			verdict := row.Verdict
			switch verdict {
			case VerdictReap:
				verdict = paint(cRed, verdict)
			case VerdictKeep:
				verdict = paint(cGreen, verdict)
			case VerdictReview:
				verdict = paint(cYell, verdict)
			}
			name := row.Name
			if row.Kept {
				name += " (kept)"
			}
			uses, last := "-", "-"
			if row.Verdict != VerdictInfo {
				uses = fmt.Sprintf("%d", row.Uses)
				last = humanTime(row.LastUsed)
			}
			tw.row(truncate(name, 32), truncate(platformLabel(row.Platform), 12), truncate(row.Source, 24), weight, uses, last, verdict)
		}
		tw.flush()
	}

	if len(r.Warnings) > 0 {
		fmt.Fprintf(w, "\n%s\n", paint(cYell, fmt.Sprintf("%d warnings (unreadable files):", len(r.Warnings))))
		for _, warn := range r.Warnings {
			fmt.Fprintf(w, "  %s: %s\n", warn.Path, warn.Msg)
		}
	}

	if r.DeadCount > 0 {
		fmt.Fprintf(w, "\n%s", paint(cBold+cRed, fmt.Sprintf("→  Run: reap prune   (%d items · ~%d tok/session reclaimed)", r.DeadCount, r.DeadTokensPerSession)))
	}
	fmt.Fprintf(w, "\n%s\n\n", paint(cDim, "  All estimates use chars/3.7 ≈ tokens. Prune is reversible: reap restore --all"))
}

// RenderMarkdown writes the report as a shareable Markdown document.
func RenderMarkdown(w io.Writer, r *Report) {
	fmt.Fprintf(w, "# skillreaper report\n\n")
	fmt.Fprintf(w, "Window: last %d days · %d sessions analyzed\n\n", r.WindowDays, r.Sessions)
	fmt.Fprintf(w, "**%d items never used · ~%d dead tokens/session · ~$%.2f/month**\n",
		r.DeadCount, r.DeadTokensPerSession, r.MoneyPerMonth)

	for _, sec := range sectionTitles {
		rows := filterRows(r.Rows, sec.cat)
		if len(rows) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n## %s\n\n", sec.title)
		fmt.Fprintln(w, "| Name | Platform | Source | Weight/session | Uses | Last used | Verdict |")
		fmt.Fprintln(w, "|---|---|---|---|---|---|---|")
		for _, row := range rows {
			weight := fmt.Sprintf("~%d tok", row.Tokens)
			if row.Category == scan.CatMCP || row.Category == scan.CatHook {
				weight = "?"
			}
			uses, last := "-", "-"
			if row.Verdict != VerdictInfo {
				uses = fmt.Sprintf("%d", row.Uses)
				last = humanTime(row.LastUsed)
			}
			fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %s | %s |\n",
				row.Name, platformLabel(row.Platform), row.Source, weight, uses, last, row.Verdict)
		}
	}
}

func platformLabel(id string) string {
	if id == "" {
		return "—"
	}
	return id
}

func filterRows(rows []Row, cat scan.Category) []Row {
	var out []Row
	for _, r := range rows {
		if r.Category == cat {
			out = append(out, r)
		}
	}
	return out
}

func humanTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Format("2006-01-02")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// table aligns columns with two-space gutters.
type table struct {
	w    io.Writer
	rows [][]string
}

func newTable(w io.Writer) *table { return &table{w: w} }

func (t *table) row(cells ...string) { t.rows = append(t.rows, cells) }

func (t *table) flush() {
	if len(t.rows) == 0 {
		return
	}
	widths := make([]int, len(t.rows[0]))
	for _, r := range t.rows {
		for i, c := range r {
			if l := visibleLen(c); l > widths[i] {
				widths[i] = l
			}
		}
	}
	for _, r := range t.rows {
		var b strings.Builder
		for i, c := range r {
			b.WriteString(c)
			if i < len(r)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-visibleLen(c)+2))
			}
		}
		fmt.Fprintln(t.w, b.String())
	}
}

// visibleLen ignores ANSI escape sequences when measuring width.
func visibleLen(s string) int {
	n := 0
	inEsc := false
	for _, r := range s {
		switch {
		case inEsc:
			if r == 'm' {
				inEsc = false
			}
		case r == '\x1b':
			inEsc = true
		default:
			n++
		}
	}
	return n
}
