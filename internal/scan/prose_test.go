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

func TestScanProseSkipsSymlinkedRulesRootOutsideTree(t *testing.T) {
	dir := t.TempDir()
	outsideRules := filepath.Join(t.TempDir(), "rules")
	if err := os.MkdirAll(outsideRules, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outsideRules, "external.md"), []byte("external"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideRules, filepath.Join(dir, "rules")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	items, warns := ScanProse(dir, "", "claude-code")
	for _, it := range items {
		if it.Source == "rules" {
			t.Errorf("symlinked rules root should be skipped, got %q", it.Path)
		}
	}
	if len(warns) != 1 {
		t.Fatalf("warnings = %d, want 1", len(warns))
	}
}

func TestScanProseFileReadsANamedFile(t *testing.T) {
	// ScanProse only knows the name CLAUDE.md, so a platform whose global
	// prose is GEMINI.md or AGENTS.md gets nothing unless the named path is
	// read directly.
	dir := t.TempDir()
	path := filepath.Join(dir, "GEMINI.md")
	body := "# prose\nloaded into every session.\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	items := ScanProseFile(path, "gemini")
	if len(items) != 1 {
		t.Fatalf("ScanProseFile returned %d items, want 1", len(items))
	}
	if items[0].Category != CatProse || items[0].Platform != "gemini" {
		t.Errorf("got category %q platform %q", items[0].Category, items[0].Platform)
	}
	if items[0].DescChars != len(body) {
		t.Errorf("DescChars = %d, want %d", items[0].DescChars, len(body))
	}
	if items[0].Removable {
		t.Error("prose must not be marked removable")
	}

	if got := ScanProseFile(filepath.Join(dir, "missing.md"), "gemini"); got != nil {
		t.Errorf("missing file returned %v, want nil", got)
	}
	if got := ScanProseFile(dir, "gemini"); got != nil {
		t.Errorf("directory returned %v, want nil", got)
	}
}
