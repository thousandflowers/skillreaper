package scan

import (
	"os"
	"path/filepath"
)

// ProjectLoaded names the skills a project's own agents preload (`skills:` in
// .claude/agents/*.md) and the ones its own SessionStart hooks read. They are
// at work in that project's sessions with no use of their own on record, like
// the ones the user's folder loads.
func ProjectLoaded(project string) (byAgents, byHooks map[string]bool) {
	byAgents = map[string]bool{}
	dir := filepath.Join(project, ".claude")
	entries, _ := os.ReadDir(filepath.Join(dir, "agents"))
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		b, err := readCapped(filepath.Join(dir, "agents", e.Name()))
		if err != nil {
			continue
		}
		for _, name := range frontmatterList(b, "skills") {
			byAgents[name] = true
		}
	}
	return byAgents, userHookLoadedSkills(dir)
}
