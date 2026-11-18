package atomicfile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestWriteCreatesWithContentAndPerm(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.json")
	if err := Write(p, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "hello" {
		t.Errorf("content = %q", b)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(p)
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("perm = %v, want 0600", fi.Mode().Perm())
		}
	}
}

func TestWriteLeavesNoTempResidue(t *testing.T) {
	dir := t.TempDir()
	if err := Write(filepath.Join(dir, "f.json"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("expected only the target file, got %d entries", len(entries))
	}
}

func TestWriteFailureKeepsOriginal(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-based failure injection does not work as root")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "f.json")
	if err := os.WriteFile(p, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil { // read-only dir: CreateTemp fails
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)
	if err := Write(p, []byte("new"), 0o644); err == nil {
		t.Error("expected Write to fail in a read-only directory")
	}
	os.Chmod(dir, 0o755)
	if b, _ := os.ReadFile(p); string(b) != "original" {
		t.Errorf("original file was clobbered: %q", b)
	}
}

// The file arrives with its date: one given after the rename leaves a moment in
// which a killed run has put new content in place under today's date.
func TestWriteDatedPutsTheFileInPlaceWithItsDate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.md")
	when := time.Date(2024, 1, 1, 0, 1, 0, 0, time.UTC)
	if err := WriteDated(path, []byte("x"), 0o640, when); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(when) || info.Mode().Perm() != 0o640 {
		t.Errorf("mtime %s, mode %v", info.ModTime(), info.Mode().Perm())
	}
}
