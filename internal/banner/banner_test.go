package banner

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestAllow(t *testing.T) {
	tests := []struct {
		name       string
		opts       Options
		tty        bool
		cols       int
		widthKnown bool
		noColor    string
		want       bool
	}{
		{name: "wide interactive terminal", tty: true, cols: 80, widthKnown: true, want: true},
		{name: "unknown width is not narrow", tty: true, widthKnown: false, want: true},
		{name: "not a terminal", tty: false, cols: 80, widthKnown: true, want: false},
		{name: "narrower than the minimum", tty: true, cols: 19, widthKnown: true, want: false},
		{name: "exactly the minimum", tty: true, cols: 20, widthKnown: true, want: true},
		{name: "NO_COLOR set", tty: true, cols: 80, widthKnown: true, noColor: "1", want: false},
		{name: "json", opts: Options{JSON: true}, tty: true, cols: 80, widthKnown: true, want: false},
		{name: "markdown", opts: Options{Markdown: true}, tty: true, cols: 80, widthKnown: true, want: false},
		{name: "agent", opts: Options{Agent: true}, tty: true, cols: 80, widthKnown: true, want: false},
		{name: "no-banner", opts: Options{NoBanner: true}, tty: true, cols: 80, widthKnown: true, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := allow(tt.opts, tt.tty, tt.cols, tt.widthKnown, tt.noColor); got != tt.want {
				t.Errorf("allow = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPrintWritesOnlyToErrOut(t *testing.T) {
	stubDetect(t, true, 80, true)
	var errOut, stdOut bytes.Buffer

	Print(&errOut, &stdOut, Options{})

	if !strings.Contains(errOut.String(), Mark) {
		t.Errorf("errOut = %q, want it to contain the mark", errOut.String())
	}
	if stdOut.Len() != 0 {
		t.Errorf("stdOut = %q, want empty", stdOut.String())
	}
}

func TestPrintSuppressedWithoutTerminal(t *testing.T) {
	stubDetect(t, false, 0, false)
	var errOut bytes.Buffer

	Print(&errOut, io.Discard, Options{})

	if errOut.Len() != 0 {
		t.Errorf("errOut = %q, want empty", errOut.String())
	}
}

func TestMarkIsTwoLinesOfElevenColumns(t *testing.T) {
	lines := strings.Split(Mark, "\n")
	if len(lines) != 2 {
		t.Fatalf("mark has %d lines, want 2", len(lines))
	}
	for i, l := range lines {
		if len(l) != 11 {
			t.Errorf("line %d is %d columns, want 11", i+1, len(l))
		}
		for _, r := range l {
			if r > 126 || r < 32 {
				t.Errorf("line %d contains a non-printable-ASCII rune %q", i+1, r)
			}
		}
	}
}

// stubDetect replaces the terminal probe for one test. A real terminal cannot
// be assumed in CI, and the point under test is the decision, not the ioctl.
func stubDetect(t *testing.T, tty bool, cols int, widthKnown bool) {
	t.Helper()
	original := Detect
	Detect = func(io.Writer) (bool, int, bool) { return tty, cols, widthKnown }
	t.Cleanup(func() { Detect = original })
}
