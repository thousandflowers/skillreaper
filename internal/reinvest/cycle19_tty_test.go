//go:build darwin || linux

package reinvest

import (
	"strings"
	"testing"
)

// With no terminal to choose on, the list of swaps ends by saying that nothing
// was changed and how to choose.
func TestCycle19_without_a_terminal_the_list_says_nothing_changed(t *testing.T) {
	a := fakeApp(t)

	if err := a.show(outcome{swaps: ttyCovSwaps()}, false); err != nil {
		t.Fatal(err)
	}

	if said := termText(a); !strings.Contains(said, "nothing was changed") || !strings.Contains(said, "in a terminal") {
		t.Errorf("the list ends:\n%s", said)
	}
}
