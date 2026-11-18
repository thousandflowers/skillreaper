package reinvest

import (
	"errors"
	"testing"
)

// A swap mutes and, when a later mute fails, unmutes by the file's path: by
// name, `reap unmute` would also undo a copy of that name the person had muted
// in another tool, and `reap mute` refuses a name two tools share.
func TestApply_mutes_and_rolls_back_by_path(t *testing.T) {
	tempHome(t)
	a := fakeApp(t)
	calls := stepsRecorded(a, func(cmd []string) error {
		if sameWords(cmd, []string{reapName, "--yes", "mute", "/s/second/SKILL.md"}) {
			return errors.New("refused")
		}
		return nil
	})
	s := writtenSwap()
	s.Mute = []skill{{Name: "first", Path: "/s/first/SKILL.md"}, {Name: "second", Path: "/s/second/SKILL.md"}}

	log := a.applyOne(s)

	if !containsCmd(*calls, reapName, "--yes", "mute", "/s/first/SKILL.md") {
		t.Errorf("the first mute did not name its file: %v", *calls)
	}
	if !containsCmd(*calls, reapName, "unmute", "/s/first/SKILL.md") || containsCmd(*calls, reapName, "unmute", "first") {
		t.Errorf("the roll back did not unmute by path: %v", *calls)
	}
	if n := len(log); n != 3 || log[1] != "  ✗  reap mute second: refused" || log[2] != "  ↺  rolled back: nothing changed" {
		t.Errorf("got %q", log)
	}
}

func TestReapState_a_skill_row_keeps_its_path(t *testing.T) {
	state, err := parseReapState(`{"Rows": [{"Category": "skill", "Name": "old", "Path": "/s/old/SKILL.md"}]}`)
	if err != nil || len(state.Installed) != 1 || state.Installed[0].Path != "/s/old/SKILL.md" {
		t.Errorf("state %+v, %v", state.Installed, err)
	}
}
