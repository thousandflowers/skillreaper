package scan

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeMDReferences(t *testing.T) {
	lines := []string{
		"Always use the graphify skill for graphs.",
		"Prefer ecc:plan when planning.",
	}
	cases := []struct {
		name string
		want bool
	}{
		{"graphify", true},     // bare name substring
		{"ecc:plan", true},     // full plugin key
		{"plan", true},         // bare suffix of a plugin key
		{"nonexistent", false}, // absent
		{"", false},            // empty never matches
	}
	for _, c := range cases {
		if got := ClaudeMDReferences(lines, c.name); got != c.want {
			t.Errorf("ClaudeMDReferences(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestLoadClaudeMDSkipsComments(t *testing.T) {
	dir := t.TempDir()
	content := "# heading comment mentions graphify\nuse the realskill here\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// home empty so only the temp cwd CLAUDE.md is read (hermetic).
	lines := LoadClaudeMD(dir, "")
	if ClaudeMDReferences(lines, "graphify") {
		t.Error("a name appearing only in a comment line must not count")
	}
	if !ClaudeMDReferences(lines, "realskill") {
		t.Error("a name in a non-comment line should count")
	}
}
