package reinvest

import (
	"math"
	"strings"
	"unicode"
)

// decimalValue is the value of a decimal digit in any script. Unicode encodes
// each script's digits as ten consecutive code points, zero first.
func decimalValue(r rune) (int, bool) {
	if r >= '0' && r <= '9' {
		return int(r - '0'), true
	}
	for _, span := range unicode.Nd.R16 {
		if r >= rune(span.Lo) && r <= rune(span.Hi) {
			return int(r-rune(span.Lo)) % 10, true
		}
	}
	for _, span := range unicode.Nd.R32 {
		if r >= rune(span.Lo) && r <= rune(span.Hi) {
			return int(r-rune(span.Lo)) % 10, true
		}
	}
	return 0, false
}

const maxSafeInt = math.MaxInt32 // far above any count or index reinvest handles

// parseDigits reads a run of decimal digits. underscores allows one between
// two digits, as Python's int() does.
func parseDigits(text string, underscores bool) (int, bool) {
	n, digits, lastWasDigit := 0, 0, false
	for _, r := range text {
		if r == '_' && underscores && lastWasDigit {
			lastWasDigit = false
			continue
		}
		d, ok := decimalValue(r)
		if !ok {
			return 0, false
		}
		if n = n*10 + d; n > maxSafeInt {
			return 0, false
		}
		digits++
		lastWasDigit = true
	}
	return n, digits > 0 && lastWasDigit
}

// pyInt is int(v or 0) for a JSON value: ok is false where Python would raise.
func pyInt(v any) (int, bool) {
	if !truthy(v) {
		return 0, true
	}
	switch x := v.(type) {
	case bool:
		return 1, true
	case int64:
		if x > maxSafeInt || x < -maxSafeInt {
			return 0, false
		}
		return int(x), true
	case float64:
		if math.IsNaN(x) || math.Abs(x) > maxSafeInt {
			return 0, false
		}
		return int(x), true // toward zero, as int() truncates
	case string:
		text := pyStrip(x)
		sign := 1
		if strings.HasPrefix(text, "-") {
			sign, text = -1, text[1:]
		} else if strings.HasPrefix(text, "+") {
			text = text[1:]
		}
		n, ok := parseDigits(text, true)
		return sign * n, ok
	}
	return 0, false
}

// listIndex reads a position the model gave, as `str(v).isdigit() and int(v)`
// does: a whole number that is not negative, written as a number or as text.
func listIndex(v any) (int, bool) {
	switch x := v.(type) {
	case int64:
		if x < 0 || x > maxSafeInt {
			return 0, false
		}
		return int(x), true
	case string:
		return parseDigits(x, false)
	}
	return 0, false
}

// groupIndex reads a group number the model gave: isinstance(v, int), which
// in Python also takes true and false as 1 and 0.
func groupIndex(v any) (int, bool) {
	switch x := v.(type) {
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case int64:
		if x < 0 || x > maxSafeInt {
			return 0, false
		}
		return int(x), true
	}
	return 0, false
}
