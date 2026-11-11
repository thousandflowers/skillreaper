package menu

import (
	"strings"
	"unicode/utf8"

	"github.com/thousandflowers/skillreaper/internal/term"
)

const (
	tintRed   = 60
	tintAmber = 61
	tintGreen = 62
	strikeRun = "̶"
)

var (
	plain = term.Style{}
	bold  = term.Style{Bold: true}
	dim   = term.Style{Dim: true}
	// health runs from red to green; ramp is the same the other way round.
	health = []int{196, 202, 208, 214, 220, 226, 190, 154, 118, 46}
	ramp   = []int{46, 118, 154, 190, 226, 220, 214, 208, 202, 196}
	// tints are one colour each on a dark window, on a light one, and with eight colours.
	tints = map[int][3]int{tintRed: {174, 124, 1}, tintAmber: {180, 130, 3}, tintGreen: {114, 28, 2}}
)

func cells(s string) int              { return term.Cells(s) }
func cut(s string, n int) string      { return term.Cut(s, n) }
func pad(s string, n int) string      { return term.Pad(s, n) }
func cutMid(s string, n int) string   { return term.CutMid(s, n) }
func runes(s string) int              { return utf8.RuneCountInString(s) }
func asBold(st term.Style) term.Style { st.Bold = true; return st }

// put writes at a cell, cut at the window's edge. Outside the window it does
// nothing.
func (m *Menu) put(y, x int, s string, st term.Style) {
	h, wd := m.s.Size()
	if y < 0 || y >= h || x < 0 || x >= wd {
		return
	}
	m.s.Put(y, x, cut(s, wd-x-1), st)
}

func (m *Menu) red() term.Style { return term.Style{FG: term.Idx(1)} }

// hl is the cursor's row and whatever is switched on.
func (m *Menu) hl() term.Style {
	if m.s.Colors() == 0 {
		return term.Style{Reverse: true, Bold: true}
	}
	return term.Style{FG: term.Idx(0), BG: term.Idx(6), Bold: true}
}

// tint is a colour from the 256 palette, or its nearest basic one on a smaller
// terminal.
func (m *Menu) tint(n int) term.Style {
	t := tints[n]
	switch {
	case m.s.Colors() < 256:
		return term.Style{FG: term.Idx(t[2])}
	case m.s.Light():
		return term.Style{FG: term.Idx(t[1])}
	}
	return term.Style{FG: term.Idx(t[0])}
}

// rampColour is the colour at t along a ramp, as text or as a background.
func (m *Menu) rampColour(r []int, t float64, bg bool) term.Style {
	at := min(len(r)-1, max(0, int(t*float64(len(r)))))
	switch {
	case m.s.Colors() < 256:
		return term.Style{Reverse: bg}
	case bg:
		return term.Style{FG: term.Idx(16), BG: term.Idx(r[at])}
	}
	return term.Style{FG: term.Idx(r[at])}
}

// tooSmall: under the size a screen needs, it says so and no key can act on what
// cannot be seen.
func (m *Menu) tooSmall(rows, cols int, hint string) bool {
	h, wd := m.s.Size()
	if h >= rows && wd >= cols {
		return false
	}
	m.s.Erase()
	m.put(0, 0, "window too small: needs "+itoa(cols)+"x"+itoa(rows), plain)
	m.put(1, 0, hint, dim)
	m.s.Refresh()
	return true
}

const partials = " ▏▎▍▌▋▊▉"

// grad is a bar whose colour depends on the position of the cell, so a fuller
// bar reaches further along the ramp.
func (m *Menu) grad(y, x, width int, frac float64, r []int) {
	filled := frac * float64(width)
	for i := 0; i < width; i++ {
		t := float64(i) / float64(max(1, width-1))
		switch {
		case float64(i+1) <= filled:
			m.put(y, x+i, "█", m.rampColour(r, t, false))
		case float64(i) < filled:
			m.put(y, x+i, string([]rune(partials)[int((filled-float64(i))*8)]), m.rampColour(r, t, false))
		default:
			m.put(y, x+i, "░", dim)
		}
	}
}

var glyph = map[rune][3]string{
	'0': {"█▀█", "█ █", "▀▀▀"}, '1': {"▄█ ", " █ ", "▀▀▀"}, '2': {"▀▀█", "█▀▀", "▀▀▀"}, '3': {"▀▀█", " ▀█", "▀▀▀"}, '4': {"█ █", "▀▀█", "  ▀"},
	'5': {"█▀▀", "▀▀█", "▀▀▀"}, '6': {"█▀▀", "█▀█", "▀▀▀"}, '7': {"▀▀█", "  █", "  ▀"}, '8': {"█▀█", "█▀█", "▀▀▀"}, '9': {"█▀█", "▀▀█", "▀▀▀"},
	'<': {"▄▀ ", "▀▄ ", "   "}, ',': {"  ", "  ", "█ "}, '.': {"  ", "  ", "▄ "}, 'k': {"█ ▄", "██ ", "▀ ▀"}, '€': {"▄▀▀", "█▀ ", " ▀▀"}, '$': {"▄█▄", "▀█▄", "▀█▀"},
}

