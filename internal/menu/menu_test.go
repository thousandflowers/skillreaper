package menu

// The checks here are check_plan of experiments/menu-prototype/test_menu.py,
// whose asserts are decisions made one by one. Same inputs, same expected values;
// a row is keyed by its pointer where the prototype used a tuple.

import (
	"slices"
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/prune"
	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/scan"
)

func TestAutoRemembersWhatStoodBefore(t *testing.T) {
	m, _, _ := newMenu(t, Data{Report: fixtureReport()})
	rows := byName(t, m)
	pruneme, muteme, odd := rows["pruneme"], rows["muteme"], rows["odd"]

	t.Run("a prune set by hand that matches reap survives one a", func(t *testing.T) {
		// a, n, space, a.
		p := newPlan()
		m.auto(p, []*Row{pruneme})
		p.pop(pruneme)
		p.set(pruneme, vPrune)

		m.auto(p, []*Row{pruneme})

		mustEqual(t, "choice", p.get(pruneme), vPrune)
	})

	t.Run("a lock cleared with n stays gone", func(t *testing.T) {
		// l, a, n, m, a.
		p := newPlan()
		p.set(muteme, vLock)
		m.auto(p, []*Row{muteme})
		p.pop(muteme)
		p.set(muteme, vMute)

		m.auto(p, []*Row{muteme})

		mustEqual(t, "choice", p.get(muteme), vMute)
	})

	t.Run("a second a puts back what the boxes held, also for a row given twice", func(t *testing.T) {
		p := newPlan()
		p.set(muteme, vLock)
		given := []*Row{muteme, pruneme, muteme}

		m.auto(p, given)
		mustEqual(t, "muteme after a", p.get(muteme), vMute)
		mustEqual(t, "pruneme after a", p.get(pruneme), vPrune)
		undone := m.auto(p, given)

		mustEqual(t, "message", undone, "reap's choice undone")
		mustEqual(t, "muteme after the second a", p.get(muteme), vLock)
		mustEqual(t, "pruneme still chosen", p.has(pruneme), false)
	})

	t.Run("a on a row reap is not sure about takes what a click takes, and gives it back", func(t *testing.T) {
		p := newPlan()

		first := m.auto(p, []*Row{odd})
		mustEqual(t, "choice", p.get(odd), best(odd))
		if !strings.HasPrefix(first, "reap chose: 1 ") {
			t.Errorf("message = %q, want it to start with %q", first, "reap chose: 1 ")
		}
		second := m.auto(p, []*Row{odd})

		mustEqual(t, "message", second, "reap's choice undone")
		mustEqual(t, "still chosen", p.has(odd), false)
	})

	t.Run("a over a lock, then a again, gives the lock back", func(t *testing.T) {
		p := newPlan()
		p.set(odd, vLock)

		m.auto(p, []*Row{odd})
		mustEqual(t, "choice after a", p.get(odd), best(odd))
		m.auto(p, []*Row{odd})

		mustEqual(t, "choice after the second a", p.get(odd), vLock)
	})

	t.Run("F034: the second a puts back what stood before that press, not the first", func(t *testing.T) {
		p := newPlan()
		p.set(muteme, vLock)
		m.auto(p, []*Row{muteme})
		m.auto(p, []*Row{muteme, pruneme})

		m.auto(p, []*Row{muteme, pruneme})

		mustEqual(t, "muteme", p.get(muteme), vMute)
		mustEqual(t, "pruneme still chosen", p.has(pruneme), false)
	})
}

func TestGroupKeyReachesEveryRowButTheOnesReapKeeps(t *testing.T) {
	m, _, _ := newMenu(t, Data{Report: fixtureReport()})
	kept := byName(t, m)["lockme"]
	stuck := row("stuck", source("plugin:pl"), category(scan.CatMCP))
	plain := row("plain", verdict(report.VerdictReview))

	t.Run("the rows reap would lock are left out", func(t *testing.T) {
		got := rowItems(line{isGroup: true, name: "x", rows: []*Row{stuck, plain, kept}})

		if !slices.Equal(got, []*Row{stuck, plain}) {
			t.Errorf("got %d rows, want stuck and plain", len(got))
		}
	})

	t.Run("a group of nothing else is acted on whole", func(t *testing.T) {
		got := rowItems(line{isGroup: true, name: "x", rows: []*Row{kept}})

		if !slices.Equal(got, []*Row{kept}) {
			t.Errorf("got %d rows, want the kept one", len(got))
		}
	})

	t.Run("a single row is itself", func(t *testing.T) {
		got := rowItems(line{row: kept, rows: []*Row{stuck, kept}})

		if !slices.Equal(got, []*Row{kept}) {
			t.Errorf("got %d rows, want the row alone", len(got))
		}
	})
}

