package menu

// The list itself: how it is laid out, what each key does on a group, on a row
// and on a selection, and what the mouse does.

import (
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/scan"
	"github.com/thousandflowers/skillreaper/internal/term"
)

// pricedData is the flow fixture with a price: 950 unused tokens cost 19 a month.
func pricedData() Data {
	d := flowData()
	d.Report.MoneyPerMonth = 19
	return d
}

func TestListOpensByWhatReapSuggests(t *testing.T) {
	x := play(t)
	screen := x.final()

	t.Run("the headline is what is wasted, every session and in a month", func(t *testing.T) {
		mustShow(t, screen, "tokens wasted every session", "~203.3k in a month of sessions", "NAME · by suggestion", "TOKENS")
	})

	t.Run("one group for each thing reap suggests, the empty ones left out", func(t *testing.T) {
		want := []string{
			"> [ ] ▸ suggest to prune                          2 of 2 unused                       ~600 tok",
			"  [ ] ▸ suggest to mute                           2 of 2 unused                       ~350 tok",
			"  [ ] ▸ suggest to lock                           0 of 1 unused                        ~50 tok",
			"  [ ] ▸ already muted                             1 muted",
			"  [ ] ▸ already pruned                            1 pruned",
		}
		for n, line := range want {
			mustEqual(t, "list row "+itoa(n), strings.TrimPrefix(screen[listY+n], " "), line)
		}
		mustNotShow(t, screen, "not sure, you decide")
	})

	t.Run("the hint says what space would do where the cursor is", func(t *testing.T) {
		mustShow(t, screen, "space prunes suggest to prune: 2 items")
	})

	t.Run("nothing is chosen, so nothing waits to be applied", func(t *testing.T) {
		mustNotShow(t, screen, "apply")
	})
}

func TestListGroupsOpenAndClose(t *testing.T) {
	t.Run("right opens a group: its rows, the heaviest first", func(t *testing.T) {
		x := play(t, right)

		mustContain(t, lineWith(x.final(), "suggest to prune"), "▾")
		mustEqual(t, "first row", strings.TrimSpace(x.final()[listY+1]), "[ ] srv                                       0 uses                              ~500 tok")
		mustEqual(t, "second row", strings.TrimSpace(x.final()[listY+2]), "[ ] old-notes                                 0 uses                              ~100 tok")
	})

	t.Run("enter on a group opens it, and closes it again", func(t *testing.T) {
		opened, closed := play(t, enter), play(t, enter, enter)

		mustShow(t, opened.final(), "old-notes")
		mustNotShow(t, closed.final(), "old-notes")
	})

	t.Run("left on a row closes its group and stands on it", func(t *testing.T) {
		x := play(t, right, down, down, left)

		mustNotShow(t, x.final(), "old-notes")
		mustContain(t, lineWith(x.final(), "suggest to prune"), "> ")
	})

	t.Run("the cursor stops at both ends", func(t *testing.T) {
		top, bottom := play(t, up, up), play(t, down, down, down, down, down, down, down)

		mustContain(t, lineWith(top.final(), "suggest to prune"), "> ")
		mustContain(t, lineWith(bottom.final(), "already pruned"), "> ")
	})

	t.Run("end and home go to the last and the first line, page keys by a page", func(t *testing.T) {
		last, first := play(t, term.KeyEnd), play(t, term.KeyEnd, term.KeyHome)
		paged, back := play(t, term.KeyPgDn), play(t, term.KeyPgDn, term.KeyPgUp)

		mustContain(t, lineWith(last.final(), "already pruned"), "> ")
		mustContain(t, lineWith(first.final(), "suggest to prune"), "> ")
		mustContain(t, lineWith(paged.final(), "already pruned"), "> ")
		mustContain(t, lineWith(back.final(), "suggest to prune"), "> ")
	})

	t.Run("each row's hint says what space does, and the way out that fits it", func(t *testing.T) {
		own, plugin := play(t, right, down), play(t, down, right, down)
		chosen := play(t, right, down, space)

		mustShow(t, own.final(), "space prunes srv · d deletes it for good")
		mustShow(t, plugin.final(), "space mutes acme:lint · o turns its plugin off")
		mustShow(t, chosen.final(), "space clears srv · d deletes it for good")
	})

	t.Run("a group that is one plugin offers plugin off", func(t *testing.T) {
		x := play(t, down)

		mustShow(t, x.final(), "space mutes suggest to mute: 2 items · o turns the whole plugin off")
	})
}

