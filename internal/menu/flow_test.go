package menu

// The menu end to end, through a screen with no terminal behind it: keys go in,
// frames come out, and an applier that only records stands where the disk would
// be. The one rule above all the others: nothing reaches it without a y.

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/term"
)

const (
	doIt       = "Do it?  [y/n]"
	cancelled  = "cancelled: your choices are still here"
	tooSmall   = "window too small"
	twoChanges = "shift+enter apply 2 changes"
)

func TestSpaceOnAGroupTakesReapsSuggestion(t *testing.T) {
	t.Run("the group is chosen and the summary line says so", func(t *testing.T) {
		x := play(t, space)

		group := lineWith(x.final(), "suggest to prune")
		mustContain(t, group, "[p]")
		mustContain(t, group, "2 of 2 chosen")
		mustContain(t, statusLine(x.final()), "prune 2")
		mustContain(t, statusLine(x.final()), twoChanges)
	})

	t.Run("choosing carries nothing out", func(t *testing.T) {
		x := play(t, space)

		mustApply(t, x)
	})

	t.Run("the headline falls by what was chosen", func(t *testing.T) {
		x := open(t, flowData()).press(space).wait().run()

		mustShow(t, x.final(), "you saved ~600 tokens per session")
	})

	t.Run("space again gives the group back", func(t *testing.T) {
		x := play(t, space, space)

		mustContain(t, lineWith(x.final(), "suggest to prune"), "[ ]")
		mustNotShow(t, x.final(), "apply")
		mustEqual(t, "status", strings.TrimSpace(statusLine(x.final())), "")
	})

	groups := []struct {
		name    string
		keys    []term.Key
		summary string
	}{
		{"the rows reap would mute are muted", []term.Key{down, space}, "mute 2"},
		{"the rows reap would keep are locked", []term.Key{down, down, space}, "lock 1"},
		{"a muted group has nothing for reap to choose", []term.Key{down, down, down, space}, "reap has nothing to change here: u undoes it"},
		{"a pruned group neither", []term.Key{down, down, down, down, space}, "reap has nothing to change here: u undoes it"},
	}
	for _, g := range groups {
		t.Run(g.name, func(t *testing.T) {
			x := play(t, g.keys...)

			mustContain(t, statusLine(x.final()), g.summary)
		})
	}

	same := map[string]func(*session) *session{
		"a takes the same choice":       func(x *session) *session { return x.press('a') },
		"a click takes the same choice": func(x *session) *session { return x.click(30, listY) },
	}
	for name, do := range same {
		t.Run(name, func(t *testing.T) {
			x := do(open(t, flowData())).run()

			mustContain(t, lineWith(x.final(), "suggest to prune"), "[p]")
			mustContain(t, statusLine(x.final()), "prune 2")
			mustApply(t, x)
		})
	}
}

func TestQuitWithChoicesAsksFirst(t *testing.T) {
	const question = "Quit and lose 2 choices? [y/n]"

	t.Run("with nothing chosen q leaves at once", func(t *testing.T) {
		x := play(t, 'q', 'g')

		mustEqual(t, "asked", x.saw("Quit and lose"), false)
		mustShow(t, x.final(), "NAME · by suggestion") // g was never read
	})

	t.Run("with choices it asks, on the status line", func(t *testing.T) {
		x := play(t, space, 'q')

		mustContain(t, statusLine(x.frame(question)), question)
	})

	t.Run("one choice is one choice", func(t *testing.T) {
		x := play(t, right, down, space, 'q')

		mustEqual(t, "asked", x.saw("Quit and lose 1 choice? [y/n]"), true)
	})

	t.Run("n stays, with every choice still there", func(t *testing.T) {
		x := play(t, space, 'q', 'n', 'g')

		mustShow(t, x.final(), "NAME · by plugin", "apply 2 changes") // g was read by the list
	})

	t.Run("esc stays", func(t *testing.T) {
		x := play(t, space, 'q', esc, 'g')

		mustShow(t, x.final(), "NAME · by plugin", "apply 2 changes")
	})

	t.Run("y leaves, and nothing was applied", func(t *testing.T) {
		x := play(t, space, 'q', 'y', 'g')

		mustShow(t, x.final(), question, "NAME · by suggestion") // the question is the last thing shown
		mustApply(t, x)
	})

	t.Run("a y held from before the question does not answer it", func(t *testing.T) {
		x := open(t, flowData())
		x.m.now = stepClock(yesGuard - time.Millisecond)

		x.press(space, 'q', 'y').run()

		mustNotShow(t, x.final(), question) // the list was drawn again: the menu stayed
		mustShow(t, x.final(), twoChanges)
	})

	t.Run("a y that is a key repeat does not answer it", func(t *testing.T) {
		x := open(t, flowData())
		x.s.Gap = repeatGap - time.Millisecond

		x.press(space, 'q', 'y').run()

		mustNotShow(t, x.final(), question)
		mustShow(t, x.final(), twoChanges)
	})
}

