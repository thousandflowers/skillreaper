package main

import (
	"strings"
	"testing"
)

// Naming a muted item in prune reads as a sentence, not "reap says - (muted)".
func TestNamingAMutedItemInPruneSaysItIsMuted(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	base := []string{"--claude-dir", claudeDir, "--claude-json", claudeJSON}
	if code, out := reapRun(append(base, "--yes", "mute", "deadskill")...); code != 0 {
		t.Fatalf("mute: %d %s", code, out)
	}
	_, out := reapRun(append(base, "prune", "deadskill")...)
	if strings.Contains(out, "reap says - ") || !strings.Contains(out, "reap gives it no verdict (muted)") {
		t.Errorf("named prune of a muted item:\n%s", out)
	}
}
