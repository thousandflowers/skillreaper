package menu

// What stands between a key and the disk: a window too small shows nothing to
// act on, a y counts only as a fresh key, and nothing reaches the applier
// without one.

import (
	"math/rand"
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/term"
)

func TestWindowTooSmallShowsNothingToActOn(t *testing.T) {
	sizes := []struct {
		name       string
		rows, cols int
		small      bool
	}{
		{"one row short", listRows - 1, testCols, true},
		{"one column short", testRows, listCols - 1, true},
		{"just large enough", listRows, listCols, false},
	}
	for _, c := range sizes {
		t.Run(c.name, func(t *testing.T) {
			x := openSized(t, c.rows, c.cols, flowData()).run()

			mustEqual(t, "says too small", has(x.final(), tooSmall), c.small)
			if c.small {
				mustEqual(t, "first line", x.final()[0], "window too small: needs 58x14")
				mustEqual(t, "second line", x.final()[1], "q quits")
			}
		})
	}

	t.Run("no key chooses anything while the list cannot be seen", func(t *testing.T) {
		x := openSized(t, listRows-1, testCols, flowData()).press(space, 'd', applyKey, 'y', 'a').run()

		mustApply(t, x)
		mustEqual(t, "frames that are not the notice", len(x.s.frames)-countFrames(x, tooSmall), 0)
	})

	t.Run("q leaves a window too small when nothing is chosen", func(t *testing.T) {
		x := openSized(t, listRows-1, testCols, flowData()).press('q', space, space, space).run()

		mustEqual(t, "frames shown", len(x.s.frames), 1)
	})

	t.Run("with choices, q waits for a window large enough to ask", func(t *testing.T) {
		x := open(t, flowData())
		x.s.resizeAt = map[int][2]int{2: {listRows - 1, testCols}, 5: {testRows, testCols}}

		x.press(space, term.KeyResize, 'q', 'y', term.KeyResize).run()

		mustEqual(t, "said so", x.saw(tooSmall, "q quits, once the window is large enough to ask about your choices"), true)
		mustShow(t, x.final(), twoChanges) // back on a large window, the choices are still there
		mustApply(t, x)
	})

	t.Run("a receipt that cannot be shown cannot be answered with y", func(t *testing.T) {
		x := openSized(t, receiptRows-1, testCols, flowData()).press(space, applyKey, 'y').run()

		mustEqual(t, "said so", x.saw("window too small: needs 40x19", "too small to confirm: esc cancels"), true)
		mustApply(t, x)
		mustContain(t, statusLine(x.final()), cancelled)
	})

	t.Run("a receipt shrunk under the question cannot be answered with y", func(t *testing.T) {
		x := open(t, flowData())
		x.s.resizeAt = map[int][2]int{3: {receiptRows - 1, testCols}}

		x.press(space, applyKey, term.KeyResize, 'y').run()

		mustEqual(t, "question was shown first", x.saw(doIt), true)
		mustApply(t, x)
	})

	t.Run("a receipt grown back can be answered", func(t *testing.T) {
		x := openSized(t, receiptRows-1, testCols, flowData())
		x.s.resizeAt = map[int][2]int{3: {testRows, testCols}}

		x.press(space, applyKey, term.KeyResize, 'y').run()

		mustApply(t, x, []string{"prune srv", "prune old-notes"})
	})
}

func countFrames(x *session, sub string) int {
	n := 0
	for _, f := range x.s.frames {
		if has(f, sub) {
			n++
		}
	}
	return n
}

