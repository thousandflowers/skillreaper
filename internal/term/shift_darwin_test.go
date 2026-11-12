package term

import (
	"os"
	"testing"
	"time"
)

func TestStartKeyboardWatcher(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	old := keyboardHelper
	t.Cleanup(func() { keyboardHelper = old })

	keyboardHelper = t.TempDir() + "/missing"
	if w := startKeyboardWatcher(); w != nil {
		t.Error("a helper that cannot start gave a watcher")
	}

	// a helper that says Shift is down, then waits as the real one would
	fake := t.TempDir() + "/helper"
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho 10\nexec sleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	keyboardHelper = fake
	w := startKeyboardWatcher()
	if w == nil {
		t.Fatal("no watcher on a local macOS session")
	}
	for deadline := time.Now().Add(patience); !w.shiftHeld(time.Now()); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the helper's line never reached the watcher")
		}
	}
	if !w.readable() {
		t.Error("a watcher that just started is not readable")
	}
	w.stop()
	for deadline := time.Now().Add(patience); w.readable(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the watcher still reads as alive after its helper was killed")
		}
	}
	if w.shiftHeld(time.Now().Add(shiftGrace)) || w.sHeld() {
		t.Error("a watcher that ended still reports keys held")
	}
}

func TestTheRealKeyboardHelperStarts(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	w := startKeyboardWatcher()
	if w == nil {
		t.Fatal("osascript did not start")
	}
	defer w.stop()
	if !w.readable() {
		t.Error("the real helper is not readable right after it started")
	}
}
