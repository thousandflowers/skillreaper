package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A skill of your own written today is in its grace period, not "safe to prune".
func TestASkillWrittenTodayIsInItsGracePeriod(t *testing.T) {
	// Arrange
	claudeDir, claudeJSON := buildFixture(t)
	fresh := filepath.Join(claudeDir, "skills", "brandnew", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(fresh), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fresh, []byte("---\nname: brandnew\ndescription: written just now\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	var out, errOut bytes.Buffer
	code := run([]string{"why", "--claude-dir", claudeDir, "--claude-json", claudeJSON, "brandnew"}, strings.NewReader(""), &out, &errOut)

	// Assert
	if code != 0 || !strings.Contains(out.String(), "grace") || strings.Contains(out.String(), "safe to prune") {
		t.Errorf("why brandnew (%d):\n%s%s", code, out.String(), errOut.String())
	}
}
