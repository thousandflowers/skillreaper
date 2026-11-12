package main

import "testing"

// The weekly nudge names what grew, in items (a server is no skill).
func TestTheNudgeSaysWhatItCounts(t *testing.T) {
	for _, c := range []struct {
		dead, mute int
		want       string
	}{
		{0, 1, "skillreaper: 1 item to mute since the last check. Run `reap` to review."},
		{1, 0, "skillreaper: 1 unused item to prune since the last check. Run `reap` to review."},
		{3, 2, "skillreaper: 3 unused items to prune and 2 items to mute since the last check. Run `reap` to review."},
	} {
		if got := nudgeText(c.dead, c.mute); got != c.want {
			t.Errorf("nudgeText(%d, %d) = %q, want %q", c.dead, c.mute, got, c.want)
		}
	}
}
