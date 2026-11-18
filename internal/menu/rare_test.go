package menu

// Rows a stack seldom holds, each of which changes what the menu may offer: a
// server in a file reap cannot edit, a skill whose whole folder is a link, a
// plugin skill with no description, a weight never measured, a name too long
// for the big font, a project's own skill.

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

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

// Two projects whose folders share a name are two groups with two names: the
// list opens and closes a group by its name, so one name opened both at once
// and nothing told them apart.
func TestProjectsWithTheSameFolderNameStayApart(t *testing.T) {
	rows := []report.Row{raw("alpha", source("project:/work/a/app")), raw("beta", source("project:/work/b/app")),
		raw("gamma", source("project:/work/site"))}
	m, _, _ := newMenu(t, one(rows...))

	names := map[string]int{}
	for _, g := range m.bySource() {
		names[g.name]++
	}

	for _, want := range []string{"this project · /work/a/app", "this project · /work/b/app", "this project · site"} {
		mustEqual(t, "groups named "+want, names[want], 1)
	}
}

// The way out is never the key the legend cuts: in a narrow window "q quit"
// was the tenth key of two short rows, and was on no screen at all.
func TestLegendAlwaysShowsTheWayOut(t *testing.T) {
	for _, cols := range []int{listCols, 60, 64, 80, 120} {
		x := openSized(t, 24, cols, one(raw("x"))).run()
		legend := strings.Join(x.final()[22:], "\n")

		mustEqual(t, fmt.Sprintf("q quit in the legend at %d columns", cols), strings.Contains(legend, "q quit"), true)
		for _, line := range x.final()[22:] {
			mustEqual(t, fmt.Sprintf("legend row fits %d columns", cols), runes(strings.TrimRight(line, " ")) <= cols, true)
		}
	}
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

// The receipt names the folder a linked skill really lives in. That path comes
// from a link's target, which a plugin can ship: it is cleaned like every other
// text from disk before it is drawn on the screen the person answers y to.
func TestSharedFolderIsCleanedBeforeItIsShown(t *testing.T) {
	linked := row("lnk", func(r *report.Row) { r.LinkTarget = "/home/u/.claude/plugins/ev\u202e\u009bil/skills/lnk" })

	got := sharedDir(linked)

	mustEqual(t, "no direction override or C1 control", strings.ContainsAny(got, "\u202e\u009b"), false)
	mustEqual(t, "the rest of the path", got, "/home/u/.claude/plugins/ev\ufffd\ufffdil/skills")
}

// A count stands clear of what is before it: in the narrowest window a group's
// name ran into its count ("prune22 of 22"), and a row's uses into its tag.
func TestCountsStandClearOfTheTextBeforeThem(t *testing.T) {
	x := openSized(t, listRows, listCols, flowData()).run()
	touching := regexp.MustCompile(`[^ \d]\d+ of \d+`)
	for _, line := range x.final() {
		mustEqual(t, "a count touches the name in "+strings.TrimSpace(line), touching.MatchString(line), false)
	}

	m, _, _ := newMenu(t, one(raw("x")))
	for _, uses := range []int{1, 99, 133, 999, 12345} {
		field := m.usesField(uses)
		mustEqual(t, fmt.Sprintf("the field for %d uses ends in a gap", uses), strings.HasSuffix(field, " "), true)
		mustEqual(t, fmt.Sprintf("the field for %d uses keeps its width", uses), runes(field), usesWidth)
	}
}

// On a flat plan what was chosen is worth money at API prices: said so, once.
// It read "you saved worth $1.07 at API prices a month".
func TestSavedOnAFlatPlanReadsAsASentence(t *testing.T) {
	d := pricedData()
	d.Billing = "stripe_subscription"

	x := open(t, d).press(tab).press(space).wait().run()

	mustShow(t, x.final(), "you would save US$12.00 a month, at API prices")
	mustNotShow(t, x.final(), "saved worth")
}

// One name, one tool, two marketplaces: the two rows are two plugins' and say
// which. They read the same in the list and on the receipt, so nobody could
// tell which one they were choosing or undoing.
func TestSameNameInOneToolFromTwoPluginsIsToldApart(t *testing.T) {
	rows := []report.Row{raw("alpha:a-one", source("plugin:alpha@mk1")), raw("alpha:a-one", source("plugin:alpha@mk2")),
		raw("twice"), raw("twice", platform("codex")), raw("solo")}
	m, _, _ := newMenu(t, one(rows...))

	names := map[string]int{}
	for _, r := range m.rows {
		names[m.displayName(r)]++
	}

	for _, want := range []string{"alpha:a-one (" + defaultTool + " · alpha@mk1)", "alpha:a-one (" + defaultTool + " · alpha@mk2)",
		"twice (" + defaultTool + ")", "twice (codex)", "solo"} {
		mustEqual(t, "rows named "+want, names[want], 1)
	}
}

// The footer of a selection counts what space is about to choose, a row reap
// would lock included: it said 22 items and space chose 26.
func TestSelectionFooterCountsWhatSpaceChooses(t *testing.T) {
	d := one(raw("a"), raw("b"), raw("kept", verdict(report.VerdictKeep), uses(5)))
	said, took := regexp.MustCompile(`· (\d+) items? ·`), regexp.MustCompile(`(\d+) of \d+ chosen`)

	marked := open(t, d).press('g', 'v').run()
	chosen := open(t, d).press('g', 'v', space).run()

	footer, row := said.FindStringSubmatch(strings.Join(marked.final(), "\n")), took.FindStringSubmatch(strings.Join(chosen.final(), "\n"))
	if footer == nil || row == nil {
		t.Fatalf("no count on screen:\n%s\n%s", strings.Join(marked.final(), "\n"), strings.Join(chosen.final(), "\n"))
	}
	mustEqual(t, "items the footer counts, against items space chose", footer[1], row[1])
}

// In the narrowest window what stands left of the apply label is cut to the
// room there is: the label was painted over the end of it
// ("whole plu shift+enter apply"), and a group's "· 1 muted" ran into its count
// to read "· 12 of 3 unused".
func TestNarrowWindowCutsTextInsteadOfPaintingOverIt(t *testing.T) {
	x := openSized(t, 20, listCols, flowData()).press('g', down, 'o').run()
	lines := x.final()

	status := lines[20-3]
	at := strings.Index(status, " apply ")
	if at < 0 {
		t.Fatalf("no apply label on the status row: %q", status)
	}
	key := strings.LastIndex(status[:at], " ") // the space the label opens with, before the key's name
	mustEqual(t, "a gap stands between the status and the label: "+status, status[key-1], byte(' '))
	for _, line := range lines {
		mustEqual(t, "a count runs into the text before it: "+strings.TrimSpace(line),
			regexp.MustCompile(`\d\d+ of \d+ unused`).MatchString(line) && strings.Contains(line, "· 1"), false)
	}

	narrowest := openSized(t, listRows, listCols, flowData()).run().final()[6]
	mustEqual(t, "no title cut in the middle of a word: "+strings.TrimSpace(narrowest), strings.Contains(narrowest, "suggesti "), false)
	mustEqual(t, "the column still has its name", strings.Contains(narrowest, "NAME"), true)
}

// With nothing chosen there is no apply label, and the status has the row to
// itself: it was still cut 28 columns short, which lost the reason of a failure.
func TestStatusHasTheWholeRowWhenNothingIsChosen(t *testing.T) {
	m, _, _ := newMenu(t, one(raw("x")))
	mustEqual(t, "room with no label", m.leftOfApplyLabel(100, 0, 0), 94)

	x := openSized(t, listRows, listCols, flowData()).press('u').run()

	mustShow(t, x.final(), "nothing to undo here: u takes back a mute, a lock")
}

// A skill and an agent of one name, in one tool, from one place: the category is
// the only thing that tells them apart, and the name says it.
func TestSameNameToolAndSourceSaysTheCategory(t *testing.T) {
	rows := []report.Row{raw("review"), raw("review", category(scan.CatAgent)), raw("solo")}
	m, _, _ := newMenu(t, one(rows...))

	names := map[string]int{}
	for _, r := range m.rows {
		names[m.displayName(r)]++
	}

	for _, want := range []string{"review (" + defaultTool + " · personal · skill)", "review (" + defaultTool + " · personal · agent)", "solo"} {
		mustEqual(t, "rows named "+want, names[want], 1)
	}
}

// On a narrow receipt a gap stays between a long name and its amount.
func TestReceiptLineKeepsAGapBeforeTheAmount(t *testing.T) {
	m, _, _ := newMenu(t, one(raw("a-very-long-skill-name-that-goes-on-and-on-forever", tokens(1480))))

	line := m.receiptLine(Pick{Row: m.rows[0], Verb: vDelete}, 27)

	at := strings.Index(line, "~")
	if at < 1 {
		t.Fatalf("no amount on the line: %q", line)
	}
	mustEqual(t, "the cell before the amount in "+line, line[at-1], byte(' '))
	mustEqual(t, "the line keeps its width", runes(line), 27)
}

// Rows and groups of equal weight keep one order from launch to launch: the
// sorts had no tie-break and inherited the order a map happened to give.
func TestEqualWeightsHaveAFixedOrder(t *testing.T) {
	rows := []report.Row{raw("b"), raw("a"), raw("y", source("plugin:zeta@m")), raw("x", source("plugin:alpha@m"))}
	m, _, _ := newMenu(t, one(rows...))

	var flat, groups []string
	for _, r := range m.flatList()[0].rows {
		flat = append(flat, r.Name)
	}
	for _, g := range m.bySource() {
		groups = append(groups, g.name)
		if g.name == "your own" {
			mustEqual(t, "rows of one group", g.rows[0].Name+g.rows[1].Name, "ab")
		}
	}

	mustEqual(t, "flat list", strings.Join(flat, " "), "a b x y")
	mustEqual(t, "groups", strings.Join(groups, " | "), "your own | alpha | zeta")
}

// The keep list goes by category and name. A lock on one copy is a lock on
// every copy, in any marketplace or tool: the list shows it at once, and the
// receipt says it before the y.
func TestALockIsEveryCopysLock(t *testing.T) {
	rows := []report.Row{
		raw("common", source("plugin:foo@mktA")), raw("common", source("plugin:foo@mktB")),
		raw("common", category(scan.CatAgent)), raw("other"),
	}
	m, _, _ := newMenu(t, one(rows...))
	by := map[string]*Row{}
	for _, r := range m.rows {
		by[r.cat()+" "+r.Source] = r
	}
	a, b, agent := by["skill plugin:foo@mktA"], by["skill plugin:foo@mktB"], by["agent personal"]

	said := newReceiptView(m, []Pick{{a, vLock}}, "tok").warnings
	mustEqual(t, "warnings", strings.Join(said, " | "), "⚠ a lock goes by name: every copy of common is locked")
	mustEqual(t, "warnings with both chosen", len(newReceiptView(m, []Pick{{a, vLock}, {b, vLock}}, "tok").warnings), 0)

	m.settle([]Outcome{{Pick: Pick{a, vLock}}})

	mustEqual(t, "the other copy is locked", b.Kept, true)
	mustEqual(t, "an agent of that name is locked", agent.Kept, false)

	m.settle([]Outcome{{Pick: Pick{a, vUnlock}}})

	mustEqual(t, "the other copy is unlocked with it", m.applied[b], vUnlock)
	_, touched := m.applied[agent]
	mustEqual(t, "the agent is touched", touched, false)
}

// Two rows in quarantine under one name say where each was and its id: the
// receipt of an undo can then be checked against what comes back.
func TestTwoPrunedRowsOfOneNameSayWhichIsWhich(t *testing.T) {
	d := one(raw("other"))
	d.Pruned = []prune.Entry{
		{ID: "001", Name: "foo:common", Category: "skill", Platform: defaultTool, Source: "plugin:foo@mktA"},
		{ID: "002", Name: "foo:common", Category: "skill", Platform: defaultTool},
	}
	m, _, _ := newMenu(t, d)

	var names []string
	for _, r := range m.rows {
		if isPruned(r) {
			names = append(names, m.displayName(r))
		}
	}

	mustEqual(t, "names", strings.Join(names, " | "),
		"foo:common ("+defaultTool+" · quarantine · foo@mktA, id 001) | foo:common ("+defaultTool+" · quarantine · id 002)")
}

// The name comes first on a receipt line: an amount too wide for the window
// loses its money, then its end, before the name loses its place.
func TestReceiptLineKeepsTheNameBesideAHugeAmount(t *testing.T) {
	m, _, _ := newMenu(t, one(raw("alpha", tokens(4_000_000))))

	for _, pw := range []int{34, 22} {
		line := m.receiptLine(Pick{Row: m.rows[0], Verb: vPrune}, pw)

		mustContain(t, line, "alpha ")
		mustEqual(t, "width at "+itoa(pw), runes(line), pw)
	}
}

// Two plugins of one name from two marketplaces: the card says which.
func TestCardSourceNamesTheMarketplaceWhenItTellsApart(t *testing.T) {
	rows := []report.Row{raw("common", source("plugin:foo@mktA")), raw("common", source("plugin:foo@mktB")), raw("solo", source("plugin:bar@mktA"))}
	m, _, _ := newMenu(t, one(rows...))

	var got []string
	for _, r := range m.rows {
		got = append(got, m.cardSource(r))
	}
	slices.Sort(got)

	mustEqual(t, "sources", strings.Join(got, " | "), "plugin:bar | plugin:foo@mktA | plugin:foo@mktB")
}

// A lock goes first in an apply. With a prune of another copy of the name in
// the same apply, which of the two won depended on their weights.
func TestALockIsAppliedBeforeTheRest(t *testing.T) {
	rows := []report.Row{raw("dup", tokens(500)), raw("dup", platform("codex"), tokens(50))}
	m, _, _ := newMenu(t, one(rows...))
	p := newPlan()
	for _, r := range m.rows {
		verb := vPrune
		if r.Platform == "codex" {
			verb = vLock
		}
		p.set(r, verb)
	}

	picks := m.receiptPicks(p)

	mustEqual(t, "first", picks[0].Verb+" "+picks[0].Row.Platform, vLock+" codex")
}

// Rows of one name and one weight keep one order too: by tool, then by source.
func TestTwinsOfOneWeightHaveAFixedOrder(t *testing.T) {
	twins := []report.Row{
		raw("same", source("plugin:tool@mktb")), raw("same", source("plugin:tool@mkta")),
		raw("same", platform("codex")), raw("same"),
	}
	want := ""
	for _, order := range [][]int{{0, 1, 2, 3}, {3, 2, 1, 0}, {1, 3, 0, 2}} {
		var rows []report.Row
		for _, i := range order {
			rows = append(rows, twins[i])
		}
		m, _, _ := newMenu(t, one(rows...))
		got := ""
		for _, r := range m.flatList()[0].rows {
			got += r.Platform + "/" + r.Source + " "
		}
		if want == "" {
			want = got
		}
		mustEqual(t, "order", got, want)
	}
}

// The screen for a platform with nothing left to review has q on it too, and q
// asks before it drops choices made on the other platforms.
func TestQuitFromAnEmptyPlatformAsksAboutPendingChoices(t *testing.T) {
	rows := []report.Row{raw("here"), raw("there", platform("codex"), verdict(report.VerdictKeep))}
	m, s, _ := newMenu(t, one(rows...))
	l := &list{m: m, p: newPlan()}
	l.p.set(m.rows[0], vPrune)
	l.plat = 1

	s.Feed('q', 'n')
	stay := l.emptyScreen()

	mustEqual(t, "stays after n", stay, true)
	mustEqual(t, "choices kept", l.p.size(), 1)
}

// Keys typed while an apply is at work are thrown away: a q pressed to give up
// on a slow one left the menu the moment it ended, before its failures could
// be read.
func TestKeysTypedWhileApplyingAreThrownAway(t *testing.T) {
	x := open(t, flowData())
	x.m.now = clockAt(0, 0, time.Second)

	x.press(space, applyKey, 'y', 'y').run()

	mustEqual(t, "flushes", x.s.Flushes, 2)
}

// The receipt's big figure gives way to a short one when the words beside it
// would not fit, as the header's does: at 58 columns they were cut.
func TestReceiptFigureFitsItsWords(t *testing.T) {
	m, _, _ := newMenu(t, one(raw("alpha", tokens(810_000))))
	words := "tokens to save every session"

	mustEqual(t, "narrow", m.fitBig("810,000", "810k", words, 54), "810k")
	mustEqual(t, "wide", m.fitBig("810,000", "810k", words, 120), "810,000")
}

// A name of marks alone takes no cell: it is shown as a row with no name, not
// as a blank one that can be chosen unread.
func TestANameThatTakesNoCellIsShownAsNoName(t *testing.T) {
	if got := NewRow(raw("́̂")).Name; got != noName {
		t.Errorf("name = %q", got)
	}
}

// A choice that could not be carried out is still chosen after the apply: with
// its cause put right, the same key applies it again. It used to be cleared,
// and had to be found and chosen once more.
func TestAFailedChoiceStaysChosen(t *testing.T) {
	x := open(t, flowData())
	x.a.fail = map[string]error{"srv": errors.New("permission denied")}
	x.m.now = clockAt(0, 0, time.Second)

	x.press(space, applyKey, 'y', 'y').run()

	mustEqual(t, "applies", len(x.a.calls), 1)
	mustContain(t, strings.Join(x.final(), "\n"), "apply 1 change")
}

// An apply started from the screen after the last row says what came of it, as
// the list and the card do: it said nothing, and a refusal went unseen.
func TestAnApplyFromTheLastScreenShowsItsResult(t *testing.T) {
	x := open(t, flowData())
	x.a.fail = map[string]error{"srv": errors.New("permission denied")}
	x.m.now = clockAt(0, 0, time.Second)

	keys := append([]term.Key{space}, on(goneCard, down, applyKey, 'y', 'y')...) // choose, walk past the last row, apply there
	x.press(keys...).run()

	mustShow(t, x.frame("That was the last list.", "not done"), "srv: permission denied")
}

// After an apply started on the screen between two lists, that screen counts
// what was just applied: it said "nothing chosen" over an empty bar, beside a
// line saying "✓ 2 gone".
func TestTheScreenBetweenListsCountsWhatWasApplied(t *testing.T) {
	x := openSized(t, 26, 90, flowData())

	x.press(space, right, down, enter, down, down, applyKey, 'y').run()

	shown := x.frame("left to choose", "✓")
	mustShow(t, shown, "2 of 2 chosen")
	mustNotShow(t, shown, "nothing chosen")
}

// The result of an apply started there stops short of the apply label, as on
// the list: the label was drawn over it and the text's tail showed after it.
func TestTheResultOnTheScreenBetweenListsStopsBeforeTheLabel(t *testing.T) {
	x := openSized(t, 26, 80, flowData())
	x.a.fail = map[string]error{"srv": errors.New("another reap is changing this directory; wait for it to finish"), "old-notes": errors.New("another reap is changing this directory; wait for it to finish")}

	x.press(space, right, down, enter, down, down, applyKey, 'y').run()

	line := lineWith(x.frame("not done", "left to choose"), "not done")
	at := strings.Index(line, "apply 2 changes")
	if at < 0 || strings.TrimSpace(line[at+len("apply 2 changes"):]) != "" {
		t.Errorf("the label is not the end of the line: %q", line)
	}
}

// "Saved" is for what was applied. A choice not applied yet, or one that
// failed, would save: the header said "you saved" for both.
func TestTheHeaderSaysSavedOnlyForWhatWasApplied(t *testing.T) {
	chosen := open(t, flowData()).press(space).wait().run()
	mustShow(t, chosen.final(), "you would save ~600 tokens per session")
	mustNotShow(t, chosen.final(), "you saved")

	x := open(t, flowData())
	x.m.now = clockAt(0, 0, time.Second)
	x.press(space, applyKey, 'y', 'y').wait().run()
	mustShow(t, x.final(), "you saved ~600 tokens per session")

	failing := open(t, flowData())
	failing.a.fail = map[string]error{"srv": errors.New("permission denied")}
	failing.m.now = clockAt(0, 0, time.Second)
	failing.press(space, applyKey, 'y', 'y').wait().run()
	mustShow(t, failing.final(), "you saved ~100, would save ~500 more tokens per session")
}

// The two are told apart, each with its own figure: a choice taken back eased
// the figure down under "you saved" with nothing applied, and a lock chosen
// after an apply turned what was saved into "would save".
func TestTheHeaderNeverCallsSavedWhatWasNot(t *testing.T) {
	undone := open(t, flowData()).press(space).wait().press(space).wait().run()
	for n, frame := range undone.s.frames {
		if line := lineWith(frame, "you saved"); line != "" {
			t.Fatalf("frame %d, nothing applied: %q", n, line)
		}
	}

	locked := open(t, flowData())
	locked.m.now = clockAt(0, 0, time.Second)
	locked.press(space, applyKey, 'y', 'y').wait().press('l').wait().run()
	mustShow(t, locked.final(), "you saved ~600 tokens per session")
	mustNotShow(t, locked.final(), "would save")

	m := open(t, pricedData()).m
	mustEqual(t, "money", m.savedMoney("$1", "$5", "", "")[0], "you saved at least $1, would save at least $5 more a month")
	m.subscriber = true
	mustEqual(t, "flat plan", m.savedMoney("$1", "$5", "", "")[0], "you saved $1, would save $5 more a month, at API prices")
	mustEqual(t, "flat plan, applied", m.savedMoney("$1", "", "", "")[0], "you saved $1 a month, at API prices")
}

// A sum under one unit of the currency is not "0" in the short figure: the
// receipt said "$0 saved every month" under a line that said $0.09.
func TestTheShortMoneyFigureIsNeverAFalseZero(t *testing.T) {
	m := open(t, flowData()).m
	for v, want := range map[float64]string{0.08: "<" + m.cur.Sym + "1", 0: m.cur.Sym + "0", 12.6: m.cur.Sym + "13", 1.99: m.cur.Sym + "2", 999.4: m.cur.Sym + "999", 999.96: m.cur.Sym + m.short(1000), 2500: m.cur.Sym + m.short(2500)} {
		if got := m.shortMoney(v); got != want {
			t.Errorf("shortMoney(%v) = %q, want %q", v, got, want)
		}
	}
}

// In plain text a title is cut at the window, not at what the big font could
// have drawn: the name was cut mid-word with the whole of it on the row below.
func TestAPlainTitleIsCutAtTheWindowOnly(t *testing.T) {
	d := flowData()
	d.Report.Rows[0].Name = "a-very-long-skill-name-indeed"
	x := open(t, d)
	x.m.plain = true
	if got := x.m.fitTitle("a-very-long-skill-name-indeed", 60); got != "a-very-long-skill-name-indeed" {
		t.Errorf("plain: %q", got)
	}
	x.m.plain = false
	if got := x.m.fitTitle("a-very-long-skill-name-indeed", 60); got == "a-very-long-skill-name-indeed" {
		t.Errorf("big font: the whole name in 60 cells: %q", got)
	}
}

// Where the locale cannot draw the big font (its blocks all come out "#"), the
// figures and the titles are plain text, on the line of the words beside them.
func TestWithoutBlockCharactersTheBigFontIsPlainText(t *testing.T) {
	noBlocks := func(frame []string) {
		t.Helper()
		for _, line := range frame {
			if strings.ContainsAny(line, "▀▄") { // the font's half blocks; a bar is made of whole ones
				t.Errorf("a line in the big font: %q", line)
			}
		}
	}
	list := open(t, flowData())
	list.m.plain = true
	list.press(space).wait().run()
	noBlocks(list.final())
	if line := lineWith(list.final(), "tokens wasted every session"); !strings.ContainsAny(line, "0123456789") {
		t.Errorf("the figure is not on the line of its words: %q", line)
	}

	card := open(t, flowData())
	card.m.plain = true
	card.press(on(srvCard, 'p')...).run()
	noBlocks(card.final())
	mustShow(t, card.final(), "srv")

	receipt := open(t, flowData())
	receipt.m.plain = true
	receipt.press(space, applyKey).run()
	shown := receipt.frame("Do it?")
	noBlocks(shown)
	if line := lineWith(shown, "tokens to save every session"); !strings.ContainsAny(line, "0123456789") {
		t.Errorf("the receipt's figure is not on the line of its words: %q", line)
	}

	between := openSized(t, 26, 90, flowData())
	between.m.plain = true
	between.press(right, down, enter, down, down).run()
	noBlocks(between.final())
}

// A lock applied from the screen between lists is counted there like every
// other apply: it said "nothing chosen" beside "✓ 1 locked".
func TestTheScreenBetweenListsCountsALock(t *testing.T) {
	x := openSized(t, 26, 90, flowData())

	x.press(on(dailyCard, 'l', down, applyKey, 'y')...).run()

	shown := x.frame("✓ 1 locked")
	mustShow(t, shown, "1 of 1 chosen")
	mustNotShow(t, shown, "nothing chosen")
}

// Where the locale cannot draw a currency's symbol, the amount carries the
// currency's code: "¥" came out as "JPY" for a sum in yuan, and a symbol the
// table did not know as "?".
func TestAPlainLocaleNamesTheCurrencyByItsCode(t *testing.T) {
	yuan := Currency{Code: "CNY", Sym: "¥", Rate: 7, Decimal: ".", Group: ",", Digits: 2}
	if got := inLocale(yuan, true).Sym; got != "CNY" {
		t.Errorf("yuan in a plain locale: %q", got)
	}
	if got := inLocale(yuan, false).Sym; got != "¥" {
		t.Errorf("yuan where it can be drawn: %q", got)
	}
	if got := inLocale(USD, true).Sym; got != "US$" {
		t.Errorf("a symbol that is plain already: %q", got)
	}
}

// The green line is said whole in the window it has: at 80 columns "you saved
// at least US$12.00, would save at leas" lost the figure it was there for.
func TestTheSavedLineFitsItsWindow(t *testing.T) {
	for _, cols := range []int{58, 80, 120} {
		x := openSized(t, 24, cols, pricedData())
		x.m.now = clockAt(0, 0, time.Second)
		x.press(space, applyKey, 'y', 'y').wait().press(space).wait().run()
		line := strings.TrimRight(lineWith(x.final(), "would save"), " ")
		if line == "" || !(strings.HasSuffix(line, " more") || strings.HasSuffix(line, " more a month")) {
			t.Errorf("%d columns: %q", cols, lineWith(x.final(), "saved"))
		}
	}
	if got := firstFitting(10, "a long sentence", "shorter", "tiny"); got != "shorter" {
		t.Errorf("first fitting: %q", got)
	}
	if got := firstFitting(2, "a long sentence", "shorter", "tiny"); got != "tiny" {
		t.Errorf("nothing fits: %q", got)
	}
}

// The question's words are said whole too, in shorter words where the window
// is narrow (it can shrink while the question is open): "tokens to save every s".
func TestTheReceiptsWordsFitANarrowWindow(t *testing.T) {
	for _, keys := range [][]term.Key{{space, applyKey, term.KeyResize, term.KeyNone}, {tab, space, applyKey, term.KeyResize, term.KeyNone}} {
		x := openSized(t, 30, 80, pricedData())
		x.s.resizeAt = map[int][2]int{len(keys) - 2: {30, 40}}
		x.press(keys...).run()
		var shown []string // the last frame of the question, drawn after the window shrank
		for _, frame := range x.s.frames {
			if lineWith(frame, "Do it?") != "" {
				shown = frame
			}
		}
		if shown == nil {
			t.Fatalf("no frame shows the question: %q", x.final())
		}
		whole := false // the words beside the figure end where they should
		for _, line := range shown {
			line = strings.TrimRight(line, " ")
			whole = whole || strings.HasSuffix(line, " a session") || strings.HasSuffix(line, " a month")
		}
		if !whole {
			t.Errorf("40 columns, the words beside the figure are cut:\n%s", strings.Join(shown, "\n"))
		}
		if line := strings.TrimRight(lineWith(shown, "lighter"), " "); !strings.HasSuffix(line, "session") && !strings.HasSuffix(line, "lighter") {
			t.Errorf("40 columns, the summary: %q", line)
		}
	}
}

// Where the locale cannot draw them, the marks between a figure's digits are
// plain too: a no-break space between thousands came out "1?344".
func TestAPlainLocaleWritesFiguresWithPlainMarks(t *testing.T) {
	czech := Currency{Code: "CZK", Sym: "Kč", Rate: 25, Decimal: ",", Group: " ", Digits: 2}
	plain := inLocale(czech, true)
	if plain.Group != " " || plain.Decimal != "," || plain.Sym != "CZK" {
		t.Errorf("plain: %+v", plain)
	}
	arabic := inLocale(Currency{Code: "SAR", Sym: "SAR", Rate: 4, Decimal: "٫", Group: "٬", Digits: 2}, true)
	if arabic.Group != " " || arabic.Decimal != "." {
		t.Errorf("plain, marks of another script: %+v", arabic)
	}
	if kept := inLocale(czech, false); kept.Group != " " {
		t.Errorf("where it can be drawn: %+v", kept)
	}
}

// The green line never shows part of a figure: "you would save US$1" stood
// for US$1,200.00 where the window had no room for the rest.
func TestTheSavedLineNeverCutsAFigure(t *testing.T) {
	for cols := 58; cols <= 90; cols++ {
		d := pricedData()
		d.Report.MoneyPerMonth *= 100
		x := openSized(t, 24, cols, d)
		x.m.now = clockAt(0, 0, time.Second)
		x.press(space).wait().run()
		line := strings.TrimRight(lineWith(x.final(), "would save"), " ")
		if line == "" {
			continue // no room: nothing is better than a wrong figure
		}
		said := line[strings.Index(line, "would save"):]
		if !strings.Contains(said, "1,200.00") && !strings.Contains(said, "1.2k") {
			t.Errorf("%d columns: %q", cols, said)
		}
	}
}

// What comes back heavier is said whole on the narrowest receipt: "2 com".
func TestTheReceiptSummaryKeepsWhatComesBackWhole(t *testing.T) {
	m := open(t, flowData()).m
	v := &receiptView{m: m, picks: make([]Pick, 3), tok: 100, back: 2}
	for pw := 30; pw <= 70; pw++ {
		got := v.summary(pw)
		if cells(got) > pw || !strings.HasSuffix(got, "heavier") || !strings.Contains(got, "~100") {
			t.Errorf("%d cells: %q", pw, got)
		}
	}
	v.back = 1
	if got := v.summary(80); got != "3 items · ~100 tokens lighter every session · 1 comes back heavier" {
		t.Errorf("wide: %q", got)
	}
	v.back, v.tok = 0, 0
	if got := v.summary(80); got != "3 items" {
		t.Errorf("nothing lighter: %q", got)
	}
}

// The words beside the big figure are whole whenever the figure was judged
// to fit: the check was one cell short, and "wasted a mont" was drawn.
func TestTheWordsBesideTheBigFigureAreNeverOneLetterShort(t *testing.T) {
	for _, scale := range []float64{1, 10, 100, 1000, 10000, 100000, 1000000} {
		for cols := 66; cols <= 130; cols++ {
			d := pricedData()
			if cols%2 == 0 {
				d.Billing = "stripe_subscription"
			}
			d.Report.MoneyPerMonth *= scale
			x := openSized(t, 24, cols, d)
			x.run()
			if line := strings.TrimRight(lineWith(x.final(), "wasted a mont"), " "); line != "" && !strings.HasSuffix(line, "wasted a month") {
				t.Errorf("x%v at %d columns: %q", scale, cols, line)
			}
		}
	}
}

// The size the receipt asks for is the smallest that shows it, whatever the
// window is now: at 9 rows it asked for 29, and drew at 19.
func TestTheReceiptAsksForTheSizeItNeeds(t *testing.T) {
	keys := []term.Key{space, applyKey, term.KeyResize, term.KeyNone}
	x := openSized(t, 30, 80, flowData())
	x.s.resizeAt = map[int][2]int{len(keys) - 2: {9, 80}}
	x.press(keys...).run()
	if !x.saw("window too small: needs 40x19") {
		t.Errorf("at 9 rows: %q", x.final()[0])
	}
}

// The last form of the receipt's summary fits the narrowest receipt with
// long figures too, and still says "lighter" while there is room.
func TestTheReceiptSummaryFitsWithLongFigures(t *testing.T) {
	m := open(t, flowData()).m
	v := &receiptView{m: m, picks: make([]Pick, 99), tok: 1234567, back: 12}
	if got := v.summary(34); cells(got) > 34 || !strings.HasSuffix(got, "12 heavier") || !strings.Contains(got, "tok less") {
		t.Errorf("34 cells: %q", got)
	}
	v = &receiptView{m: m, picks: make([]Pick, 2), tok: 5, back: 1}
	if got := v.summary(34); !strings.Contains(got, "2 items · ~5 tok less") {
		t.Errorf("room for the word: %q", got)
	}
}

// Space in a row's card takes reap's own suggestion, as space in the list
// does: the card said "reap suggests mute" (or lock), and space marked the
// row to prune.
func TestSpaceInTheCardTakesReapsSuggestion(t *testing.T) {
	skill := func(v string) *Row {
		r := raw("x", verdict(v), tokens(600))
		r.Category, r.Path, r.Removable = "skill", "/s/x/SKILL.md", true
		return cleanRow(r)
	}
	for name, c := range map[string]struct {
		row    *Row
		chosen string
		want   rune
	}{
		"reap suggests mute":           {skill(report.VerdictMute), "", 'm'},
		"reap suggests lock":           {skill(report.VerdictKeep), "", 'l'},
		"reap suggests prune":          {skill(report.VerdictReap), "", 'p'},
		"the suggestion is chosen":     {skill(report.VerdictMute), vMute, 'n'},
		"another choice is taken over": {skill(report.VerdictMute), vPrune, 'm'},
	} {
		if got := cardSpace(c.row, c.chosen); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

// reap's own choice never mutes a skill that is a link into the folder other
// tools read too: a, space and a click left it out of nothing, though a bulk
// reap mute does. The m key still takes it, and the receipt says whose it is.
func TestReapsOwnChoiceLeavesASharedSkillUnmuted(t *testing.T) {
	r := raw("shared", verdict(report.VerdictMute), tokens(600))
	r.Category, r.Path, r.Removable, r.LinkTarget = "skill", "/h/.claude/skills/shared/SKILL.md", true, "/h/.agents/skills/shared"
	row := cleanRow(r)
	if got := best(row); got != "" {
		t.Errorf("reap's own choice: %q", got)
	}
	if !can(row, vMute) {
		t.Error("a mute by hand is refused")
	}
}

// Where reap is not sure it has no choice of its own to fill in: a on such
// rows chose prune and said "reap chose", and a group held back for want of
// evidence was one key from a bulk prune. A single row is still chosen by
// space: there the person is deciding about one item.
func TestReapFillsInNothingWhereItIsNotSure(t *testing.T) {
	unsure := func(name string) *Row { return cleanRow(raw(name, verdict(report.VerdictReview))) }
	m, _, _ := newMenu(t, one(raw("x")))

	p := newPlan()
	got := m.auto(p, []*Row{unsure("a"), unsure("b")})
	if p.size() != 0 || !strings.Contains(got, "not sure") || !strings.Contains(got, "p, m, l or d") {
		t.Errorf("a on two rows reap is not sure about: %q, %d chosen", got, p.size())
	}
	p = newPlan()
	got = m.auto(p, []*Row{cleanRow(raw("dead")), unsure("a")})
	if p.size() != 1 || !strings.HasPrefix(got, "reap chose: 1 prune") || !strings.Contains(got, "1 left: reap is not sure") {
		t.Errorf("a on a mix: %q, %d chosen", got, p.size())
	}
	if choiceFor(unsure("a"), true) != "" || choiceFor(unsure("a"), false) == "" {
		t.Error("many rows must leave it, one row must take it")
	}
}
