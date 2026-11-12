package main

import (
	"bytes"
	"strings"
	"testing"
)

// uninstall-hook says what it did: removing a hook that is not there is not a removal.
func TestUninstallHookSaysWhenThereWasNothingToRemove(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	args := func(cmd string) []string {
		return []string{cmd, "--claude-dir", claudeDir, "--claude-json", claudeJSON}
	}
	say := func(cmd string) string {
		t.Helper()
		var out, errOut bytes.Buffer
		if code := run(args(cmd), strings.NewReader(""), &out, &errOut); code != 0 {
			t.Fatalf("%s: %d %s", cmd, code, errOut.String())
		}
		return out.String()
	}

	if got := say("uninstall-hook"); !strings.Contains(got, "no skillreaper nudge hook") {
		t.Errorf("with no settings.json: %q", got)
	}
	say("install-hook")
	if got := say("uninstall-hook"); !strings.Contains(got, "removed skillreaper nudge hook") {
		t.Errorf("after install-hook: %q", got)
	}
	if got := say("uninstall-hook"); !strings.Contains(got, "no skillreaper nudge hook") {
		t.Errorf("a second time: %q", got)
	}
}
