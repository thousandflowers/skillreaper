package prune

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// TestQuarantineMovesTheLinkNotItsTarget: pruning a linked skill moves the
// symlink into the quarantine; the shared folder it points at is untouched,
// and restore puts the link back.
func TestQuarantineMovesTheLinkNotItsTarget(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	target := filepath.Join(home, ".agents", "skills", "shared")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\nname: shared\ndescription: linked\n---\n"
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(claudeDir, "skills", "shared")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "..", ".agents", "skills", "shared"), link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	items, _ := scan.ScanSkills(claudeDir, "claude-code")
	if len(items) != 1 || items[0].LinkTarget == "" {
		t.Fatalf("want one linked skill, got %+v", items)
	}

	e, err := QuarantineItem(claudeDir, items[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("link still in skills/: %v", err)
	}
	if info, err := os.Lstat(e.To); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("quarantine should hold the link itself: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(target, "SKILL.md")); err != nil || string(b) != md {
		t.Fatalf("target was touched: %q %v", b, err)
	}

	if err := Restore(claudeDir, e.ID); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(link, "SKILL.md")); err != nil || string(b) != md {
		t.Fatalf("restored link does not resolve: %q %v", b, err)
	}
}

// TestQuarantineRefusesALinkItemThatIsNoLongerALink: the item says linked but
// the path is now a real directory; it is not moved on the link's terms.
func TestQuarantineRefusesALinkItemThatIsNoLongerALink(t *testing.T) {
	claudeDir := filepath.Join(t.TempDir(), ".claude")
	dir := filepath.Join(claudeDir, "skills", "x")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	it := scan.Item{Category: scan.CatSkill, Name: "x", Path: filepath.Join(dir, "SKILL.md"),
		Removable: true, LinkTarget: "/somewhere"}
	if _, err := QuarantineItem(claudeDir, it); err == nil {
		t.Fatal("expected refusal: not a symlink any more")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("directory moved anyway: %v", err)
	}
}

// TestPruningAWrittenSkillDeletesItsFolderAndRestoreBringsItBack: a skill
// reinvest wrote has no other copy, so prune takes the folder with the link.
func TestPruningAWrittenSkillDeletesItsFolderAndRestoreBringsItBack(t *testing.T) {
	claudeDir := filepath.Join(t.TempDir(), ".claude")
	folder := filepath.Join(claudeDir, "skills-reinvest", "ship-release")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\nname: ship-release\ndescription: written\n---\n"
	if err := os.WriteFile(filepath.Join(folder, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(claudeDir, "skills", "ship-release")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(folder, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	items, _ := scan.ScanSkills(claudeDir, "claude-code")
	if len(items) != 1 || !items[0].Written() {
		t.Fatalf("want one written skill, got %+v", items)
	}

	e, err := QuarantineItem(claudeDir, items[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{link, folder} {
		if _, err := os.Lstat(gone); !os.IsNotExist(err) {
			t.Fatalf("%s still there after prune: %v", gone, err)
		}
	}
	if err := Restore(claudeDir, e.ID); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(link, "SKILL.md")); err != nil || string(b) != md {
		t.Fatalf("restore did not bring the written skill back: %q %v", b, err)
	}
}

// TestASharedLinkedSkillIsNotWritten: a skill linked from ~/.agents/skills
// is somebody's install; prune never deletes its folder.
func TestASharedLinkedSkillIsNotWritten(t *testing.T) {
	it := scan.Item{Path: "/h/.claude/skills/x/SKILL.md", LinkTarget: "/h/.agents/skills/x"}
	if it.Written() {
		t.Fatal("a shared linked skill must not count as written")
	}
}

// TestChosenStaysInsideItsRoots: naming an item lifts Removable and nothing else.
func TestChosenStaysInsideItsRoots(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	outside := filepath.Join(home, "elsewhere", "x")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := filepath.Join(outside, "SKILL.md")
	if err := os.WriteFile(md, []byte("---\nname: x\ndescription: d\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	it := scan.Item{Category: scan.CatSkill, Name: "x", Path: md, RootDir: claudeDir}
	if _, err := QuarantineChosen(claudeDir, it); err == nil {
		t.Error("prune took an item outside its root")
	}
	if err := DeleteItem(it); err == nil {
		t.Error("delete took an item outside its root")
	}
	// A link living outside the root.
	link := filepath.Join(home, "elsewhere", "l")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip(err)
	}
	via := scan.Item{Category: scan.CatSkill, Name: "l", Path: filepath.Join(link, "SKILL.md"), RootDir: claudeDir, ViaLink: outside}
	if _, err := QuarantineChosen(claudeDir, via); err == nil {
		t.Error("prune moved a link outside its root")
	}
	if err := DeleteItem(via); err == nil {
		t.Error("delete removed a link outside its root")
	}
	if _, err := os.Stat(md); err != nil {
		t.Fatalf("target touched: %v", err)
	}
}

// TestQuarantineItemStillRefusesWhatIsNotRemovable: the door of a bulk run.
func TestQuarantineItemStillRefusesWhatIsNotRemovable(t *testing.T) {
	claudeDir := filepath.Join(t.TempDir(), ".claude")
	dir := filepath.Join(claudeDir, "plugins", "cache", "m", "p", "1", "skills", "x")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(md, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := QuarantineItem(claudeDir, scan.Item{Category: scan.CatSkill, Name: "p:x", Path: md, RootDir: claudeDir, Source: "plugin:p@m"}); err == nil {
		t.Fatal("expected refusal: not removable")
	}
	if _, err := os.Stat(md); err != nil {
		t.Fatal(err)
	}
}
