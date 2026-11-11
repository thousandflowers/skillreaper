package menu

// The card: one row on a page of its own, and the page between two lists. Its
// keys differ from the prototype's on purpose in three places: n and the
// "nothing" option clear a pending choice, and u undoes a mute, a lock or a prune.

import (
	"strings"
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/term"
)

const (
	optionsRow = "nothing     lock     mute     prune     ⚠ delete"
	oneChange  = "apply 1 change"
)

// The keys that open a card from the list as it starts.
var (
	srvCard   = []term.Key{right, down, enter}                   // first row of "suggest to prune"
	lintCard  = []term.Key{down, right, down, enter}             // first row of "suggest to mute", in a plugin
	dailyCard = []term.Key{down, down, right, down, enter}       // the row reap keeps
	quietCard = []term.Key{down, down, down, right, down, enter} // the muted row
	goneCard  = []term.Key{down, down, down, down, right, down, enter}
)

// on is the keys that open a card, then more keys.
func on(card []term.Key, more ...term.Key) []term.Key {
	return append(append([]term.Key(nil), card...), more...)
}

func TestCardShowsOneRowOnAPage(t *testing.T) {
	t.Run("what it is, what it weighs, what reap says and why", func(t *testing.T) {
		x := play(t, srvCard...)

		mustShow(t, x.final(), "1 of 2", "srv · mcp · user-config · claude-code", optionsRow, "leave it as it is",
			"~500 tokens every session", "reap suggests prune", "Never fired, with enough sessions to judge.",
			"0 uses in 7 days · last used never", "0 errors", "/p/srv")
	})

	t.Run("a plugin row is named without its plugin, and offers plugin off", func(t *testing.T) {
		x := play(t, lintCard...)

		mustShow(t, x.final(), "lint · skill · plugin:acme · claude-code", "reap suggests mute", "o plugin off")
	})

	t.Run("a row outside a plugin has no plugin off to offer", func(t *testing.T) {
		x := play(t, srvCard...)

		mustNotShow(t, x.final(), "o plugin off")
	})

	t.Run("a row reap keeps says so", func(t *testing.T) {
		x := play(t, dailyCard...)

		mustShow(t, x.final(), "reap suggests lock", "Used in this window.", "5 uses in 7 days")
	})

	t.Run("a muted row stands on mute", func(t *testing.T) {
		x := play(t, quietCard...)

		mustShow(t, x.final(), "already muted", "keeps it, strips its description, reap unmute brings it back", "You muted it, so its uses prove nothing.")
	})

	t.Run("a pruned row says only u applies", func(t *testing.T) {
		x := play(t, goneCard...)

		mustShow(t, x.final(), "gone · skill · quarantine · claude-code", "prune is not available: it is pruned: u brings it back", "In quarantine: u brings it back.")
	})

	t.Run("the mouse is handed back while the card is up", func(t *testing.T) {
		x := play(t, on(srvCard, esc)...)

		mustEqual(t, "mouse on under the card", x.s.mouseOn[x.frameAt("1 of 2")], false)
		mustEqual(t, "mouse on back in the list", x.s.MouseOn, true)
	})

	t.Run("a click or a resize changes nothing on it", func(t *testing.T) {
		x := open(t, flowData()).press(srvCard...).click(10, 5).press(term.KeyResize).run()

		mustShow(t, x.final(), "1 of 2", "leave it as it is")
		mustNotShow(t, x.final(), "apply")
	})
}

