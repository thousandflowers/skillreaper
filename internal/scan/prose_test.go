package scan

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanProseIncludesRealRule(t *testing.T) {
	dir := t.TempDir()
	rule := filepath.Join(dir, "rules", "a.md")
	if err := os.MkdirAll(filepath.Dir(rule), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rule, []byte("rule body"), 0o644); err != nil {
		t.Fatal(err)
	}
	items, _ := ScanProse(dir, "", "claude-code")
	var found bool
	for _, it := range items {
		if it.Source == "rules" {
			found = true
		}
	}
	if !found {
		t.Error("expected the real rule file to be included")
	}
}

func TestScanProseSkipsRuleSymlinkEscapingTree(t *testing.T) {
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(t.TempDir(), "secret.md")
	if err := os.WriteFile(secret, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(rulesDir, "evil.md")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	items, _ := ScanProse(dir, "", "claude-code")
	for _, it := range items {
		if it.Source == "rules" {
			t.Errorf("symlink escaping the rules tree should be skipped, got %q", it.Path)
		}
	}
}
