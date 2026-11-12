package main

import (
	"strings"
	"testing"
)

// reinvest reads its own flags, so it comes first; a shared flag in front of
// it says that, instead of calling a command it lists unknown.
func TestReinvestAfterAFlagSaysToPutItFirst(t *testing.T) {
	code, out := reapRun("--no-color", "reinvest", "--dry-run")
	if code != 2 || strings.Contains(out, "unknown command") || !strings.Contains(out, "reap reinvest --dry-run") {
		t.Errorf("%d %s", code, out)
	}
}