func TestCardSaysWhenItWasLastUsed(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	ago := func(days int) time.Time { return now.AddDate(0, 0, -days) }
	cases := []struct {
		name string
		row  report.Row
		want string
	}{
		{"never", raw("x"), "last used never"},
		{"today", raw("x", uses(1), func(r *report.Row) { r.LastUsed = now }), "1 use in 7 days · last used today"},
		{"days ago", raw("x", uses(2), func(r *report.Row) { r.LastUsed = ago(3) }), "2 uses in 7 days · last used 3d ago"},
		{"a clock in the future is today", raw("x", func(r *report.Row) { r.LastUsed = ago(-2) }), "last used today"},
		{"before this window, when the durable record knows", raw("x", func(r *report.Row) { r.HistoricalLast = ago(40) }), "last used 40d ago, before this window"},
		{"on a longer cadence, with no date known", raw("x", reason("rare")), "last used not in this window"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x := open(t, Data{Report: &report.Report{Sessions: 9, WindowDays: 7, Rows: []report.Row{c.row}}})
			x.m.now = func() time.Time { return now }

			x.press(right, down, enter).run()

			mustShow(t, x.final(), c.want)
		})
	}

	t.Run("with no window length known it says this window", func(t *testing.T) {
		x := open(t, Data{Report: &report.Report{Sessions: 9, Rows: []report.Row{raw("x")}}}).press(right, down, enter).run()

		mustShow(t, x.final(), "0 uses in this window")
	})
}

func TestCardSaysWhatItCannotMeasureOrDraw(t *testing.T) {
	card := func(t *testing.T, d Data) []string {
		return open(t, d).press(right, down, enter).run().final()
	}

	t.Run("a weight never measured is not a weight of nothing", func(t *testing.T) {
		unknown := raw("hooked", tokens(0), func(r *report.Row) { r.WeightUnknown = true })

		got := card(t, Data{Report: &report.Report{Sessions: 9, Rows: []report.Row{unknown}}})

		mustShow(t, got, "weight unknown")
		mustNotShow(t, got, "tokens every session")
	})

	t.Run("with a price, the weight is worth money a month", func(t *testing.T) {
		rep := &report.Report{Sessions: 9, SessionsPerMonth: 40, DeadTokensPerSession: 100, MoneyPerMonth: 2, Rows: []report.Row{raw("x")}}

		got := card(t, Data{Report: rep})

		mustShow(t, got, "~100 tokens every session", "at least US$2.00 a month")
	})

	t.Run("on a flat plan the money is a value, not a bill", func(t *testing.T) {
		rep := &report.Report{Sessions: 9, SessionsPerMonth: 40, DeadTokensPerSession: 100, MoneyPerMonth: 2, Rows: []report.Row{raw("x")}}

		got := card(t, Data{Report: rep, Billing: "stripe_subscription"})

		mustShow(t, got, "worth US$2.00 at API prices a month")
	})

	t.Run("a name the big font has no letters for is written plainly", func(t *testing.T) {
		got := card(t, Data{Report: &report.Report{Sessions: 9, Rows: []report.Row{raw("漢字")}}})

		mustEqual(t, "title", strings.TrimSpace(got[2]), "漢字")
	})

	t.Run("a long description is wrapped under the figures", func(t *testing.T) {
		long := raw("x", func(r *report.Row) { r.Description = strings.Repeat("word ", 60) })

		got := card(t, Data{Report: &report.Report{Sessions: 9, Rows: []report.Row{long}}})

		mustEqual(t, "first line", strings.TrimSpace(got[17]), strings.TrimSpace(strings.Repeat("word ", 18)))
		mustEqual(t, "second line starts", strings.HasPrefix(strings.TrimSpace(got[18]), "word word"), true)
	})

	t.Run("no description says so", func(t *testing.T) {
		got := card(t, Data{Report: &report.Report{Sessions: 9, Rows: []report.Row{raw("x", noDescription)}}})

		mustEqual(t, "description", strings.TrimSpace(got[17]), "no description")
	})

	t.Run("errors are counted", func(t *testing.T) {
		broken := raw("x", reason("broken"), func(r *report.Row) { r.ErrorCount = 4 })

		got := card(t, Data{Report: &report.Report{Sessions: 9, Rows: []report.Row{broken}}})

		mustShow(t, got, "4 errors", "Invoked, and it only ever errored.")
	})
}

