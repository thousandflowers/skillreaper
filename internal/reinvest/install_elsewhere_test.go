package reinvest

import (
	"errors"
	"path/filepath"
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

// An install step that failed (out of time, say) after it had begun writing
// leaves a folder that was not there before and was never checked: it goes.
func TestInstall_a_failed_step_takes_out_what_it_left(t *testing.T) {
	home := tempHome(t)
	a := fakeApp(t)
	c := candidate{Kind: kindSkill, Name: "release", Source: "acme/tools", Slug: "release"}
	install, _ := installCommand(c)
	calls := stepsRecorded(a, func(cmd []string) error {
		if sameWords(cmd, install) {
			writeFile(t, filepath.Join(home, ".claude", "skills", "release", skillFile), "half")
			return errors.New("no answer in 120 s")
		}
		return nil
	})

	lines, stop := a.install(swap{Cands: []candidate{c}})

	if !stop || !containsCmd(*calls, uninstallCommand(c)...) || len(lines) != 1 || !strings.Contains(lines[0], "no answer in 120 s · removed again") {
		t.Errorf("stop %v, ran %v, said %q", stop, *calls, lines)
	}
	b := candidate{Kind: kindSkill, Name: "other", Source: "acme/tools", Slug: "other"}
	calls = stepsRecorded(a, func([]string) error { return errors.New("refused") })
	if lines, _ := a.install(swap{Cands: []candidate{b}}); containsCmd(*calls, uninstallCommand(b)...) || strings.Contains(lines[0], "removed") {
		t.Errorf("a step that wrote nothing was taken out: %v %q", *calls, lines)
	}
}
