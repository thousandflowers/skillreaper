package scan

import (
	"os"
	"path/filepath"
	"testing"
)

// linkedHome builds a home with one skill linked in from ~/.agents/skills (a
// relative link, as `npx skills add` makes), one from skills-reinvest, and one
// linked from a folder that is not a skill home.
func linkedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	write := func(dir string) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		md := "---\nname: x\ndescription: does a linked thing\n---\nbody\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	skills := filepath.Join(home, ".claude", "skills")
	write(filepath.Join(home, ".agents", "skills", "shared"))
	write(filepath.Join(home, ".claude", "skills-reinvest", "written"))
	write(filepath.Join(home, "elsewhere", "stray"))
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatal(err)
	}
	links := map[string]string{
		"shared":  filepath.Join("..", "..", ".agents", "skills", "shared"),
		"written": filepath.Join(home, ".claude", "skills-reinvest", "written"),
		"stray":   filepath.Join(home, "elsewhere", "stray"),
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(skills, name)); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
	}
	return home
}

func TestScanSkillsReadsLinkedSkillsFromSkillHomesOnly(t *testing.T) {
	home := linkedHome(t)
	items, _ := ScanSkills(filepath.Join(home, ".claude"), "claude-code")
	got := map[string]Item{}
	for _, it := range items {
		got[it.Name] = it
	}
	for _, name := range []string{"shared", "written"} {
		it, ok := got[name]
		if !ok {
			t.Fatalf("linked skill %q not scanned; got %v", name, got)
		}
		if it.LinkTarget == "" || it.Description != "does a linked thing" || it.DescChars == 0 {
			t.Errorf("%s: LinkTarget=%q Description=%q DescChars=%d", name, it.LinkTarget, it.Description, it.DescChars)
		}
	}
	// Anywhere else a link is reported and read-only.
	stray, ok := got["stray"]
	if !ok || stray.Removable || stray.ViaLink == "" || stray.LinkTarget != "" {
		t.Errorf("a skill linked from outside the skill homes must be a read-only row: %+v, %v", stray, ok)
	}
}
