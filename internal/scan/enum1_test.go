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
	agents, hooks := ProjectLoaded(proj)
	if !agents["deploy-x"] || !agents["other"] || !hooks["hooked"] || len(agents) != 2 {
		t.Errorf("agents %v, hooks %v", agents, hooks)
	}
	if a, h := ProjectLoaded(filepath.Join(proj, "nothing")); len(a)+len(h) != 0 {
		t.Errorf("a project with no .claude: %v %v", a, h)
	}
}
