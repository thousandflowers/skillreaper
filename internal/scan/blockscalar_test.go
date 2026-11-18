package scan

import (
	"testing"
	"unicode/utf8"
)

// A line indented less than the block keeps all its text: only the spaces it
// really has in front go, never a letter, and never half of one.
func TestBlockScalarALessIndentedLineKeepsItsText(t *testing.T) {
	cases := []struct{ name, line, want string }{
		{"two spaces short", "  abcdef", "abcdef"},
		{"an accent where the cut fell", "  éé", "éé"},
		{"no spaces at all", "xyz", "xyz"},
	}
	for _, c := range cases {
		got := blockScalar([]string{"    first", c.line}, '|', '-')
		if want := "first\n" + c.want; got != want || !utf8.ValidString(got) {
			t.Errorf("%s: %q, want %q", c.name, got, want)
		}
	}
}
