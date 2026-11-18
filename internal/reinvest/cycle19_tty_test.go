//go:build darwin || linux

package reinvest

import (
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
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

// Ctrl-C typed in the pager is the pager's: it reached reinvest too, waited,
// and ended the picker without a word once the pager closed.
func TestCycle25_an_interrupt_during_the_pager_is_not_the_pickers(t *testing.T) {
	pt := ttyCovOpen(t, 30, 160)
	a := fakeApp(t)
	scr, err := openScreen(pt.slave, pt.slave)
	if err != nil {
		t.Fatal(err)
	}
	defer scr.giveBack()
	interrupts, resizes := make(chan os.Signal, 1), make(chan os.Signal, 1)
	a.runPager = func([]string, string) error {
		interrupts <- os.Interrupt // what the terminal sends the whole foreground group
		go func() {                // typed once the picker has the screen back: what waits is dropped
			time.Sleep(300 * time.Millisecond)
			pt.typeKeys(t, "q")
		}()
		return nil
	}
	pt.typeKeys(t, "p")
	if _, err := a.pickLoop(scr, ttyCovSwaps(), interrupts, resizes); err != nil {
		t.Errorf("p, Ctrl-C in the pager, q: %v", err)
	}
}

// Only Ctrl-C is the pager's. A request to end (SIGTERM, a hang-up) that
// came while the pager was open is still obeyed when it closes.
func TestCycle26_a_request_to_end_during_the_pager_is_obeyed(t *testing.T) {
	pt := ttyCovOpen(t, 30, 160)
	a := fakeApp(t)
	scr, err := openScreen(pt.slave, pt.slave)
	if err != nil {
		t.Fatal(err)
	}
	defer scr.giveBack()
	interrupts, resizes := make(chan os.Signal, 1), make(chan os.Signal, 1)
	a.runPager = func([]string, string) error {
		interrupts <- syscall.SIGTERM
		return nil
	}
	pt.typeKeys(t, "p")
	if _, err := a.pickLoop(scr, ttyCovSwaps(), interrupts, resizes); !errors.Is(err, errInterrupted) {
		t.Errorf("SIGTERM during the pager: %v", err)
	}
}