func TestListGroupsThreeWays(t *testing.T) {
	t.Run("g groups by where rows come from, the heaviest unused first", func(t *testing.T) {
		x := play(t, 'g')

		mustShow(t, x.final(), "NAME · by plugin")
		for n, want := range []string{"your config", "acme", "your own · 1 muted", "quarantine"} {
			mustContain(t, x.final()[listY+n], "▸ "+want)
		}
		mustContain(t, statusLine(x.final()), "by plugin")
	})

	t.Run("g again is one flat list by weight, with no group to open", func(t *testing.T) {
		x := play(t, 'g', 'g')

		mustShow(t, x.final(), "NAME · flat")
		for n, want := range []string{"srv", "acme:lint", "acme:fmt", "old-notes", "daily", "quiet", "gone"} {
			mustContain(t, x.final()[listY+n], "] "+want)
		}
		mustNotShow(t, x.final(), "▸", "▾")
	})

	t.Run("a third g is back where it started, with every group closed", func(t *testing.T) {
		x := play(t, right, 'g', 'g', 'g')

		mustShow(t, x.final(), "NAME · by suggestion")
		mustNotShow(t, x.final(), "old-notes", "▾")
	})

	t.Run("two sources that read the same are told apart by their marketplace", func(t *testing.T) {
		rep := &report.Report{Sessions: 9, Rows: []report.Row{raw("a:x", source("plugin:acme@one")), raw("a:y", source("plugin:acme@two")), raw("mine")}}

		x := open(t, Data{Report: rep}).press('g').run()

		mustShow(t, x.final(), "acme · one", "acme · two", "your own")
	})

	t.Run("two rows of one name are told apart by their platform", func(t *testing.T) {
		rep := &report.Report{Sessions: 9, Rows: []report.Row{raw("twin"), raw("twin", platform("codex")), raw("single")}}

		x := open(t, Data{Report: rep}).press(right).run()

		mustShow(t, x.final(), "twin (claude-code)", "twin (codex)")
		mustNotShow(t, x.final(), "single (")
	})
}

func TestListKeysOnAGroup(t *testing.T) {
	cases := []struct {
		name   string
		keys   []term.Key
		status string
		box    string
	}{
		{"p prunes every row of it", []term.Key{'p'}, "prune 2", "[p]"},
		{"l locks every row of it", []term.Key{'l'}, "lock 2", "[l]"},
		{"m mutes what it can and names what it cannot: the group is mixed", []term.Key{'m'}, "1 muted · 1 cannot be muted: srv: an MCP server has nothing to", "[-]"},
		{"n clears what was chosen", []term.Key{space, 'n'}, "", "[ ]"},
		{"u has nothing to undo on rows that are as they came", []term.Key{'u'}, "nothing to undo here: u takes back a mute, a lock or a prune", "[ ]"},
		{"a over a choice replaces it, a again puts it back", []term.Key{'l', 'a', 'a'}, "reap's choice undone", "[l]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x := play(t, c.keys...)

			if got := statusText(x.final()); !strings.HasPrefix(got, c.status) || (c.status == "" && got != "") {
				t.Errorf("status = %q, want %q", got, c.status)
			}
			mustContain(t, lineWith(x.final(), "suggest to prune"), c.box)
		})
	}

	t.Run("the row that refused says so, until the next key that chooses", func(t *testing.T) {
		refused := play(t, 'm', right, down)

		mustContain(t, lineWith(refused.final(), "srv"), "can't mute")
		for name, next := range map[string]term.Key{"n": 'n', "space": space, "a": 'a', "l": 'l'} {
			after := play(t, 'm', right, next)

			if has(after.final(), "can't mute") {
				t.Errorf("after %s the row still says it refused", name)
			}
		}
	})

	t.Run("u on the muted group and on the pruned group marks an undo", func(t *testing.T) {
		x := play(t, down, down, down, 'u', down, 'u')

		mustContain(t, lineWith(x.final(), "already muted"), "[u]")
		mustContain(t, lineWith(x.final(), "already pruned"), "[u]")
		mustContain(t, statusLine(x.final()), "undo 2")
	})

	t.Run("an undo applied says the row is back at the next launch, and leaves the list", func(t *testing.T) {
		x := play(t, down, down, down, 'u', down, 'u', applyKey, 'y')

		mustEqual(t, "receipt", x.saw(doIt, "undo     quiet", "undo     gone", "2 items · 2 come back heavier"), true)
		mustApply(t, x, []string{"unmute quiet", "restore gone"})
		mustContain(t, statusLine(x.final()), "✓ 2 undone: back in the list at the next launch")
		mustNotShow(t, x.final(), "already muted", "already pruned")
	})

	t.Run("a lock applied shows as locked, and stays", func(t *testing.T) {
		x := play(t, right, down, 'l', applyKey, 'y')

		mustApply(t, x, []string{"lock srv"})
		mustContain(t, lineWith(x.final(), "srv"), "locked")
		mustContain(t, statusLine(x.final()), "✓ 1 locked")
	})

	t.Run("what goes is struck through, what is muted only fades", func(t *testing.T) {
		x := play(t, 'p', down, 'm')

		mustEqual(t, "pruned group struck", isStruck(x.final(), "suggest to prune"), true)
		mustEqual(t, "muted group struck", isStruck(x.final(), "suggest to mute"), false)
	})

	t.Run("a key that is not a Latin letter says which are read", func(t *testing.T) {
		x := play(t, term.KeyNonLatin)

		mustContain(t, statusLine(x.final()), latinHint)
	})
}

