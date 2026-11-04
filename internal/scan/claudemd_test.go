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
		"always check first.",
		"- deploy: ship it",
	}
	cases := []struct {
		name string
		want bool
	}{
		{"graphify", true},     // a skill's whole name
		{"ecc:plan", true},     // full plugin key
		{"plan", false},        // "ecc:plan" names another skill
		{"graph", false},       // part of a longer word
		{"ecc:graphify", true}, // bare suffix followed by "skill"
		{"ecc:always", false},  // bare suffix not marked
		{"deploy", true},       // a colon closing the word does not extend it
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
	lines := LoadClaudeMD(dir, "", "")
	if ClaudeMDReferences(lines, "graphify") {
		t.Error("a name appearing only in a comment line must not count")
	}
	if !ClaudeMDReferences(lines, "realskill") {
		t.Error("a name in a non-comment line should count")
	}
}

func TestClaudeMDReferencesBareSuffixNeedsAMarker(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"run `review` before merging", true},
		{"run /review before merging", true},
		{"- **review** after each change", true},
		{"use the review skill", true},
		{"every change gets a review before merge", false},
		{"see code-review for details", false},
	}
	for _, c := range cases {
		if got := ClaudeMDReferences([]string{c.line}, "ecc:review"); got != c.want {
			t.Errorf("%q references ecc:review = %v, want %v", c.line, got, c.want)
		}
	}
}

func writeAll(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		mustWrite(t, filepath.Join(root, rel), body)
	}
}

// What protects a skill must not depend on where reap runs from: the user's own
// memory, its imports and its rules load in every session.
func TestLoadClaudeMDReadsWhatEverySessionLoads(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	claudeDir := filepath.Join(home, ".claude")
	cwd := filepath.Join(root, "repo", "sub")
	writeAll(t, root, map[string]string{
		"home/.claude/CLAUDE.md":          "use globalskill\n@EXTRA.md\n@~/shared.md\n",
		"home/.claude/EXTRA.md":           "use importedskill\n@EXTRA.md\n",
		"home/shared.md":                  "use tildeskill\n",
		"home/.claude/rules/a/b.md":       "use ruleskill\n",
		"home/.claude/rules/a/notes.txt":  "use txtskill\n",
		"repo/CLAUDE.md":                  "use parentskill\n",
		"repo/sub/.claude/rules/local.md": "use projectruleskill\n",
		"repo/sub/CLAUDE.md":              "use cwdskill\n",
	})
	lines := LoadClaudeMD(cwd, home, claudeDir)
	for _, name := range []string{"globalskill", "importedskill", "tildeskill", "ruleskill", "parentskill", "projectruleskill", "cwdskill"} {
		if !ClaudeMDReferences(lines, name) {
			t.Errorf("%s not found", name)
		}
	}
	if ClaudeMDReferences(lines, "txtskill") {
		t.Error("a rules file that is not markdown must not count")
	}
}
