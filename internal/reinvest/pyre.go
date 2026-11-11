package reinvest

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// The prototype's patterns were written for Python's re, where \w, \d, \s and
// \b are Unicode-wide and where ignoring case also folds four letters outside
// ASCII. Go's regexp (RE2) keeps those classes to ASCII and has no Unicode \b.
//
// Rather than rewrite every pattern, the text is projected first: character
// for character it becomes a text on which RE2's ASCII classes mean what
// Python's meant on the original.
//
//   - whitespace RE2's \s does not know (vertical tab, the four separators,
//     and every space outside ASCII)                        -> form feed
//   - a decimal digit outside ASCII                           -> D
//   - any other word character outside ASCII                  -> Z
//   - with fold: A-Z -> a-z, and İ ı -> i, ſ -> s, K -> k, which is the whole
//     of what re.IGNORECASE adds to ASCII letters (the Python documentation
//     names these four)
//
// The projection is only valid on text with no ASCII capitals (lower-cased
// already, or folded here) and for patterns that name no capital letter and
// no form feed: then Z and D can stand for nothing else. A pattern writes
// [0-9D] where Python had \d. Every rune maps to one rune, so a match found
// in the projection has the same position, counted in characters, in the
// original.

const (
	projectedSpace = '\f'
	projectedDigit = 'D'
	projectedWord  = 'Z'
)

// ignoreCaseExtras are the letters outside ASCII that Python's re.IGNORECASE
// matches against an ASCII letter.
var ignoreCaseExtras = map[rune]rune{0x130: 'i', 0x131: 'i', 0x17f: 's', 0x212a: 'k'}

func foldRune(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	if folded, ok := ignoreCaseExtras[r]; ok {
		return folded
	}
	return r
}

func projectRune(r rune) rune {
	switch {
	case r < utf8.RuneSelf:
		if isPySpace(r) && !strings.ContainsRune("\t\n\f\r ", r) {
			return projectedSpace
		}
		return r
	case isPySpace(r):
		return projectedSpace
	case isPyDigit(r):
		return projectedDigit
	case isPyWord(r):
		return projectedWord
	}
	return r
}

// project returns s as RE2 must read it to agree with Python (see above).
func project(s string, fold bool) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if fold {
			r = foldRune(r)
		}
		b.WriteRune(projectRune(r))
	}
	return b.String()
}

// pattern is one Python pattern compiled for projected text.
type pattern struct {
	re   *regexp.Regexp
	fold bool // compiled from a pattern that had re.IGNORECASE
}

func mustPattern(expr string, fold bool) pattern {
	return pattern{re: regexp.MustCompile(expr), fold: fold}
}

// fullPattern compiles expr for re.fullmatch: the whole text or nothing.
func fullPattern(expr string, fold bool) pattern {
	return mustPattern(`\A(?:`+expr+`)\z`, fold)
}

// matches is re.search (or re.fullmatch for a fullPattern) on s.
func (p pattern) matches(s string) bool { return p.re.MatchString(project(s, p.fold)) }

// matchesProjected is the same on text the caller has already projected.
func (p pattern) matchesProjected(projected string) bool { return p.re.MatchString(projected) }

// replaceAll is re.sub(pattern, with, s): every match, left to right, none overlapping.
func (p pattern) replaceAll(s, with string) string {
	projected := project(s, p.fold)
	spans := p.re.FindAllStringIndex(projected, -1)
	if len(spans) == 0 {
		return s
	}
	// offsets in the projection are not offsets in s: count characters instead
	original := []rune(s)
	var b strings.Builder
	done, seenBytes, seenRunes := 0, 0, 0
	for _, span := range spans {
		seenRunes += utf8.RuneCountInString(projected[seenBytes:span[0]])
		first := seenRunes
		seenRunes += utf8.RuneCountInString(projected[span[0]:span[1]])
		seenBytes = span[1]
		b.WriteString(string(original[done:first]))
		b.WriteString(with)
		done = seenRunes
	}
	b.WriteString(string(original[done:]))
	return b.String()
}

// pyWordClass and pySpaceClass are \w and \s written out for RE2, for the
// patterns that run on text as it is (no projection): the inside of a
// character class.
const (
	pyWordClass  = `\p{L}\p{N}_`
	pySpaceClass = `\t-\r\x1c-\x20\x{85}\x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}`
)
