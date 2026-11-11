package menu

// Rows a stack seldom holds, each of which changes what the menu may offer: a
// server in a file reap cannot edit, a skill whose whole folder is a link, a
// plugin skill with no description, a weight never measured, a name too long
// for the big font, a project's own skill.

import (
	"slices"
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/prune"
	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/scan"
	"github.com/thousandflowers/skillreaper/internal/term"
)

// one is a stack of these rows alone.
func one(rows ...report.Row) Data {
	return Data{Report: &report.Report{Sessions: 9, WindowDays: 7, Rows: rows}}
}

// firstRow puts the cursor on the first row of the first group.
var firstRow = []term.Key{right, down}

func folderLinked(r *report.Row) {
	r.Removable, r.FolderLink, r.ViaLink, r.RootDir = false, true, "/h/.agents/skills/fl", "/h/.claude"
}

func TestRowsBuiltForTheCommandLine(t *testing.T) {
	t.Run("NewRow is a scanned row made safe to show, the scan kept beside it", func(t *testing.T) {
		got := NewRow(raw("cr\rSPOOF", tokens(-4)))

		mustEqual(t, "shown name has the carriage return", strings.ContainsRune(got.Name, '\r'), false)
		mustEqual(t, "weight", got.Tokens, 0)
		mustEqual(t, "scanned name", got.Raw().Name, "cr\rSPOOF")
		mustEqual(t, "scanned weight", got.Raw().Tokens, -4)
	})

	t.Run("PrunedRow is a quarantine entry as a row only u applies to", func(t *testing.T) {
		got := PrunedRow(prune.Entry{ID: "a1", Name: "old", Category: "agent", Platform: "codex", To: "/q/old"})

		mustEqual(t, "id", got.PruneID, "a1")
		mustEqual(t, "kind", got.cat(), "agent")
		mustEqual(t, "tool", got.Raw().Platform, "codex")
		mustEqual(t, "path", got.Path, "/q/old")
		mustEqual(t, "undo", existingUndo[existing(got)], vRestore)
		mustEqual(t, "can prune", can(got, vPrune), false)
	})
}

func TestServerInAFileReapCannotEdit(t *testing.T) {
	toml := raw("cfg-srv", category(scan.CatMCP), source("user-config"), notRemovable)

	t.Run("it is listed with no box, tagged read-only in a quiet colour", func(t *testing.T) {
		x := open(t, one(toml, raw("mine")))
		x.s.watch = []string{suggestRO}

		x.press(down, right).run() // the cursor stays on the group, so the row is not lit

		row := lineWith(x.final(), "cfg-srv")
		mustContain(t, row, suggestRO)
		mustEqual(t, "has a box", strings.Contains(row, "["), false)
		mustEqual(t, "tag style", x.s.styles[suggestRO], dim)
	})

	t.Run("reap has no choice to make for it, and says where to remove it by hand", func(t *testing.T) {
		x := open(t, one(toml)).press(firstRow...).press(space, 'p', 'd', 'm', applyKey, 'y').run()

		mustEqual(t, "best", best(row("cfg-srv", category(scan.CatMCP), notRemovable)), "")
		mustEqual(t, "hint", x.saw("cfg-srv · reap cannot edit it: remove it by hand from /p/cfg-srv"), true)
		mustApply(t, x)
	})

	t.Run("with no path known it says its file", func(t *testing.T) {
		nowhere := row("cfg-srv", category(scan.CatMCP), notRemovable, func(r *report.Row) { r.Path = "" })

		mustEqual(t, "why not", whyNot(nowhere, vPrune), "reap cannot edit it: remove it by hand from its file")
	})

	t.Run("it can still be locked", func(t *testing.T) {
		x := open(t, one(toml)).press(firstRow...).press('l', applyKey, 'y').run()

		mustApply(t, x, []string{"lock cfg-srv"})
	})
}

func TestSkillWhoseWholeFolderIsALink(t *testing.T) {
	linked := raw("fl", folderLinked)

	t.Run("it cannot be pruned or deleted, and reap's own choice is the mute it can take", func(t *testing.T) {
		r := row("fl", folderLinked)

		mustEqual(t, "can prune", can(r, vPrune), false)
		mustEqual(t, "can delete", can(r, vDelete), false)
		mustEqual(t, "best", best(r), vMute)
	})

	t.Run("space on its card mutes it", func(t *testing.T) {
		x := open(t, one(linked)).press(firstRow...).press(enter, space, applyKey, 'y').run()

		mustApply(t, x, []string{"mute fl"})
	})

	t.Run("space again on its card clears the mute", func(t *testing.T) {
		x := open(t, one(linked)).press(firstRow...).press(enter, space, space).run()

		mustNotShow(t, x.final(), "apply")
	})

	t.Run("p on its card stands on the grey option, which says the link is the whole folder", func(t *testing.T) {
		x := open(t, one(linked)).press(firstRow...).press(enter, 'p').run()

		mustShow(t, x.final(), "prune is not available: the link it comes through is its whole skills folder")
		mustNotShow(t, x.final(), "apply")
	})

	t.Run("the receipt says the mute reaches the folder the link leads to", func(t *testing.T) {
		x := open(t, one(linked)).press(firstRow...).press('m', applyKey).run()

		mustShow(t, x.frame(doIt), "⚠ also muted for other tools using /h/.agents/skills")
	})
}

