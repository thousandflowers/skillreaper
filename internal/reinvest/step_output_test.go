//go:build darwin || linux

package reinvest

import (
	"strings"
	"testing"
)

// What a failing step prints comes from a remote or a package: it reaches the
// terminal as text, never as the escape sequences in it (clear the screen,
// write the clipboard).
func TestStep_a_failing_programs_words_reach_the_screen_as_text(t *testing.T) {
	a, _ := ttyCovStepApp(t, procResult{code: 1, stderr: "fatal: remote said \x1b[2J\x1b[Hfake prompt \x1b]52;c;ZXZpbA==\x07 done"})
	err := a.runInEmptyDir([]string{"git", "clone", "x"})
	if err == nil || strings.ContainsAny(err.Error(), "\x1b\x07") || !strings.Contains(err.Error(), "fatal: remote said") {
		t.Errorf("the step error is %q", err)
	}
}