func TestReapsOwnChoice(t *testing.T) {
	m, _, _ := newMenu(t, Data{Report: fixtureReport()})
	rows := byName(t, m)
	muted := row("was", verdict(report.VerdictInfo), reason(reasonMuted))

	t.Run("on a muted row reap has none: u undoes the mute", func(t *testing.T) {
		mustEqual(t, "best", best(muted), "")
		if got := m.auto(newPlan(), []*Row{muted}); !strings.HasPrefix(got, "reap chose nothing") {
			t.Errorf("a said %q, want it to start with %q", got, "reap chose nothing")
		}
	})

	t.Run("it locks what it would keep and mutes what is heavy and rare", func(t *testing.T) {
		mustEqual(t, "best(lockme)", best(rows["lockme"]), vLock)
		mustEqual(t, "best(muteme)", best(rows["muteme"]), vMute)
	})

	t.Run("a muted plugin row can still be pruned: it is the user's choice", func(t *testing.T) {
		plug := row("pl:x", verdict(report.VerdictInfo), source("plugin:pl"), reason(reasonMuted))

		mustEqual(t, "can prune", can(plug, vPrune), true)
		mustEqual(t, "can delete", can(plug, vDelete), true)
	})
}

func TestPrunedRowOnlyTakesUndo(t *testing.T) {
	m, _, _ := newMenu(t, Data{Report: fixtureReport()})
	muteme := byName(t, m)["muteme"]
	gone := prunedRow(prune.Entry{ID: "012", Name: "old", Category: "skill", To: "/q/old"})

	t.Run("it is listed as pruned and reap chooses nothing for it", func(t *testing.T) {
		mustEqual(t, "suggestion", suggestion(gone), reasonPruned)
		mustEqual(t, "best", best(gone), "")
		mustEqual(t, "read-only", readOnly(gone), false)
		mustEqual(t, "prune id", gone.PruneID, "012")
	})

	t.Run("u brings it back", func(t *testing.T) {
		mustEqual(t, "existing", existing(gone), vPrune)
		mustEqual(t, "undo of a prune", existingUndo[vPrune], vRestore)
	})

	t.Run("no other verb applies", func(t *testing.T) {
		for _, verb := range []string{vPrune, vMute, vLock, vDelete} {
			if can(gone, verb) {
				t.Errorf("can(%s) = true, want only u to apply", verb)
			}
		}
		mustContain(t, whyNot(gone, vMute), "u brings it back")
	})

	t.Run("an undo reads as undo on the row", func(t *testing.T) {
		mustEqual(t, "tag of a restore", rowTag(gone, vRestore, ""), "undo")
		mustEqual(t, "tag of an unmute", rowTag(muteme, vUnmute, ""), "undo")
	})
}

func TestPlanSummaryAddsUpByActionInFixedOrder(t *testing.T) {
	t.Run("every choice counted under its action", func(t *testing.T) {
		p := newPlan()
		for name, verb := range map[string]string{"a": vLock, "b": vPrune, "c": vUnmute, "d": vPrune, "e": vRestore, "f": vDisable} {
			p.verbs[row(name)] = verb
		}

		mustEqual(t, "summary", planSummary(p), "prune 2 · plugin off 1 · lock 1 · undo 2")
	})

	t.Run("nothing chosen says nothing", func(t *testing.T) {
		mustEqual(t, "summary", planSummary(newPlan()), "")
	})
}

func TestIsEchoTellsARepeatFromNews(t *testing.T) {
	cases := []struct {
		msg  string
		echo bool
	}{
		{"43 pruned", true},
		{"76 pruned · 9 muted", true},
		{"reap chose: 3 prune · 1 mute", true},
		{"1 cannot be muted: it is pruned", false},
		{"⚠ 3 marked for delete: cannot be restored", false},
	}
	for _, c := range cases {
		t.Run(c.msg, func(t *testing.T) {
			mustEqual(t, "isEcho", isEcho(c.msg), c.echo)
		})
	}
}

