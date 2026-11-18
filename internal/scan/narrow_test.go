package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A project folder that is there and cannot be entered protects nothing reap
// can see: its CLAUDE.md, settings and agents were taken for not there, and
// the skills they name came out REAP with no word.
func TestAProjectThatCannotBeEnteredIsUnread(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads everything")
	}
	proj := filepath.Join(t.TempDir(), "proj")
	put(t, filepath.Join(proj, "CLAUDE.md"), "Use /named-in-proj\n")
	put(t, filepath.Join(proj, ".claude", "settings.json"), "{}")
	if err := os.Chmod(proj, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(proj, 0o755) })

	if _, unread := LoadClaudeMDChecked("/nowhere", "/nohome", "/noconfig", proj); len(unread) == 0 {
		t.Error("the memory file behind a closed folder was taken for not there")
	}
	if _, _, unread := ProjectLoaded(proj, ""); unread == "" {
		t.Error("the settings behind a closed folder were taken for not there")
	}
}

// An agents folder that cannot be listed, and an agent file too large to
// read, may preload any skill.
func TestAgentsThatCannotBeReadAreUnread(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads everything")
	}
	proj := t.TempDir()
	agents := filepath.Join(proj, ".claude", "agents")
	put(t, filepath.Join(agents, "a.md"), "---\nskills: [x]\n---\n")
	if err := os.Chmod(agents, 0); err != nil {
		t.Fatal(err)
	}
	if got := UnreadProtection(filepath.Join(proj, ".claude")); got == "" {
		t.Error("an agents folder that cannot be listed was taken for empty")
	}
	_ = os.Chmod(agents, 0o755)

	if err := os.Truncate(filepath.Join(agents, "a.md"), maxFileSize+1); err != nil {
		t.Fatal(err)
	}
	if _, _, unread := ProjectLoaded(proj, ""); !strings.Contains(unread, "a.md") {
		t.Errorf("an agent file over the limit was passed by in silence: %q", unread)
	}
}

// A project's hook names its script from the folder the session runs in:
// with no folder at all, or by $PWD.
func TestProjectHookScriptByBareNameAndPWD(t *testing.T) {
	for _, command := range []string{"bash x.sh", "bash $PWD/x.sh", `bash "${PWD}/x.sh"`} {
		proj := t.TempDir()
		put(t, filepath.Join(proj, "x.sh"), "cat ~/.claude/skills/deploy-x/SKILL.md\n")
		put(t, filepath.Join(proj, ".claude", "settings.json"),
			`{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":`+quoteJSON(command)+`}]}]}}`)
		if _, hooks, _ := ProjectLoaded(proj, ""); !hooks["deploy-x"] {
			t.Errorf("%s: the script was not followed", command)
		}
	}
}

// The search for a name in a line, made quick, finds what the plain one found.
func TestIndexFoldFindsWhatThePlainSearchFound(t *testing.T) {
	plain := func(s, word string) int {
		for k := 0; k+len(word) <= len(s); k++ {
			if strings.EqualFold(s[k:k+len(word)], word) {
				return k
			}
		}
		return -1
	}
	for _, c := range [][2]string{
		{"Use /Deploy-X here", "deploy-x"}, {"use the SKILL", "skill"}, {"nothing", "skill"}, {"", "a"},
		{"a", ""}, {"Über ÜBER über", "über"}, {"xx ÉCOLE", "école"}, {"sk skill", "skill"}, {"1Password", "1password"},
	} {
		if got, want := indexFold(c[0], c[1]), plain(c[0], c[1]); got != want {
			t.Errorf("indexFold(%q, %q) = %d, want %d", c[0], c[1], got, want)
		}
	}
}

// The same line in many memory files is searched once: every package of a
// repository with the same CLAUDE.md made each run search it again per skill.
func TestMemoryLinesAreKeptOnce(t *testing.T) {
	root := t.TempDir()
	var projects []string
	for _, name := range []string{"a", "b", "c"} {
		put(t, filepath.Join(root, name, "CLAUDE.md"), "Use /same\nonly in "+name+"\n")
		projects = append(projects, filepath.Join(root, name))
	}
	lines, _ := LoadClaudeMDChecked("/nowhere", "/nohome", "/noconfig", projects...)
	if len(lines) != 4 || !ClaudeMDReferences(lines, "same") {
		t.Errorf("lines = %q", lines)
	}
}
