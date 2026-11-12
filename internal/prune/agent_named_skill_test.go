package prune

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// An agent's file is the agent, whatever it is called: one named SKILL.md is
// no skill folder, and its folder holds every other agent.
func TestPruningAnAgentNamedSkillMovesOnlyThatFile(t *testing.T) {
	claudeDir := t.TempDir()
	agents := filepath.Join(claudeDir, "agents")
	for _, name := range []string{"SKILL.md", "other.md"} {
		mustWrite(t, filepath.Join(agents, name), "---\nname: x\ndescription: y\n---\n")
	}
	item := scan.Item{Category: scan.CatAgent, Name: "SKILL", Path: filepath.Join(agents, "SKILL.md"), Removable: true}

	if _, err := QuarantineItem(claudeDir, item); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(agents, "other.md")); err != nil {
		t.Errorf("the other agent went with it: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agents, "SKILL.md")); !os.IsNotExist(err) {
		t.Errorf("the pruned agent is still there: %v", err)
	}
}
