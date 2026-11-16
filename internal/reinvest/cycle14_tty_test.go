//go:build darwin || linux

package reinvest

import (
	"testing"
	"time"
)

// A run takes minutes, and keys typed while it works are not answers to a
// picker that was not on screen yet: an impatient Enter accepted the first swap.
func TestCycle14_keys_typed_before_the_picker_are_dropped(t *testing.T) {
	pt := ttyCovOpen(t, 24, 100)
	pt.typeKeys(t, "\n\n")
	time.Sleep(100 * time.Millisecond) // the keys reach the terminal's queue
	scr, err := openScreen(pt.slave, pt.slave)
	if err != nil {
		t.Fatal(err)
	}
	defer scr.giveBack()

	if key, err := scr.readKey(); err != nil || key != keyNone {
		t.Errorf("the picker read %d (%v) from before it opened", key, err)
	}
}
