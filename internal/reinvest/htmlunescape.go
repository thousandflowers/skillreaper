package reinvest

import (
	_ "embed"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// html.unescape as the prototype's Python did it. Go's html.UnescapeString
// differs where it matters to a security rule: "cu&#27;rl" loses the control
// character in Python (and reads "curl"), and keeps it in Go.

//go:embed data/html5.txt
var html5Source string

type htmlTables struct {
	named      map[string]string // "amp;" and, for the old ones, "amp"
	replaced   map[int64]string  // numeric references HTML maps elsewhere
	disallowed map[int64]bool    // numeric references that produce nothing
}

var (
	htmlOnce sync.Once
	htmlData htmlTables
)

func loadHTML() htmlTables {
	htmlOnce.Do(func() {
		t := htmlTables{named: map[string]string{}, replaced: map[int64]string{}, disallowed: map[int64]bool{}}
		for _, line := range strings.Split(html5Source, "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 || fields[0] == "#" {
				continue
			}
			switch fields[0] {
			case "E":
				t.named[fields[1]] = string(hexRunes(fields[2:]))
			case "R":
				n, _ := strconv.ParseInt(fields[1], 16, 64)
				t.replaced[n] = string(hexRunes(fields[2:]))
			case "X":
				n, _ := strconv.ParseInt(fields[1], 16, 64)
				t.disallowed[n] = true
			}
		}
		htmlData = t
	})
	return htmlData
}

const (
	maxEntityName  = 32 // characters of a name Python looks at
	maxCodePoint   = 0x10ffff
	surrogateFirst = 0xd800
	surrogateLast  = 0xdfff
	replacement    = "�"
)

// htmlUnescape is html.unescape(s).
func htmlUnescape(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	t := loadHTML()
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != '&' {
			next := strings.IndexByte(s[i:], '&')
			if next < 0 {
				next = len(s) - i
			}
			b.WriteString(s[i : i+next])
			i += next
			continue
		}
		text, size := charRef(t, s[i+1:])
		if size == 0 {
			b.WriteByte('&')
			i++
			continue
		}
		b.WriteString(text)
		i += 1 + size
	}
	return b.String()
}

// charRef reads one reference after its "&": what it stands for and how many
// bytes it took. Zero bytes means the "&" was just an ampersand.
func charRef(t htmlTables, rest string) (string, int) {
	if strings.HasPrefix(rest, "#") {
		return numericRef(t, rest)
	}
	end, count := 0, 0
	for end < len(rest) && count < maxEntityName {
		r, size := utf8.DecodeRuneInString(rest[end:])
		if strings.ContainsRune("\t\n\f <&#;", r) {
			break
		}
		end += size
		count++
	}
	if end == 0 {
		return "", 0
	}
	if end < len(rest) && rest[end] == ';' {
		end++
	}
	name := rest[:end]
	if text, ok := t.named[name]; ok {
		return text, end
	}
	// the longest known name the text starts with, as the HTML standard says
	runes := []rune(name)
	for cut := len(runes) - 1; cut > 1; cut-- {
		if text, ok := t.named[string(runes[:cut])]; ok {
			return text + string(runes[cut:]), end
		}
	}
	return "&" + name, end
}

func numericRef(t htmlTables, rest string) (string, int) {
	base, start := 10, 1
	if len(rest) > 1 && (rest[1] == 'x' || rest[1] == 'X') {
		base, start = 16, 2
	}
	end := start
	for end < len(rest) && isDigitIn(rest[end], base) {
		end++
	}
	if end == start {
		return "", 0
	}
	num, err := strconv.ParseInt(rest[start:end], base, 64)
	if err != nil {
		num = maxCodePoint + 1 // too large to hold: past the last code point either way
	}
	if end < len(rest) && rest[end] == ';' {
		end++
	}
	switch {
	case t.replaced[num] != "":
		return t.replaced[num], end
	case (num >= surrogateFirst && num <= surrogateLast) || num > maxCodePoint:
		return replacement, end
	case t.disallowed[num]:
		return "", end
	}
	return string(rune(num)), end
}

func isDigitIn(c byte, base int) bool {
	if c >= '0' && c <= '9' {
		return true
	}
	return base == 16 && ((c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F'))
}
