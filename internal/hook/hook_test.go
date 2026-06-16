package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstallCreatesFile(t *testing.T) {
	settings := filepath.Join(t.TempDir(), ".claude", "settings.json")
	if _, err := Install(settings, Command("/usr/local/bin/reap"), false); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{Marker, "SessionStart"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("settings missing %q: %s", want, b)
		}
	}
}

func TestInstallPreservesExistingAndIdempotent(t *testing.T) {
	settings := filepath.Join(t.TempDir(), "settings.json")
	existing := `{"model":"claude","hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"echo hi"}]}],"PreToolUse":[{"hooks":[{"type":"command","command":"guard"}]}]}}`
	if err := os.WriteFile(settings, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(settings, Command("reap"), false); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(settings)
	for _, want := range []string{`"model"`, "echo hi", "guard", Marker} {
		if !strings.Contains(string(b), want) {
			t.Errorf("install dropped %q: %s", want, b)
		}
	}
	// A second install must not add a duplicate entry.
	if _, err := Install(settings, Command("reap"), false); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(settings)
	if n := strings.Count(string(b), Marker); n != 1 {
		t.Errorf("install not idempotent, marker count = %d", n)
	}
}

func TestUninstallRemovesOnlyOurs(t *testing.T) {
	settings := filepath.Join(t.TempDir(), "settings.json")
	existing := `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"keep-me"}]}]}}`
	if err := os.WriteFile(settings, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(settings, Command("reap"), false); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(settings); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(settings)
	if strings.Contains(string(b), Marker) {
		t.Error("uninstall left our marker")
	}
	if !strings.Contains(string(b), "keep-me") {
		t.Errorf("uninstall removed a foreign hook: %s", b)
	}
}

func TestUninstallMissingFileIsNoop(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nope.json")
	if err := Uninstall(p); err != nil {
		t.Errorf("uninstall of a missing file should be a no-op, got %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("uninstall must not create the file")
	}
}

func TestInstallDryRun(t *testing.T) {
	settings := filepath.Join(t.TempDir(), "settings.json")
	out, err := Install(settings, Command("reap"), true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), Marker) {
		t.Error("dry-run output missing marker")
	}
	if _, err := os.Stat(settings); !os.IsNotExist(err) {
		t.Error("dry-run must not write the file")
	}
}

func TestShouldNudge(t *testing.T) {
	now := time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)
	week := now.AddDate(0, 0, -8)
	recent := now.AddDate(0, 0, -2)
	cases := []struct {
		name       string
		reap, mute int
		st         NudgeState
		want       bool
	}{
		{"first run with reaps", 3, 0, NudgeState{}, true},
		{"too soon", 5, 0, NudgeState{LastNudgeAt: recent, LastReapCount: 1}, false},
		{"week passed, reap grew", 3, 0, NudgeState{LastNudgeAt: week, LastReapCount: 1}, true},
		{"week passed, no change", 1, 0, NudgeState{LastNudgeAt: week, LastReapCount: 1}, false},
		{"week passed, mute grew", 1, 2, NudgeState{LastNudgeAt: week, LastReapCount: 1, LastMuteCount: 1}, true},
	}
	for _, c := range cases {
		if got := ShouldNudge(now, c.reap, c.mute, c.st); got != c.want {
			t.Errorf("%s: ShouldNudge = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestNudgeStateRoundtrip(t *testing.T) {
	claudeDir := filepath.Join(t.TempDir(), ".claude")
	want := NudgeState{
		LastNudgeAt:   time.Now().Truncate(time.Second).UTC(),
		LastReapCount: 4,
		LastMuteCount: 2,
		LastStarCtaAt: time.Now().Truncate(time.Second).UTC(),
		StarCtaCount:  1,
	}
	if err := SaveNudgeState(claudeDir, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadNudgeState(claudeDir)
	if err != nil {
		t.Fatal(err)
	}
	if !got.LastNudgeAt.Equal(want.LastNudgeAt) || got.LastReapCount != 4 || got.LastMuteCount != 2 {
		t.Errorf("roundtrip got %+v want %+v", got, want)
	}
	if !got.LastStarCtaAt.Equal(want.LastStarCtaAt) || got.StarCtaCount != 1 {
		t.Errorf("star cta fields: got %+v want %+v", got, want)
	}
	// Missing state file yields a zero state, not an error.
	z, err := LoadNudgeState(filepath.Join(t.TempDir(), "x"))
	if err != nil || !z.LastNudgeAt.IsZero() {
		t.Errorf("missing state: %+v err %v", z, err)
	}
}

func TestShouldShowStarCta(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	past := now.AddDate(0, 0, -40)  // 40 days ago → outside 30-day cooldown
	recent := now.AddDate(0, 0, -5) // 5 days ago → inside cooldown

	cases := []struct {
		name string
		st   NudgeState
		want bool
	}{
		{"never shown", NudgeState{}, true},
		{"shown 40 days ago", NudgeState{LastStarCtaAt: past}, true},
		{"shown 5 days ago", NudgeState{LastStarCtaAt: recent}, false},
		{"shown today", NudgeState{LastStarCtaAt: now}, false},
	}
	for _, c := range cases {
		if got := ShouldShowStarCta(now, c.st); got != c.want {
			t.Errorf("%s: ShouldShowStarCta = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestShouldShowStarCtaBoundary(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	// 29 days ago → still inside cooldown.
	day29 := now.AddDate(0, 0, -(starCtaCooldownDays - 1))
	if ShouldShowStarCta(now, NudgeState{LastStarCtaAt: day29}) {
		t.Error("day 29 should still be inside cooldown")
	}
	// 30 days ago → cooldown expired, can show again.
	day30 := now.AddDate(0, 0, -starCtaCooldownDays)
	if !ShouldShowStarCta(now, NudgeState{LastStarCtaAt: day30}) {
		t.Error("day 30 should allow showing again")
	}
}

func TestShouldShowShareHint(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	past := now.AddDate(0, 0, -40)  // 40 days ago → outside 30-day cooldown
	recent := now.AddDate(0, 0, -5) // 5 days ago → inside cooldown

	cases := []struct {
		name string
		st   NudgeState
		want bool
	}{
		{"never shown", NudgeState{}, true},
		{"shown 40 days ago", NudgeState{LastShareHintAt: past}, true},
		{"shown 5 days ago", NudgeState{LastShareHintAt: recent}, false},
		{"shown today", NudgeState{LastShareHintAt: now}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ShouldShowShareHint(now, c.st); got != c.want {
				t.Errorf("ShouldShowShareHint = %v, want %v", got, c.want)
			}
		})
	}
}

func TestShouldShowShareHintBoundary(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	// 29 days ago → still inside cooldown.
	day29 := now.AddDate(0, 0, -(shareHintCooldownDays - 1))
	if ShouldShowShareHint(now, NudgeState{LastShareHintAt: day29}) {
		t.Error("day 29 should still be inside cooldown")
	}
	// 30 days ago → cooldown expired, can show again.
	day30 := now.AddDate(0, 0, -shareHintCooldownDays)
	if !ShouldShowShareHint(now, NudgeState{LastShareHintAt: day30}) {
		t.Error("day 30 should allow showing again")
	}
}

func TestCommandFormat(t *testing.T) {
	cmd := Command("/usr/local/bin/reap")
	if cmd != "/usr/local/bin/reap nudge  # "+Marker {
		t.Errorf("Command() = %q", cmd)
	}
}

func TestUninstallNoopOnCleanFile(t *testing.T) {
	settings := filepath.Join(t.TempDir(), "settings.json")
	clean := `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"echo hi"}]}]}}`
	if err := os.WriteFile(settings, []byte(clean), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(settings); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(settings)
	if !strings.Contains(string(b), "echo hi") {
		t.Errorf("clean file modified: %s", b)
	}
}

func TestShouldNudgeNever(t *testing.T) {
	now := time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC)
	if ShouldNudge(now, 0, 0, NudgeState{LastNudgeAt: now}) {
		t.Error("ShouldNudge should block same-day repeat")
	}
}
