package reinvest

import (
	"strings"
	"testing"
)

// A swap the person changed in the picker (mutes set aside, another
// candidate) and then applied is still the proposal it came from: it leaves
// reap's "swaps ready" list.
func TestApply_a_swap_changed_in_the_picker_leaves_the_proposals(t *testing.T) {
	tempHome(t)
	s := writtenSwap("old-a")
	s.Pick = pickKey(s.cand())
	a := fakeApp(t)
	a.stdin = strings.NewReader("y\n")
	changed := toggleMute(s)
	a.pick = func([]swap) ([]swap, error) { changed.OK = true; return []swap{changed}, nil }
	stepsRecorded(a, nil)
	if err := a.saveProposals([]swap{s}); err != nil {
		t.Fatal(err)
	}

	if err := a.chooseAndApply([]swap{s}, false); err != nil {
		t.Fatal(err)
	}

	if left := proposalsLeft(t, a); len(left) != 0 {
		t.Errorf("an applied swap is still proposed: %v", left)
	}
}
