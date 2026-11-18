package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// A --claude-dir that is not there is a typo, not an empty stack: the state
// commands say so, as the report does, instead of "Nothing to restore."
func TestStateCommandsOnAMissingClaudeDirSaySo(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	for _, args := range [][]string{{"restore"}, {"unmute"}, {"keep", "--list"}, {"restore", "--all", "--yes"}} {
		code, out := reapRun(append([]string{"--claude-dir", missing}, args...)...)
		if code != 1 || !strings.Contains(out, "no Claude Code directory at "+missing) {
			t.Errorf("%v: %d %s", args, code, out)
		}
	}
}