func TestListSelection(t *testing.T) {
	const selecting = "SELECTING 2 rows · 4 items · ~950 tok · shift+↑↓ to extend · space prunes them · esc cancels"

	t.Run("shift+down selects from the cursor and says what is in it", func(t *testing.T) {
		x := play(t, term.KeyShiftDown)

		mustShow(t, x.final(), selecting)
	})

	t.Run("J and K stand in for shift+down and shift+up", func(t *testing.T) {
		grown, shrunk := play(t, 'J'), play(t, 'J', 'J', 'K')

		mustShow(t, grown.final(), selecting)
		mustShow(t, shrunk.final(), selecting)
	})

	t.Run("an arrow under a held shift selects too", func(t *testing.T) {
		x := open(t, flowData())
		x.s.SetShift(true)

		x.press(down).run()

		mustShow(t, x.final(), selecting)
	})

	t.Run("a plain arrow ends the selection", func(t *testing.T) {
		x := play(t, term.KeyShiftDown, down)

		mustNotShow(t, x.final(), "SELECTING")
	})

	t.Run("esc ends the selection", func(t *testing.T) {
		x := play(t, term.KeyShiftDown, esc, space)

		mustNotShow(t, x.final(), "SELECTING")
		mustContain(t, statusLine(x.final()), "mute 2") // space acted on the cursor's group alone
	})

	t.Run("space takes reap's choice on everything selected", func(t *testing.T) {
		x := play(t, term.KeyShiftDown, space)

		mustContain(t, statusLine(x.final()), "prune 2 · mute 2")
		mustContain(t, statusLine(x.final()), "apply 4 changes")
		mustNotShow(t, x.final(), "SELECTING")
	})

	t.Run("space again, with the selection gone, gives the same rows back", func(t *testing.T) {
		x := play(t, term.KeyShiftDown, space, space)

		mustNotShow(t, x.final(), "apply")
	})

	t.Run("a key on a selection reaches every row in it", func(t *testing.T) {
		x := play(t, term.KeyShiftDown, term.KeyShiftDown, 'l')

		mustContain(t, statusLine(x.final()), "lock 5")
	})

	t.Run("v marks: plain arrows extend, v again ends it", func(t *testing.T) {
		marking, ended := play(t, 'v', down), play(t, 'v', down, 'v')

		mustShow(t, marking.final(), "SELECTING 2 rows · 4 items · ~950 tok · arrows to extend")
		mustNotShow(t, ended.final(), "SELECTING")
	})

	t.Run("a on a selection, and a again, puts everything back", func(t *testing.T) {
		chosen, back := play(t, term.KeyShiftDown, 'a'), play(t, term.KeyShiftDown, 'a', 'a')

		mustContain(t, statusLine(chosen.final()), "prune 2 · mute 2")
		mustContain(t, statusLine(back.final()), "reap's choice undone")
		mustNotShow(t, back.final(), "apply")
	})
}

