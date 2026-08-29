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
	// Monthly, not annualised. docs/decisions.md (2026-08-17) records why a
	// dollar figure is not a measurement here — it is dead tokens times a
	// price we do not control, and the same stack has been reported between
	// $1.54 and $24 in two months. Multiplying by twelve multiplies that
	// spread along with the number, and this line runs immediately after a
	// destructive action, where it carries the most weight. Every other
	// surface that shows money shows it per month; this one now agrees.
	money := ""
	if sessionsPerMonth > 0 && price > 0 {
		mPerM := cost.MoneyPerMonth(tokensPerSession, sessionsPerMonth, price)
		if mPerM < 0.01 {
			money = " (< $0.01/month at your usage)"
		} else {
			money = fmt.Sprintf(" (≈$%.2f/month at your usage)", mPerM)
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

// RenderFooter prints the permanent attribution line that closes a text
// report. Unlike RenderStarCta this is a signature, not a nudge: no cooldown,
// no savings threshold, no opt-out, and it never touches NudgeState. The
// caller owns the only exclusion that applies — JSON, Markdown and quiet
// output never reach it. The color flag decides how the line is painted,
// never whether it is printed, so redirecting to a file still yields the URL.
func RenderFooter(w io.Writer, color bool) {
	paint := painter(color)
	fmt.Fprintf(w, "  %s\n\n", paint(cDim, "⟡ skillreaper · github.com/thousandflowers/skillreaper"))
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

// sectionTitles names each category. title is the single string Markdown has
// always printed and must keep printing byte for byte. The text report reads
// the same words as two pieces instead: a short name it can set in a rule, and
// a gloss it drops when the measure cannot take it. Splitting the string is
// what lets the heading degrade instead of wrapping.
var sectionTitles = []struct {
	cat   scan.Category
	title string
	short string
	gloss string
}{
	{scan.CatSkill, "SKILLS (description injected every session)",
		"SKILLS", "description injected every session"},
	{scan.CatMCP, "MCP SERVERS (tool schemas injected; weight unknown without running them)",
		"MCP SERVERS", "tool schemas injected; weight unknown without running them"},
	{scan.CatAgent, "AGENTS (description injected every session)",
		"AGENTS", "description injected every session"},
	{scan.CatHook, "HOOKS (report-only: output cost varies per event)",
		"HOOKS", "report-only: output cost varies per event"},
	{scan.CatProse, "ALWAYS-LOADED PROSE (CLAUDE.md, rules)",
		"ALWAYS-LOADED PROSE", "CLAUDE.md, rules"},
}

// RenderJSON writes the report as indented JSON.
func RenderJSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// RenderText writes the human-readable report. color toggles ANSI codes.
//
// The layout is bound to a measured width and nothing may exceed it: every
// line below is produced by layout, wrapped by wrap, or truncated. The
// 80-column guarantee is therefore a property of this code rather than of
// whichever names happen to be installed on the machine it runs on.
//
// Colour is emphasis only. Hierarchy is carried by spacing, alignment, order
// and rules; each verdict is carried by its word and an ASCII glyph. Turning
// every escape code off changes how the report looks and nothing about what it
// says, which is what makes NO_COLOR, a monochrome terminal and a pipe safe.
func RenderText(w io.Writer, r *Report, color bool) {
	paint := painter(color)
	width := termWidth(w)

	// One masthead line: window, evidence quality, and whether caveats follow.
	// These are one thought — how far the numbers below can be trusted — and a
	// reader who discovers four warnings only after scrolling past the
	// verdicts has already read the verdicts wrong.
	head := fmt.Sprintf("skillreaper · last %d days · %d sessions", r.WindowDays, r.Sessions)
	if r.MalformedLines > 0 {
		head += " · " + MalformedSummary(r)
	}
	if n := len(r.Warnings); n > 0 {
		head += fmt.Sprintf(" · %d %s below", n, plural(n, "warning"))
	}
	fmt.Fprintf(w, "\n  %s\n", paint(cBold, truncate(head, width-2)))

	if r.Sessions == 0 {
		fmt.Fprintln(w)
		for _, l := range wrap("no transcripts found in window — verdicts unavailable, inventory only.",
			width, "  ! ", "    ") {
			fmt.Fprintln(w, paint(cBold, l))
		}
	}

	renderHeadline(w, r, width, paint)
	renderGapLine(w, r, width, color)
	renderSections(w, r, width, paint)
	renderWarnings(w, r, width, paint)
	renderActions(w, r, width, paint)
}

// renderHeadline prints the three figures the report exists to deliver.
//
// They used to share a single line inside a double-ruled box, separated by
// interpuncts and painted bright red. Three reading problems in one. The box
// was sized from its own text, so a wider number pushed it past the terminal
// and split the rules from the content. The interpuncts ran a count, a rate
// and a price together as one phrase, so the eye had to parse before it could
// compare. And the emphasis was hue, which a monochrome terminal, NO_COLOR, a
// pipe, and a meaningful share of readers all fail to receive.
//
// Stacked, right-aligned, one unit named per line: the digits line up so
// magnitudes compare down the column, each figure says what it measures, and
// the emphasis is weight and isolation instead of colour. Nothing in the block
// is sized from the data, so nothing in it can overflow.
func renderHeadline(w io.Writer, r *Report, width int, paint func(code, s string) string) {
	head := NewHeadline(r)
	deadVal, deadUnit := head.Figure()
	figs := []struct{ val, unit string }{
		{deadVal, deadUnit},
		{"~" + humanNum(r.DeadTokensPerSession), "dead tokens per session"},
		{fmt.Sprintf("~$%.2f", r.MoneyPerMonth), "per month at your usage"},
	}
	vw := 0
	for _, f := range figs {
		if l := utf8.RuneCountInString(f.val); l > vw {
			vw = l
		}
	}
	fmt.Fprintln(w)
	for _, f := range figs {
		pad := strings.Repeat(" ", vw-utf8.RuneCountInString(f.val))
		fmt.Fprintf(w, "  %s%s  %s\n", pad, paint(cBold, f.val), truncate(f.unit, width-4-vw))
	}
	for _, note := range headlineNotes(r) {
		for _, l := range wrap(note, width, "  ", "  ") {
			fmt.Fprintln(w, paint(cDim, l))
		}
	}
}

// headlineNotes are the qualifications on the figures above: what the total
// leaves out, and what the provider actually billed over the window. They were
// single lines running past two hundred characters, which at any real terminal
// width broke into fragments starting at column zero. They are wrapped now,
// and they sit directly under the figures they qualify.
func headlineNotes(r *Report) []string {
	var out []string
	// What the figure above does not say on its own: how many of the items that
	// never fired reap will actually condemn. Without this line the reader has
	// to subtract the two counts themselves and gets a number that matches
	// neither, which is exactly how the old wording read as an arithmetic error.
	if v := NewHeadline(r).Verdict(); v != "" {
		out = append(out, v)
	}
	// The figures above are an estimate, per session and per item. This is
	// neither: it is what the provider billed, and the only place the cache
	// split can be seen. Resident context is paid once as a cache creation and
	// again on every later turn as a cache read, which is why the read total
	// dwarfs the fresh input — that re-reading is the mechanism this tool has
	// always asserted and never measured.
	if r.Measured.Messages > 0 && r.Measured.CacheRead > 0 {
		out = append(out, fmt.Sprintf(
			"measured over the window: %s tokens re-read from cache across %d messages, against %s of fresh input",
			humanBig(r.Measured.CacheRead), r.Measured.Messages, humanBig(r.Measured.Input)))
	}
	// Without this line the total reads as the whole bill. It is not: the dead
	// items whose weight could never be measured contribute 0 to it.
	if r.DeadUnknownWeight > 0 {
		out = append(out, fmt.Sprintf(
			"a floor, not a total: %d unused %s (MCP servers, hooks) carry weight that was never measured",
			r.DeadUnknownWeight, plural(r.DeadUnknownWeight, "item")))
	}
	if r.DeadToolChars > 0 {
		// DeadToolChars is a total summed across every session; divide to show
		// the per-session average the label promises.
		perSession := r.DeadToolChars
		if r.Sessions > 1 {
			perSession = r.DeadToolChars / r.Sessions
		}
		out = append(out, fmt.Sprintf("init: ~%d chars of tool descriptions unused per session", perSession))
	}
	return out
}

// renderSections prints one block per category, in the order a reader acts on
// them. The rule fills the measure exactly rather than carrying sixty fixed
// dashes onto whatever width the terminal happens to be.
func renderSections(w io.Writer, r *Report, width int, paint func(code, s string) string) {
	for _, sec := range sectionTitles {
		rows := filterRows(r.Rows, sec.cat)
		if len(rows) == 0 {
			continue
		}
		// The gloss explains why a category costs tokens at all. It earns a
		// place when there is room for it and costs a wrapped heading when
		// there is not, so the measure decides rather than the author.
		title := sec.short
		if full := sec.short + " · " + sec.gloss; utf8.RuneCountInString(full)+6 <= width-2 {
			title = full
		}
		fmt.Fprintf(w, "\n  %s\n", paint(cCyan, rule(title, width-2)))
		renderGroups(w, rows, width, paint)
	}
}

// maxGroupRows caps how many rows of one verdict group the text report prints.
//
// A real stack produces 383 rows and the old report printed every one: 469
// lines, 289 of which said "REAP · unused". That is a single fact repeated 289
// times, and it buried both the caveats and the command under six screens.
// Six rows is enough to recognise what a group is made of — rows are sorted
// heaviest first, so these are the ones worth recognising — and the line after
// them states exactly how much was withheld and where the rest lives. Nothing
// is concealed: reap prune lists every candidate before it touches anything,
// and --json and --md still emit the whole inventory.
const maxGroupRows = 6

// renderGroups prints one table per verdict, heaviest verdict first.
func renderGroups(w io.Writer, rows []Row, width int, paint func(code, s string) string) {
	groups := groupByVerdict(rows)
	for _, v := range []string{VerdictReap, VerdictMute, VerdictReview, VerdictKeep, VerdictInfo} {
		items := groups[v]
		if len(items) == 0 {
			continue
		}

		label := v
		if v == VerdictInfo {
			label = "info"
		}
		// When every row in a group carries the same reason, the reason
		// belongs to the group rather than to each row. Saying it once here is
		// what removes a column of 289 identical cells from the page.
		if reason, same := commonReason(items); same && reason != "" {
			label += " · " + reason
		}

		total := 0
		for _, it := range items {
			total += it.Tokens
		}
		head := fmt.Sprintf("%s  %-22s %d %s", mark(v), label, len(items), plural(len(items), "item"))
		if total > 0 {
			head += fmt.Sprintf(" · ~%s tok/session", humanNum(total))
		}
		// Bold on the two groups that carry an action, dim on the rest. Weight
		// is the only emphasis a terminal has that a monochrome reader also
		// receives, so it is spent on the rows a reader is here to act on.
		emph := cDim
		if v == VerdictReap || v == VerdictMute {
			emph = cBold
		}
		fmt.Fprintf(w, "\n    %s\n", paint(emph, truncate(head, width-4)))

		shown := items
		if len(shown) > maxGroupRows {
			shown = shown[:maxGroupRows]
		}
		// The column header is structure, the rows are content: the header is
		// dimmed so the eye lands on the data first.
		if tbl := layout(groupColumns(shown), width, 4); len(tbl) > 0 {
			fmt.Fprintln(w, paint(cDim, tbl[0]))
			for _, l := range tbl[1:] {
				fmt.Fprintln(w, l)
			}
		}

		if rest := len(items) - len(shown); rest > 0 {
			restTok := 0
			for _, it := range items[len(shown):] {
				restTok += it.Tokens
			}
			more := fmt.Sprintf("… %d more", rest)
			if restTok > 0 {
				more += fmt.Sprintf(" · ~%s tok/session", humanNum(restTok))
			}
			more += " · full list: reap --md"
			fmt.Fprintf(w, "    %s\n", paint(cDim, truncate(more, width-4)))
		}
	}
}

// groupColumns builds the cells for one verdict group.
//
// The empty strings are deliberate. layout drops any column whose cells are
// all empty, which is what keeps USES, LAST and PERM out of the prose and hook
// tables instead of printing them there as a field of dashes — and what
// removes NOTE from a group whose rows all share one reason.
func groupColumns(rows []Row) []col {
	n := len(rows)
	glyph := make([]string, n)
	name := make([]string, n)
	tok := make([]string, n)
	src := make([]string, n)
	perm := make([]string, n)
	uses := make([]string, n)
	last := make([]string, n)
	note := make([]string, n)

	_, sameReason := commonReason(rows)
	for i, r := range rows {
		glyph[i] = mark(r.Verdict)
		name[i] = r.Name
		tok[i] = tokens(r.Tokens, r.WeightUnknown)
		src[i] = shortSource(r.Source)
		if p := permDisplay(r); p != "-" {
			perm[i] = p
		}
		if r.Verdict != VerdictInfo {
			uses[i] = fmt.Sprintf("%d", r.Uses)
			last[i] = age(r.LastUsed)
		}
		if !sameReason && r.Verdict != VerdictInfo {
			note[i] = r.Reason
		}
	}
	return []col{
		{cells: glyph},
		{head: "NAME", flex: true, cells: name},
		{head: "TOK", right: true, cells: tok},
		{head: "SRC", cells: src, shed: 1},
		{head: "PERM", right: true, cells: perm, shed: 3},
		{head: "USES", right: true, cells: uses},
		{head: "LAST", right: true, cells: last, shed: 2},
		{head: "NOTE", cells: note},
	}
}

// commonReason reports the reason shared by every row, and whether they share
// one at all.
func commonReason(rows []Row) (string, bool) {
	if len(rows) == 0 {
		return "", false
	}
	first := rows[0].Reason
	for _, r := range rows[1:] {
		if r.Reason != first {
			return "", false
		}
	}
	return first, true
}

// renderWarnings prints the caveats, wrapped with a hanging indent.
//
// Each one says the verdicts above may be wrong about an entire platform, and
// the longest in a real run is 379 characters. Printed as one line it became
// five ragged rows all starting at column zero, so two adjacent warnings were
// indistinguishable from one. The hanging indent is what keeps each one a
// single visual block.
func renderWarnings(w io.Writer, r *Report, width int, paint func(code, s string) string) {
	if len(r.Warnings) == 0 {
		return
	}
	title := fmt.Sprintf("%d %s", len(r.Warnings), plural(len(r.Warnings), "warning"))
	fmt.Fprintf(w, "\n  %s\n", paint(cCyan, rule(title, width-2)))
	for _, warn := range r.Warnings {
		for _, l := range wrap(warn.Path+": "+warn.Msg, width, "    ", "      ") {
			fmt.Fprintln(w, paint(cDim, l))
		}
	}
}

// renderActions closes the report with what to do about it.
//
// This block is the answer, and in a terminal the end of the output is the
// part still on screen when the prompt returns — the top has scrolled away. So
// it repeats the count and the reclaimable weight beside the command that acts
// on them, which makes the last screenful self-contained: what to run, and
// what running it buys.
func renderActions(w io.Writer, r *Report, width int, paint func(code, s string) string) {
	var muteNames []string
	muteTokens := 0
	for _, row := range r.Rows {
		if row.Verdict == VerdictMute {
			muteNames = append(muteNames, row.Name)
			muteTokens += row.Tokens
		}
	}

	if r.DeadCount > 0 || len(muteNames) > 0 {
		fmt.Fprintf(w, "\n  %s\n", paint(cCyan, rule("DO", width-2)))
		type action struct{ cmd, gain string }
		var acts []action
		if r.DeadCount > 0 {
			acts = append(acts, action{"reap prune", fmt.Sprintf("%d %s · ~%s tok/session reclaimed",
				r.DeadCount, plural(r.DeadCount, "item"), humanNum(r.DeadTokensPerSession))})
		}
		if len(muteNames) > 0 {
			acts = append(acts, action{"reap mute", fmt.Sprintf("%d %s · ~%s tok · %s",
				len(muteNames), plural(len(muteNames), "skill"), humanNum(muteTokens),
				strings.Join(muteNames, ", "))})
		}
		cw := 0
		for _, a := range acts {
			if l := utf8.RuneCountInString(a.cmd); l > cw {
				cw = l
			}
		}
		for _, a := range acts {
			pad := strings.Repeat(" ", cw-utf8.RuneCountInString(a.cmd))
			fmt.Fprintf(w, "    %s%s   %s\n", paint(cBold, a.cmd), pad, truncate(a.gain, width-7-cw))
		}
	}

	fmt.Fprintln(w)
	for _, l := range wrap("All estimates use chars/3.7 ≈ tokens. Prune is reversible: reap restore --all",
		width, "  ", "  ") {
		fmt.Fprintln(w, paint(cDim, l))
	}
	fmt.Fprintln(w)
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

// RenderMarkdown writes the report as a shareable Markdown document.
func RenderMarkdown(w io.Writer, r *Report) {
	fmt.Fprintf(w, "# skillreaper report\n\n")
	fmt.Fprintf(w, "Window: last %d days · %d sessions analyzed\n\n", r.WindowDays, r.Sessions)
	fmt.Fprintf(w, "**%s · ~%d dead tokens/session · ~$%.2f/month**\n",
		NewHeadline(r).Line(), r.DeadTokensPerSession, r.MoneyPerMonth)
	if r.DeadUnknownWeight > 0 {
		fmt.Fprintf(w, "\nA floor, not a total: %d unused items (MCP servers, hooks) carry weight that was never measured.\n",
			r.DeadUnknownWeight)
	}

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
			if row.WeightUnknown {
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

// MalformedSummary describes what actually went wrong while reading
// transcripts. "2 unreadable lines" is true of a corrupt file, a truncated
// one, and a healthy file holding one oversized record, and a reader deciding
// whether to trust the verdicts needs to tell those apart. Falls back to the
// undifferentiated total when no kind was recorded, so older callers and
// merged stats without a breakdown still read sensibly.
func MalformedSummary(r *Report) string {
	kinds := []struct {
		n    int
		noun string
	}{
		{r.UnreadableFiles, "unreadable file"},
		{r.TruncatedReads, "truncated read"},
		{r.OversizedLines, "oversized record"},
		{r.UnparsableLines, "unparsable line"},
	}
	var parts []string
	for _, k := range kinds {
		if k.n == 0 {
			continue
		}
		noun := k.noun
		if k.n != 1 {
			noun += "s"
		}
		parts = append(parts, fmt.Sprintf("%d %s", k.n, noun))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%d unreadable lines", r.MalformedLines)
	}
	return strings.Join(parts, ", ")
}

// plural appends an "s" unless n is exactly one. Only regular plurals are
// needed here, and a count of one is not a rare edge: a single held-back item
// produces exactly one warning, which is the moment the tool is asking to be
// trusted about evidence.
func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// humanBig formats a large count compactly: 3738237538 -> "3.7B". The measured
// totals run into the billions over a month of transcripts, and humanChars
// only scales to "k", which would print 3738237.5k.
func humanBig(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprintf("%d", n)
}
