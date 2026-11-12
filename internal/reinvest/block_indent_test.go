package reinvest

import "testing"

// The same rule as reap's: a line indented less than the block keeps its text,
// so the weight reinvest weighs is the one reap reports.
func TestBlock_a_less_indented_line_keeps_its_text(t *testing.T) {
	for _, line := range []string{"  abcdef", "  éé", "xyz"} {
		got := block([]string{"    first", line}, "|", "-")
		if want := "first\n" + pyStrip(line); got != want {
			t.Errorf("block with %q = %q, want %q", line, got, want)
		}
	}
}
