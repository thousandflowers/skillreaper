package reinvest

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The prototype was Python, and what it hashed, cached and matched was Python
// text: whitespace, lower-casing and line breaks are Unicode-wide there. The
// helpers below reproduce those meanings, so a cache key or a security rule
// reads the same text the prototype read.

// isPySpace reports what str.isspace() and the regex class \s accept.
func isPySpace(r rune) bool {
	switch {
	case r >= 0x09 && r <= 0x0d, r >= 0x1c && r <= 0x20, r == 0x85, r == 0xa0, r == 0x1680,
		r >= 0x2000 && r <= 0x200a, r == 0x2028, r == 0x2029, r == 0x202f, r == 0x205f, r == 0x3000:
		return true
	}
	return false
}

// isPyWord reports what the regex class \w accepts: letters, numbers, underscore.
func isPyWord(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
}

// isPyDigit reports what the regex class \d accepts: any decimal digit.
func isPyDigit(r rune) bool { return unicode.Is(unicode.Nd, r) }

func pyStrip(s string) string { return strings.TrimFunc(s, isPySpace) }

// pyFields is str.split() with no argument.
func pyFields(s string) []string { return strings.FieldsFunc(s, isPySpace) }

// collapseSpace is re.sub(r"\s+", " ", s).strip().
func collapseSpace(s string) string { return strings.Join(pyFields(s), " ") }

func isLineBreak(r rune) bool {
	switch r {
	case '\n', '\r', 0x0b, 0x0c, 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
		return true
	}
	return false
}

// pySplitLines is str.splitlines(): every Unicode line boundary ends a line,
// "\r\n" counts once, and a final boundary adds no empty line.
func pySplitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !isLineBreak(r) {
			i += size
			continue
		}
		lines = append(lines, s[start:i])
		i += size
		if r == '\r' && i < len(s) && s[i] == '\n' {
			i++
		}
		start = i
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

// firstRunes is s[:n] counted in characters.
func firstRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}

// lastRunes is s[-n:] counted in characters.
func lastRunes(s string, n int) string {
	total := runeLen(s)
	if total <= n {
		return s
	}
	skip := total - n
	for i := range s {
		if skip == 0 {
			return s[i:]
		}
		skip--
	}
	return ""
}

// decodeUTF8Replace is bytes.decode("utf-8", "replace"): each ill-formed
// sequence becomes one U+FFFD, sized as the longest prefix that could still
// have begun a valid character.
func decodeUTF8Replace(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var out strings.Builder
	out.Grow(len(b))
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		if r != utf8.RuneError || size > 1 {
			out.Write(b[i : i+size])
			i += size
			continue
		}
		out.WriteRune(utf8.RuneError)
		i += illFormedLen(b[i:])
	}
	return out.String()
}

// illFormedLen is how many bytes one replacement character stands for.
func illFormedLen(b []byte) int {
	lead := b[0]
	var need int
	lo, hi := byte(0x80), byte(0xbf)
	switch {
	case lead >= 0xc2 && lead <= 0xdf:
		need = 1
	case lead >= 0xe0 && lead <= 0xef:
		need = 2
		if lead == 0xe0 {
			lo = 0xa0
		}
		if lead == 0xed {
			hi = 0x9f
		}
	case lead >= 0xf0 && lead <= 0xf4:
		need = 3
		if lead == 0xf0 {
			lo = 0x90
		}
		if lead == 0xf4 {
			hi = 0x8f
		}
	default:
		return 1
	}
	n := 1
	for n <= need && n < len(b) && b[n] >= lo && b[n] <= hi {
		n++
		lo, hi = 0x80, 0xbf
	}
	return n
}

// pyStr is str(v) for a value read from JSON.
func pyStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return pyRepr(v)
}

// pyRepr is repr(v) for a value read from JSON.
func pyRepr(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case bool:
		if x {
			return "True"
		}
		return "False"
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case bigInt:
		return string(x)
	case float64:
		return pyFloat(x)
	case string:
		return pyQuote(x)
	case []any:
		parts := make([]string, len(x))
		for i, item := range x {
			parts[i] = pyRepr(item)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *obj:
		parts := make([]string, 0, x.len())
		for _, k := range x.keyList() {
			item, _ := x.get(k)
			parts = append(parts, pyQuote(k)+": "+pyRepr(item))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return ""
}

// pyQuote is repr() of a string: single quotes unless only those occur in it.
func pyQuote(s string) string {
	quote := '\''
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}
	var b strings.Builder
	b.WriteRune(quote)
	for _, r := range s {
		switch {
		case r == quote || r == '\\':
			b.WriteRune('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case unicode.IsPrint(r):
			b.WriteRune(r)
		case r < 0x100:
			b.WriteString(`\x` + hexPad(r, 2))
		case r < 0x10000:
			b.WriteString(`\u` + hexPad(r, 4))
		default:
			b.WriteString(`\U` + hexPad(r, 8))
		}
	}
	b.WriteRune(quote)
	return b.String()
}

func hexPad(r rune, width int) string {
	s := strconv.FormatInt(int64(r), 16)
	return strings.Repeat("0", max(0, width-len(s))) + s
}

// Python's repr(float) switches to an exponent outside these decimal points.
const (
	floatMinFixedPoint = -4
	floatMaxFixedPoint = 16
)

// pyFloat is repr() of a float: the shortest digits that read back the same,
// in Python's layout.
func pyFloat(f float64) string {
	sci := strconv.FormatFloat(f, 'e', -1, 64) // d.ddde±xx
	sign := ""
	if strings.HasPrefix(sci, "-") {
		sign, sci = "-", sci[1:]
	}
	mantissa, expText, _ := strings.Cut(sci, "e")
	exp, err := strconv.Atoi(expText)
	if err != nil {
		return sign + strings.ToLower(strings.TrimPrefix(sci, "+")) // "inf" and "nan" carry no exponent
	}
	digits := strings.Replace(mantissa, ".", "", 1)
	point := exp + 1 // where the decimal point falls among the digits
	switch {
	case point <= floatMinFixedPoint || point > floatMaxFixedPoint:
		out := digits[:1]
		if len(digits) > 1 {
			out += "." + digits[1:]
		}
		expSign := "+"
		if exp < 0 {
			expSign, exp = "-", -exp
		}
		return sign + out + "e" + expSign + hexlessPad(exp)
	case point <= 0:
		return sign + "0." + strings.Repeat("0", -point) + digits
	case point >= len(digits):
		return sign + digits + strings.Repeat("0", point-len(digits)) + ".0"
	}
	return sign + digits[:point] + "." + digits[point:]
}

// hexlessPad writes an exponent with at least two digits.
func hexlessPad(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}
