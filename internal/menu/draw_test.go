package menu

// How things are drawn: nothing outside the window, colours the terminal really
// has, a figure shortened before it is cut, text wrapped at the window's width.

import (
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/term"
)

func TestPutNeverWritesOutsideTheWindow(t *testing.T) {
	outside := []struct {
		name string
		y, x int
	}{
		{"above the first row", -1, 0},
		{"below the last row", testRows, 0},
		{"left of the first column", 0, -1},
		{"right of the last column", 0, testCols},
	}
	for _, c := range outside {
		t.Run(c.name, func(t *testing.T) {
			m, s, _ := newMenu(t, Data{Report: &report.Report{}})

			m.put(c.y, c.x, "text", plain)
			s.Refresh()

			mustEqual(t, "screen", strings.Join(s.Lines(), ""), "")
		})
	}

	t.Run("text that runs past the right edge is cut there", func(t *testing.T) {
		m, s, _ := newMenu(t, Data{Report: &report.Report{}})

		m.put(2, testCols-4, "abcdefgh", plain)
		s.Refresh()

		mustEqual(t, "row", strings.TrimSpace(s.Lines()[2]), "abc") // the last column is never written
	})
}

func TestColoursAreTheOnesTheTerminalHas(t *testing.T) {
	const message, cursorRow, warning = "by plugin", "▸ your config", "deleted for good"
	tint := func(n int) term.Style { return term.Style{FG: term.Idx(n)} }
	cursorOn := term.Style{FG: term.Idx(0), BG: term.Idx(6), Bold: true}
	onRed := term.Style{FG: term.Idx(16), BG: term.Idx(196), Bold: true}
	reversed := term.Style{Reverse: true, Bold: true}
	cases := []struct {
		name            string
		colours         int
		light           bool
		message, cursor term.Style // the status message, the cursor's row
		bar, warning    term.Style // the first cell of the bar, the receipt's delete warning
	}{
		{"256 colours on a dark window", 256, false, tint(114), cursorOn, tint(46), onRed},
		{"256 colours on a light window: darker tints", 256, true, tint(28), cursorOn, tint(46), onRed},
		{"eight colours: the basic ones, and no ramp", 8, false, tint(2), cursorOn, plain, reversed},
		{"no colour: the cursor and the warning are reversed", 0, false, tint(2), reversed, plain, reversed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x := open(t, flowData())
			x.s.ColorCount, x.s.LightBackground = c.colours, c.light
			x.s.watch = []string{message, cursorRow, warning}

			x.press('g', 'd', applyKey).run() // by plugin, a delete on its first group, the receipt

			mustEqual(t, "message", x.s.styles[message], c.message)
			mustEqual(t, "cursor row", x.s.styles[cursorRow], c.cursor)
			mustEqual(t, "delete warning", x.s.styles[warning], c.warning)
			mustEqual(t, "bar", x.s.StyleAt(5, 3), c.bar)
		})
	}
}

// bigRows is a figure in the big font, as it sits on its three rows.
func bigRows(figure string) [3]string {
	var rows [3]string
	for _, c := range figure {
		g := bigGlyph(c)
		for n := range rows {
			rows[n] += g[n] + " "
		}
	}
	return rows
}