func TestPluginSkillWithNoDescription(t *testing.T) {
	bare := raw("acme:bare", source(acmePlugin), notRemovable, noDescription)

	t.Run("reap never chooses a mute the row would refuse", func(t *testing.T) {
		r := row("acme:bare", source(acmePlugin), notRemovable, noDescription)

		mustEqual(t, "suggestion", suggestion(r), vMute)
		mustEqual(t, "can mute", can(r, vMute), false)
		mustEqual(t, "best", best(r), "")
	})

	t.Run("space, a and a click choose nothing for it", func(t *testing.T) {
		x := open(t, one(bare)).press(firstRow...).press(space, 'a').click(30, listY+1).press(applyKey, 'y').run()

		mustApply(t, x)
		mustNotShow(t, x.final(), "apply")
	})

	t.Run("the hint says the keys still choose", func(t *testing.T) {
		x := open(t, one(bare)).press(firstRow...).run()

		mustShow(t, x.final(), "acme:bare · it has no description to strip: p, l and d still choose")
	})

	t.Run("p still prunes it", func(t *testing.T) {
		x := open(t, one(bare)).press(firstRow...).press('p', applyKey, 'y').run()

		mustApply(t, x, []string{"prune acme:bare"})
	})
}

func TestMutedRowSaysHowToGetAtIt(t *testing.T) {
	x := play(t, down, down, down, right, down) // the muted row of the flow fixture

	mustShow(t, x.final(), "quiet · it is muted: u unmutes it, p prunes it")
}

func TestWeightNeverMeasuredIsAQuestionMark(t *testing.T) {
	hooked := raw("hooked", category(scan.CatMCP), source("user-config"), tokens(0), func(r *report.Row) { r.WeightUnknown = true })

	t.Run("on its row", func(t *testing.T) {
		x := open(t, one(hooked)).press(firstRow...).run()

		mustEqual(t, "row ends", strings.HasSuffix(lineWith(x.final(), "] hooked"), "?"), true)
	})

	t.Run("on its line of the receipt, which adds nothing up for it", func(t *testing.T) {
		x := open(t, one(hooked)).press(space, applyKey, 'y').run()
		receipt := x.frame(doIt)

		line := strings.TrimSpace(receipt[3])
		mustEqual(t, "line", strings.HasPrefix(line, "prune    hooked") && strings.HasSuffix(line, "?"), true)
		mustShow(t, receipt, "1 item")
		mustNotShow(t, receipt, "tokens lighter", "tokens saved")
		mustApply(t, x, []string{"prune hooked"})
	})
}

func TestNameTooLongForTheBigFont(t *testing.T) {
	const long = "acme:a-very-long-skill-name-that-cannot-fit"
	x := open(t, one(raw(long, source(acmePlugin), notRemovable))).press(firstRow...).press(enter).run()

	t.Run("the card writes it whole under what the big font could draw", func(t *testing.T) {
		mustShow(t, x.final(), long+" · skill · plugin:acme · claude-code")
	})

	t.Run("the big font draws the start of it, cut at the window", func(t *testing.T) {
		drawn := fitTitle("a-very-long-skill-name-that-cannot-fit", testCols-8)

		mustEqual(t, "cut", drawn != "" && drawn != "a-very-long-skill-name-that-cannot-fit", true)
		mustEqual(t, "title row", strings.TrimRight(x.final()[1], " "), "   "+strings.TrimRight(banner(drawn)[0], " "))
	})
}

func TestProjectSkillIsThisProjects(t *testing.T) {
	rows := []report.Row{raw("local", source("project:/work/site")), raw("mine")}

	t.Run("by plugin it sits under this project", func(t *testing.T) {
		x := open(t, one(rows...)).press('g').run()

		mustShow(t, x.final(), "▸ this project", "▸ your own")
	})

	t.Run("where a row comes from, as a person would say it", func(t *testing.T) {
		said := map[string]string{"project:/work/site": "this project", "personal": "your own", "user-config": "your config",
			"plugin:acme@market": "acme", "quarantine": "quarantine", "somewhere-new": "somewhere-new"}
		for from, want := range said {
			mustEqual(t, "source name of "+from, sourceName(row("x", source(from))), want)
		}
	})
}

