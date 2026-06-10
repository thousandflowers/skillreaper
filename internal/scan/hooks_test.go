package scan

import (
	"path/filepath"
	"testing"
)

func TestScanHooks(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, "settings.json"), `{
		"hooks": {
			"SessionStart": [
				{"hooks": [{"type": "command", "command": "node a.js"}]}
			],
			"UserPromptSubmit": [
				{"hooks": [
					{"type": "command", "command": "node b.js"},
					{"type": "command", "command": "node c.js"}
				]}
			]
		}
	}`)

	items, warns := ScanHooks(home)
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3: %+v", len(items), items)
	}
	if h := findItem(items, "UserPromptSubmit#1"); h == nil || h.Description != "node c.js" {
		t.Errorf("hook indexing wrong: %+v", h)
	}
}

func TestScanHooksCorrupt(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, "settings.json"), "{bad")
	_, warns := ScanHooks(home)
	if len(warns) != 1 {
		t.Errorf("expected 1 warning, got %d", len(warns))
	}
}

func TestScanProse(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	mustWrite(t, filepath.Join(home, "CLAUDE.md"), "global instructions, 30 chars!")
	mustWrite(t, filepath.Join(home, "rules", "common", "style.md"), "rule body")
	mustWrite(t, filepath.Join(cwd, "CLAUDE.md"), "project notes")

	items, warns := ScanProse(home, cwd)
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3: %+v", len(items), items)
	}
	var total int
	for _, it := range items {
		total += it.DescChars
	}
	want := len("global instructions, 30 chars!") + len("rule body") + len("project notes")
	if total != want {
		t.Errorf("total chars = %d, want %d", total, want)
	}
}
