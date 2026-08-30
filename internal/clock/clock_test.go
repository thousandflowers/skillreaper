package clock

import (
	"sync"
	"testing"
	"time"
)

// resetForTest hands back a fresh once/pinned pair, so each case reads the
// environment again instead of inheriting whatever the first one cached.
func resetForTest() (sync.Once, time.Time) { return sync.Once{}, time.Time{} }

func TestNowHonorsTheOverride(t *testing.T) {
	t.Setenv(EnvVar, "1788091200") // 2026-08-30T12:00:00Z
	once, pinned = resetForTest()

	got := Now()
	if want := time.Unix(1788091200, 0).UTC(); !got.Equal(want) {
		t.Fatalf("Now() = %v, want %v", got, want)
	}
	if got2 := Now(); !got2.Equal(got) {
		t.Fatalf("Now() is not stable across calls: %v then %v", got, got2)
	}
}

func TestNowFallsBackOnAMalformedOverride(t *testing.T) {
	t.Setenv(EnvVar, "not-a-number")
	once, pinned = resetForTest()

	if Now().IsZero() {
		t.Fatal("a malformed override must fall back to the real clock, not a zero time")
	}
}

func TestNowUsesTheRealClockWhenUnset(t *testing.T) {
	t.Setenv(EnvVar, "")
	once, pinned = resetForTest()

	if delta := time.Since(Now()); delta > time.Minute || delta < -time.Minute {
		t.Fatalf("expected the real clock, got one %v away from it", delta)
	}
}