func TestGroupKeyNamesWhatItLeftOut(t *testing.T) {
	m, _, _ := newMenu(t, Data{Report: fixtureReport()})
	pruneme := byName(t, m)["pruneme"]
	left := row("srv2", verdict(report.VerdictReview), category(scan.CatMCP), noDescription)
	p := newPlan()

	msg := m.actOn(p, []*Row{pruneme, left}, 'm')

	t.Run("the message says what was done and what could not be, and why", func(t *testing.T) {
		mustContain(t, msg, "1 muted")
		mustContain(t, msg, "1 cannot be muted: srv2: an MCP server has nothing to mute, p prunes it")
	})

	t.Run("the refused row is not chosen, and is remembered as refused", func(t *testing.T) {
		mustEqual(t, "chosen", p.has(left), false)
		mustEqual(t, "rows refused", len(p.refused), 1)
		mustEqual(t, "verb refused", p.refused[left], vMute)
	})

	t.Run("the refused row says so, the other does not", func(t *testing.T) {
		mustEqual(t, "tag of the refused row", rowTag(left, "", p.refused[left]), "can't mute")
		if got := rowTag(pruneme, vMute, ""); got == "can't mute" {
			t.Errorf("tag of the muted row = %q", got)
		}
	})

	t.Run("the next key forgets it", func(t *testing.T) {
		m.actOn(p, []*Row{pruneme}, 'n')

		mustEqual(t, "rows refused", len(p.refused), 0)
	})
}

func TestPluginOffIsAllOrNothing(t *testing.T) {
	t.Run("F043 F129: another choice on one row ends it for the whole plugin, and says so once", func(t *testing.T) {
		a, b := row("pa", source(fooPlugin)), row("pb", source(fooPlugin))
		p := newPlan()
		p.set(a, vDisable)
		p.set(b, vDisable)

		p.set(a, vLock)

		mustEqual(t, "choices left", p.size(), 1)
		mustEqual(t, "choice on the row", p.get(a), vLock)
		mustEqual(t, "plugin whose off ended", p.takeEnded(), fooPlugin)
		mustEqual(t, "asked again", p.takeEnded(), "")
	})

	t.Run("F211: clearing one row clears them all", func(t *testing.T) {
		a, b := row("pa", source(fooPlugin)), row("pb", source(fooPlugin))
		p := newPlan()
		p.set(a, vDisable)
		p.set(b, vDisable)

		p.pop(b)

		mustEqual(t, "choices left", p.size(), 0)
	})

	t.Run("another plugin's off is untouched", func(t *testing.T) {
		a, other := row("pa", source(fooPlugin)), row("qa", source("plugin:bar@m"))
		p := newPlan()
		p.set(a, vDisable)
		p.set(other, vDisable)

		p.pop(a)

		mustEqual(t, "other plugin", p.get(other), vDisable)
	})
}

func TestUsingWarningSaysWhatIsChosenAgainstTheEvidence(t *testing.T) {
	used := row("u", uses(2))
	kept := row("k", verdict(report.VerdictKeep))
	unused := row("n")
	cases := []struct {
		name  string
		picks []Pick
		want  string
	}{
		{"F208: one in use", picks(used, vPrune), "⚠ 1 of these is in use"},
		{"one that fires and one reap would lock", picks(used, vPrune, kept, vMute), "⚠ 2 of these are in use"},
		{"a lock takes nothing away", picks(used, vLock, unused, vPrune), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mustEqual(t, "warning", usingWarning(c.picks), c.want)
		})
	}
}

func TestPrunedEntriesAreListedAsRows(t *testing.T) {
	d := Data{Report: &report.Report{Sessions: 9}, Pruned: []prune.Entry{
		{ID: "a1", Name: "old-skill", Category: "skill", To: "/q/old-skill"},
		{ID: "b2", Name: "old-server", Category: "mcp", Platform: "codex"},
		{ID: "c3"},
	}}
	m, _, _ := newMenu(t, d)
	rows := byName(t, m)

	t.Run("each is a row that weighs nothing, under already pruned", func(t *testing.T) {
		mustEqual(t, "rows", len(m.rows), 3)
		for _, r := range m.rows {
			mustEqual(t, "suggestion of "+r.Name, suggestion(r), reasonPruned)
			mustEqual(t, "weight of "+r.Name, r.Tokens, 0)
		}
	})

	t.Run("it keeps its manifest id, its kind and its tool", func(t *testing.T) {
		server := rows["old-server"]

		mustEqual(t, "id", server.PruneID, "b2")
		mustEqual(t, "kind", server.cat(), "mcp")
		mustEqual(t, "tool", server.Platform, "codex")
		mustEqual(t, "path with no quarantine path", server.Path, "b2")
	})

	t.Run("an older entry is a Claude Code skill, and one with no name still shows", func(t *testing.T) {
		mustEqual(t, "tool", rows["old-skill"].Platform, defaultTool)
		mustEqual(t, "path", rows["old-skill"].Path, "/q/old-skill")
		mustEqual(t, "name", rows[""].Name, noName)
		mustEqual(t, "kind", rows[""].cat(), "skill")
	})
}
