package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The state files record where things are. Recorded from a relative
// --claude-dir, they named nothing once run from another folder: a mute made
// there could not be undone, and its record was dropped.
func TestARelativeClaudeDirIsRecordedAsAFullPath(t *testing.T) {
	// Arrange
	claudeDir, claudeJSON := buildFixture(t)
	parent := filepath.Dir(claudeDir)
	t.Chdir(parent)
	rel := func(p string) string { r, _ := filepath.Rel(parent, p); return r }
	skill := filepath.Join(claudeDir, "skills", "deadskill", "SKILL.md")
	if code, out := reapRun("--claude-dir", rel(claudeDir), "--claude-json", rel(claudeJSON), "--yes", "mute", "deadskill"); code != 0 {
		t.Fatalf("mute: %d %s", code, out)
	}

	// Act
	t.Chdir(t.TempDir())
	code, out := reapRun("--claude-dir", claudeDir, "--claude-json", claudeJSON, "--yes", "unmute", "--all")

	// Assert
	b, _ := os.ReadFile(skill)
	if code != 0 || !strings.Contains(string(b), "description:") || strings.Contains(out, "gone") {
		t.Errorf("unmute from elsewhere: %d %s\n%s", code, out, b)
	}
}
