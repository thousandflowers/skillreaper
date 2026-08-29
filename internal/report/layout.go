package report

import (
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/thousandflowers/skillreaper/internal/banner"
)

// Width limits for the text report.
//
// minWidth is the narrowest layout the table is allowed to compute. Below it
// the name column would be shorter than a useful prefix, and a table nobody
// can identify a row in is worse than one that overflows.
//
// maxWidth caps the measure. A terminal can be 300 columns wide; a line that
// wide is not read, it is scanned back and forth, and the eye loses the row it
// was on. Beyond this the extra space buys nothing the reader wants, so the
// report stops growing and stays a fixed, comparable shape.
//
// fallbackWidth is what a report assumes when it cannot measure: a pipe, a
// redirect, a CI log. 80 is the width every one of those is safe at, and a
// fixed value keeps piped output byte-stable between runs, which is what makes
// it diffable.
const (
	minWidth      = 60
	maxWidth      = 100
	fallbackWidth = 80
)

// termWidth is the width the report lays itself out to.
//
// COLUMNS wins over the measured terminal because it is the only knob a person
// has: it is how you ask for a narrower report than your window, how a test
// pins a width, and how the captures in docs/renders are produced. The kernel
// is asked next. When neither answers — piped, redirected, no tty — the answer
// is fallbackWidth rather than "unlimited": output that is being captured is
// output someone will read later in an unknown window.
func termWidth(w io.Writer) int {
	if c, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && c > 0 {
		return clampWidth(c)
	}
	if _, cols, ok := banner.Detect(w); ok && cols > 0 {
		return clampWidth(cols)
	}
	return fallbackWidth
}

func clampWidth(c int) int {
	if c < minWidth {
		return minWidth
	}
	if c > maxWidth {
		return maxWidth
	}
	return c
}

// rule returns a horizontal rule that fills the measure exactly: a title, then
// dashes to the right edge. The old report appended a fixed 60 dashes to a
// title of unknown length, which put every section rule past 100 columns and
// wrapped it. Filling to a measured width is the only version that cannot.
func rule(title string, width int) string {
	head := "── " + title + " "
	pad := width - utf8.RuneCountInString(head)
	if pad < 2 {
		pad = 2
	}
	return head + strings.Repeat("─", pad)
}

// wrap breaks s into lines no wider than width. The first line is prefixed by
// indent and every line after it by hang, which is the whole prefix rather
// than something added to indent — that is what lets a block lead with a
// marker ("  !  ") and continue under it on plain spaces, instead of repeating
// the marker down the left edge as if each line were a new item.
//
// Warnings are whole paragraphs — the longest in a real run is 379 characters
// — and printed as single lines they became five ragged rows starting at
// column zero, which is the same unreadable fragment a table must never become.
// A hanging indent keeps each block visually one item.
func wrap(s string, width int, indent, hang string) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	first := width - utf8.RuneCountInString(indent)
	cont := width - utf8.RuneCountInString(hang)
	if first < 20 {
		first = 20
	}
	if cont < 20 {
		cont = 20
	}
	var out []string
	line := words[0]
	for _, word := range words[1:] {
		room := first
		if len(out) > 0 {
			room = cont
		}
		if utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) > room {
			out = append(out, line)
			line = word
			continue
		}
		line += " " + word
	}
	out = append(out, line)
	for i := range out {
		if i == 0 {
			out[i] = indent + out[i]
			continue
		}
		out[i] = hang + out[i]
	}
	return out
}

