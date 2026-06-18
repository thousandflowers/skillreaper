package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/thousandflowers/skillreaper/internal/cost"
	"github.com/thousandflowers/skillreaper/internal/scan"
)

// ANSI escape codes, applied only when color is enabled.
const (
	cReset  = "\x1b[0m"
	cBold   = "\x1b[1m"
	cDim    = "\x1b[2m"
	cRed    = "\x1b[31m"
	cGreen  = "\x1b[32m"
	cYell   = "\x1b[33m"
	cCyan   = "\x1b[36m"
	cBRed   = "\x1b[1;31m"
	cBGreen = "\x1b[1;32m"
	cBYell  = "\x1b[1;33m"
	cBCyan  = "\x1b[1;36m"
)

// painter returns a function that wraps text in an ANSI code when color
// is enabled, and returns it unchanged otherwise.
func painter(color bool) func(code, s string) string {
	return func(code, s string) string {
		if !color {
			return s
		}
		return code + s + cReset
	}
}

// MinStarCtaTokens is the minimum DeadTokensPerSession for the star-CTA to
// show on a plain reap report.
const MinStarCtaTokens = 200

// RenderStarCta prints a single sober line asking for a GitHub star,
// personalized with the saved-token figure. It must only be called when
// the throttling and output-format gates have already passed.
func RenderStarCta(w io.Writer, deadTokens int, color bool) {
	paint := painter(color)
	tokStr := fmt.Sprintf("%d", deadTokens)
	if deadTokens >= 1000 {
		tokStr = fmt.Sprintf("%.1fk", float64(deadTokens)/1000)
	}
	line := "⭐ reap saved you ~" + tokStr + " tok/session. If it helps: github.com/thousandflowers/skillreaper"
	fmt.Fprintf(w, "\n  %s\n\n", paint(cDim, line))
}

// RenderValueFeedback prints a single line after a successful prune or mute
// operation, showing real savings with a conservative annualised money estimate.
func RenderValueFeedback(w io.Writer, verb string, items, tokensPerSession, sessionsPerMonth int, price float64, color bool) {
	if items == 0 {
		return
	}
	paint := painter(color)
	tokStr := fmt.Sprintf("%d", tokensPerSession)
	if tokensPerSession >= 1000 {
		tokStr = fmt.Sprintf("%.1fk", float64(tokensPerSession)/1000)
	}
	money := ""
	if sessionsPerMonth > 0 && price > 0 {
		mPerM := cost.MoneyPerMonth(tokensPerSession, sessionsPerMonth, price)
		yr := mPerM * 12
		if yr < 1.0 {
			money = " (< $1/yr at your usage)"
		} else {
			money = fmt.Sprintf(" (≈$%.0f/yr at your usage)", yr)
		}
	}
	fmt.Fprintf(w, "\n  %s\n", paint(cGreen, "✓ ")+paint(cDim, fmt.Sprintf("%s %d items · saving ~%s tok/session%s", verb, items, tokStr, money)))
}

// RenderShareHint prints a single sober line pointing users at reap share.
// It must only be called when the throttling and output-format gates have
// already passed.
func RenderShareHint(w io.Writer, color bool) {
	paint := painter(color)
	fmt.Fprintf(w, "  %s\n", paint(cDim, "↗ help your team save context too → reap share"))
}

// RenderShareText prints a ready-to-paste share message for team channels.
func RenderShareText(w io.Writer, tokensPerSession int) {
	line := shareMessage(tokensPerSession)
	fmt.Fprintln(w, line)
}

// RenderShareMarkdown prints a share message formatted as a Markdown code block.
func RenderShareMarkdown(w io.Writer, tokensPerSession int) {
	line := shareMessage(tokensPerSession)
	fmt.Fprintf(w, "```\n%s\n```\n", line)
}

// RenderShareJSON prints the share message as structured JSON.
func RenderShareJSON(w io.Writer, tokensPerSession int) {
	type shareJSON struct {
		Message               string `json:"message"`
		TokensSavedPerSession int    `json:"tokens_saved_per_session"`
		URL                   string `json:"url"`
		Install               string `json:"install"`
	}
	msg := shareMessage(tokensPerSession)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(shareJSON{
		Message:               msg,
		TokensSavedPerSession: tokensPerSession,
		URL:                   "https://github.com/thousandflowers/skillreaper",
		Install:               "brew install thousandflowers/tap/skillreaper",
	})
}

