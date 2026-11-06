package mute

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMuteEditsALinkedSkillInItsSkillHome: a skill linked in from
// ~/.agents/skills is muted where it lives, and unmuted back.
func TestMuteEditsALinkedSkillInItsSkillHome(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	target := filepath.Join(home, ".agents", "skills", "heavy")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(claudeDir, "skills", "heavy")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := Mute(claudeDir, claudeDir, "heavy", filepath.Join(link, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(target, "SKILL.md")); string(b) == skillMD {
		t.Fatal("description not stripped")
	}
	if _, err := Unmute(claudeDir, "heavy"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(target, "SKILL.md")); string(b) != skillMD {
		t.Fatalf("unmute did not restore: %q", b)
	}
}
