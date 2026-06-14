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
	want := NudgeState{LastNudgeAt: time.Now().Truncate(time.Second).UTC(), LastReapCount: 4, LastMuteCount: 2}
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
	// Missing state file yields a zero state, not an error.
	z, err := LoadNudgeState(filepath.Join(t.TempDir(), "x"))
	if err != nil || !z.LastNudgeAt.IsZero() {
		t.Errorf("missing state: %+v err %v", z, err)
	}
}
