package menu

// The mouse on the list: a click is space, a drag paints, the wheel only looks.

import (
	"testing"

	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/term"
)

func TestMouseOnTheList(t *testing.T) {
	const firstRow, secondRow, thirdRow = listY, listY + 1, listY + 2
	drag := func(x *session, line int) *session {
		x.s.FeedMouse(term.Mouse{X: 30, Y: line, Kind: "drag"})
		return x
	}
	mouse := func(x *session, line int, kind string) *session {
		x.s.FeedMouse(term.Mouse{X: 30, Y: line, Kind: kind})
		return x
	}

	t.Run("a click on a group takes reap's choice for it, a second click gives it back", func(t *testing.T) {
		once, twice := open(t, flowData()).click(30, secondRow).run(), open(t, flowData()).click(30, secondRow).click(30, secondRow).run()

		mustContain(t, statusLine(once.final()), "mute 2")
		mustContain(t, lineWith(once.final(), "suggest to mute"), "> ")
		mustNotShow(t, twice.final(), "apply")
	})

	t.Run("a click on a group's triangle opens it and chooses nothing", func(t *testing.T) {
		x := open(t, flowData()).click(7, firstRow).run()

		mustShow(t, x.final(), "▾ suggest to prune", "old-notes")
		mustNotShow(t, x.final(), "apply")
	})

	t.Run("a click on a row takes reap's choice for that row alone", func(t *testing.T) {
		x := open(t, flowData()).press(right).click(30, secondRow).run()

		mustContain(t, lineWith(x.final(), "srv"), "[p]")
		mustContain(t, lineWith(x.final(), "old-notes"), "[ ]")
	})

	t.Run("a drag paints the same choice over every row it crosses", func(t *testing.T) {
		x := open(t, flowData()).press(right)
		mouse(x, secondRow, "press")
		drag(x, thirdRow)
		mouse(x, thirdRow, "release")

		x.run()

		mustContain(t, statusLine(x.final()), "prune 2")
	})

	t.Run("a drag that began by clearing clears", func(t *testing.T) {
		x := open(t, flowData()).press(right, 'p') // both rows of the group pruned
		mouse(x, secondRow, "press")
		drag(x, thirdRow)
		mouse(x, thirdRow, "release")

		x.run()

		mustNotShow(t, x.final(), "apply")
	})

	t.Run("a drag with no press before it paints nothing", func(t *testing.T) {
		x := drag(open(t, flowData()).press(right), thirdRow).run()

		mustNotShow(t, x.final(), "apply")
	})

	t.Run("a click below the list does nothing", func(t *testing.T) {
		x := open(t, flowData()).click(30, testRows-8).run()

		mustNotShow(t, x.final(), "apply")
	})

	t.Run("the wheel moves the view, never a choice", func(t *testing.T) {
		long := &report.Report{Sessions: 9}
		for n := range 40 {
			long.Rows = append(long.Rows, raw("skill-"+itoa(100+n), tokens(1000-n)))
		}
		x := open(t, Data{Report: long}).press('g', 'g')
		mouse(x, firstRow, "wheel_down")

		x.run()

		mustContain(t, x.final()[listY], "skill-103")
		mustContain(t, lineWith(x.final(), "skill-103"), "> ") // the cursor came along only as far as it had to
		mustNotShow(t, x.final(), "apply")
	})

	t.Run("the wheel back up stops at the top", func(t *testing.T) {
		x := mouse(mouse(open(t, flowData()), firstRow, "wheel_down"), firstRow, "wheel_up").run()

		mustContain(t, x.final()[listY], "suggest to prune")
	})
}
