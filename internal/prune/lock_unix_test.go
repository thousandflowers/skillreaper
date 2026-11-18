//go:build unix

package prune

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestLockInAFolderThatIsNotThere(t *testing.T) {
	if l, err := AcquireLock(filepath.Join(t.TempDir(), "gone")); err == nil || errors.Is(err, ErrLocked) || l != nil {
		t.Errorf("AcquireLock = %v, %v; want the open error", l, err)
	}
}

func TestLockWhenTheSystemLockFails(t *testing.T) {
	old := flock
	t.Cleanup(func() { flock = old })
	flock = func(int, int) error { return syscall.ENOLCK }
	if _, err := AcquireLock(t.TempDir()); !errors.Is(err, syscall.ENOLCK) {
		t.Errorf("AcquireLock = %v, want the flock error, not ErrLocked", err)
	}
}

func TestLockWhoseFileKeepsBeingReplacedGivesUp(t *testing.T) {
	old := stillThePath
	t.Cleanup(func() { stillThePath = old })
	tries := 0
	stillThePath = func(*os.File, string) bool { tries++; return false }
	if _, err := AcquireLock(t.TempDir()); !errors.Is(err, ErrLocked) || tries != 3 {
		t.Errorf("AcquireLock = %v after %d tries, want ErrLocked after 3", err, tries)
	}
}

func TestSamePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lock")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(f, path) {
		t.Error("a file is not the path it was opened at")
	}
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	if samePath(f, path) {
		t.Error("a file moved away still counts as the path")
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if samePath(f, path) {
		t.Error("a new file at the path counts as the old one")
	}
	f.Close()
	if samePath(f, path) {
		t.Error("a closed file counts as the path")
	}
}
