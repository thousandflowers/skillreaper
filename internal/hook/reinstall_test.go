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

// A group of hooks that is someone else's stays, empty or not: uninstall takes
// only skillreaper's entry, and the group that entry alone was keeping alive.
func TestUninstallLeavesOtherEmptyGroupsAlone(t *testing.T) {
	settings := filepath.Join(t.TempDir(), "settings.json")
	doc := `{"hooks":{"SessionStart":[{"matcher":"startup","hooks":[]},{"hooks":[{"type":"command","command":"echo hi"}]}]}}`
	if err := os.WriteFile(settings, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	if removed, err := Uninstalled(settings); err != nil || removed {
		t.Errorf("removed %v, %v", removed, err)
	}
	if b, _ := os.ReadFile(settings); string(b) != doc {
		t.Errorf("the file changed:\n%s", b)
	}
}

// "hooks": null or [] holds no hook: install puts one there, uninstall finds
// nothing. Any other hooks value that is not an object is named, not called
// unreadable.
func TestHooksThatAreNullOrEmptyAreNoHooks(t *testing.T) {
	for _, doc := range []string{`{"hooks":null}`, `{"hooks":[]}`, `{"theme":"x","hooks": null }`} {
		settings := filepath.Join(t.TempDir(), "settings.json")
		if err := os.WriteFile(settings, []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
		if removed, err := Uninstalled(settings); err != nil || removed {
			t.Errorf("%s: uninstall %v, %v", doc, removed, err)
		}
		if _, change, err := Installed(settings, Command("/x/reap"), false); err != nil || change != Added {
			t.Errorf("%s: install %v, %v", doc, change, err)
		}
		if _, change, err := Installed(settings, Command("/x/reap"), false); err != nil || change != Unchanged {
			t.Errorf("%s: installed twice %v, %v", doc, change, err)
		}
	}
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"hooks":"oops"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Installed(settings, Command("/x/reap"), false); err == nil || !strings.Contains(err.Error(), `"hooks" is not an object`) {
		t.Errorf("a string hooks: %v", err)
	}
}
