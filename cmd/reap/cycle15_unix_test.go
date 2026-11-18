//go:build darwin || linux

package main

import (
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/prune"
)

// A Ctrl-C while mute waits behind another run stops it at once, with Ctrl-C's
// code: it used to sit out the whole wait and then say the folder was busy.
func TestMuteWaitingForTheLockStopsOnASignal(t *testing.T) {
	home := deleteHome(t)
	held, err := prune.AcquireLock(filepath.Join(home, ".claude"))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	seen := make(chan os.Signal, 1) // so the signal is never the test's own end
	signal.Notify(seen, syscall.SIGINT)
	defer signal.Stop(seen)
	t.Cleanup(func() { stopSignal.Store(0) })
	go func() {
		time.Sleep(500 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	}()
	start := time.Now()

	code, out := reapRun("mute", "--yes", "mine")

	if code != exitInterrupted || time.Since(start) > 5*time.Second {
		t.Errorf("mute behind a held lock, interrupted: %d after %s\n%s", code, time.Since(start).Round(time.Millisecond), out)
	}
}
