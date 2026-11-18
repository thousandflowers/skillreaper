package prune

import (
	"errors"
	"os"
	"time"
)

// ErrLocked reports that another reap run holds the lock on this directory.
var ErrLocked = errors.New("another prune is already running")

// staleLockAfter is how long a lock file may sit untouched before it is
// treated as abandoned, where there is no flock (see lock_other.go). A prune of ~1000 items measured under eight seconds,
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
type Lock struct {
	path string
	f    *os.File // held open while the lock is: the system lock lives on it
}

const lockName = ".reap-prune.lock"

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