func TestCardKeysChooseForItsRow(t *testing.T) {
	chosen := []struct {
		name    string
		keys    []term.Key
		explain string
	}{
		{"p prunes", on(srvCard, 'p'), "moved to quarantine, reap restore brings it back"},
		{"l locks", on(srvCard, 'l'), "locked: reap never suggests touching it"},
		{"m mutes, and on a plugin row says an update can undo it", on(lintCard, 'm'), "keeps it, strips its description, reap unmute brings it back · a plugin update"},
		{"p on a plugin row says an update can undo it", on(lintCard, 'p'), "moved to quarantine, reap restore brings it back · a plugin update can bring it back"},
		{"space takes what reap can do to it at once", on(srvCard, space), "moved to quarantine, reap restore brings it back"},
		{"a takes reap's own choice", on(lintCard, 'a'), "keeps it, strips its description"},
		{"u on a muted row undoes the mute", on(quietCard, 'u'), "brings its description back"},
		{"u on a pruned row brings it back", on(goneCard, 'u'), "brings it back from quarantine"},
	}
	for _, c := range chosen {
		t.Run(c.name, func(t *testing.T) {
			x := play(t, c.keys...)

			mustShow(t, x.final(), c.explain, oneChange)
			mustApply(t, x)
		})
	}

	cleared := []struct {
		name string
		keys []term.Key
	}{
		{"n clears a pending choice", on(srvCard, 'p', 'n')},
		{"space again clears what space chose", on(srvCard, space, space)},
		{"n clears a pending undo", on(quietCard, 'u', 'n')},
		{"u on a row with nothing to undo chooses nothing", on(srvCard, 'u')},
		{"a again gives back what stood before", on(lintCard, 'a', 'a')},
	}
	for _, c := range cleared {
		t.Run(c.name, func(t *testing.T) {
			x := play(t, c.keys...)

			mustNotShow(t, x.final(), "apply")
		})
	}

	t.Run("d marks a delete: it rings and says it cannot be restored", func(t *testing.T) {
		x := play(t, on(srvCard, 'd')...)

		mustShow(t, x.final(), "⚠ gone for good, it cannot be restored", oneChange)
		mustEqual(t, "bells", x.s.Beeps, 1)
		mustEqual(t, "title struck through", strings.Contains(x.final()[2], "━━━"), true)
	})

	t.Run("a key the row cannot take stands on the grey option, which says why", func(t *testing.T) {
		x := play(t, on(srvCard, 'm')...)

		mustShow(t, x.final(), "mute is not available: an MCP server has nothing to mute, p prunes it")
		mustNotShow(t, x.final(), "apply")
	})

	t.Run("a says what reap chose", func(t *testing.T) {
		x := play(t, on(srvCard, 'a')...)

		mustContain(t, statusLine(x.final()), "reap chose: 1 prune")
	})

	t.Run("a key that is not a Latin letter says which are read", func(t *testing.T) {
		x := play(t, on(srvCard, term.KeyNonLatin)...)

		mustContain(t, statusLine(x.final()), latinHint)
	})
}

func TestCardExplainsWhatAChoiceMeans(t *testing.T) {
	inPlugin, mine := row("ps", source(fooPlugin), notRemovable), row("mine")
	cases := []struct {
		name string
		row  *Row
		verb string
		want string
	}{
		{"a mute", mine, vMute, "keeps it, strips its description, reap unmute brings it back"},
		{"a mute in a plugin can be undone by an update", inPlugin, vMute, "keeps it, strips its description, reap unmute brings it back · a plugin update can bring the description back"},
		{"a prune", mine, vPrune, "moved to quarantine, reap restore brings it back"},
		{"a prune in a plugin can be undone by an update", inPlugin, vPrune, "moved to quarantine, reap restore brings it back · a plugin update can bring it back"},
		{"a delete in a plugin too", inPlugin, vDelete, "⚠ gone for good, it cannot be restored · a plugin update can bring it back"},
		{"plugin off", inPlugin, vDisable, "goes with its whole plugin: claude plugin disable"},
		{"nothing", mine, choiceNothing, "leave it as it is"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mustEqual(t, "explanation", cardExplain(c.row, c.verb), c.want)
		})
	}
}

