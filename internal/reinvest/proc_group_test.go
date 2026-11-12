//go:build darwin || linux

package reinvest

import (
	"os"
	"path/filepath"
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
