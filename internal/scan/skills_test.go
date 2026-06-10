package scan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// buildFixtureHome creates a fake ~/.claude tree with one personal
// skill, one plugin (with one skill and one agent), and one personal agent.
func buildFixtureHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()

	// Personal skill.
	mustWrite(t, filepath.Join(home, "skills", "myskill", "SKILL.md"),
		"---\nname: myskill\ndescription: Personal test skill\n---\nbody")

	// Personal agent.
	mustWrite(t, filepath.Join(home, "agents", "helper.md"),
		"---\nname: helper\ndescription: Personal test agent\n---\nagent body")

	// Plugin with a skill and an agent.
	plugDir := filepath.Join(home, "plugins", "cache", "mkt", "coolplug", "1.0.0")
	mustWrite(t, filepath.Join(plugDir, "skills", "subskill", "SKILL.md"),
		"---\nname: subskill\ndescription: Plugin test skill\n---\nplugin body")
	mustWrite(t, filepath.Join(plugDir, "agents", "worker.md"),
		"---\nname: worker\ndescription: Plugin test agent\n---\nworker body")

	reg := map[string]any{
		"version": 2,
		"plugins": map[string]any{
			"coolplug@mkt": []map[string]any{{
				"installPath": plugDir,
				"installedAt": "2026-05-15T22:41:04.874Z",
			}},
		},
	}
	b, _ := json.Marshal(reg)
	mustWrite(t, filepath.Join(home, "plugins", "installed_plugins.json"), string(b))
	return home
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func findItem(items []Item, name string) *Item {
	for i := range items {
		if items[i].Name == name {
			return &items[i]
		}
	}
	return nil
}

func TestScanSkills(t *testing.T) {
	home := buildFixtureHome(t)
	items, warns := ScanSkills(home, "test")
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2: %+v", len(items), items)
	}

	personal := findItem(items, "myskill")
	if personal == nil {
		t.Fatal("personal skill not found")
	}
	if !personal.Removable || personal.Source != "personal" {
		t.Errorf("personal skill wrong: %+v", personal)
	}
	if personal.Description != "Personal test skill" {
		t.Errorf("description = %q", personal.Description)
	}

	plug := findItem(items, "coolplug:subskill")
	if plug == nil {
		t.Fatal("plugin skill not found")
	}
	if plug.Removable || plug.Source != "plugin:coolplug@mkt" {
		t.Errorf("plugin skill wrong: %+v", plug)
	}
	want, _ := time.Parse(time.RFC3339, "2026-05-15T22:41:04.874Z")
	if !plug.InstalledAt.Equal(want) {
		t.Errorf("InstalledAt = %v, want %v", plug.InstalledAt, want)
	}
}

func TestScanSkillsMissingDirs(t *testing.T) {
	items, warns := ScanSkills(t.TempDir(), "test")
	if len(items) != 0 || len(warns) != 0 {
		t.Errorf("expected empty results, got %d items %d warns", len(items), len(warns))
	}
}

func TestScanSkillsCorruptRegistry(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, "skills", "ok", "SKILL.md"),
		"---\nname: ok\ndescription: still works\n---\n")
	mustWrite(t, filepath.Join(home, "plugins", "installed_plugins.json"), "{not json")

	items, warns := ScanSkills(home, "test")
	if len(items) != 1 {
		t.Errorf("personal skills should survive corrupt registry, got %d", len(items))
	}
	if len(warns) != 1 {
		t.Errorf("expected 1 warning, got %d", len(warns))
	}
}

func TestScanAgents(t *testing.T) {
	home := buildFixtureHome(t)
	items, warns := ScanAgents(home, "test")
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2: %+v", len(items), items)
	}
	if a := findItem(items, "helper"); a == nil || !a.Removable {
		t.Errorf("personal agent wrong: %+v", a)
	}
	if a := findItem(items, "coolplug:worker"); a == nil || a.Removable {
		t.Errorf("plugin agent wrong: %+v", a)
	}
}
