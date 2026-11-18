//go:build darwin || linux

package main

import (
	"os"
	"syscall"
	"testing"
	"time"
)

// A run stopped by SIGTERM exits with SIGTERM's code, not Ctrl-C's: whoever
// wraps it tells a supervisor's stop from a person's.
func TestARunStoppedBySIGTERMExitsWithItsCode(t *testing.T) {
	stopped, release := applyGuard()
	defer release()
	t.Cleanup(func() { stopSignal.Store(0) })
	if got := (pruneOutcome{interrupted: 1}).exitCode(); got != exitInterrupted {
		t.Errorf("with no signal recorded the code is %d", got)
	}

	_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	for deadline := time.Now().Add(2 * time.Second); !stopped() && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}

	if !stopped() {
		t.Fatal("the signal was not seen")
	}
	if got := (pruneOutcome{interrupted: 1}).exitCode(); got != 143 {
		t.Errorf("prune stopped by SIGTERM exits %d, want 143", got)
	}
	if got := (muteOutcome{interrupted: 1}).exitCode(); got != 143 {
		t.Errorf("mute stopped by SIGTERM exits %d, want 143", got)
	}
}
