package report

import (
	"testing"
	"time"
)

// The sessions an item installed inside the window has to be judged on grow
// with the time it has existed. The scale ran the other way: installed 29
// days ago it needed 1 session, installed 31 days ago 10.
func TestMinSessionsGrowWithTheTimeAnItemHasExisted(t *testing.T) {
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	start := now.AddDate(0, 0, -30)
	need := func(daysAgo int) int {
		return minSessionsFor(now.AddDate(0, 0, -daysAgo), start, 10, 30)
	}
	for daysAgo, want := range map[int]int{29: 9, 16: 5, 2: 1, 31: 10, 90: 10} {
		if got := need(daysAgo); got != want {
			t.Errorf("installed %d days ago: %d sessions, want %d", daysAgo, got, want)
		}
	}
	for d := 1; d < 30; d++ {
		if need(d) > need(d+1) {
			t.Errorf("an item %d days old needs more sessions (%d) than one %d days old (%d)", d, need(d), d+1, need(d+1))
		}
	}
	if got := minSessionsFor(time.Time{}, start, 10, 30); got != 10 {
		t.Errorf("no date: %d", got)
	}
}