var font = map[rune]string{
	'a': "█▀█|█▀█|▀ ▀", 'b': "█▀▄|█▀▄|▀▀ ", 'c': "█▀▀|█  |▀▀▀", 'd': "█▀▄|█ █|▀▀ ", 'e': "█▀▀|█▀▀|▀▀▀", 'f': "█▀▀|█▀▀|▀  ", 'g': "█▀▀|█ █|▀▀▀",
	'h': "█ █|█▀█|▀ ▀", 'i': "█|█|▀", 'j': "  █|█ █|▀▀▀", 'k': "█ █|█▀▄|▀ ▀", 'l': "█  |█  |▀▀▀", 'm': "█▀▄▀█|█ ▀ █|▀   ▀", 'n': "█▄ █|█ ▀█|▀  ▀", 'o': "█▀█|█ █|▀▀▀",
	'p': "█▀█|█▀▀|▀  ", 'q': "█▀█|█ █|▀▀█", 'r': "█▀█|█▀▄|▀ ▀", 's': "█▀▀|▀▀█|▀▀▀", 't': "▀█▀| █ | ▀ ", 'u': "█ █|█ █|▀▀▀", 'v': "█ █|█ █| ▀ ", 'w': "█   █|█ █ █|▀▀▀▀▀",
	'x': "█ █|▄▀▄|▀ ▀", 'y': "█ █|▀█▀| ▀ ", 'z': "▀▀█|▄▀ |▀▀▀", '0': "█▀█|█ █|▀▀▀", '1': "▄█ | █ |▀▀▀", '2': "▀▀█|█▀▀|▀▀▀", '3': "▀▀█| ▀█|▀▀▀", '4': "█ █|▀▀█|  ▀",
	'5': "█▀▀|▀▀█|▀▀▀", '6': "█▀▀|█▀█|▀▀▀", '7': "▀▀█|  █|  ▀", '8': "█▀█|█▀█|▀▀▀", '9': "█▀█|▀▀█|▀▀▀", '-': "  |▀▀|  ", '_': "  |  |▀▀", '.': " | |▀", ',': " | |█",
}

func lower(c rune) rune {
	if c >= 'A' && c <= 'Z' {
		return c + 32
	}
	return c
}

func inFont(s string) bool {
	for _, c := range s {
		if _, ok := font[lower(c)]; ok {
			return true
		}
	}
	return false
}

// bigGlyph is one character of a figure in the big font. A currency with no big
// glyph still shows.
func bigGlyph(c rune) [3]string {
	if g, ok := glyph[c]; ok {
		return g
	}
	if f, ok := font[lower(c)]; ok {
		parts := strings.Split(f, "|")
		return [3]string{parts[0], parts[1], parts[2]}
	}
	return [3]string{"  ", string(c) + " ", "  "}
}

func bigWidth(s string) int {
	n := 0
	for _, c := range s {
		n += runes(bigGlyph(c)[0]) + 1
	}
	return n
}

// big draws a figure three rows tall and returns the column after it.
func (m *Menu) big(y, x int, s string, rampAt float64) int {
	st := bold
	if !m.s.Light() {
		st = asBold(m.rampColour(ramp, rampAt, false))
	}
	for _, c := range s {
		g := bigGlyph(c)
		for row := 0; row < 3; row++ {
			m.put(y+row, x, g[row], st)
		}
		x += runes(g[0]) + 1
	}
	return x
}

// banner is a word in the big font, three rows.
func banner(word string) [3]string {
	var rows [3]string
	for _, c := range strings.ToLower(word) {
		g := []string{"   ", "   ", "   "}
		if f, ok := font[c]; ok {
			g = strings.Split(f, "|")
		}
		for n := range rows {
			rows[n] += g[n] + " "
		}
	}
	return rows
}

// fitTitle is the longest start of name the big font can draw in width. Cutting
// first keeps a huge name from costing seconds.
func fitTitle(name string, width int) string {
	fit := []rune(name)
	if len(fit) > max(0, width) {
		fit = fit[:max(0, width)]
	}
	for len(fit) > 0 && runes(banner(string(fit))[0]) > width {
		fit = fit[:len(fit)-1]
	}
	return string(fit)
}

// legend puts the keys, most needed first, on the two bottom rows. What does not
// fit is cut as a whole key, never mid-word.
func (m *Menu) legend(h int, keys []string) {
	_, wd := m.s.Size()
	var rows []string
	row := ""
	for _, k := range keys {
		joined := k
		if row != "" {
			joined = row + " · " + k
		}
		if runes(joined) > wd-6 && row != "" {
			rows, row = append(rows, row), k
		} else {
			row = joined
		}
	}
	rows = append(rows, row)
	for n, text := range rows[:min(2, len(rows))] {
		m.put(h-2+n, 3, text, dim)
	}
}

// strike runs a line through text. A space keeps its gap.
func strike(s string) string {
	var b strings.Builder
	for _, c := range s {
		b.WriteRune(c)
		if c != ' ' {
			b.WriteString(strikeRun)
		}
	}
	return b.String()
}

// window is the first row to show so the cursor sits mid-screen.
func window(i, h, n int) int {
	if n <= h {
		return 0
	}
	return max(0, min(i-h/2, n-h))
}

// wrap breaks running text into lines of at most width cells, at spaces.
func wrap(text string, width int) []string {
	if width <= 0 {
		return nil
	}
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		for cells(word) > width {
			if line != "" {
				lines, line = append(lines, line), ""
			}
			head := cut(word, width)
			if head == "" { // one character wider than the window: a line of its own, never a loop
				_, size := utf8.DecodeRuneInString(word)
				head = word[:size]
			}
			lines, word = append(lines, head), word[len(head):]
		}
		switch {
		case line == "":
			line = word
		case cells(line)+1+cells(word) <= width:
			line += " " + word
		default:
			lines, line = append(lines, line), word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
