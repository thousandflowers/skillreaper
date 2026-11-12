package main

import (
	"strings"
	"testing"
)

// With one snapshot taken, one more is what diff needs.
func TestDiffCountsTheSnapshotsStillMissing(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	base := []string{"--claude-dir", claudeDir, "--claude-json", claudeJSON}
	if code, out := reapRun(append(base, "snapshot")...); code != 0 {
		t.Fatalf("snapshot: %d %s", code, out)
	}
	code, out := reapRun(append(base, "diff")...)
	if code != 1 || !strings.Contains(out, "need 1 more snapshot to compare, found 1") {
		t.Errorf("diff with one snapshot: %d %s", code, out)
	}
}
