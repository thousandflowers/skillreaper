package term

import (
	"os"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const (
	maxMarks    = 2   // combining marks kept per character: more only hide what follows
	placeholder = '�' // what an unsafe character is shown as
)

// unsafeRunes is what a name must not do to the screen: control, bidi and
// zero-width characters, and line and paragraph separators.
var unsafeRunes = []*unicode.RangeTable{unicode.Cc, unicode.Cf, unicode.Cs, unicode.Zl, unicode.Zp}

func printableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// Visible makes a string from outside safe to lay out: what could move the
// cursor or hide text becomes a mark you can see.
func Visible(s string) string {
	if printableASCII(s) {
		return s
	}
	var b strings.Builder
	marks := 0
	for _, c := range s {
		if unicode.Is(unicode.M, c) {
			marks++
		} else {
			marks = 0
		}
		if marks > maxMarks {
			continue
		}
		if unicode.IsOneOf(unsafeRunes, c) {
			c = placeholder
		}
		b.WriteRune(c)
	}
	return b.String()
}

func isWide(c rune) bool {
	if c < wideRanges[0][0] {
		return false
	}
	i := sort.Search(len(wideRanges), func(i int) bool { return wideRanges[i][1] >= c })
	return i < len(wideRanges) && wideRanges[i][0] <= c
}

// cellWidth is the number of cells one character fills: none for a combining
// mark, two for a wide one.
func cellWidth(c rune) int {
	switch {
	case c < utf8.RuneSelf:
		return 1
	case unicode.Is(unicode.M, c):
		return 0
	case isWide(c):
		return 2
	}
	return 1
}

// Cells is the number of cells s fills on the screen.
func Cells(s string) int {
	if isASCII(s) {
		return len(s)
	}
	n := 0
	for _, c := range s {
		n += cellWidth(c)
	}
	return n
}

// Cut is the start of s that fills at most n cells.
func Cut(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if isASCII(s) {
		if n < len(s) {
			return s[:n]
		}
		return s
	}
	used := 0
	for i, c := range s {
		used += cellWidth(c)
		if used > n {
			return s[:i]
		}
	}
	return s
}

// Pad fits s in exactly n cells: cut if longer, filled with spaces if shorter.
func Pad(s string, n int) string {
	s = Cut(s, n)
	if room := n - Cells(s); room > 0 {
		return s + strings.Repeat(" ", room)
	}
	return s
}

// CutMid fits s in n cells, keeping the end: long names often differ only there.
func CutMid(s string, n int) string {
	if Cells(s) <= n || n < 3 {
		return Cut(s, n)
	}
	head := Cut(s, (n-1)/2)
	room := n - 1 - Cells(head)
	start := len(s)
	for start > 0 {
		c, size := utf8.DecodeLastRuneInString(s[:start])
		room -= cellWidth(c)
		if room < 0 {
			break
		}
		start -= size
	}
	return head + "…" + s[start:]
}

// fallback is what a locale that cannot draw a character gets instead: the main
// figure and the warnings stay, in plain characters.
var fallback = map[rune]string{
	'█': "#", '▀': "#", '▄': "#", '▌': "#", '▏': "#", '▎': "#", '▍': "#", '▋': "#", '▊': "#", '▉': "#",
	'▁': "_", '▂': "_", '▃': "_", '▅': "_", '▆': "#", '▇': "#", '░': ".", '▒': ":", '━': "-", '┄': "-",
	'▸': ">", '▾': "v", '·': "-", '…': "~", '⚠': "!", '✓': "ok", '←': "<", '→': ">", '↑': "^", '↓': "v",
	'€': "EUR", '£': "GBP", '¥': "JPY", '⠋': "|", '⠙': "/", '⠹': "-", '⠸': "\\", '⠼': "|", '⠴': "/",
	'⠦': "-", '⠧': "\\", '⠇': "|", '⠏': "/", placeholder: "?",
}

var narrowLocale = sync.OnceValue(func() bool {
	for _, name := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := os.Getenv(name); v != "" {
			return !isUTF8Locale(v)
		}
	}
	return true // no locale at all is the C locale: ASCII
})

func isUTF8Locale(v string) bool {
	return strings.Contains(strings.ReplaceAll(strings.ToUpper(v), "-", ""), "UTF8")
}

// Narrow reports whether this terminal's locale cannot draw everything the menu
// uses: then block characters and marks are replaced by plain ones.
func Narrow() bool { return narrowLocale() }

// Glyphs is s in characters an ASCII-only locale can draw.
func Glyphs(s string) string {
	if isASCII(s) {
		return s
	}
	var b strings.Builder
	for _, c := range s {
		switch plain, known := fallback[c]; {
		case c < utf8.RuneSelf:
			b.WriteRune(c)
		case known:
			b.WriteString(plain)
		case cellWidth(c) > 0:
			b.WriteByte('?')
		}
	}
	return b.String()
}
