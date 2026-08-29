package report

import (
	"bytes"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/thousandflowers/skillreaper/internal/scan"
	"github.com/thousandflowers/skillreaper/internal/usage"
)

// crowdedReport is shaped like a real run rather than like a fixture: many
// rows in one group, names long enough to force truncation, mixed reasons so
// the NOTE column survives, an unmeasurable weight, and a warning that runs
// past two hundred characters. The width guarantees below are only worth
// asserting against data that could break them.
func crowdedReport() *Report {
	st := usage.NewStats(30)
	st.Sessions = 60
	st.Uses[scan.CatSkill]["kept-one"] = 9
	st.Last[scan.CatSkill]["kept-one"] = time.Now().AddDate(0, 0, -3)

	items := []scan.Item{
		{Category: scan.CatSkill, Name: "kept-one", Source: "personal", DescChars: 400, Removable: true},
		{Category: scan.CatMCP, Name: "an-mcp-server-with-no-measurable-weight", Source: "user-config", Removable: true},
		{Category: scan.CatProse, Name: "~/.claude/rules/some/deeply/nested/always-loaded-prose.md", Source: "global", DescChars: 4000},
	}
	for i := 0; i < 40; i++ {
		items = append(items, scan.Item{
			Category:  scan.CatSkill,
			Name:      strings.Repeat("very-long-plugin-skill-name-", 3) + string(rune('a'+i%26)),
			Source:    "plugin:some-very-long-plugin-name@marketplace",
			DescChars: 400 + i*13,
			Removable: true,
		})
	}
	return Build(items, st, []scan.Warning{{
		Path: "/home/someone/.config/some-agent",
		Msg: "usage is not counted because this platform exposes no session transcripts at all, " +
			"so nothing whatsoever can be observed about its items; they are shown as REVIEW rather " +
			"than REAP or MUTE, which is a statement about the evidence and not about the items.",
	}}, Opts{MinSessions: 10, PricePerMTok: 3.0, Cutoff: time.Now().AddDate(0, 0, -30)})
}

// TestTextReportNeverExceedsMeasure is the 80-column constraint expressed as
// code rather than as a habit.
//
// The old renderer put 409 of its 469 lines past 80 columns, the widest at
// 379, because every width came from the data: a fixed 60-dash rule appended
// to a title, a headline box sized to its own text, a name column pinned at 44
// regardless of what shared the line. Nothing may now be wider than the
// measure it was given, at any measure.
func TestTextReportNeverExceedsMeasure(t *testing.T) {
	for _, width := range []int{60, 72, 80, 96, 100, 120, 200} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			t.Setenv("COLUMNS", strconv.Itoa(width))
			// Below minWidth the report holds at its floor rather than
			// shrinking into illegibility, so that is the bound to check.
			bound := width
			if bound < minWidth {
				bound = minWidth
			}
			for _, r := range []*Report{crowdedReport(), fixtureReport()} {
				var buf bytes.Buffer
				RenderText(&buf, r, false)
				for i, line := range strings.Split(buf.String(), "\n") {
					if n := utf8.RuneCountInString(line); n > bound {
						t.Errorf("width %d: line %d is %d columns:\n%s", width, i+1, n, line)
					}
				}
			}
		})
	}
}

// TestTextReportEmitsNoEscapesWithoutColor covers NO_COLOR, --no-color and
// every non-terminal destination at once: all three arrive here as
// color=false, and the only safe behaviour is that not one escape byte is
// written.
func TestTextReportEmitsNoEscapesWithoutColor(t *testing.T) {
	t.Setenv("COLUMNS", "80")
	var buf bytes.Buffer
	RenderText(&buf, crowdedReport(), false)
	if strings.Contains(buf.String(), "\x1b") {
		t.Error("color disabled but an ANSI escape was written")
	}
}

// everyTextView is every human-readable renderer the CLI can print. The width
// and colour guarantees belong to all of them, not only to the default report:
// gap put its payload table at 83 columns, route ran prose to 109, and the
// prune by-source summary reached 97. A reader on an 80-column terminal met
// those just as often as the report.
func everyTextView() []struct {
	name   string
	render func(w io.Writer, r *Report, color bool)
} {
	return []struct {
		name   string
		render func(w io.Writer, r *Report, color bool)
	}{
		{"report", RenderText},
		{"gap", RenderGap},
		{"by-project", RenderByProject},
		{"route", func(w io.Writer, r *Report, color bool) {
			RenderRoutePlan(w, BuildRoutePlan(r, 0), color)
		}},
		{"source-totals", func(w io.Writer, r *Report, _ bool) {
			var dead []Row
			for _, row := range r.Rows {
				if row.Verdict == VerdictReap {
					dead = append(dead, row)
				}
			}
			RenderSourceTotals(w, SourceTotals(dead), len(dead), func(n int) string { return humanNum(n) })
		}},
	}
}

