package prune

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLockIsExclusive(t *testing.T) {
	dir := t.TempDir()

	first, err := AcquireLock(dir)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	if _, err := AcquireLock(dir); !errors.Is(err, ErrLocked) {
		t.Fatalf("second acquire = %v, want ErrLocked", err)
	}

	first.Release()

	second, err := AcquireLock(dir)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	second.Release()

	if _, err := os.Stat(filepath.Join(dir, ".reap-prune.lock")); !os.IsNotExist(err) {
		t.Error("lock file survived Release")
	}
}

// A process killed mid-prune leaves the file behind. Without takeover, every
// later prune on that directory would fail forever.
func TestStaleLockIsTakenOver(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".reap-prune.lock")
	if err := os.WriteFile(path, []byte("pid 1 at whenever\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * staleLockAfter)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	l, err := AcquireLock(dir)
	if err != nil {
		t.Fatalf("stale lock was not taken over: %v", err)
	}
	defer l.Release()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(info.ModTime()) > staleLockAfter {
		t.Error("the lock was reused rather than rewritten")
	}
}

// A lock younger than the staleness window belongs to a live run and must not
// be stolen, however impatient the second process is.
func TestFreshLockIsNotStolen(t *testing.T) {
	dir := t.TempDir()
	l, err := AcquireLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()

	for i := 0; i < 3; i++ {
		if _, err := AcquireLock(dir); !errors.Is(err, ErrLocked) {
			t.Fatalf("attempt %d = %v, want ErrLocked", i, err)
		}
	}
}

// Release is deferred unconditionally by the caller, including on the path
// where the lock was never taken.
func TestReleaseOnNilLockIsSafe(t *testing.T) {
	var l *Lock
	l.Release()
	(&Lock{}).Release()
}

// A lock whose process has exited is not a running prune.
func TestLockOfADeadProcessIsTakenOverAtOnce(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skipf("no true command: %v", err)
	}
	path := filepath.Join(dir, ".reap-prune.lock")
	body := fmt.Sprintf("pid %d at now\n", cmd.Process.Pid)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := AcquireLock(dir)
	if err != nil {
		t.Fatalf("lock of a dead process must be taken over: %v", err)
	}
	l.Release()
}

// AcquireLockWait waits for a holder and gives up when told to.
func TestAcquireLockWait(t *testing.T) {
	dir := t.TempDir()
	held, err := AcquireLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireLockWait(dir, 30*time.Millisecond); !errors.Is(err, ErrLocked) {
		t.Fatalf("err = %v, want ErrLocked", err)
	}
	go func() {
		time.Sleep(30 * time.Millisecond)
		held.Release()
	}()
	l, err := AcquireLockWait(dir, 5*time.Second)
	if err != nil {
		t.Fatalf("did not wait for the holder: %v", err)
	}
	l.Release()
}
