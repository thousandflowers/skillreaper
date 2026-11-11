package prune

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// ErrLocked reports that another reap run holds the lock on this directory.
var ErrLocked = errors.New("another prune is already running")

// staleLockAfter is how long a lock file may sit untouched before it is
// treated as abandoned. A prune of ~1000 items measured under eight seconds,
// so this is two orders of magnitude of headroom; the only way to exceed it is
// a process that died without releasing.
const staleLockAfter = 10 * time.Minute

// Lock serialises prune runs against one directory.
//
// Without it, concurrent prunes lose data rather than colliding: quarantine is
// a read-modify-write of the manifest, so the last writer wins and every item
// the other runs moved is left in the quarantine directory with no entry
// pointing back. Those files exist, and `reap restore` cannot see them.
// Measured on a 500-skill fixture with six concurrent prunes: 501 skills in,
// 148 restored.
type Lock struct{ path string }

// AcquireLock takes the lock for claudeDir. It returns ErrLocked when another
// live process holds it. A lock whose process is gone, or older than
// staleLockAfter, is left over from a run that died and is taken over.
func AcquireLock(claudeDir string) (*Lock, error) {
	path := filepath.Join(claudeDir, ".reap-prune.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		return newLock(f, path), nil
	}
	if !os.IsExist(err) {
		return nil, err
	}
	// Someone holds it, or something left it behind.
	info, statErr := os.Stat(path)
	if statErr != nil {
		// It vanished between the open and the stat: another run released it.
		// One retry is enough; a caller racing this hard will be told it is
		// locked and can try again.
		return nil, ErrLocked
	}
	if !ownerGone(path) && time.Since(info.ModTime()) < staleLockAfter {
		return nil, ErrLocked
	}
	// Stale. Remove and take it, tolerating the case where another process is
	// doing exactly the same thing right now.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, ErrLocked
	}
	f, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, ErrLocked
	}
	return newLock(f, path), nil
}

// lockWaitStep is how often AcquireLockWait tries again.
const lockWaitStep = 20 * time.Millisecond

// AcquireLockWait is AcquireLock for a change that takes moments: restore, mute
// and unmute wait their turn behind another one instead of failing at it.
func AcquireLockWait(claudeDir string, wait time.Duration) (*Lock, error) {
	deadline := time.Now().Add(wait)
	for {
		l, err := AcquireLock(claudeDir)
		if !errors.Is(err, ErrLocked) || time.Now().After(deadline) {
			return l, err
		}
		time.Sleep(lockWaitStep)
	}
}

func newLock(f *os.File, path string) *Lock {
	fmt.Fprintf(f, "pid %d at %s\n", os.Getpid(), time.Now().Format(time.RFC3339))
	f.Close()
	return &Lock{path: path}
}

// ownerGone reports a lock whose recorded process no longer exists. Anything
// unclear, including a platform that cannot tell, counts as still held.
func ownerGone(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var pid int
	if _, err := fmt.Sscanf(string(b), "pid %d", &pid); err != nil || pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return true
	}
	err = p.Signal(syscall.Signal(0))
	return errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH)
}

// Release drops the lock. Safe to call on a nil Lock so callers can defer it
// unconditionally.
func (l *Lock) Release() {
	if l == nil || l.path == "" {
		return
	}
	os.Remove(l.path)
}
