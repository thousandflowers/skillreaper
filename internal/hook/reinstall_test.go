package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Installing again with the binary somewhere else points the hook at it: the
// old command names a program that may be gone. Installing again with the same
// one changes nothing, and says so.
func TestInstallAgainRepairsAMovedBinaryAndLeavesTheSameOne(t *testing.T) {
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"theme": "dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	old, now := Command("/old/place/reap"), Command("/new/place/reap")
	if _, change, err := Installed(settings, old, false); err != nil || change != Added {
		t.Fatalf("first install: %v, %v", change, err)
	}

	_, change, err := Installed(settings, now, false)

	b, _ := os.ReadFile(settings)
	if err != nil || change != Updated || strings.Contains(string(b), "/old/place") || !strings.Contains(string(b), "/new/place") || !strings.Contains(string(b), `"theme": "dark"`) {
		t.Errorf("reinstall from a new place: %v, %v\n%s", change, err, b)
	}
	before, _ := os.ReadFile(settings)
	if _, change, err := Installed(settings, now, false); err != nil || change != Unchanged {
		t.Errorf("same install again: %v, %v", change, err)
	}
	if after, _ := os.ReadFile(settings); string(after) != string(before) {
		t.Error("an unchanged install rewrote the file")
	}
	if out, change, _ := Installed(settings, old, true); change != Updated || !strings.Contains(string(out), "/old/place") {
		t.Errorf("a dry run shows %v and\n%s", change, out)
	}
	if b, _ := os.ReadFile(settings); strings.Contains(string(b), "/old/place") {
		t.Error("a dry run wrote")
	}
}