func TestEveryTextViewFitsMeasure(t *testing.T) {
	for _, width := range []int{60, 80, 100, 120} {
		for _, view := range everyTextView() {
			t.Run(view.name+"/"+strconv.Itoa(width), func(t *testing.T) {
				t.Setenv("COLUMNS", strconv.Itoa(width))
				bound := width
				if bound < minWidth {
					bound = minWidth
				}
				var buf bytes.Buffer
				view.render(&buf, crowdedReport(), false)
				for i, line := range strings.Split(buf.String(), "\n") {
					if n := utf8.RuneCountInString(line); n > bound {
						t.Errorf("%s at %d: line %d is %d columns:\n%s", view.name, width, i+1, n, line)
					}
				}
			})
		}
	}
}

func TestEveryTextViewEmitsNoEscapesWithoutColor(t *testing.T) {
	t.Setenv("COLUMNS", "80")
	for _, view := range everyTextView() {
		var buf bytes.Buffer
		view.render(&buf, crowdedReport(), false)
		if strings.Contains(buf.String(), "\x1b") {
			t.Errorf("%s: color disabled but an ANSI escape was written", view.name)
		}
	}
}

// TestUtilBarSeparatesNoneFromNearlyNone is the defect that made the
// utilization view misreport its own headline finding: pct/10 floored 0% and
// 9% to the same ten empty segments, so "never fired once" and "fires, but
// rarely" drew identically in the one place that distinction is the point.
func TestUtilBarSeparatesNoneFromNearlyNone(t *testing.T) {
	none, sliver := utilBar(0), utilBar(2)
	if none == sliver {
		t.Errorf("0%% and 2%% render identically: %q", none)
	}
	if strings.ContainsRune(sliver, '▰') {
		t.Errorf("2%% should not fill a whole segment, got %q", sliver)
	}
	for _, pct := range []int{-5, 0, 1, 50, 99, 100, 250} {
		if n := utf8.RuneCountInString(utilBar(pct)); n != 10 {
			t.Errorf("utilBar(%d) is %d columns, want 10: %q", pct, n, utilBar(pct))
		}
	}
}

// TestWrapMarkerDoesNotRepeat: a block that leads with a marker continues
// under it on spaces. Repeating the marker down the left edge made one wrapped
// warning read as three separate ones.
func TestWrapMarkerDoesNotRepeat(t *testing.T) {
	got := wrap(strings.Repeat("word ", 40), 40, "  !  ", "     ")
	if len(got) < 2 {
		t.Fatal("expected the text to wrap")
	}
	for i, l := range got[1:] {
		if strings.Contains(l, "!") {
			t.Errorf("continuation line %d repeats the marker: %q", i+2, l)
		}
	}
}

// TestVerdictsSurviveMonochrome is the no-colour-as-sole-carrier constraint.
//
// With every escape code off, each verdict must still be distinguishable by
// something a monochrome reader receives. Two carriers are checked: the word,
// and a glyph no other verdict uses.
func TestVerdictsSurviveMonochrome(t *testing.T) {
	seen := map[string]string{}
	for verdict, glyph := range marks {
		if utf8.RuneCountInString(glyph) != 1 {
			t.Errorf("%s: glyph %q must be exactly one column", verdict, glyph)
		}
		if glyph[0] > 127 {
			t.Errorf("%s: glyph %q must be ASCII so it survives any font", verdict, glyph)
		}
		if other, dup := seen[glyph]; dup {
			t.Errorf("%s and %s share the glyph %q — colour would be the only thing telling them apart",
				verdict, other, glyph)
		}
		seen[glyph] = verdict
	}

	t.Setenv("COLUMNS", "80")
	var buf bytes.Buffer
	RenderText(&buf, crowdedReport(), false)
	out := buf.String()
	for _, verdict := range []string{VerdictReap, VerdictKeep} {
		if !strings.Contains(out, verdict) {
			t.Errorf("verdict %s is not named anywhere in a monochrome report", verdict)
		}
	}
}

// TestWidthPrecedence pins the order the measure is decided in: an explicit
// COLUMNS beats everything, and a destination that cannot be measured — a
// buffer, a pipe, a redirect — falls back rather than running unbounded.
func TestWidthPrecedence(t *testing.T) {
	var buf bytes.Buffer
	t.Setenv("COLUMNS", "")
	if got := termWidth(&buf); got != fallbackWidth {
		t.Errorf("unmeasurable writer = %d, want the %d fallback", got, fallbackWidth)
	}
	t.Setenv("COLUMNS", "133")
	if got := termWidth(&buf); got != maxWidth {
		t.Errorf("COLUMNS=133 = %d, want it clamped to %d", got, maxWidth)
	}
	t.Setenv("COLUMNS", "31")
	if got := termWidth(&buf); got != minWidth {
		t.Errorf("COLUMNS=31 = %d, want it held at %d", got, minWidth)
	}
	t.Setenv("COLUMNS", "not-a-number")
	if got := termWidth(&buf); got != fallbackWidth {
		t.Errorf("garbage COLUMNS = %d, want the %d fallback", got, fallbackWidth)
	}
}

