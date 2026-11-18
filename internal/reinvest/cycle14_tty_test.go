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

// A y typed before the question is on screen answers nothing: the skill's text
// was not shown yet when it was typed. The answer is what is typed afterwards.
func TestCycle15_an_answer_typed_before_the_question_is_dropped(t *testing.T) {
	pt := ttyCovOpen(t, 24, 100)
	a := fakeApp(t)
	a.stdin = pt.slave
	pt.typeKeys(t, "y\n")
	time.Sleep(100 * time.Millisecond) // the keys reach the terminal's queue
	go func() {
		time.Sleep(300 * time.Millisecond)
		_, _ = pt.master.WriteString("n\n")
	}()

	yes, err := a.confirmed()

	if err != nil || yes {
		t.Errorf("confirmed() = %v, %v: the y typed ahead was taken", yes, err)
	}
}
