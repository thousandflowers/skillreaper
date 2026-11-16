package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The weekly nudge names what grew, in items (a server is no skill): the
// counts are totals, and the nudge speaks because one is higher than before.
func TestTheNudgeSaysWhatItCounts(t *testing.T) {
	for _, c := range []struct {
		dead, mute int
		want       string
	}{
		{0, 1, "skillreaper: 1 item to mute, more than at the last check. Run `reap` to review."},
		{1, 0, "skillreaper: 1 unused item to prune, more than at the last check. Run `reap` to review."},
		{3, 2, "skillreaper: 3 unused items to prune and 2 items to mute, more than at the last check. Run `reap` to review."},
	} {
		if got := nudgeText(c.dead, c.mute); got != c.want {
			t.Errorf("nudgeText(%d, %d) = %q, want %q", c.dead, c.mute, got, c.want)
		}
	}
}

// The nudge is weekly because it writes down when it last spoke. Where that
// cannot be written it stays quiet: it used to speak at the start of every
// session, for ever.
func TestTheNudgeStaysQuietWhereItCannotRememberSpeaking(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("needs a user that file permissions apply to")
	}
	home := deleteHome(t)
	state := filepath.Join(home, ".claude", "reaped")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(state, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(state, 0o755) })

	if code, out := reapRun("nudge", "--min-sessions", "1"); code != 0 || strings.Contains(out, "skillreaper:") {
		t.Errorf("a nudge that cannot be recorded spoke: %d %q", code, out)
	}

	// The same stack, writable: it speaks once, then holds for a week.
	if err := os.Chmod(state, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, out := reapRun("nudge", "--min-sessions", "1"); !strings.Contains(out, "skillreaper:") {
		t.Fatalf("this stack gives no nudge at all, so the test proves nothing: %q", out)
	}
	if _, out := reapRun("nudge", "--min-sessions", "1"); strings.Contains(out, "skillreaper:") {
		t.Errorf("the nudge spoke twice in a week: %q", out)
	}
}