func TestListPlatformFilter(t *testing.T) {
	mixed := func() Data {
		d := flowData()
		d.Report.Rows = append(d.Report.Rows, raw("cx-skill", platform("codex"), tokens(70)))
		return d
	}

	t.Run("f shows one platform at a time, and says which", func(t *testing.T) {
		x := open(t, mixed()).press('f', 'f', right).run()

		mustShow(t, x.final(), "NAME · by suggestion  (codex)", "cx-skill")
		mustNotShow(t, x.final(), "srv", "suggest to mute")
		mustEqual(t, "said which", x.saw("showing claude-code") && x.saw("showing codex"), true)
	})

	t.Run("after the last platform f shows them all again", func(t *testing.T) {
		x := open(t, mixed()).press('f', 'f', 'f').run()

		mustContain(t, statusLine(x.final()), "showing all")
		mustShow(t, x.final(), "suggest to mute")
	})

	t.Run("the headline counts only what is shown", func(t *testing.T) {
		x := open(t, mixed()).press('f', 'f').run()

		mustShow(t, x.final(), "1 of 1 unused                        ~70 tok")
	})

	t.Run("a choice the filter hides still waits to be applied, and the label says so", func(t *testing.T) {
		x := open(t, mixed()).press('f', space, 'f').run() // chosen on claude-code, then looking at codex

		mustContain(t, statusLine(x.final()), "apply 2 changes · 2 not shown")
	})

	t.Run("and it is applied with the rest", func(t *testing.T) {
		x := open(t, mixed()).press('f', space, 'f', space, applyKey, 'y').run()

		mustApply(t, x, []string{"prune srv", "prune old-notes", "prune cx-skill"})
	})

	t.Run("a platform with nothing left says so, and f moves on", func(t *testing.T) {
		x := open(t, mixed()).press('f', 'f', space, applyKey, 'y').run()
		moved := open(t, mixed()).press('f', 'f', space, applyKey, 'y', 'f').run()

		mustShow(t, x.final(), "nothing to review for codex", "✓ 1 gone", "f platform · g group by · q")
		mustShow(t, moved.final(), "suggest to prune")
	})
}

func TestListWithNothingInIt(t *testing.T) {
	empty := Data{Report: &report.Report{Sessions: 9, Rows: []report.Row{raw("rules", category(scan.CatProse))}}}

	t.Run("a stack with nothing to list says so", func(t *testing.T) {
		x := open(t, empty).run()

		mustShow(t, x.final(), "nothing to review", "f platform · g group by · q")
	})

	t.Run("no key chooses anything there, and q leaves", func(t *testing.T) {
		x := open(t, empty).press(space, 'g', 'f', applyKey, 'y', 'q', 'g').run()

		mustApply(t, x)
		mustShow(t, x.final(), "nothing to review")
	})

	t.Run("applying the last rows empties the list and says what was done", func(t *testing.T) {
		one := Data{Report: &report.Report{Sessions: 9, Rows: []report.Row{raw("only")}}}

		x := open(t, one).press(space, applyKey, 'y').run()

		mustShow(t, x.final(), "nothing to review", "✓ 1 gone")
	})
}