func TestYCountsOnlyAsAFreshKey(t *testing.T) {
	cases := []struct {
		name    string
		elapsed time.Duration // between the question and the y
		gap     time.Duration // between the y and the key before it
		applies bool
	}{
		{"long after the question, after a pause", time.Second, time.Second, true},
		{"exactly at the guard", yesGuard, repeatGap, true},
		{"just inside the guard: still held from before", yesGuard - time.Millisecond, time.Second, false},
		{"typed ahead", 0, time.Second, false},
		{"a repeat or a paste", time.Second, repeatGap - time.Millisecond, false},
		{"no gap at all", time.Second, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x := open(t, flowData())
			x.m.now = stepClock(c.elapsed)
			x.s.Gap = c.gap

			x.press(space, applyKey, 'y').run()

			mustEqual(t, "applied", len(x.a.calls) == 1, c.applies)
		})
	}

	t.Run("a y that did not count leaves the question up for a real one", func(t *testing.T) {
		x := open(t, flowData())
		x.m.now = clockAt(0, 0, time.Second) // asked, a y at once, a y a second later

		x.press(space, applyKey, 'y', 'y').run()

		mustApply(t, x, []string{"prune srv", "prune old-notes"})
	})

	t.Run("input typed before the question is thrown away", func(t *testing.T) {
		x := play(t, space, applyKey, 'n')

		mustEqual(t, "flushes", x.s.Flushes, 1)
	})

	t.Run("a non-Latin key says which letters are read", func(t *testing.T) {
		x := play(t, space, applyKey, term.KeyNonLatin)

		mustEqual(t, "said so", x.saw(doIt, "y and n are read as Latin letters: switch keyboard layout"), true)
		mustApply(t, x)
	})
}

// keysWithoutY is every key the menu reads but the one that says yes.
var keysWithoutY = []term.Key{up, down, left, right, enter, esc, tab, space, applyKey, 'a', 'p', 'm', 'l', 'd', 'n', 'u', 'o', 'g', 'f', 'v', 's', 'x', 'J', 'K',
	term.KeyPgUp, term.KeyPgDn, term.KeyHome, term.KeyEnd, term.KeyShiftUp, term.KeyShiftDown, term.KeyResize, term.KeyNonLatin, term.KeyNone, term.KeyMouse}

// mash presses n keys drawn from keys, the same ones for the same seed. A mouse
// key is a click somewhere on the window.
func mash(x *session, rnd *rand.Rand, keys []term.Key, n int) {
	kinds := []string{"press", "drag", "release", "wheel_up", "wheel_down", "other"}
	for range n {
		k := keys[rnd.Intn(len(keys))]
		if k == term.KeyMouse {
			x.s.FeedMouse(term.Mouse{X: rnd.Intn(testCols), Y: rnd.Intn(testRows), Kind: kinds[rnd.Intn(len(kinds))]})
			continue
		}
		x.s.Feed(k)
	}
}

func TestNothingReachesTheApplierWithoutAY(t *testing.T) {
	const sessions, keysEach = 30, 60

	t.Run("no run of keys without a y applies anything", func(t *testing.T) {
		rnd := rand.New(rand.NewSource(1))
		for n := range sessions {
			x := open(t, flowData())
			x.s.SetShift(n%3 == 0)         // enter under shift is apply too
			x.s.ShiftUnreadable = n%5 == 0 // and so is x where shift cannot be read

			mash(x, rnd, keysWithoutY, keysEach)
			x.press('q') // and leaving does not apply either
			x.run()

			if len(x.a.calls) != 0 {
				t.Fatalf("session %d: the applier was handed %v with no y pressed", n, x.applied())
			}
		}
	})

	withY := append([]term.Key{'y', 'y', 'y'}, keysWithoutY...)

	t.Run("nor does a y that came too soon after its question", func(t *testing.T) {
		rnd := rand.New(rand.NewSource(2))
		for n := range sessions {
			x := open(t, flowData())
			x.m.now = stepClock(0)

			mash(x, rnd, withY, keysEach)
			x.run()

			if len(x.a.calls) != 0 {
				t.Fatalf("session %d: the applier was handed %v", n, x.applied())
			}
		}
	})

	t.Run("nor a y that is a key repeat", func(t *testing.T) {
		rnd := rand.New(rand.NewSource(3))
		for n := range sessions {
			x := open(t, flowData())
			x.s.Gap = 0

			mash(x, rnd, withY, keysEach)
			x.run()

			if len(x.a.calls) != 0 {
				t.Fatalf("session %d: the applier was handed %v", n, x.applied())
			}
		}
	})

	t.Run("with real answers, every apply is exactly what its receipt listed", func(t *testing.T) {
		rnd := rand.New(rand.NewSource(4))
		applies := 0
		for range sessions {
			x := open(t, flowData())

			mash(x, rnd, withY, keysEach)
			x.run()

			applies += len(x.a.calls)
			for _, call := range x.a.calls {
				seen := map[*Row]bool{}
				for _, pk := range call {
					if seen[pk.Row] || pk.Verb == "" {
						t.Fatalf("the applier was handed %v", said(call))
					}
					seen[pk.Row] = true
				}
			}
			mustEqual(t, "questions asked before the applies", countFrames(x, doIt) >= len(x.a.calls), true)
		}
		if applies == 0 {
			t.Error("no session ever applied: the check above checked nothing")
		}
	})
}

