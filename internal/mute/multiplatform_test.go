package mute

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Issue #41. Since #29, gather returns items from every detected platform, but
// mute resolved everything against the Claude directory. A skill living under
// another platform's config root earns a verdict the tool then refuses to act
// on, and the caller treats that refusal as fatal — so a multi-platform run
// stops partway through.
func TestMuteAcceptsAnotherPlatformRoot(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	otherRoot := filepath.Join(home, ".config", "opencode")

	skillDir := filepath.Join(otherRoot, "skills", "some-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(skillDir, "SKILL.md")
	body := "---\nname: some-skill\ndescription: a description worth stripping\n---\n\nbody\n"
	if err := os.WriteFile(skill, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// The item's own platform root is what the path must be confined against.
	if err := Mute(claudeDir, otherRoot, "some-skill", skill); err != nil {
		t.Fatalf("Mute refused an item from another platform: %v", err)
	}

	got, err := os.ReadFile(skill)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "a description worth stripping") {
		t.Error("the description was not stripped")
	}

	if err := Unmute(claudeDir, "some-skill"); err != nil {
		t.Fatalf("Unmute: %v", err)
	}
	restored, err := os.ReadFile(skill)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != body {
		t.Error("unmute did not restore the original file byte for byte")
	}
}

// The confinement itself must still hold: a path outside the root it was given
// is refused, whichever root that is.
func TestMuteStillRefusesOutsideItsRoot(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(filepath.Join(claudeDir, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(home, "elsewhere.md")
	if err := os.WriteFile(stray, []byte("---\nname: x\ndescription: d\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Mute(claudeDir, claudeDir, "x", stray); err == nil {
		t.Fatal("a path outside every mutable root was accepted")
	}
}
