package term

import (
	"strconv"
	"strings"
)

// cell is one position on the screen. An empty text is a blank. cont marks the
// second half of a wide character, drawn by the cell before it.
type cell struct {
	text string
	st   Style
	cont bool
}

// frame is the screen as the menu last painted it.
type frame struct {
	rows, cols int
	cells      []cell
}

func newFrame(rows, cols int) *frame {
	rows, cols = max(rows, 0), max(cols, 0)
	return &frame{rows: rows, cols: cols, cells: make([]cell, rows*cols)}
}

func (f *frame) erase() { clear(f.cells) }

// put writes s from (y, x), clipped by cells. The last column is never written,
// as under curses, where a character there would wrap the cursor.
func (f *frame) put(y, x int, s string, st Style) {
	if y < 0 || y >= f.rows || x < 0 || x >= f.cols {
		return
	}
	row := f.cells[y*f.cols : (y+1)*f.cols]
	col, last := x, -1
	for _, c := range Cut(s, f.cols-x-1) {
		w := cellWidth(c)
		if w == 0 {
			if last >= 0 { // a mark with nothing before it in this text has nothing to sit on
				row[last].text += string(c)
			}
			continue
		}
		if c < 0x20 || c == 0x7f {
			c = ' ' // a control character must never reach the terminal
		}
		if row[col].cont && col > 0 {
			row[col-1] = cell{st: row[col-1].st} // half a wide character is nothing
		}
		if after := col + w; after < f.cols && row[after].cont {
			row[after] = cell{st: row[after].st}
		}
		row[col] = cell{text: string(c), st: st}
		if w == 2 {
			row[col+1] = cell{st: st, cont: true}
		}
		last = col
		col += w
	}
}

// blank says a cell shows nothing, so a row can end before it.
func (c cell) blank() bool {
	return !c.cont && (c.text == "" || c.text == " ") && c.st.BG == 0 && !c.st.Reverse
}

// line is the bytes that draw row y from its first column, ending by clearing
// what is left of the row.
func (f *frame) line(y, colors int) string {
	row := f.cells[y*f.cols : (y+1)*f.cols]
	end := len(row)
	for end > 0 && row[end-1].blank() {
		end--
	}
	var b strings.Builder
	var drawn Style
	for _, c := range row[:end] {
		if c.cont {
			continue
		}
		if c.st != drawn {
			b.WriteString(sgr(c.st, colors))
			drawn = c.st
		}
		if c.text == "" {
			b.WriteByte(' ')
		} else {
			b.WriteString(c.text)
		}
	}
	if drawn != (Style{}) {
		b.WriteString("\x1b[0m")
	}
	b.WriteString("\x1b[K")
	return b.String()
}

// text is row y as plain characters, with nothing after its last one.
func (f *frame) text(y int) string {
	var b strings.Builder
	for _, c := range f.cells[y*f.cols : (y+1)*f.cols] {
		switch {
		case c.cont:
		case c.text == "":
			b.WriteByte(' ')
		default:
			b.WriteString(c.text)
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// sgr selects a style from scratch, so no attribute of the one before survives.
func sgr(st Style, colors int) string {
	var b strings.Builder
	b.WriteString("\x1b[0")
	if st.Bold {
		b.WriteString(";1")
	}
	if st.Dim {
		b.WriteString(";2")
	}
	if st.Reverse {
		b.WriteString(";7")
	}
	writeColor(&b, st.FG, colors, "3")
	writeColor(&b, st.BG, colors, "4")
	b.WriteByte('m')
	return b.String()
}

func writeColor(b *strings.Builder, c Color, colors int, layer string) {
	if c <= 0 || colors <= 0 {
		return
	}
	n := int(c) - 1
	if colors >= 256 {
		b.WriteString(";" + layer + "8;5;" + strconv.Itoa(n))
		return
	}
	b.WriteString(";" + layer + strconv.Itoa(basicColor(n)))
}

// basicColor is the nearest of the eight basic colours to xterm palette index n.
func basicColor(n int) int {
	switch {
	case n < 8:
		return n
	case n < 16:
		return n - 8
	case n >= 232: // the grey ramp
		if n >= 244 {
			return 7
		}
		return 0
	}
	n -= 16 // the 6x6x6 cube
	bit := func(level int) int {
		if level >= 3 {
			return 1
		}
		return 0
	}
	return bit(n/36) | bit(n/6%6)<<1 | bit(n%6)<<2
}