func TestReasonReapHasNoSentenceFor(t *testing.T) {
	t.Run("the card shows the reason as the report gave it", func(t *testing.T) {
		x := open(t, one(raw("x", reason("brand-new-reason")))).press(firstRow...).press(enter).run()

		mustEqual(t, "why", strings.TrimSpace(x.final()[11]), "brand-new-reason")
	})

	t.Run("a note about missing evidence comes before any reason", func(t *testing.T) {
		noted := row("x", reason("no-transcript"), func(r *report.Row) { r.EvidenceNote = "this tool keeps no transcripts" })

		mustEqual(t, "why", whyLine(noted), "No usage evidence: this tool keeps no transcripts.")
	})
}

func TestRowReapSuggestsNothingFor(t *testing.T) {
	linked := raw("via", notRemovable, func(r *report.Row) { r.ViaLink, r.RootDir = "/x/via", "/h/.codex" })

	t.Run("it has no word in the list's terms", func(t *testing.T) {
		r := row("via", notRemovable, func(r *report.Row) { r.ViaLink, r.RootDir = "/x/via", "/h/.codex" })

		mustEqual(t, "suggestion", suggestion(r), suggestNone)
		mustEqual(t, "word", suggestionWord(r), "")
		mustEqual(t, "line on the card", suggestionLine(r), "reap suggests nothing")
	})

	t.Run("holding s, its group counts it as nothing", func(t *testing.T) {
		x := open(t, one(linked, raw("other", verdict(report.VerdictReview)))).press('s').run()

		mustShow(t, x.final(), "1 nothing · 1 not sure")
	})

	t.Run("what reap suggests inside a group, the two commonest first", func(t *testing.T) {
		rows := []*Row{row("a"), row("b", verdict(report.VerdictKeep)), row("c"), row("d", verdict(report.VerdictKeep)), row("e"),
			row("f", verdict(report.VerdictReview))}

		mustEqual(t, "phrase", peekPhrase(rows), "3 prune · 2 lock")
	})
}

func TestBetweenTwoListsOnANarrowWindow(t *testing.T) {
	// 62 columns: "suggest to mute" does not fit in the big font.
	x := openSized(t, testRows, 62, flowData()).press(right, down, enter, down, down).run()
	page := x.frame("suggest to prune · done")

	t.Run("the big font draws what fits of the next list's name", func(t *testing.T) {
		drawn := fitTitle("suggest to mute", 62-8)

		mustEqual(t, "cut", drawn != "suggest to mute", true)
		mustEqual(t, "banner shown", slices.Contains(page, "   "+strings.TrimRight(banner(drawn)[0], " ")), true)
	})

	t.Run("and the whole name is written under it", func(t *testing.T) {
		mustEqual(t, "name written plainly", slices.Contains(page, "   suggest to mute"), true)
	})

	t.Run("a name that fits is not written twice", func(t *testing.T) {
		wide := play(t, right, down, enter, down, down).frame("suggest to prune · done")

		mustEqual(t, "name written plainly", slices.Contains(wide, "   suggest to mute"), false)
	})

	t.Run("a name the big font has no letters for is written plainly", func(t *testing.T) {
		d := one(raw("a"), raw("b:x", source("plugin:漢字@m"), notRemovable))

		y := open(t, d).press('g', right, down, enter, down).run() // by plugin: your own, then 漢字

		mustEqual(t, "name written plainly", slices.Contains(y.frame("your own · done"), "   漢字"), true)
	})
}

