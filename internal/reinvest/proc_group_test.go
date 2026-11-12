//go:build darwin || linux

package reinvest

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A step that runs out of time ends with everything it started: npx starts
// node, node starts git, and one of them left alive would finish an install
// after reinvest had said it failed and checked nothing.
func TestProcess_a_timed_out_step_leaves_nothing_running(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "late")
	res := runProcess(procSpec{argv: []string{"/bin/sh", "-c", "(sleep 2; echo late > '" + marker + "') & wait"}, timeout: 300 * time.Millisecond})
	if !res.timedOut {
		t.Fatalf("not timed out: %+v", res)
	}
	time.Sleep(3 * time.Second)
	if _, err := os.Stat(marker); err == nil {
		t.Error("a child of the step finished after the timeout")
	}
}

// Ctrl-C while a step runs ends the step's whole group too: in its own group,
// the terminal's signal reaches reap alone, and a child left alive could
// finish an install nobody checks.
func TestProcess_a_signal_during_a_step_ends_the_step_too(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	exited := make(chan int, 1)
	old := exitOnSignal
	t.Cleanup(func() { exitOnSignal = old })
	exitOnSignal = func(code int) { exited <- code }
	go func() {
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			if b, err := os.ReadFile(pidFile); err == nil && len(b) > 0 {
				_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
				return
			}
		}
	}()
	runProcess(procSpec{argv: []string{"/bin/sh", "-c", "echo $$ > '" + pidFile + "'; sleep 30"}, timeout: time.Minute})
	select {
	case code := <-exited:
		if code != 130 {
			t.Errorf("exit code %d, want 130", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the run never ended for the signal")
	}
	b, _ := os.ReadFile(pidFile)
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if pid == 0 || syscall.Kill(pid, 0) != syscall.ESRCH {
		t.Errorf("the step (pid %d) outlived the signal", pid)
	}
}