func TestCardArrowsMoveAlongTheOptions(t *testing.T) {
	t.Run("right of nothing is lock", func(t *testing.T) {
		x := play(t, on(srvCard, right)...)

		mustShow(t, x.final(), "locked: reap never suggests touching it", oneChange)
	})

	t.Run("back on nothing, the pending choice is cleared", func(t *testing.T) {
		x := play(t, on(srvCard, right, left)...)

		mustShow(t, x.final(), "leave it as it is")
		mustNotShow(t, x.final(), "apply")
	})

	t.Run("no wrap: left of nothing is not delete", func(t *testing.T) {
		x := play(t, on(srvCard, left)...)

		mustShow(t, x.final(), "leave it as it is")
		mustNotShow(t, x.final(), "apply")
		mustEqual(t, "bells", x.s.Beeps, 0)
	})

	t.Run("a grey option can be stood on and read, and changes nothing", func(t *testing.T) {
		x := play(t, on(srvCard, right, right)...) // lock, then mute, which a server cannot take

		mustShow(t, x.final(), "mute is not available: an MCP server has nothing to mute, p prunes it", oneChange)
		mustEqual(t, "choices", len(x.applied()), 0)
	})

	t.Run("the last option is delete, and landing on it rings", func(t *testing.T) {
		x := play(t, on(srvCard, right, right, right, right, right)...)

		mustShow(t, x.final(), "⚠ gone for good, it cannot be restored")
		mustEqual(t, "bells", x.s.Beeps, 1)
	})

	t.Run("on a muted row, nothing is the undo of the mute", func(t *testing.T) {
		x := play(t, on(quietCard, left, left)...) // mute, lock, nothing

		mustShow(t, x.final(), "brings its description back", oneChange)
	})

	t.Run("on a muted row, back on mute nothing is chosen", func(t *testing.T) {
		x := play(t, on(quietCard, left, right)...)

		mustNotShow(t, x.final(), "apply")
	})
}

func TestCardLeavesToTheListOnItsRow(t *testing.T) {
	for name, key := range map[string]term.Key{"esc": esc, "enter": enter} {
		t.Run(name+" goes back, the cursor on the row and its choice in the box", func(t *testing.T) {
			x := play(t, on(srvCard, 'p', key)...)

			mustShow(t, x.final(), "NAME · by suggestion")
			mustContain(t, lineWith(x.final(), "srv"), "> ")
			mustContain(t, lineWith(x.final(), "srv"), "[p]")
			mustEqual(t, "name struck through", isStruck(x.final(), "srv"), true)
		})
	}

	t.Run("an undo chosen on the card reads undo on the row", func(t *testing.T) {
		x := play(t, on(quietCard, 'u', esc)...)

		mustContain(t, lineWith(x.final(), "quiet"), "[u]")
		mustContain(t, lineWith(x.final(), "quiet"), "undo")
		mustContain(t, statusLine(x.final()), "undo 1")
	})

	t.Run("down is the next row of the list, and esc lands on that one", func(t *testing.T) {
		x := play(t, on(srvCard, down, esc)...)

		mustEqual(t, "showed the next row", x.saw("2 of 2", "old-notes · skill · personal · claude-code"), true)
		mustContain(t, lineWith(x.final(), "old-notes"), "> ")
	})

	t.Run("up comes back", func(t *testing.T) {
		x := play(t, on(srvCard, down, up)...)

		mustShow(t, x.final(), "1 of 2", "srv · mcp")
	})

	t.Run("q with nothing chosen leaves the menu", func(t *testing.T) {
		x := play(t, on(srvCard, 'q', 'p')...)

		mustShow(t, x.final(), "1 of 2") // the card is the last thing shown
		mustNotShow(t, x.final(), "apply")
	})

	t.Run("q with a choice asks first, and n stays on the card", func(t *testing.T) {
		x := play(t, on(srvCard, 'p', 'q', 'n')...)

		mustEqual(t, "asked", x.saw("Quit and lose 1 choice? [y/n]"), true)
		mustShow(t, x.final(), "1 of 2", oneChange)
	})

	t.Run("q with a choice and a y leaves, applying nothing", func(t *testing.T) {
		x := play(t, on(srvCard, 'p', 'q', 'y', 'g')...)

		mustNotShow(t, x.final(), "NAME · by plugin")
		mustApply(t, x)
	})
}

