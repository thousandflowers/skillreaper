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

// A line that starts with "#" is a Markdown heading, not a comment: a session
// loads it like every other line. A skill named only there is one the person
// relies on, and it used to be left open to REAP.
func TestLoadClaudeMDReadsHeadings(t *testing.T) {
	dir := t.TempDir()
	content := "## Use the `graphify` skill before every release\nuse the real-skill here\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// home empty so only the temp cwd CLAUDE.md is read (hermetic).
	lines := LoadClaudeMD(dir, "", "")
	if !ClaudeMDReferences(lines, "graphify") {
		t.Error("a skill named in a heading is not protected")
	}
	if !ClaudeMDReferences(lines, "real-skill") {
		t.Error("a name in a line of text should count")
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
		{"usa la skill review prima di finire", true},
		{"Skill review, sempre", true},
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
		"home/.claude/CLAUDE.md":          "use global-skill\n@EXTRA.md\n@~/shared.md\n",
		"home/.claude/EXTRA.md":           "use imported-skill\n@EXTRA.md\n",
		"home/shared.md":                  "use tilde-skill\n",
		"home/.claude/rules/a/b.md":       "use rule-skill\n",
		"home/.claude/rules/a/notes.txt":  "use txtskill\n",
		"repo/CLAUDE.md":                  "use parent-skill\n",
		"repo/sub/.claude/rules/local.md": "use projectrule-skill\n",
		"repo/sub/CLAUDE.md":              "use cwd-skill\n",
	})
	lines := LoadClaudeMD(cwd, home, claudeDir)
	for _, name := range []string{"global-skill", "imported-skill", "tilde-skill", "rule-skill", "parent-skill", "projectrule-skill", "cwd-skill"} {
		if !ClaudeMDReferences(lines, name) {
			t.Errorf("%s not found", name)
		}
	}
	if ClaudeMDReferences(lines, "txtskill") {
		t.Error("a rules file that is not markdown must not count")
	}
}

// A rules folder kept elsewhere and linked in is still the user's rules.
func TestLoadClaudeMDFollowsALinkedRulesFolder(t *testing.T) {
	root := t.TempDir()
	claudeDir := filepath.Join(root, "claude")
	writeAll(t, root, map[string]string{"dotfiles/rules/a/r.md": "use linked-skill\n"})
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "dotfiles", "rules"), filepath.Join(claudeDir, "rules")); err != nil {
		t.Fatal(err)
	}
	if !ClaudeMDReferences(LoadClaudeMD("", "", claudeDir), "linked-skill") {
		t.Error("a skill named in a linked rules folder must count")
	}
}

// A skill is named in a memory file in whatever case the sentence wants, as it
// is used in whatever case it is typed. And CLAUDE.local.md is read like CLAUDE.md.
func TestClaudeMDProtectionFoldsCaseAndReadsTheLocalFile(t *testing.T) {
	for line, name := range map[string]string{
		"Always run the S-unused skill first.": "s-unused",
		"Then use /Deploy when done.":          "deploy",
		"use the `Plan` skill":                 "ecc:plan",
	} {
		if !ClaudeMDReferences([]string{line}, name) {
			t.Errorf("%q does not protect %s", line, name)
		}
	}
	if ClaudeMDReferences([]string{"Redeploy the planner"}, "deploy") {
		t.Error("a longer word protects a shorter name")
	}
	cwd := t.TempDir()
	mustWrite(t, filepath.Join(cwd, "CLAUDE.local.md"), "use /only-local for this project\n")
	if !ClaudeMDReferences(LoadClaudeMD(cwd, "", ""), "only-local") {
		t.Error("CLAUDE.local.md is not read")
	}
}

// A name that is one plain word is also a word of the language: "review" in
// "always review the diff" is not a mention of a skill named review. Such a
// name protects its skill only where it is written as a name. A name with a
// dash, a colon, an underscore or a digit in it is no sentence's word, and
// counts wherever it stands.
func TestAPlainWordProtectsOnlyWhereItIsWrittenAsAName(t *testing.T) {
	for line, want := range map[string]bool{
		"always review the diff before committing": false,
		"Review everything twice.":                 false,
		"run /review before a merge":               true,
		"use `review` before a merge":              true,
		"the **review** step comes last":           true,
		"use the review skill":                     true,
		"usa la skill review":                      true,
		"- review: reads the diff":                 true,
		"* Review: reads the diff":                 true,
		"a note: review: later":                    false,
	} {
		if got := ClaudeMDReferences([]string{line}, "review"); got != want {
			t.Errorf("%q protects review = %v, want %v", line, got, want)
		}
	}
	for _, name := range []string{"code-review", "ecc:plan", "pdf2md", "my_skill"} {
		if !ClaudeMDReferences([]string{"then run " + name + " on it"}, name) {
			t.Errorf("%s in a sentence does not protect it", name)
		}
	}
}
