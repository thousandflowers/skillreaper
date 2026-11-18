package scan

import (
	"os"
	"path/filepath"
	"testing"
)

func put(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The memory files of the projects sessions ran in are read wherever reap is
// run from: a skill named only in one project's CLAUDE.md was protected from
// inside that project and REAP from anywhere else.
func TestMemoryFilesOfEveryProjectAreRead(t *testing.T) {
	home, proj := t.TempDir(), t.TempDir()
	put(t, filepath.Join(proj, "CLAUDE.md"), "This project ships with the deploy-x skill.\n")
	put(t, filepath.Join(proj, ".claude", "rules", "style.md"), "Use `lint-y` here.\n")
	lines, unread := LoadClaudeMDChecked(home, home, filepath.Join(home, ".claude"), proj, filepath.Join(home, "gone"))
	if len(unread) != 0 || !ClaudeMDReferences(lines, "deploy-x") || !ClaudeMDReferences(lines, "lint-y") {
		t.Errorf("lines %q, unread %v", lines, unread)
	}
}

// What a project's own agents preload and its own SessionStart hooks read.
func TestProjectLoadedNamesWhatAProjectsAgentsAndHooksLoad(t *testing.T) {
	proj := t.TempDir()
	put(t, filepath.Join(proj, ".claude", "agents", "pa.md"), "---\nname: pa\ndescription: d\nskills: [deploy-x, other]\n---\nbody\n")
	put(t, filepath.Join(proj, ".claude", "settings.json"), `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"cat ~/.claude/skills/hooked/SKILL.md"}]}]}}`)
	agents, hooks, _ := ProjectLoaded(proj, filepath.Join(proj, "user"))
	if !agents["deploy-x"] || !agents["other"] || !hooks["hooked"] || len(agents) != 2 {
		t.Errorf("agents %v, hooks %v", agents, hooks)
	}
	if a, h, unread := ProjectLoaded(filepath.Join(proj, "nothing"), ""); len(a)+len(h) != 0 || unread != "" {
		t.Errorf("a project with no .claude: %v %v", a, h)
	}
}

// Claude Code loads the CLAUDE.md of every folder above the one a session
// runs in. For a project named by a transcript only the project's own were
// read: in a monorepo, /repo/CLAUDE.md protected nothing for /repo/pkg.
func TestMemoryFilesAboveAProjectAreRead(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	pkg := filepath.Join(repo, "work", "pkg")
	put(t, filepath.Join(pkg, "main.go"), "package main\n")
	put(t, filepath.Join(repo, "CLAUDE.md"), "Use the /deploy-x skill for shipping.\n")
	if lines, _ := LoadClaudeMDChecked(home, home, filepath.Join(home, ".claude"), pkg); !ClaudeMDReferences(lines, "deploy-x") {
		t.Errorf("the repository's CLAUDE.md was not read for a session in %s", pkg)
	}
}

// A project's hook names its scripts by $CLAUDE_PROJECT_DIR or by a path
// from the project's root: only an absolute path was followed.
func TestAProjectsHookIsFollowedHoweverItNamesItsScript(t *testing.T) {
	for _, command := range []string{
		`$CLAUDE_PROJECT_DIR/.claude/hooks/load.sh`,
		`"$CLAUDE_PROJECT_DIR"/.claude/hooks/load.sh`,
		`${CLAUDE_PROJECT_DIR}/.claude/hooks/load.sh`,
		`.claude/hooks/load.sh`,
		`bash ./.claude/hooks/load.sh`,
	} {
		proj := t.TempDir()
		put(t, filepath.Join(proj, ".claude", "hooks", "load.sh"), "cat ~/.claude/skills/deploy-x/SKILL.md\n")
		put(t, filepath.Join(proj, ".claude", "settings.json"), `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":`+quoteJSON(command)+`}]}]}}`)
		if _, hooks, _ := ProjectLoaded(proj, filepath.Join(proj, "user")); !hooks["deploy-x"] {
			t.Errorf("%s: %v", command, hooks)
		}
	}
}

func quoteJSON(s string) string {
	out := `"`
	for _, r := range s {
		if r == '"' || r == '\\' {
			out += `\`
		}
		out += string(r)
	}
	return out + `"`
}

// A file that tells what is at work and cannot be read is said, in a project
// as in the user's folder, and one that is not a file (a pipe) is passed by:
// opening it hung the run.
func TestUnreadProtectionSaysWhatCannotBeReadAndNeverBlocks(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	dir := filepath.Join(t.TempDir(), ".claude")
	put(t, filepath.Join(dir, "agents", "ok.md"), "---\nname: ok\n---\n")
	if why := UnreadProtection(dir); why != "" {
		t.Errorf("all readable: %q", why)
	}
	closed := filepath.Join(dir, "agents", "closed.md")
	put(t, closed, "---\nname: c\nskills: [x]\n---\n")
	if err := os.Chmod(closed, 0); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { _ = os.Chmod(closed, 0o644) })
	if why := UnreadProtection(dir); why == "" {
		t.Error("an agent file that cannot be read is not said")
	}
	proj := filepath.Dir(dir)
	if _, _, unread := ProjectLoaded(proj, ""); unread == "" {
		t.Error("the same file in a project is not said")
	}
	if err := os.Mkdir(filepath.Join(dir, "settings.json"), 0o755); err != nil { // not a file: passed by
		t.Fatal(err)
	}
	_ = os.Chmod(closed, 0o644)
	if why := UnreadProtection(dir); why != "" {
		t.Errorf("a settings.json that is a folder: %q", why)
	}
}