func TestCardAppliesThroughTheSameReceipt(t *testing.T) {
	t.Run("shift+enter with a choice shows the receipt, and n comes back to the card", func(t *testing.T) {
		x := play(t, on(srvCard, 'p', applyKey, 'n')...)

		mustEqual(t, "receipt", x.saw(doIt, "prune    srv"), true)
		mustShow(t, x.final(), "1 of 2", cancelled)
		mustApply(t, x)
	})

	t.Run("y applies, and the card says what was done", func(t *testing.T) {
		x := play(t, on(srvCard, 'p', applyKey, 'y')...)

		mustApply(t, x, []string{"prune srv"})
		mustContain(t, statusLine(x.final()), "✓ 1 gone")
	})

	t.Run("the row that went takes no more choices from its card", func(t *testing.T) {
		x := play(t, on(srvCard, 'p', applyKey, 'y', 'l', 'p', space, 'a', applyKey, 'y')...)

		mustApply(t, x, []string{"prune srv"})
		mustNotShow(t, x.final(), "apply")
	})

	t.Run("back in the list the row is gone", func(t *testing.T) {
		x := play(t, on(srvCard, 'p', applyKey, 'y', esc)...)

		mustShow(t, x.final(), "old-notes")
		mustNotShow(t, x.final(), "srv")
	})

	t.Run("with nothing chosen shift+enter only goes back", func(t *testing.T) {
		x := play(t, on(srvCard, applyKey)...)

		mustEqual(t, "receipt", x.saw(doIt), false)
		mustShow(t, x.final(), "NAME · by suggestion")
	})
}

func TestCardPluginOff(t *testing.T) {
	t.Run("o marks the whole plugin and the card shows it as a sixth option", func(t *testing.T) {
		x := play(t, on(lintCard, 'o')...)

		mustShow(t, x.final(), "plugin off", "goes with its whole plugin: claude plugin disable", "apply 2 changes")
		mustContain(t, statusLine(x.final()), "acme@market: whole plugin will be disabled, 2 rows")
	})

	t.Run("another choice on the row ends it, and the card says so", func(t *testing.T) {
		x := play(t, on(lintCard, 'o', 'm')...)

		mustContain(t, statusLine(x.final()), "plugin off cancelled: it takes the whole plugin")
		mustShow(t, x.final(), oneChange)
	})

	t.Run("o on a row outside a plugin says why not", func(t *testing.T) {
		x := play(t, on(srvCard, 'o')...)

		mustContain(t, statusLine(x.final()), "o cannot act here: it is not in a plugin")
	})
}