// crowdedData is three rows whose receipt carries a warning each way: a plugin
// skill and an own one to delete, and a skill in a folder other tools read.
func crowdedData() Data {
	return Data{Report: &report.Report{Sessions: 9, Rows: []report.Row{
		raw("acme:a", source(acmePlugin), notRemovable, tokens(300)),
		raw("mine-b", tokens(200)),
		raw("shared-c", notRemovable, tokens(100), func(r *report.Row) { r.Path = "/srv/.agents/skills/shared-c/SKILL.md" }),
	}}}
}

func TestReceiptMustShowItsRowsToBeAnswered(t *testing.T) {
	// Flat, by weight: delete the first two, prune the third, apply, yes.
	keys := []term.Key{'g', 'g', 'd', down, 'd', down, 'p', applyKey, 'y'}
	rowsOfIt := []string{"⚠ delete acme:a", "⚠ delete mine-b", "prune    shared-c"}
	warnings := []string{"⚠ 2 deleted for good: they cannot be restored", "⚠ also gone for other tools using /srv/.agents/skills", "⚠ 1 plugin item may return with an update"}

	t.Run("at 19 rows the warnings leave no room for its three rows: y cannot answer", func(t *testing.T) {
		x := openSized(t, receiptRows, testCols, crowdedData()).press(keys...).run()

		mustEqual(t, "said so", x.saw("window too small: needs 40x20", "too small to confirm: esc cancels"), true)
		mustEqual(t, "question shown", x.saw(doIt), false)
		mustApply(t, x)
		mustContain(t, statusText(x.final()), cancelled)
	})

	t.Run("one row taller, every row and every warning shows, and y answers", func(t *testing.T) {
		x := openSized(t, receiptRows+1, testCols, crowdedData()).press(keys...).run()

		mustShow(t, x.frame(doIt), append(rowsOfIt, warnings...)...)
		mustNotShow(t, x.frame(doIt), "… and")
		mustApply(t, x, []string{"delete acme:a", "delete mine-b", "prune shared-c"})
	})

	t.Run("at 19 rows a receipt with no warnings still fits", func(t *testing.T) {
		x := openSized(t, receiptRows, testCols, flowData()).press(space, applyKey, 'y').run()

		mustApply(t, x, []string{"prune srv", "prune old-notes"})
	})

	t.Run("more rows than fit are counted, never dropped in silence", func(t *testing.T) {
		many := &report.Report{Sessions: 9}
		for n := range 30 {
			many.Rows = append(many.Rows, raw("skill-"+itoa(100+n), tokens(1000-n)))
		}

		x := open(t, Data{Report: many}).press(space, applyKey, 'y').run()
		receipt := x.frame(doIt)

		mustShow(t, receipt, "prune    skill-100", "… and 17 more", "30 items · ~29,565 tokens lighter every session")
		mustNotShow(t, receipt, "skill-129")
		mustEqual(t, "picks handed over", len(x.a.calls[0]), 30)
	})
}
