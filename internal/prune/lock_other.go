//go:build !unix

package prune

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// ponytail: without flock, a lock left by a dead run is taken over by removing
// it, and two runs that take it over at the same moment can both get in.
// LockFileEx would close that, at the price of golang.org/x/sys.

// AcquireLock takes the lock for claudeDir. It returns ErrLocked when another
// live process holds it. A lock whose process is gone, or older than
// staleLockAfter, is left over from a run that died and is taken over.
func AcquireLock(claudeDir string) (*Lock, error) {
	path := filepath.Join(claudeDir, lockName)
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
