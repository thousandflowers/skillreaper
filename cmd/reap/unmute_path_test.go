package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `reap unmute <path>` undoes that one file. By name it undoes every copy, and
// reinvest's roll back must not undo a copy the person muted in another tool.
func TestUnmuteByPathUndoesOnlyThatCopy(t *testing.T) {
	// Arrange
	home := deleteHome(t)
	mine := filepath.Join(home, ".claude", "skills", "dup", "SKILL.md")
	other := filepath.Join(home, ".codex", "skills", "dup", "SKILL.md")
	for _, p := range []string{mine, other} {
		if code, out := reapRun("--yes", "mute", p); code != 0 {
			t.Fatalf("mute %s: %d %s", p, code, out)
		}
	}

	// Act
	code, out := reapRun("unmute", mine)

	// Assert
	if code != 0 {
		t.Fatalf("unmute by path: %d %s", code, out)
	}
	if b, _ := os.ReadFile(mine); !strings.Contains(string(b), "description:") {
		t.Errorf("the named copy is still muted:\n%s", out)
	}
	if b, _ := os.ReadFile(other); strings.Contains(string(b), "description:") {
		t.Error("unmuting one path unmuted the copy in another tool")
	}
	if code, out := reapRun("unmute", filepath.Join(home, "nothing.md")); code == 0 {
		t.Errorf("a path that is not muted was unmuted: %s", out)
	}
}

// The undo line is pasted as it is: a skill named with a space is one word.
func TestUndoHintsQuoteANameWithASpace(t *testing.T) {
	home := deleteHome(t)
	p := filepath.Join(home, ".claude/skills/my skill/SKILL.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("---\nname: my skill\ndescription: some words here that are long enough\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	backdated(t, p)
	if code, out := reapRun("mute", "my skill", "--yes"); code != 0 || !strings.Contains(out, "Undo: reap unmute 'my skill'\n") {
		t.Errorf("mute: %d %s", code, out)
	}
	if code, out := reapRun("keep", "my skill"); code != 0 || !strings.Contains(out, "Undo: reap keep --remove 'skill:my skill'\n") {
		t.Errorf("keep: %d %s", code, out)
	}
}
