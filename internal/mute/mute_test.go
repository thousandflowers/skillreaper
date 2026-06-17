package mute

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const skillMD = "---\nname: heavy\ndescription: a very long description that costs tokens every session\n---\nbody stays\n"

func TestStripDescription(t *testing.T) {
	out, ok := stripDescription([]byte(skillMD))
	if !ok {
		t.Fatal("expected a description to strip")
	}
	s := string(out)
	if strings.Contains(s, "description:") {
		t.Error("description line should be gone")
	}
	for _, want := range []string{"name: heavy", "body stays", "---"} {
		if !strings.Contains(s, want) {
			t.Errorf("stripped output missing %q", want)
		}
	}
}

func TestStripDescriptionMultiLine(t *testing.T) {
	// A folded/block scalar description spans several indented continuation
	// lines. Dropping only the "description:" line leaves the continuation
	// lines behind as malformed frontmatter.
	in := "---\nname: foo\ndescription: >\n  line one\n  line two\ntools: Read\n---\nbody\n"
	out, ok := stripDescription([]byte(in))
	if !ok {
		t.Fatal("expected a description to strip")
	}
	s := string(out)
	if strings.Contains(s, "line one") || strings.Contains(s, "line two") {
		t.Errorf("continuation lines of the description were left behind:\n%s", s)
	}
	for _, want := range []string{"name: foo", "tools: Read", "body"} {
		if !strings.Contains(s, want) {
			t.Errorf("stripped output missing %q:\n%s", want, s)
		}
	}
}

func TestBackupNameUniquePerName(t *testing.T) {
	// Names that collapse to the same sanitized form must not share a backup
	// file, or muting both would clobber the first's backup.
	seen := map[string]string{}
	for _, name := range []string{"a:b", "a/b", "a-b", "a\\b"} {
		bn := backupName(name)
		if prev, ok := seen[bn]; ok {
			t.Errorf("backupName(%q) collides with backupName(%q) = %q", name, prev, bn)
		}
		seen[bn] = name
	}
}

func TestStripDescriptionNoFrontmatter(t *testing.T) {
	if _, ok := stripDescription([]byte("no frontmatter here\n")); ok {
		t.Error("should report nothing stripped")
	}
}

func writeSkill(t *testing.T, claudeDir, name string) string {
	t.Helper()
	p := filepath.Join(claudeDir, "skills", name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(skillMD), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMuteUnmuteRoundtrip(t *testing.T) {
	claudeDir := filepath.Join(t.TempDir(), ".claude")
	skillPath := writeSkill(t, claudeDir, "heavy")

	if err := Mute(claudeDir, "heavy", skillPath); err != nil {
		t.Fatalf("Mute: %v", err)
	}
	b, _ := os.ReadFile(skillPath)
	if strings.Contains(string(b), "description:") {
		t.Error("muted file still has a description")
	}

	// Idempotency guard: muting again must fail, not double-strip.
	if err := Mute(claudeDir, "heavy", skillPath); err == nil {
		t.Error("muting an already-muted skill should error")
	}

	if err := Unmute(claudeDir, "heavy"); err != nil {
		t.Fatalf("Unmute: %v", err)
	}
	b, _ = os.ReadFile(skillPath)
	if string(b) != skillMD {
		t.Errorf("unmute did not restore original:\n%q", string(b))
	}
}

func TestUnmuteAll(t *testing.T) {
	claudeDir := filepath.Join(t.TempDir(), ".claude")
	a := writeSkill(t, claudeDir, "a")
	b := writeSkill(t, claudeDir, "b")
	if err := Mute(claudeDir, "a", a); err != nil {
		t.Fatal(err)
	}
	if err := Mute(claudeDir, "b", b); err != nil {
		t.Fatal(err)
	}
	n, err := UnmuteAll(claudeDir)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("UnmuteAll = %d, want 2", n)
	}
	for _, p := range []string{a, b} {
		got, _ := os.ReadFile(p)
		if string(got) != skillMD {
			t.Errorf("%s not restored", p)
		}
	}
}

func TestMuteRollbackOnStateFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-based write-failure injection does not work as root")
	}
	claudeDir := filepath.Join(t.TempDir(), ".claude")
	skillPath := writeSkill(t, claudeDir, "heavy")
	// Pre-create a read-only state file: loadState succeeds, saveState fails.
	if err := os.MkdirAll(mutedDir(claudeDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath(claudeDir), []byte(`{"muted":{}}`), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := Mute(claudeDir, "heavy", skillPath); err == nil {
		t.Fatal("expected Mute to fail when state cannot be saved")
	}
	b, _ := os.ReadFile(skillPath)
	if !strings.Contains(string(b), "description:") {
		t.Errorf("skill was stripped but mute not recorded — should have rolled back:\n%s", b)
	}
}

func TestUnmuteAllPersistsPartialProgress(t *testing.T) {
	claudeDir := filepath.Join(t.TempDir(), ".claude")
	a := writeSkill(t, claudeDir, "a")
	b := writeSkill(t, claudeDir, "b")
	if err := Mute(claudeDir, "a", a); err != nil {
		t.Fatal(err)
	}
	if err := Mute(claudeDir, "b", b); err != nil {
		t.Fatal(err)
	}
	// Break "b"'s restore by removing its backup; "a" (sorted first) restores.
	if err := os.Remove(filepath.Join(mutedDir(claudeDir), backupName("b"))); err != nil {
		t.Fatal(err)
	}
	if _, err := UnmuteAll(claudeDir); err == nil {
		t.Fatal("expected UnmuteAll to fail on the broken backup")
	}
	s, err := loadState(claudeDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Muted["a"]; ok {
		t.Error("\"a\" restored but progress not persisted before the error")
	}
	if _, ok := s.Muted["b"]; !ok {
		t.Error("\"b\" failed to restore but was dropped from state")
	}
}

func TestListEmpty(t *testing.T) {
	claudeDir := filepath.Join(t.TempDir(), ".claude")
	list, err := List(claudeDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %v", list)
	}
}

func TestListAfterMute(t *testing.T) {
	claudeDir := filepath.Join(t.TempDir(), ".claude")
	skillPath := writeSkill(t, claudeDir, "test-skill")
	if err := Mute(claudeDir, "test-skill", skillPath); err != nil {
		t.Fatal(err)
	}
	list, err := List(claudeDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0] != "test-skill" {
		t.Errorf("list = %v, want [test-skill]", list)
	}
}

func TestUnmuteNotMuted(t *testing.T) {
	claudeDir := filepath.Join(t.TempDir(), ".claude")
	if err := Unmute(claudeDir, "never-muted"); err == nil {
		t.Error("expected error for not-muted skill")
	}
}

func TestMuteInvalidPath(t *testing.T) {
	claudeDir := filepath.Join(t.TempDir(), ".claude")
	if err := Mute(claudeDir, "ghost", "/nonexistent/SKILL.md"); err == nil {
		t.Error("expected error for invalid path")
	}
}

func TestStripDescriptionEdgeCases(t *testing.T) {
	if _, ok := stripDescription([]byte("just a normal line\nwithout frontmatter\n")); ok {
		t.Error("should report nothing stripped")
	}
	if _, ok := stripDescription([]byte("")); ok {
		t.Error("empty input should report nothing stripped")
	}
}