func TestReceiptAsksBeforeAnythingIsApplied(t *testing.T) {
	t.Run("it lists every pick, the heaviest first, and what they add up to", func(t *testing.T) {
		x := play(t, space, applyKey)
		receipt := x.frame(doIt)

		first, second := strings.TrimSpace(receipt[3]), strings.TrimSpace(receipt[4])
		if !strings.HasPrefix(first, "prune    srv ") || !strings.HasSuffix(first, "~500 tok") {
			t.Errorf("first line = %q", first)
		}
		if !strings.HasPrefix(second, "prune    old-notes ") || !strings.HasSuffix(second, "~100 tok") {
			t.Errorf("second line = %q", second)
		}
		mustShow(t, receipt, "SKILLREAPER", "2 items · ~600 tokens lighter every session", "tokens saved every session")
	})

	t.Run("the mouse is handed back while it is up, and taken again after", func(t *testing.T) {
		x := play(t, space, applyKey, 'n')

		mustEqual(t, "mouse on under the receipt", x.s.mouseOn[x.frameAt(doIt)], false)
		mustEqual(t, "mouse on after it", x.s.MouseOn, true)
	})

	for name, key := range map[string]term.Key{"n": 'n', "esc": esc} {
		t.Run(name+" cancels: nothing reaches the applier and the choices stay", func(t *testing.T) {
			x := play(t, space, applyKey, key)

			mustApply(t, x)
			mustContain(t, statusLine(x.final()), cancelled)
			mustContain(t, statusLine(x.final()), twoChanges)
			mustContain(t, lineWith(x.final(), "suggest to prune"), "[p]")
		})
	}

	t.Run("the window closing on the question cancels", func(t *testing.T) {
		x := play(t, space, applyKey) // no key left: stdin is gone

		mustApply(t, x)
	})

	t.Run("y hands over exactly the picks it listed, in that order", func(t *testing.T) {
		x := play(t, space, applyKey, 'y')

		mustApply(t, x, []string{"prune srv", "prune old-notes"})
		mustEqual(t, "said it was working", x.saw("working…"), true)
	})

	t.Run("the applier is given the row as it was scanned, not as it is shown", func(t *testing.T) {
		rep := &report.Report{Sessions: 1, Rows: []report.Row{raw("cr\rSPOOF")}}

		x := open(t, Data{Report: rep}).press(space, applyKey, 'y').run()

		mustApply(t, x, []string{"prune cr\rSPOOF"})
		for _, frame := range x.s.frames {
			mustNotShow(t, frame, "\r")
		}
	})

	t.Run("applied rows leave the list and keep their saving", func(t *testing.T) {
		x := open(t, flowData()).press(space, applyKey, 'y').wait().run()

		mustNotShow(t, x.final(), "suggest to prune", "srv", "old-notes", "apply")
		mustShow(t, x.final(), "✓ 2 gone", "suggest to mute", "you saved ~600 tokens per session")
	})

	t.Run("what is applied cannot be applied twice", func(t *testing.T) {
		x := play(t, space, applyKey, 'y', applyKey, 'y')

		mustApply(t, x, []string{"prune srv", "prune old-notes"})
		mustContain(t, statusLine(x.final()), "nothing chosen yet")
	})

	t.Run("a second round applies only what was chosen after the first", func(t *testing.T) {
		x := play(t, space, applyKey, 'y', space, applyKey, 'y')

		mustApply(t, x, []string{"prune srv", "prune old-notes"}, []string{"mute acme:lint", "mute acme:fmt"})
	})

	t.Run("shift+enter with nothing chosen says what to do", func(t *testing.T) {
		x := play(t, applyKey, 'y')

		mustApply(t, x)
		mustContain(t, statusLine(x.final()), "nothing chosen yet: choose something, then shift+enter applies it")
	})
}

