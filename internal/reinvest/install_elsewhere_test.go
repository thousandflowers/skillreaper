package reinvest

import (
	"strings"
	"testing"
)

// `skills add` that worked but left nothing Claude Code loads put something on
// disk nobody compared with what was verified: it is taken out again.
func TestInstall_what_is_not_for_Claude_Code_is_taken_out_again(t *testing.T) {
	a := fakeApp(t)
	calls := stepsRecorded(a, nil)
	a.isInstalled = func(candidate) bool { return false }
	c := candidate{Kind: kindSkill, Name: "release", Source: "acme/tools", Slug: "release"}

	lines, stop := a.install(swap{Cands: []candidate{c}})

	if !stop || !containsCmd(*calls, uninstallCommand(c)...) {
		t.Errorf("not taken out: stop %v, ran %v", stop, *calls)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "not for Claude Code · removed again") {
		t.Errorf("said %q", lines)
	}
}
