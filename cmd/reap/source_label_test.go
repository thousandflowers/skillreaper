package main

import (
	"strings"
	"testing"
)

// --source takes the label the report shows for a source of one name ("own").
func TestSourceTakesTheReportsLabelAndListsTheChoices(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	base := []string{"--claude-dir", claudeDir, "--claude-json", claudeJSON}
	if code, out := reapRun(append(base, "prune", "--json", "--min-sessions", "1", "--source", "own")...); code != 0 || !strings.Contains(out, "deadskill") {
		t.Errorf("prune --source own: %d %s", code, out)
	}
}
