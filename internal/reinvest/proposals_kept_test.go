package reinvest

import (
	"errors"
	"strings"
	"testing"
)

// The "swaps ready" hint in reap's report stays for a swap that failed and was
// rolled back: nothing of it was applied. Only what was applied leaves the list.
func TestApply_a_swap_that_failed_is_still_proposed(t *testing.T) {
	tempHome(t)
	failed, done := writtenSwap("refused"), writtenSwap("fine")
	done.Cands[0].Name, done.Cands[0].Slug = "other", "other"
	a := fakeApp(t)
	a.stdin = strings.NewReader("y\n")
	a.pick = func([]swap) ([]swap, error) { return []swap{failed, done}, nil }
	stepsRecorded(a, func(cmd []string) error {
		if sameWords(cmd, []string{reapName, "--yes", "mute", "refused"}) {
			return errors.New("refused")
		}
		return nil
	})
	if err := a.saveProposals([]swap{failed, done}); err != nil {
		t.Fatal(err)
	}

	if err := a.chooseAndApply([]swap{failed, done}, false); err != nil {
		t.Fatal(err)
	}

	left := proposalsLeft(t, a)
	if len(left) != 1 || !jsonEqual(left[0], proposal(failed)) {
		t.Errorf("proposals left %v, want only the failed one", left)
	}
}

func proposalsLeft(t *testing.T, a *app) []any {
	t.Helper()
	data, _ := readJSONFile(a.cachePath(swapsFile)).(*obj)
	return data.list("swaps")
}