func TestHeadlineFigureIsShortenedBeforeItIsCut(t *testing.T) {
	heavy := func(money float64) Data {
		rep := &report.Report{Sessions: 9, SessionsPerMonth: 40, DeadTokensPerSession: 1234567, MoneyPerMonth: money,
			Rows: []report.Row{raw("heavy", tokens(1234567))}}
		return Data{Report: rep}
	}
	headline := func(lines []string) [3]string { return [3]string{lines[1], lines[2], lines[3]} }
	starts := func(t *testing.T, got [3]string, figure, words string) {
		t.Helper()
		want := bigRows(figure)
		if !strings.HasPrefix(got[0], "   "+want[0]+"  "+words) {
			t.Errorf("first row = %q, want %q then %q", got[0], want[0], words)
		}
		for n := 1; n < 3; n++ {
			if !strings.HasPrefix(got[n], "   "+strings.TrimRight(want[n], " ")) {
				t.Errorf("row %d = %q, want it to start with %q", n+1, got[n], want[n])
			}
		}
	}

	t.Run("tokens on a wide window are written in full", func(t *testing.T) {
		x := open(t, heavy(0)).run()

		starts(t, headline(x.final()), "1,234,567", "tokens wasted every session")
	})

	t.Run("tokens on a narrow window are shortened, with their words still beside them", func(t *testing.T) {
		x := openSized(t, testRows, listCols, heavy(0)).run()

		starts(t, headline(x.final()), "1.2M", "tokens wasted every session")
	})

	t.Run("money on a wide window is written in full", func(t *testing.T) {
		x := openSized(t, testRows, 140, heavy(1234567)).run()

		starts(t, headline(x.final()), "US$1,234,567.00", "wasted a month")
	})

	t.Run("money on a narrow window is shortened", func(t *testing.T) {
		x := openSized(t, testRows, listCols, heavy(1234567)).run()

		starts(t, headline(x.final()), "US$1.2M", "wasted a month")
	})

	t.Run("an amount wider than its column keeps every digit", func(t *testing.T) {
		x := openSized(t, testRows, listCols, heavy(1234567)).run()

		if got := x.final()[listY]; !strings.HasSuffix(got, "~US$1,234,567.00") {
			t.Errorf("group row = %q", got)
		}
	})

	t.Run("an amount under a cent says so, with no about in front of it", func(t *testing.T) {
		d := flowData()
		d.Report.MoneyPerMonth = 0.01 // 950 unused tokens cost a cent a month: every row costs less

		x := open(t, d).run()

		if got := lineWith(x.final(), "▸ suggest to prune"); !strings.HasSuffix(got, " <US$0.01") {
			t.Errorf("group row = %q", got)
		}
		mustNotShow(t, x.final(), "~<")
	})
}

func TestViewHeldByTheWheelFollowsTheCursorOutOfIt(t *testing.T) {
	long := &report.Report{Sessions: 9}
	for n := range 40 {
		long.Rows = append(long.Rows, raw("skill-"+itoa(100+n), tokens(1000-n)))
	}
	wheel := func(x *session) *session {
		x.s.FeedMouse(term.Mouse{X: 30, Y: listY, Kind: "wheel_down"})
		return x
	}

	t.Run("the wheel holds the view three rows down, the cursor on its first row", func(t *testing.T) {
		x := wheel(open(t, Data{Report: long}).press('g', 'g')).run()

		mustContain(t, x.final()[listY], "> ")
		mustContain(t, x.final()[listY], "skill-103")
	})

	t.Run("an arrow that takes the cursor above it lets the view go", func(t *testing.T) {
		x := wheel(open(t, Data{Report: long}).press('g', 'g')).press(up).run()

		mustContain(t, x.final()[listY], "skill-100")
		mustContain(t, lineWith(x.final(), "skill-102"), "> ")
	})

	t.Run("an arrow that stays inside it leaves the view held", func(t *testing.T) {
		x := wheel(open(t, Data{Report: long}).press('g', 'g')).press(down).run()

		mustContain(t, x.final()[listY], "skill-103")
		mustContain(t, lineWith(x.final(), "skill-104"), "> ")
	})
}

func TestWrapBreaksTextAtTheWidthItIsGiven(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		width int
		want  []string
	}{
		{"at spaces", "one two three", 7, []string{"one two", "three"}},
		{"runs of spaces and line breaks are one space", "one   two\n three", 9, []string{"one two", "three"}},
		{"a word longer than a line is cut across lines", "abcdefghij kl", 4, []string{"abcd", "efgh", "ij", "kl"}},
		{"what stood before a long word is written first", "ab abcdefgh", 4, []string{"ab", "abcd", "efgh"}},
		{"what is left of a long word shares its line", "abcdef g", 4, []string{"abcd", "ef g"}},
		{"nothing to wrap", "   ", 10, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := wrap(c.text, c.width)

			if strings.Join(got, "|") != strings.Join(c.want, "|") {
				t.Errorf("wrap = %q, want %q", got, c.want)
			}
		})
	}

	t.Run("on the card, a word wider than the window is not lost", func(t *testing.T) {
		path := strings.Repeat("x", 150)
		long := raw("x", func(r *report.Row) { r.Description = "see " + path + " for more" })

		got := open(t, Data{Report: &report.Report{Sessions: 9, Rows: []report.Row{long}}}).press(right, down, enter).run().final()

		mustEqual(t, "first line", strings.TrimSpace(got[17]), "see")
		mustEqual(t, "second line", strings.TrimSpace(got[18]), strings.Repeat("x", testCols-8))
		mustEqual(t, "third line", strings.TrimSpace(got[19]), strings.Repeat("x", 150-(testCols-8))+" for more")
	})
}