func TestBetweenTwoLists(t *testing.T) {
	pastLast := on(srvCard, down, down) // srv, old-notes, then past the end of "suggest to prune"

	t.Run("past the last row: what was done here, and what comes next", func(t *testing.T) {
		x := play(t, pastLast...)

		mustShow(t, x.frame("suggest to prune · done"), "nothing chosen out of 2", "next", "2 items · 2 left to choose · ~350 tok", "↓ or enter continue · esc stay here")
	})

	t.Run("it counts what was chosen in the list just left", func(t *testing.T) {
		x := play(t, on(srvCard, 'p', down, down)...)

		mustShow(t, x.frame("suggest to prune · done"), "1 of 2 chosen · ~500 tok", oneChange)
	})

	for name, key := range map[string]term.Key{"down": down, "enter": enter} {
		t.Run(name+" goes on to the first row of the next list", func(t *testing.T) {
			x := play(t, append(pastLast, key)...)

			mustShow(t, x.final(), "1 of 2", "lint · skill · plugin:acme · claude-code")
		})
	}

	t.Run("esc stays on the row it came from", func(t *testing.T) {
		x := play(t, append(pastLast, esc)...)

		mustShow(t, x.final(), "2 of 2", "old-notes · skill")
	})

	t.Run("leaving the card after crossing lands in the list it crossed into", func(t *testing.T) {
		x := play(t, append(pastLast, down, esc)...)

		mustContain(t, lineWith(x.final(), "acme:lint"), "> ")
	})

	t.Run("before the first row of the first list there is nothing", func(t *testing.T) {
		x := play(t, on(srvCard, up)...)

		mustShow(t, x.frame("This is the first list."), "suggest to prune", "nothing chosen out of 2", "esc back, then shift+enter to apply")
	})

	t.Run("before the first row of a later list is the list before it", func(t *testing.T) {
		x := play(t, on(lintCard, up)...)

		mustShow(t, x.frame("previous"), "suggest to mute", "2 items · 2 left to choose · ~600 tok", "↑ or enter go back to it · esc stay here")
	})

	t.Run("up from there is the last row of that list", func(t *testing.T) {
		x := play(t, on(lintCard, up, up)...)

		mustShow(t, x.final(), "2 of 2", "old-notes · skill")
	})

	t.Run("past the last row of the last list there is nothing", func(t *testing.T) {
		x := play(t, on(goneCard, down)...)

		mustShow(t, x.frame("That was the last list."), "already pruned · done", "nothing chosen out of 1")
	})

	t.Run("shift+enter applies from here too, through the receipt", func(t *testing.T) {
		x := play(t, on(srvCard, 'p', down, down, applyKey, 'y')...)

		mustApply(t, x, []string{"prune srv"})
	})

	t.Run("on a short window the next list's name is plain text", func(t *testing.T) {
		x := openSized(t, bannerRows-1, testCols, flowData()).press(pastLast...).run()

		mustShow(t, x.frame("suggest to prune · done"), "suggest to mute", "2 items · 2 left to choose")
	})
}

func TestCardAndSectionOnAWindowTooSmall(t *testing.T) {
	t.Run("the card says so, and esc goes back", func(t *testing.T) {
		x := open(t, flowData())
		x.s.resizeAt = map[int][2]int{4: {cardRows - 1, testCols}, 7: {testRows, testCols}}

		x.press(on(srvCard, term.KeyResize, 'd', esc, term.KeyResize)...).run()

		mustEqual(t, "said so", x.saw("window too small: needs 58x11", "esc goes back"), true)
		mustShow(t, x.final(), "NAME · by suggestion")
		mustNotShow(t, x.final(), "apply") // the d pressed blind chose nothing
	})

	t.Run("the page between two lists says so too", func(t *testing.T) {
		x := open(t, flowData())
		x.s.resizeAt = map[int][2]int{6: {sectionRows - 1, testCols}}

		x.press(on(srvCard, down, down, term.KeyResize, down, esc)...).run()

		mustEqual(t, "said so", x.saw("window too small: needs 30x13", "esc goes back"), true)
	})
}

func TestCardOfARowThatWentTakesNothingMore(t *testing.T) {
	t.Run("the options row cannot queue the same undo again", func(t *testing.T) {
		// Unmute applied from the card; then walk the options to lock and back to nothing.
		x := play(t, on(quietCard, 'u', applyKey, 'y', right, left, applyKey, 'y')...)

		mustApply(t, x, []string{"unmute quiet"})
		mustNotShow(t, x.final(), "apply")
	})

	t.Run("nor a restore", func(t *testing.T) {
		x := play(t, on(goneCard, 'u', applyKey, 'y', right, left, left, left, 'u', applyKey, 'y')...)

		mustApply(t, x, []string{"restore gone"})
	})

	t.Run("nor a delete, which does not even ring", func(t *testing.T) {
		x := play(t, on(srvCard, 'p', applyKey, 'y', right, right, right, right, right, applyKey, 'y')...)

		mustApply(t, x, []string{"prune srv"})
		mustEqual(t, "bells", x.s.Beeps, 0)
	})
}
