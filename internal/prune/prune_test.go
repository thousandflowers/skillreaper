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
