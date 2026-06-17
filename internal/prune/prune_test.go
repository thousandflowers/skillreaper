package prune

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// freezeManifest pre-creates a read-only manifest so LoadManifest succeeds but
// saveManifest fails, simulating a write failure after the file mutation.
func freezeManifest(t *testing.T, claudeDir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("permission-based write-failure injection does not work as root")
	}
	p := filepath.Join(claudeDir, "reaped", "manifest.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"version":1,"entries":[]}`), 0o400); err != nil {
		t.Fatal(err)
	}
}

func TestQuarantineItemRollbackOnManifestFailure(t *testing.T) {
	claudeDir := t.TempDir()
	skillMd := filepath.Join(claudeDir, "skills", "deadskill", "SKILL.md")
	mustWrite(t, skillMd, "---\nname: deadskill\n---\nbody")
	freezeManifest(t, claudeDir)

	item := scan.Item{Category: scan.CatSkill, Name: "deadskill", Path: skillMd, Removable: true}
	if _, err := QuarantineItem(claudeDir, item); err == nil {
		t.Fatal("expected QuarantineItem to fail when the manifest cannot be written")
	}
	if _, err := os.Stat(filepath.Dir(skillMd)); err != nil {
		t.Errorf("skill was moved but not recorded — should have rolled back: %v", err)
	}
}

func TestRemoveMCPRollbackOnManifestFailure(t *testing.T) {
	claudeDir := t.TempDir()
	configPath := filepath.Join(claudeDir, "config.json")
	const cfg = `{"mcpServers":{"keep":{"command":"a"},"drop":{"command":"b"}}}`
	mustWrite(t, configPath, cfg)
	freezeManifest(t, claudeDir)

	if _, err := RemoveMCP(claudeDir, configPath, "", "drop"); err == nil {
		t.Fatal("expected RemoveMCP to fail when the manifest cannot be written")
	}
	b, _ := os.ReadFile(configPath)
	var top map[string]map[string]any
	if err := json.Unmarshal(b, &top); err != nil {
		t.Fatalf("config corrupted: %v", err)
	}
	if _, ok := top["mcpServers"]["drop"]; !ok {
		t.Error("server removed from config but not recorded — should have rolled back the config edit")
	}
}

func TestRestoreAllPersistsPartialProgress(t *testing.T) {
	claudeDir := t.TempDir()
	for _, name := range []string{"a", "b"} {
		md := filepath.Join(claudeDir, "skills", name, "SKILL.md")
		mustWrite(t, md, "---\nname: "+name+"\n---\nbody")
		if _, err := QuarantineItem(claudeDir, scan.Item{Category: scan.CatSkill, Name: name, Path: md, Removable: true}); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := LoadManifest(claudeDir)
	// Break the second entry's restore by removing its quarantined copy.
	if err := os.RemoveAll(entries[1].To); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreAll(claudeDir); err == nil {
		t.Fatal("expected RestoreAll to fail on the broken entry")
	}
	reloaded, _ := LoadManifest(claudeDir)
	if !reloaded[0].Restored {
		t.Error("first entry restored but progress not persisted before the error")
	}
}

func TestRestoreRefusesPathOutsideClaudeDir(t *testing.T) {
	claudeDir := t.TempDir()
	// A quarantined file that a tampered manifest tries to move outside the tree.
	to := filepath.Join(reapedDir(claudeDir), "skill", "payload")
	mustWrite(t, to, "payload")
	outside := filepath.Join(t.TempDir(), "victim") // not under claudeDir
	if err := saveManifest(claudeDir, []Entry{{ID: "001", Category: "skill", Name: "x", From: outside, To: to}}); err != nil {
		t.Fatal(err)
	}
	if err := Restore(claudeDir, "001"); err == nil {
		t.Fatal("expected Restore to refuse a destination outside the claude dir")
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Error("restore moved a file to a location outside the claude dir")
	}
}

func TestQuarantineAndRestoreSkill(t *testing.T) {
	claudeDir := t.TempDir()
	skillMd := filepath.Join(claudeDir, "skills", "deadskill", "SKILL.md")
	mustWrite(t, skillMd, "---\nname: deadskill\n---\nbody")
	mustWrite(t, filepath.Join(claudeDir, "skills", "deadskill", "extra.txt"), "extra")

	item := scan.Item{Category: scan.CatSkill, Name: "deadskill", Path: skillMd, Removable: true}
	e, err := QuarantineItem(claudeDir, item)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Dir(skillMd)); !os.IsNotExist(err) {
		t.Error("skill dir should be gone from original location")
	}
	if _, err := os.Stat(filepath.Join(e.To, "SKILL.md")); err != nil {
		t.Errorf("quarantined SKILL.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(e.To, "extra.txt")); err != nil {
		t.Errorf("quarantined extra file missing: %v", err)
	}

	entries, err := LoadManifest(claudeDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("manifest entries = %d (err %v), want 1", len(entries), err)
	}

	if err := Restore(claudeDir, e.ID); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(skillMd)
	if err != nil {
		t.Fatalf("restored SKILL.md unreadable: %v", err)
	}
	if string(b) != "---\nname: deadskill\n---\nbody" {
		t.Error("restored content differs")
	}

	entries, _ = LoadManifest(claudeDir)
	if !entries[0].Restored {
		t.Error("manifest entry not marked restored")
	}
	if err := Restore(claudeDir, e.ID); err == nil {
		t.Error("double restore should fail")
	}
}

func TestRemoveAndRestoreMCP(t *testing.T) {
	claudeDir := t.TempDir()
	cfg := filepath.Join(claudeDir, "claude.json")
	mustWrite(t, cfg, `{
		"unrelated": {"keep": true},
		"mcpServers": {"deadsrv": {"command": "uvx", "args": ["deadsrv"]}, "live": {"command": "x"}},
		"projects": {"/p": {"mcpServers": {"projsrv": {"command": "y"}}, "other": 1}}
	}`)

	e, err := RemoveMCP(claudeDir, cfg, "", "deadsrv")
	if err != nil {
		t.Fatal(err)
	}

	var top map[string]json.RawMessage
	b, _ := os.ReadFile(cfg)
	if err := json.Unmarshal(b, &top); err != nil {
		t.Fatalf("config corrupted after removal: %v", err)
	}
	var servers map[string]json.RawMessage
	_ = json.Unmarshal(top["mcpServers"], &servers)
	if _, ok := servers["deadsrv"]; ok {
		t.Error("deadsrv still present")
	}
	if _, ok := servers["live"]; !ok {
		t.Error("live server lost")
	}
	if _, ok := top["unrelated"]; !ok {
		t.Error("unrelated key lost")
	}

	// Backup exists.
	backups, _ := os.ReadDir(filepath.Join(claudeDir, "reaped", "backups"))
	if len(backups) != 1 {
		t.Errorf("backups = %d, want 1", len(backups))
	}

	// Project-scope removal.
	if _, err := RemoveMCP(claudeDir, cfg, "/p", "projsrv"); err != nil {
		t.Fatal(err)
	}

	// Restore everything.
	n, err := RestoreAll(claudeDir)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("restored %d entries, want 2", n)
	}

	b, _ = os.ReadFile(cfg)
	top = nil
	if err := json.Unmarshal(b, &top); err != nil {
		t.Fatal(err)
	}
	servers = nil
	_ = json.Unmarshal(top["mcpServers"], &servers)
	if _, ok := servers["deadsrv"]; !ok {
		t.Error("deadsrv not restored")
	}
	var projects map[string]json.RawMessage
	_ = json.Unmarshal(top["projects"], &projects)
	var proj map[string]json.RawMessage
	_ = json.Unmarshal(projects["/p"], &proj)
	var projServers map[string]json.RawMessage
	_ = json.Unmarshal(proj["mcpServers"], &projServers)
	if _, ok := projServers["projsrv"]; !ok {
		t.Error("projsrv not restored")
	}
	if _, ok := proj["other"]; !ok {
		t.Error("project sibling key lost")
	}
	_ = e
}

