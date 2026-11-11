package menu

// Plugin off: o on any row or group of a plugin is about the whole plugin, and
// the receipt says it in one line.

import (
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/term"
)

func TestPluginOffTakesTheWholePlugin(t *testing.T) {
	onRow := []term.Key{down, right, down, 'o'} // the cursor on acme:lint

	t.Run("o on one of its rows marks every row of the plugin", func(t *testing.T) {
		x := play(t, onRow...)

		mustContain(t, statusLine(x.final()), "acme@market: whole plugin will be disabled, 2 rows")
		mustContain(t, lineWith(x.final(), "lint"), "[O]")
		mustContain(t, lineWith(x.final(), "fmt"), "[O]")
		mustContain(t, statusLine(x.final()), twoChanges)
	})

	t.Run("the receipt shows one disable line for it", func(t *testing.T) {
		x := play(t, append(onRow, applyKey)...)
		receipt := x.frame(doIt)

		disables := 0
		for _, l := range receipt {
			if strings.HasPrefix(strings.TrimSpace(l), "disable") {
				disables++
			}
		}
		mustEqual(t, "disable lines", disables, 1)
		mustEqual(t, "the line", strings.TrimSpace(receipt[3]), "disable  acme@market")
		mustShow(t, receipt, "2 items · ~350 tokens lighter every session")
		mustNotShow(t, receipt, "acme:lint", "acme:fmt")
	})

	t.Run("y hands over every row of it as a disable", func(t *testing.T) {
		x := play(t, append(onRow, applyKey, 'y')...)

		mustApply(t, x, []string{"disable acme:lint", "disable acme:fmt"})
		mustContain(t, statusLine(x.final()), "✓ 2 off")
		mustNotShow(t, x.final(), "suggest to mute")
	})

	t.Run("o on the plugin's group does the same", func(t *testing.T) {
		x := play(t, 'g', down, 'o', applyKey, 'y') // by plugin: your config, acme

		mustApply(t, x, []string{"disable acme:lint", "disable acme:fmt"})
	})

	t.Run("o again leaves the plugin enabled", func(t *testing.T) {
		x := play(t, append(onRow, 'o')...)

		mustContain(t, statusLine(x.final()), "acme@market stays enabled")
		mustNotShow(t, x.final(), "[O]", "apply")
	})

	t.Run("o again gives back what the rows held before", func(t *testing.T) {
		x := play(t, down, right, down, 'm', 'o', 'o')

		mustContain(t, lineWith(x.final(), "lint"), "[m]")
		mustContain(t, lineWith(x.final(), "fmt"), "[ ]")
	})

	t.Run("another choice on one row ends it for all of them, and says so", func(t *testing.T) {
		x := play(t, append(onRow, 'm')...)

		mustContain(t, statusLine(x.final()), "plugin off cancelled: it takes the whole plugin")
		mustContain(t, lineWith(x.final(), "lint"), "[m]")
		mustContain(t, lineWith(x.final(), "fmt"), "[ ]")
		if !strings.HasSuffix(statusLine(x.final()), "apply 1 change") {
			t.Errorf("status = %q, want one change left to apply", statusLine(x.final()))
		}
	})

	t.Run("o on a row outside any plugin says why not", func(t *testing.T) {
		x := play(t, right, down, 'o')

		mustContain(t, statusLine(x.final()), "o cannot act here: it is not in a plugin")
		mustNotShow(t, x.final(), "apply")
	})

	t.Run("o over a selection is refused: it takes one plugin", func(t *testing.T) {
		x := play(t, term.KeyShiftDown, 'o')

		mustContain(t, statusLine(x.final()), "o takes one plugin: esc ends the selection first")
		mustNotShow(t, x.final(), "[O]")
	})

	t.Run("o on a group of several plugins takes each, and counts what is in none", func(t *testing.T) {
		d := flowData()
		d.Report.Rows = append(d.Report.Rows, raw("beta:x", source("plugin:beta@market"), notRemovable), raw("heavy", verdict(report.VerdictMute)))

		x := open(t, d).press(down, 'o', applyKey, 'y').run() // suggest to mute: acme twice, beta, an own skill

		mustEqual(t, "said", x.saw("2 of 2 plugins will be disabled · 1 not in a plugin"), true)
		mustApply(t, x, []string{"disable acme:lint", "disable acme:fmt", "disable beta:x"})
	})
}

func TestPluginOffAsksWhenSomethingInItIsInUse(t *testing.T) {
	const question = "Disable acme@market? [y/N] all 2 rows go, 1 you use, keep, muted or reap is unsure about: fmt"
	used := func(t *testing.T, keys ...term.Key) *session {
		d := flowData()
		d.Report.Rows[3].Uses = 3 // acme:fmt fires
		return open(t, d).press(down, right, down, 'o').press(keys...).run()
	}

	t.Run("the question names what is in use", func(t *testing.T) {
		x := used(t)

		mustContain(t, statusLine(x.frame("Disable acme@market?")), question)
	})

	t.Run("n leaves the plugin alone", func(t *testing.T) {
		x := used(t, 'n')

		mustContain(t, statusLine(x.final()), "cancelled")
		mustNotShow(t, x.final(), "[O]", "apply")
	})

	t.Run("y marks the plugin: it is still only a choice", func(t *testing.T) {
		x := used(t, 'y')

		mustContain(t, statusLine(x.final()), "acme@market: whole plugin will be disabled, 2 rows")
		mustApply(t, x)
	})

	t.Run("the receipt says one of them is in use", func(t *testing.T) {
		x := used(t, 'y', applyKey)

		mustShow(t, x.frame(doIt), "⚠ 1 of these is in use")
	})
}