// shareMessage builds the human-readable share text.
// When tokens are available, it includes the real savings figure.
// When no data is available (0), it falls back to a generic message.
func shareMessage(tokensPerSession int) string {
	if tokensPerSession > 0 {
		tokStr := fmt.Sprintf("%d", tokensPerSession)
		if tokensPerSession >= 1000 {
			tokStr = fmt.Sprintf("%.1fk", float64(tokensPerSession)/1000)
		}
		return fmt.Sprintf(`Just cut ~%s tokens/session of dead context from my AI agent with skillreaper.
One read-only command, 100%% local:

  brew install thousandflowers/tap/skillreaper
  github.com/thousandflowers/skillreaper`, tokStr)
	}
	return `Check your AI agent's context diet with skillreaper.
One read-only command, 100% local:

  brew install thousandflowers/tap/skillreaper
  github.com/thousandflowers/skillreaper`
}

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
	paint := painter(color)

	fmt.Fprintf(w, "\n  %s\n\n", paint(cBold, "⟡ skillreaper — evidence-based pruning for your agent stack"))
	fmt.Fprintf(w, "  %s  %s",
		paint(cDim, "window:"), paint(cBold, fmt.Sprintf("last %d days · %d sessions", r.WindowDays, r.Sessions)))
	if r.MalformedLines > 0 {
		fmt.Fprintf(w, "  %s", paint(cDim, fmt.Sprintf("(%d unreadable lines)", r.MalformedLines)))
	}
	fmt.Fprintln(w)

	if r.Sessions == 0 {
		fmt.Fprintf(w, "\n  %s\n", paint(cYell, "⚠  no transcripts found in window — verdicts unavailable, inventory only."))
	}

	shockContent := fmt.Sprintf("%d items never used · ~%d dead tokens/session · ~$%.2f/month",
		r.DeadCount, r.DeadTokensPerSession, r.MoneyPerMonth)
	blockWidth := utf8.RuneCountInString(shockContent) + 6
	shockLine := fmt.Sprintf("  ╔%s╗", strings.Repeat("═", blockWidth-2))
	shockMid := fmt.Sprintf("  ║  %s  ║", shockContent)
	shockBot := fmt.Sprintf("  ╚%s╝", strings.Repeat("═", blockWidth-2))
	fmt.Fprintf(w, "\n%s\n%s\n%s\n",
		paint(cBRed, shockLine),
		paint(cBold+cBRed, shockMid),
		paint(cBRed, shockBot))
	if r.DeadToolChars > 0 {
		// DeadToolChars is a total summed across every session; divide to show
		// the per-session average the label promises.
		perSession := r.DeadToolChars
		if r.Sessions > 1 {
			perSession = r.DeadToolChars / r.Sessions
		}
		fmt.Fprintf(w, "  %s\n", paint(cDim, fmt.Sprintf("(init: ~%d chars of tool descriptions unused per session)", perSession)))
	}

	renderGapLine(w, r, color)

	for _, sec := range sectionTitles {
		rows := filterRows(r.Rows, sec.cat)
		if len(rows) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n  %s\n", paint(cCyan, "── "+sec.title+" "+strings.Repeat("─", 60)))
		renderSection(w, rows, paint)
	}

	if len(r.Warnings) > 0 {
		fmt.Fprintf(w, "\n  %s\n", paint(cYell, fmt.Sprintf("── %d warnings ──", len(r.Warnings))))
		for _, warn := range r.Warnings {
			fmt.Fprintf(w, "    %s\n", paint(cDim, warn.Path+": "+warn.Msg))
		}
	}

	var muteNames []string
	var muteTokens int
	for _, row := range r.Rows {
		if row.Verdict == VerdictMute {
			muteNames = append(muteNames, row.Name)
			muteTokens += row.Tokens
		}
	}
	if r.DeadCount > 0 || len(muteNames) > 0 {
		fmt.Fprintln(w)
	}
	if r.DeadCount > 0 {
		fmt.Fprintf(w, "  %s  %s\n",
			paint(cBRed, "▸ reap prune"),
			paint(cDim, fmt.Sprintf("— %d items · ~%d tok/session reclaimed", r.DeadCount, r.DeadTokensPerSession)))
	}
	if len(muteNames) > 0 {
		fmt.Fprintf(w, "  %s  %s",
			paint(cBYell, "▸ reap mute"),
			paint(cDim, fmt.Sprintf("— %s (~%d tok total)", strings.Join(muteNames, ", "), muteTokens)))
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "\n  %s\n\n", paint(cDim, "All estimates use chars/3.7 ≈ tokens. Prune is reversible: reap restore --all"))
}

