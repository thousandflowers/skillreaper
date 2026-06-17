package scan

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadCappedRejectsOversizedFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.md")
	if err := os.WriteFile(p, make([]byte, maxFileSize+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readCapped(p); err == nil {
		t.Error("expected readCapped to reject a file over the limit")
	}
}

func TestReadCappedRejectsNonRegularFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-a-file")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := readCapped(dir); err == nil {
		t.Error("expected readCapped to reject a non-regular file")
	}
}

func TestReadCappedReadsNormalFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ok.md")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := readCapped(p)
	if err != nil || string(b) != "hello" {
		t.Errorf("readCapped(small) = %q, %v", b, err)
	}
}

func TestScanSkillsSkipsOversizedSkill(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "skills", "huge", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(md), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(md, make([]byte, maxFileSize+1), 0o644); err != nil {
		t.Fatal(err)
	}
	items, _ := ScanSkills(dir, "claude-code")
	for _, it := range items {
		if it.Name == "huge" {
			t.Error("oversized skill should be skipped, not loaded")
		}
	}
}