func TestRemoveMCPNotFound(t *testing.T) {
	claudeDir := t.TempDir()
	cfg := filepath.Join(claudeDir, "claude.json")
	mustWrite(t, cfg, `{"mcpServers": {}}`)
	if _, err := RemoveMCP(claudeDir, cfg, "", "ghost"); err == nil {
		t.Error("expected error for missing server")
	}
}

func TestRestoreUnknownID(t *testing.T) {
	if err := Restore(t.TempDir(), "999"); err == nil {
		t.Error("expected error for unknown id")
	}
}

func TestQuarantineNonRemovable(t *testing.T) {
	claudeDir := t.TempDir()
	item := scan.Item{Category: scan.CatMCP, Name: "srv", Removable: false}
	if _, err := QuarantineItem(claudeDir, item); err == nil {
		t.Error("expected error for non-removable item")
	}
}

func TestRestoreAllEmpty(t *testing.T) {
	n, err := RestoreAll(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("RestoreAll on empty manifest = %d, want 0", n)
	}
}

func TestLoadManifestNone(t *testing.T) {
	entries, err := LoadManifest(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if entries != nil {
		t.Errorf("expected nil, got %v", entries)
	}
}

func TestRemoveMCPProjectScope(t *testing.T) {
	claudeDir := t.TempDir()
	cfg := filepath.Join(claudeDir, "claude.json")
	mustWrite(t, cfg, `{"mcpServers": {"keep": {"command": "x"}}, "projects": {"/p": {"mcpServers": {"proj": {"command": "y"}}}}}`)

	e, err := RemoveMCP(claudeDir, cfg, "/p", "proj")
	if err != nil {
		t.Fatal(err)
	}
	if e.JSONScope != "/p" || e.Name != "proj" {
		t.Errorf("entry = %+v", e)
	}

	// Verify top-level servers untouched.
	var top map[string]json.RawMessage
	b, _ := os.ReadFile(cfg)
	json.Unmarshal(b, &top)
	var servers map[string]json.RawMessage
	json.Unmarshal(top["mcpServers"], &servers)
	if _, ok := servers["keep"]; !ok {
		t.Error("top-level server was removed")
	}
}

func TestSanitizeName(t *testing.T) {
	claudeDir := t.TempDir()
	skillMd := filepath.Join(claudeDir, "skills", "my:skill", "SKILL.md")
	mustWrite(t, skillMd, "---\nname: my:skill\n---\nbody")

	item := scan.Item{Category: scan.CatSkill, Name: "my:skill", Path: skillMd, Removable: true}
	e, err := QuarantineItem(claudeDir, item)
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != "my:skill" {
		t.Errorf("entry name = %q", e.Name)
	}
}