// renderSection prints rows grouped by verdict, with group headers.
func renderSection(w io.Writer, rows []Row, paint func(code, s string) string) {
	maxTok := 0
	for _, r := range rows {
		if r.Tokens > maxTok {
			maxTok = r.Tokens
		}
	}
	groups := groupByVerdict(rows)
	first := true
	for _, v := range []string{VerdictReap, VerdictMute, VerdictReview, VerdictKeep, VerdictInfo} {
		items := groups[v]
		// Skip empty groups (shouldn't happen but guard).
		if len(items) == 0 {
			continue
		}

		// ── Group header ──────────────────────────────────────
		totalTok := 0
		for _, r := range items {
			totalTok += r.Tokens
		}

		// Total tokens display
		tokStr := ""
		if totalTok > 0 {
			tokStr = fmt.Sprintf(" · ~%d tok/session", totalTok)
		}

		subColor := cDim
		subIcon := "·"
		switch v {
		case VerdictReap:
			subColor = cBRed
			subIcon = "▸"
		case VerdictMute:
			subColor = cBYell
			subIcon = "▸"
		case VerdictReview:
			subColor = cBYell
			subIcon = "▸"
		case VerdictKeep:
			subColor = cBGreen
			subIcon = "▸"
		}

		label := v
		if v == VerdictInfo {
			label = "info"
		}
		groupLine := fmt.Sprintf("    %s %s  %d items%s", subIcon, strings.ToLower(label), len(items), tokStr)

		if !first {
			// Thin separator between groups
			fmt.Fprintf(w, "\n")
		}
		fmt.Fprintf(w, "  %s\n", paint(subColor, groupLine))
		first = false

		// ── Column header + rows ──────────────────────────────
		tw := newTable(w)
		tw.row("NAME", "TOK", "SRC", "PERM", "USES", "LAST", "JUDGMENT")
		for _, row := range items {
			weight := weightDisplay(row.Tokens, maxTok, row.Category, paint)
			src := shortSource(row.Source)
			perm := permDisplay(row)

			judgment := row.Verdict
			if row.Reason != "" && row.Verdict != VerdictInfo {
				judgment = row.Verdict + " · " + row.Reason
			}
			switch row.Verdict {
			case VerdictReap:
				// Broken skills (invoked, only errored) are louder than plain unused.
				if row.Reason == ReasonBroken {
					judgment = paint(cBRed, judgment)
				} else {
					judgment = paint(cRed, judgment)
				}
			case VerdictMute:
				judgment = paint(cYell, judgment)
			case VerdictKeep:
				judgment = paint(cGreen, judgment)
			case VerdictReview:
				judgment = paint(cYell, judgment)
			}

			uses, last := "-", "-"
			if row.Verdict != VerdictInfo {
				uses = fmt.Sprintf("%d", row.Uses)
				last = humanTime(row.LastUsed)
			}
			tw.row(truncate(row.Name, 44), weight, src, perm, uses, last, judgment)
		}
		tw.flush()
	}
}

// permDisplay shows a skill/agent's permission surface: "all" when
// unrestricted, otherwise the count of allowed tools. "-" where it does not
// apply (MCP, hooks, prose).
func permDisplay(row Row) string {
	switch row.Category {
	case scan.CatSkill, scan.CatAgent:
		if row.ToolSurface == scan.ToolSurfaceAll {
			return "all"
		}
		return fmt.Sprintf("%d", row.ToolSurface)
	default:
		return "-"
	}
}

// weightDisplay returns a compact visual representation of token weight:
// a number like "~248" with a mini bar proportional to maxTok.
func weightDisplay(tok, maxTok int, cat scan.Category, _ func(code, s string) string) string {
	if cat == scan.CatMCP || cat == scan.CatHook {
		return "   ?"
	}
	if maxTok == 0 {
		return fmt.Sprintf("~%d", tok)
	}
	// Calculate bar segments: 5 blocks max
	barPct := float64(tok) / float64(maxTok)
	filled := int(barPct * 5)
	if filled < 0 {
		filled = 0
	}
	if filled > 5 {
		filled = 5
	}
	// Unicode block chars: full (▰) and empty (▱)
	bar := strings.Repeat("▰", filled) + strings.Repeat("▱", 5-filled)
	return fmt.Sprintf("%-5s %s", fmt.Sprintf("~%d", tok), bar)
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
		fmt.Fprintln(w, "| Name | Source | Weight | Uses | Last used | Verdict | Reason |")
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
			reason := ""
			if row.Reason != "" && row.Verdict != VerdictInfo {
				reason = row.Reason
			}
			if row.Kept {
				reason = "user-kept"
			}
			fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %s | %s |\n",
				row.Name, shortSource(row.Source), weight, uses, last, row.Verdict, reason)
		}
	}
}

// groupByVerdict splits rows by verdict, preserving REAP→MUTE→REVIEW→KEEP→INFO order.
func groupByVerdict(rows []Row) map[string][]Row {
	order := []string{VerdictReap, VerdictMute, VerdictReview, VerdictKeep, VerdictInfo}
	m := make(map[string][]Row, len(order))
	for _, v := range order {
		m[v] = nil
	}
	for _, r := range rows {
		m[r.Verdict] = append(m[r.Verdict], r)
	}
	return m
}

func shortSource(s string) string {
	switch {
	case s == "personal":
		return "own"
	case s == "user-config":
		return "usr"
	case strings.HasPrefix(s, "plugin:"):
		return "ext"
	case strings.HasPrefix(s, "project:"):
		return "proj"
	default:
		return s
	}
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
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	// Slice on a rune boundary so a multibyte name is not split, which would
	// emit invalid UTF-8 to the terminal.
	if n <= 1 {
		return "…"
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
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
