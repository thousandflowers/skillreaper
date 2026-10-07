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