func TestAutoUndoesOnlyWhatItChose(t *testing.T) {
	m := rulesMenu(t)

	t.Run("a second a over two rows leaves alone the one chosen by hand since", func(t *testing.T) {
		byReap, byHand, p := row("a"), row("b"), newPlan()
		m.auto(p, []*Row{byReap})
		p.set(byHand, vPrune) // matches reap, but a never chose it

		msg := m.auto(p, []*Row{byReap, byHand})

		mustEqual(t, "message", msg, "reap's choice undone")
		mustEqual(t, "row reap chose", p.has(byReap), false)
		mustEqual(t, "row chosen by hand", p.get(byHand), vPrune)
	})

	t.Run("a over a row that already held reap's choice has nothing to undo for it", func(t *testing.T) {
		already, empty, p := row("a"), row("b"), newPlan()
		p.set(already, vPrune)
		m.auto(p, []*Row{already, empty}) // changes only the second row

		msg := m.auto(p, []*Row{already})

		mustEqual(t, "message", msg, "nothing to undo")
		mustEqual(t, "its choice", p.get(already), vPrune)
		mustEqual(t, "the other row", p.get(empty), vPrune)
	})

	t.Run("rows reap can do nothing to are counted in what it says", func(t *testing.T) {
		stuck := row("acme:bare", source(acmePlugin), notRemovable, noDescription)

		mustEqual(t, "message", m.auto(newPlan(), []*Row{row("a"), stuck}), "reap chose: 1 prune · 1 left: nothing reap can do to them")
		mustEqual(t, "message with nothing chosen", m.auto(newPlan(), []*Row{stuck}), "reap chose nothing · 1 left: nothing reap can do to them")
	})
}

func TestCardOpenedWithNoListAroundIt(t *testing.T) {
	m, s, _ := newMenu(t, one(raw("solo")))
	p := newPlan()
	hooks := cardHooks{act: func(rows []*Row, ch rune) { m.actOn(p, rows, ch) }}
	s.Feed('p', down, up, esc)

	ended := m.card(m.rows[0], p, nil, hooks)

	t.Run("it is a list of one", func(t *testing.T) {
		mustShow(t, s.Lines(), "1 of 1", "solo · skill · personal · claude-code")
	})
	t.Run("its keys choose, its arrows have nowhere to go, esc gives the row back", func(t *testing.T) {
		mustEqual(t, "row it ended on", ended, m.rows[0])
		mustEqual(t, "choice", p.get(m.rows[0]), vPrune)
	})
}

// The guards below can be reached by no key today: every caller checks first.
// They stay, because without them a later caller would hang or print rubbish.
func TestGuardsNoCallerNeedsToday(t *testing.T) {
	x := open(t, flowData())

	t.Run("text wraps to nothing in a window with no width", func(t *testing.T) {
		mustEqual(t, "lines", len(wrap("a b", 0)), 0)
	})
	t.Run("a character wider than the window takes a line and wrapping ends", func(t *testing.T) {
		got := wrap("日本 a", 1)

		mustEqual(t, "lines", strings.Join(got, "|"), "日|本|a")
	})
	t.Run("a negative figure keeps its sign outside the separators", func(t *testing.T) {
		mustEqual(t, "number", x.m.number(-1234.5, 2), "-1,234.50")
	})
	t.Run("a receipt with nothing chosen applies nothing", func(t *testing.T) {
		said := x.m.receipt(newPlan(), unitTokens, func() { t.Fatal("an empty receipt was applied") })

		mustEqual(t, "what it says", said, "nothing to apply")
		mustEqual(t, "applies", len(x.a.calls), 0)
	})
}

func TestWhyReapHasNoChoiceOfItsOwn(t *testing.T) {
	cases := []struct {
		name string
		row  *Row
		want string
	}{
		{"a locked row says how to unlock it", func() *Row { r := row("pinned"); r.Kept = true; return r }(), "it is locked: u unlocks it first"},
		{"a muted row says what still works", row("quiet", reason(reasonMuted), verdict(report.VerdictKeep), reason(reasonMuted)), "it is muted: u unmutes it, p prunes it"},
		{"a plugin skill with no description says why mute is refused, and what is left",
			row("acme:bare", source(acmePlugin), notRemovable, noDescription), "it has no description to strip: p, l and d still choose"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mustEqual(t, "best", best(c.row), "")
			mustEqual(t, "why", whyNothing(c.row), c.want)
		})
	}
}

func TestGroupWithNothingForReapToChooseSaysWhatStillWorks(t *testing.T) {
	x := open(t, flowData())
	bare := row("acme:bare", source(acmePlugin), notRemovable, noDescription)
	quiet := row("quiet", verdict(report.VerdictKeep), reason(reasonMuted))
	hint := func(rows ...*Row) string {
		return x.m.cursorHint(line{isGroup: true, name: "g", rows: rows}, x.m.groupStats(rows, newPlan()), newPlan())
	}

	mustContain(t, hint(quiet), "u undoes it")
	mustContain(t, hint(bare), "p, l and d still choose")
}

func TestSpaceOnARowReapCannotChooseForSaysWhy(t *testing.T) {
	x := open(t, flowData())
	l := newList(x.m)

	l.toggle([]*Row{row("acme:bare", source(acmePlugin), notRemovable, noDescription)})

	mustEqual(t, "message", l.msg, "nothing to select here: it has no description to strip: p, l and d still choose")
}