func TestWrapNeverExceedsWidth(t *testing.T) {
	long := strings.Repeat("some ordinary words that need breaking ", 12)
	for _, width := range []int{60, 80, 100} {
		for _, line := range wrap(long, width, "    ", "  ") {
			if n := utf8.RuneCountInString(line); n > width {
				t.Errorf("width %d: wrapped line is %d columns: %q", width, n, line)
			}
		}
	}
	// A word longer than the measure cannot be broken without lying about
	// where it ends, so it is emitted whole rather than silently cut.
	if got := wrap(strings.Repeat("x", 200), 80, "  ", ""); len(got) != 1 {
		t.Errorf("an unbreakable word should stay one line, got %d", len(got))
	}
}

func TestRuleFillsExactlyToMeasure(t *testing.T) {
	for _, width := range []int{60, 80, 100} {
		if n := utf8.RuneCountInString(rule("SKILLS", width)); n != width {
			t.Errorf("rule at %d is %d columns", width, n)
		}
	}
	// A title wider than the measure must not produce a negative pad.
	if n := utf8.RuneCountInString(rule(strings.Repeat("T", 200), 80)); n < 80 {
		t.Errorf("over-long title produced a short rule: %d", n)
	}
}

// TestLayoutDropsEmptyColumns is what keeps USES, LAST and PERM out of the
// prose and hook tables. A column of dashes costs width and says nothing.
func TestLayoutDropsEmptyColumns(t *testing.T) {
	cols := []col{
		{head: "NAME", flex: true, cells: []string{"a", "b"}},
		{head: "TOK", right: true, cells: []string{"1", "2"}},
		{head: "USES", cells: []string{"", ""}},
	}
	out := layout(cols, 80, 2)
	if strings.Contains(out[0], "USES") {
		t.Errorf("an all-empty column was kept:\n%s", strings.Join(out, "\n"))
	}
	if !strings.Contains(out[0], "TOK") {
		t.Errorf("a populated column was dropped:\n%s", strings.Join(out, "\n"))
	}
}

// TestLayoutShedsRatherThanOverflows: when the measure cannot hold every
// column, the expendable ones go and the table still fits. Clamping the name
// column up to a floor instead used to produce a table wider than its terminal.
func TestLayoutShedsRatherThanOverflows(t *testing.T) {
	cols := []col{
		{head: "NAME", flex: true, cells: []string{"a-fairly-long-item-name"}},
		{head: "TOK", right: true, cells: []string{"1,234"}},
		{head: "SRC", cells: []string{"plugin"}, shed: 1},
		{head: "PERM", cells: []string{"all"}, shed: 3},
		{head: "USES", cells: []string{"12"}},
		{head: "LAST", cells: []string{"never"}, shed: 2},
		{head: "NOTE", cells: []string{"no-transcript"}},
	}
	for _, width := range []int{40, 50, 60, 80} {
		for _, line := range layout(cols, width, 4) {
			if n := utf8.RuneCountInString(line); n > width {
				t.Errorf("width %d: %d columns: %q", width, n, line)
			}
		}
	}
	// PERM is the first to go, and NOTE — the reason behind the verdict — is
	// never traded away for it.
	narrow := layout(cols, 50, 4)
	if strings.Contains(narrow[0], "PERM") {
		t.Errorf("PERM should have been shed first:\n%s", strings.Join(narrow, "\n"))
	}
	if !strings.Contains(narrow[0], "NOTE") {
		t.Errorf("NOTE must survive shedding:\n%s", strings.Join(narrow, "\n"))
	}
}

func TestHumanNumGroupsThousands(t *testing.T) {
	for _, c := range []struct {
		in   int
		want string
	}{{0, "0"}, {999, "999"}, {1000, "1,000"}, {19755, "19,755"}, {-4200, "-4,200"}} {
		if got := humanNum(c.in); got != c.want {
			t.Errorf("humanNum(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAgeAnswersStalenessNotDate(t *testing.T) {
	if got := age(time.Time{}); got != "never" {
		t.Errorf("zero time = %q, want never", got)
	}
	if got := age(time.Now()); got != "today" {
		t.Errorf("now = %q, want today", got)
	}
	if got := age(time.Now().AddDate(0, 0, -8)); got != "8d" {
		t.Errorf("8 days ago = %q", got)
	}
	if got := age(time.Now().AddDate(0, 0, -400)); got != "13mo" {
		t.Errorf("400 days ago = %q", got)
	}
}