func TestApplyKeyIsOneTheTerminalCanDeliver(t *testing.T) {
	t.Run("enter under a held shift applies", func(t *testing.T) {
		x := open(t, flowData())
		x.s.SetShift(true)

		x.press(space, enter, 'y').run()

		mustApply(t, x, []string{"prune srv", "prune old-notes"})
	})

	t.Run("where shift cannot be read, x applies and the label says x", func(t *testing.T) {
		x := open(t, flowData())
		x.s.ShiftUnreadable = true

		x.press(space, 'x', 'y').run()

		mustEqual(t, "label", x.saw(" x apply 2 changes"), true)
		mustApply(t, x, []string{"prune srv", "prune old-notes"})
	})

	t.Run("where shift can be read, x is not a key", func(t *testing.T) {
		x := play(t, space, 'x', 'y')

		mustApply(t, x)
	})
}

func TestFailedPickIsNamedAndItsRowStays(t *testing.T) {
	failing := func(t *testing.T, keys ...term.Key) *session {
		x := open(t, flowData())
		x.a.fail = map[string]error{"srv": errors.New("permission denied")}
		return x.press(keys...).run()
	}

	t.Run("the status line names it, and says what was done", func(t *testing.T) {
		x := failing(t, space, applyKey, 'y')

		mustEqual(t, "status", strings.TrimSpace(statusLine(x.final())), "⚠ 1 not done: srv: permission denied · ✓ 1 gone")
	})

	t.Run("its row stays in the list, the one that went does not", func(t *testing.T) {
		x := failing(t, space, applyKey, 'y', right)

		mustShow(t, x.final(), "suggest to prune", "srv")
		mustNotShow(t, x.final(), "old-notes")
		mustContain(t, lineWith(x.final(), "suggest to prune"), "1 of 1 unused")
	})

	t.Run("only what went counts as saved", func(t *testing.T) {
		x := failing(t, space, applyKey, 'y')

		mustEqual(t, "rows applied", len(x.m.applied), 1)
		for r, verb := range x.m.applied {
			mustEqual(t, "applied", verb+" "+r.Name, "prune old-notes")
		}
	})

	t.Run("when everything fails nothing is said to be done", func(t *testing.T) {
		x := open(t, flowData())
		x.a.fail = map[string]error{"srv": errors.New("locked"), "old-notes": errors.New("locked")}

		x.press(space, applyKey, 'y').run()

		mustEqual(t, "status", strings.TrimSpace(statusLine(x.final())), "⚠ 2 not done: srv: locked")
		mustShow(t, x.final(), "2 of 2 unused")
	})
}

func TestDeleteIsSaidEveryStepOfTheWay(t *testing.T) {
	const warning = "⚠ 2 deleted for good: they cannot be restored"

	t.Run("d on a group marks it, rings, and says it cannot be restored", func(t *testing.T) {
		x := play(t, 'd')

		mustContain(t, statusLine(x.final()), "⚠ 2 marked for delete: cannot be restored")
		mustContain(t, lineWith(x.final(), "suggest to prune"), "[d]")
		mustContain(t, lineWith(x.final(), "suggest to prune"), "⚠ 2 delete")
		mustEqual(t, "bells", x.s.Beeps, 1)
	})

	t.Run("the receipt warns, rings again, and lists each as a delete", func(t *testing.T) {
		x := play(t, 'd', applyKey)
		receipt := x.frame(doIt)

		mustShow(t, receipt, warning, "⚠ delete srv", "⚠ delete old-notes")
		mustEqual(t, "bells", x.s.Beeps, 2)
	})

	t.Run("one delete is said as one", func(t *testing.T) {
		x := play(t, right, down, 'd', applyKey)

		mustShow(t, x.frame(doIt), "⚠ 1 deleted for good: it cannot be restored", "⚠ delete srv")
		mustNotShow(t, x.frame(doIt), "old-notes")
	})

	t.Run("a receipt with no delete has no such warning", func(t *testing.T) {
		x := play(t, space, applyKey)

		mustNotShow(t, x.frame(doIt), "deleted for good")
		mustEqual(t, "bells", x.s.Beeps, 0)
	})

	t.Run("n cancels a delete like anything else", func(t *testing.T) {
		x := play(t, 'd', applyKey, 'n')

		mustApply(t, x)
	})

	t.Run("y hands the deletes over", func(t *testing.T) {
		x := play(t, 'd', applyKey, 'y')

		mustApply(t, x, []string{"delete srv", "delete old-notes"})
		mustContain(t, statusLine(x.final()), "✓ 2 deleted")
	})

	t.Run("what cannot be undone is listed first, whatever it weighs", func(t *testing.T) {
		// Prune the heavy group, then delete the lightest plugin skill.
		x := play(t, space, down, right, down, down, 'd', applyKey, 'y')

		mustApply(t, x, []string{"delete acme:fmt", "prune srv", "prune old-notes"})
		mustContain(t, strings.TrimSpace(x.frame(doIt)[3]), "⚠ delete acme:fmt")
	})
}
