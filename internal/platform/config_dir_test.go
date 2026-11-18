package platform

import (
	"path/filepath"
	"testing"
)

// What reap keeps about a Claude folder outside it (the record of past uses,
// the parse cache) is filed under the folder's path. One folder is one path
// however CLAUDE_CONFIG_DIR spells it: a trailing slash or a relative path
// used to open an empty record, and a use only that record remembered was
// gone for that spelling.
func TestClaudeConfigDirHasOneSpelling(t *testing.T) {
	parent := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(parent); err == nil {
		parent = resolved
	}
	want := filepath.Join(parent, "profile")
	t.Chdir(parent)
	for _, spelled := range []string{want, want + "/", "profile", "./profile/", filepath.Join(parent, "x", "..", "profile")} {
		t.Setenv("CLAUDE_CONFIG_DIR", spelled)
		for _, p := range All() {
			if p.ID != ClaudeCode {
				continue
			}
			if p.ConfigDir != want || p.ConfigFile != filepath.Join(want, ".claude.json") {
				t.Errorf("CLAUDE_CONFIG_DIR=%q: %s and %s, want %s", spelled, p.ConfigDir, p.ConfigFile, want)
			}
		}
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "~/elsewhere/")
	for _, p := range All() {
		if p.ID == ClaudeCode && p.ConfigDir != "~/elsewhere" {
			t.Errorf("a folder under ~ reads %s", p.ConfigDir)
		}
	}
}
