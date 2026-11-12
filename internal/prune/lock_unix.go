//go:build unix

package prune

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// AcquireLock takes the lock for claudeDir. It returns ErrLocked when another
// live process holds it. The lock is the system's (flock) on the lock file, so
// a run that dies drops it with its descriptors: a file left behind is only a
// file, taken without any guess at staleness.
func AcquireLock(claudeDir string) (*Lock, error) {
	path := filepath.Join(claudeDir, lockName)
	for range 3 { // a holder can take the file away between our open and our lock
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return nil, err
		}
		if err := flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			f.Close()
			if errors.Is(err, syscall.EWOULDBLOCK) {
				return nil, ErrLocked
			}
			return nil, err
		}
		if stillThePath(f, path) {
			_ = f.Truncate(0)
			fmt.Fprintf(f, "pid %d at %s\n", os.Getpid(), time.Now().Format(time.RFC3339)) // for a person who finds it
			return &Lock{path: path, f: f}, nil
		}
		f.Close() // locked a file its holder had already let go: the lock is the one at path
	}
	return nil, ErrLocked
}

// flock and stillThePath are variables only so a test can make each fail.
var (
	flock        = syscall.Flock
	stillThePath = samePath
)

// samePath says whether the open file is still the one path names.
func samePath(f *os.File, path string) bool {
	held, err := f.Stat()
	if err != nil {
		return false
	}
	now, err := os.Stat(path)
	return err == nil && os.SameFile(held, now)
}

// Release drops the lock. Safe to call on a nil Lock so callers can defer it
// unconditionally. The file goes first, while still locked, so whoever opened
// it meanwhile sees it is no longer the lock and tries again.
func (l *Lock) Release() {
	if l == nil || l.path == "" {
		return
	}
	os.Remove(l.path)
	if l.f != nil {
		l.f.Close()
	}
}