func TestListHeadline(t *testing.T) {
	t.Run("with no sessions read it says nothing is measured", func(t *testing.T) {
		d := flowData()
		d.Report.Sessions = 0

		x := open(t, d).run()

		mustShow(t, x.final(), "no sessions read: nothing is measured yet", "the verdicts below have no evidence behind them")
	})

	t.Run("the report's warnings come first, the ones about wrong numbers before the caveats", func(t *testing.T) {
		d := flowData()
		d.Report.Warnings = []scan.Warning{{Msg: "empty directory", Advisory: true}, {Path: "/logs/a.jsonl", Msg: "unreadable\nline"}, {Msg: ""}}

		x := open(t, d).run()

		mustShow(t, x.final(), "⚠ a.jsonl: unreadable line · +1 more")
	})

	t.Run("with a price it opens in money, as a floor", func(t *testing.T) {
		x := open(t, pricedData()).run()

		mustShow(t, x.final(), "at least", "wasted a month", "~950 tokens re-sent every session", "AT LEAST/MONTH", "~US$12.00")
	})

	t.Run("tab switches between money and tokens", func(t *testing.T) {
		x := open(t, pricedData()).press(tab).run()
		back := open(t, pricedData()).press(tab, tab).run()

		mustShow(t, x.final(), "tokens wasted every session", "TOKENS", "~600 tok")
		mustShow(t, back.final(), "AT LEAST/MONTH")
	})

	t.Run("with no price tab says there are only tokens", func(t *testing.T) {
		x := play(t, tab)

		mustContain(t, statusLine(x.final()), "no price known: tokens only")
		mustShow(t, x.final(), "TOKENS")
	})

	t.Run("on a flat plan it opens in tokens, and money is worded as a value", func(t *testing.T) {
		d := pricedData()
		d.Billing = "stripe_subscription"

		tokensFirst, money := open(t, d).run(), open(t, d).press(tab).run()

		mustShow(t, tokensFirst.final(), "tokens wasted every session", "in a month of sessions · what eats your plan")
		mustShow(t, money.final(), "worth", "at API prices, wasted a month", "API PRICE/MONTH")
	})

	t.Run("what was chosen is said as saved, in the unit shown", func(t *testing.T) {
		x := open(t, pricedData()).press(space).wait().run()

		mustShow(t, x.final(), "you saved at least US$12.00 a month")
	})

	t.Run("the receipt speaks in the unit the list was in", func(t *testing.T) {
		money := open(t, pricedData()).press(space, applyKey).run()
		toks := open(t, pricedData()).press(tab, space, applyKey).run()

		mustShow(t, money.frame(doIt), "at least", "saved every month")
		mustShow(t, toks.frame(doIt), "tokens saved every session")
	})

	t.Run("the system's currency and separators are used", func(t *testing.T) {
		d := pricedData()
		d.Currency = Currency{Code: "EUR", Sym: "€", Rate: 0.5, Decimal: ",", Group: ".", Digits: 2}

		x := open(t, d).run()

		mustShow(t, x.final(), "~€6,00", "tab €/tok")
	})
}

func TestHoldingSShowsWhatReapSuggests(t *testing.T) {
	t.Run("groups say what reap suggests inside them, rows the word", func(t *testing.T) {
		x := play(t, right, 's')

		mustShow(t, x.final(), "reap suggests, while you hold s", "2 prune", "2 mute", "1 lock")
		mustContain(t, lineWith(x.final(), "srv"), "prune")
	})

	t.Run("it ends when the key is let go", func(t *testing.T) {
		x := play(t, 's', term.KeyNone)

		mustNotShow(t, x.final(), "while you hold s")
	})

	t.Run("it stays while the system says the key is still down", func(t *testing.T) {
		x := open(t, flowData())
		x.s.SKey = true

		x.press('s', term.KeyNone).run()

		mustShow(t, x.final(), "while you hold s")
	})

	t.Run("it chooses nothing", func(t *testing.T) {
		x := play(t, 's', 's', 's')

		mustNotShow(t, x.final(), "apply")
	})
}

// The headline counts what reap calls wasted. Choosing something it never
// counted must leave it exactly where it stood, not make it spring back.
func TestHeadlineDoesNotMoveForAChoiceItNeverCounted(t *testing.T) {
	// Arrange: every wasted row chosen and the number landed.
	x := open(t, flowData())
	l := newList(x.m)
	var outside *Row
	for _, r := range x.m.rows {
		switch {
		case r.Verdict == report.VerdictReap && can(r, vPrune):
			l.p.set(r, vPrune)
		case outside == nil && r.Verdict != report.VerdictReap && r.Tokens > 0 && can(r, vMute):
			outside = r
		}
	}
	if outside == nil {
		t.Skip("the fixture has no row outside the headline that can be muted")
	}
	settle := func() {
		for n := 0; n < 200; n++ {
			l.rebuild()
			if !l.draw() {
				return
			}
		}
		t.Fatal("the headline never landed")
	}
	settle()
	landed := float64(l.total) - l.shownDead

	// Act: one frame after a mute of a row the headline never held.
	l.p.set(outside, vMute)
	l.rebuild()
	l.draw()

	// Assert
	mustEqual(t, "the headline one frame after the mute", float64(l.total)-l.shownDead, landed)
	settle()
	mustEqual(t, "the headline once everything landed", float64(l.total)-l.shownDead, landed)
	if l.shown <= float64(l.dead) {
		t.Fatalf("the saved figure did not grow with the mute: shown %v, dead %v", l.shown, l.dead)
	}
}
