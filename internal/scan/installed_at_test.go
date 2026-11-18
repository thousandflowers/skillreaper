package scan

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A skill or agent of your own has no install record, so its file's date
// stands in: one written today is new, and the grace period can see it.
func TestYourOwnSkillsAndAgentsAreDatedByTheirFile(t *testing.T) {
	dir := t.TempDir()
	skill := filepath.Join(dir, "skills", "fresh", "SKILL.md")
	agent := filepath.Join(dir, "agents", "helper.md")
	for _, p := range []string{skill, agent} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("---\nname: x\ndescription: does a thing\n---\nbody\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(agent, old, old); err != nil {
		t.Fatal(err)
	}

	skills, _ := ScanSkills(dir, "claude-code")
	agents, _ := ScanAgents(dir, "claude-code")

	if len(skills) != 1 || time.Since(skills[0].InstalledAt) > time.Minute {
		t.Errorf("a skill written now is dated %v", skills)
	}
	if len(agents) != 1 || !agents[0].InstalledAt.Equal(old) {
		t.Errorf("an agent last written %v is dated %v", old, agents)
	}
}