// humanNum groups thousands. 19755 and 19,755 hold the same value, but only
// one of them is read at a glance, and five- and six-digit token counts are
// the numbers this report exists to show. Text output only — every
// machine-readable format keeps the bare integer.
func humanNum(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// age renders "how stale is this" instead of "on what date did this happen".
// A date makes the reader do arithmetic against today to answer the only
// question the column is asked, and costs ten columns to do it. Text output
// only; humanTime still renders the ISO date for Markdown.
func age(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := int(time.Since(t).Hours() / 24)
	switch {
	case d <= 0:
		return "today"
	case d < 100:
		return strconv.Itoa(d) + "d"
	default:
		return strconv.Itoa(d/30) + "mo"
	}
}

// marks are the verdict glyphs. They are ASCII on purpose.
//
// The verdict is the one thing in this report that must never be ambiguous,
// and it used to be carried by red versus green — a distinction a meaningful
// share of readers do not receive, and one that survives neither a monochrome
// terminal, NO_COLOR, nor a pipe. A glyph is carried by shape, so it survives
// all of those; ASCII rather than Unicode so it also survives a terminal whose
// font has no box-drawing glyphs, and a screenshot scaled down. The full word
// still appears in the group header directly above the rows, so the glyph is
// never the only thing naming the verdict either.
var marks = map[string]string{
	VerdictReap:   "x",
	VerdictMute:   "~",
	VerdictReview: "?",
	VerdictKeep:   "=",
	VerdictInfo:   ".",
}

func mark(verdict string) string {
	if m, ok := marks[verdict]; ok {
		return m
	}
	return " "
}

// col is one table column. flex marks the single column that absorbs whatever
// width the fixed ones leave; right-aligned columns are the numeric ones, so
// their digits line up and magnitudes can be compared down the column.
//
// shed ranks a column's expendability when the measure cannot hold them all: 0
// never goes, and the highest number goes first. It encodes a judgment about
// what a reader is here for. A permission surface and a last-used date are
// context; the name, the weight, the use count and the reason for the verdict
// are the decision. So PERM goes before LAST, LAST before SRC, and the rest
// never go at all.
type col struct {
	head  string
	right bool
	flex  bool
	shed  int
	cells []string
}

// minFlex is the narrowest the name column may become. Below this a name is no
// longer identifiable, and a table of unidentifiable rows is worth less than
// the columns that were kept at its expense.
const minFlex = 16

// layout renders a table that fits width exactly, without ever wrapping.
//
// Two rules do the work. Columns whose every cell is empty carry no
// information and are dropped, which is what removes USES/LAST/PERM from the
// prose and hook sections instead of printing a field of dashes. Whatever
// width the surviving fixed columns leave goes to the flex column, and its
// cells are truncated to it — so the line length is a function of the measure,
// not of the longest name that happened to be installed.
//
// Cells are never painted. Colour is applied to whole assembled lines by the
// caller, which keeps every width calculation here a plain rune count and
// makes it impossible for an escape sequence to break alignment.
func layout(cols []col, width, indent int) []string {
	var keep []col
	for _, c := range cols {
		if c.flex || !allEmpty(c.cells) {
			keep = append(keep, c)
		}
	}
	if len(keep) == 0 {
		return nil
	}

	const gutter = 2

	// Shed expendable columns until the name column can have its floor. This
	// is the step that makes the width guarantee real at any measure: without
	// it a narrow terminal produced a table three columns wider than itself,
	// because the floor was being clamped upward into an overflow.
	var widths []int
	var fixed, flexAt int
	for {
		widths = make([]int, len(keep))
		fixed, flexAt = 0, -1
		for i, c := range keep {
			if c.flex {
				flexAt = i
				continue
			}
			n := utf8.RuneCountInString(c.head)
			for _, cell := range c.cells {
				if l := utf8.RuneCountInString(cell); l > n {
					n = l
				}
			}
			widths[i] = n
			fixed += n
		}
		fixed += gutter * (len(keep) - 1)
		if flexAt < 0 || width-indent-fixed >= minFlex {
			break
		}
		worst, at := 0, -1
		for i, c := range keep {
			if c.shed > worst {
				worst, at = c.shed, i
			}
		}
		if at < 0 {
			break
		}
		keep = append(keep[:at:at], keep[at+1:]...)
	}

	// Nothing droppable is left and the fixed columns still do not fit. A
	// fixed column is sized to its longest cell, so one long value — a skill
	// name, a project path — could push the table past the measure on its own,
	// with the flex column already at its floor. Trim the widest fixed column
	// a character at a time until the row fits; it truncates instead of the
	// table overflowing, and taking from the widest one spreads the loss where
	// it costs the least.
	if flexAt >= 0 {
		for width-indent-fixed < minFlex {
			widest, at := 0, -1
			for i, c := range keep {
				if c.flex {
					continue
				}
				floor := utf8.RuneCountInString(c.head)
				if floor < 3 {
					floor = 3
				}
				if widths[i] > floor && widths[i] > widest {
					widest, at = widths[i], i
				}
			}
			if at < 0 {
				break
			}
			widths[at]--
			fixed--
		}
	}

	if flexAt >= 0 {
		// Whatever is left, even if that is less than minFlex. minFlex is the
		// target the shedding above aims at, not a floor that may be clamped
		// up: fitting the measure is the guarantee, and a guarantee that
		// yields to a preference is not one.
		avail := width - indent - fixed
		if avail < 1 {
			avail = 1
		}
		natural := utf8.RuneCountInString(keep[flexAt].head)
		for _, cell := range keep[flexAt].cells {
			if l := utf8.RuneCountInString(cell); l > natural {
				natural = l
			}
		}
		if natural < avail {
			avail = natural
		}
		widths[flexAt] = avail
	}

	rows := 0
	for _, c := range keep {
		if len(c.cells) > rows {
			rows = len(c.cells)
		}
	}

	pad := strings.Repeat(" ", indent)
	out := make([]string, 0, rows+1)
	out = append(out, pad+tableLine(keep, widths, -1, gutter))
	for i := 0; i < rows; i++ {
		out = append(out, pad+tableLine(keep, widths, i, gutter))
	}
	return out
}

// tableLine assembles one row; i < 0 renders the header row. Trailing space is
// trimmed so a redirected report has no invisible padding at the line ends.
func tableLine(cols []col, widths []int, i, gutter int) string {
	var b strings.Builder
	for j, c := range cols {
		cell := c.head
		if i >= 0 {
			cell = ""
			if i < len(c.cells) {
				cell = c.cells[i]
			}
		}
		cell = truncate(cell, widths[j])
		gap := widths[j] - utf8.RuneCountInString(cell)
		if c.right {
			b.WriteString(strings.Repeat(" ", gap))
			b.WriteString(cell)
		} else {
			b.WriteString(cell)
			if j < len(cols)-1 {
				b.WriteString(strings.Repeat(" ", gap))
			}
		}
		if j < len(cols)-1 {
			b.WriteString(strings.Repeat(" ", gutter))
		}
	}
	return strings.TrimRight(b.String(), " ")
}

func allEmpty(cells []string) bool {
	for _, c := range cells {
		if c != "" {
			return false
		}
	}
	return true
}

// tokens renders a per-session token weight for the text report, or a marked
// blank when the weight was never measurable. "?" is deliberately not a
// number: an MCP server's weight is unknown, not zero, and a column of
// right-aligned figures is exactly where a 0 would be read as "free".
func tokens(t int, unknown bool) string {
	if unknown {
		return "?"
	}
	return humanNum(t)
}
