package menu

// A locked row refuses prune, mute and delete until it is unlocked, as the
// command line refuses a kept item. The prototype had no such guard: this is a
// decision made after it, and the prototype is the side that was wrong.

import (
	"testing"

	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/term"
)

const lockedWhy = "it is locked: u unlocks it first"

func kept(r *report.Row) { r.Kept = true }

// lockedData is the flow fixture with a row the user locked before: reap keeps
// it, so it sits under "suggest to lock" after daily.
func lockedData() Data {
	d := flowData()
	d.Report.Rows = append(d.Report.Rows, raw("pinned", verdict(report.VerdictKeep), reason(reasonKeep), tokens(80), kept))
	return d
}

// onPinned puts the cursor on the locked row.
var onPinned = []term.Key{down, down, right, down, down}

func TestLockedRowRefusesWhatWouldTakeItAway(t *testing.T) {
	locked := map[string]*Row{
		"kept by hand":            row("k", kept),
		"kept, as the report has": row("k", verdict(report.VerdictKeep), reason(reasonKeep), kept),
		"its reason alone":        row("k", reason(reasonKeep)),
		"kept, inside a plugin":   row("k", source(fooPlugin), notRemovable, kept),
		"kept and muted already":  row("k", reason(reasonKeep), kept, noDescription),
	}
	for name, r := range locked {
		t.Run(name, func(t *testing.T) {
			for _, verb := range []string{vPrune, vDelete, vMute} {
				mustEqual(t, "can "+verb, can(r, verb), false)
				mustEqual(t, "why not "+verb, whyNot(r, verb), lockedWhy)
			}
			mustEqual(t, "can lock", can(r, vLock), true)
		})
	}

	t.Run("plugin off still takes a locked row of the plugin", func(t *testing.T) {
		mustEqual(t, "can disable", can(locked["kept, inside a plugin"], vDisable), true)
	})

	t.Run("a locked row outside a plugin is refused plugin off for that reason", func(t *testing.T) {
		mustEqual(t, "why not", whyNot(locked["kept by hand"], vDisable), "it is not in a plugin")
	})

	t.Run("a row that is not locked is not refused", func(t *testing.T) {
		for _, verb := range []string{vPrune, vDelete, vMute} {
			mustEqual(t, "can "+verb, can(row("free"), verb), true)
		}
	})

	t.Run("a choice to lock, not yet applied, refuses nothing", func(t *testing.T) {
		m, free, p := rulesMenu(t), row("free"), newPlan()
		p.set(free, vLock)

		m.actOn(p, []*Row{free}, 'p')

		mustEqual(t, "choice", p.get(free), vPrune)
	})

	t.Run("the card greys the three, and keeps lock", func(t *testing.T) {
		want := []option{{choiceNothing, true}, {vLock, true}, {vMute, false}, {vPrune, false}, {vDelete, false}}

		got := cardOptions(locked["kept by hand"])

		for n := range want {
			mustEqual(t, "option "+want[n].label, got[n], want[n])
		}
	})

	t.Run("reap chooses nothing for a row it keeps and you locked", func(t *testing.T) {
		mustEqual(t, "best", best(locked["kept, as the report has"]), "")
	})
}

func TestLockedRowInTheList(t *testing.T) {
	for name, key := range map[string]term.Key{"d": 'd', "p": 'p', "m": 'm'} {
		t.Run(name+" on the row is refused, and the row says why in full", func(t *testing.T) {
			verb := map[term.Key]string{'d': "delete", 'p': "prune", 'm': "mute"}[key]
			done := map[term.Key]string{'d': "deleted", 'p': "pruned", 'm': "muted"}[key]

			x := open(t, lockedData()).press(append(onPinned, key)...).run()

			mustEqual(t, "status", statusText(x.final()), "1 cannot be "+done+": pinned: "+lockedWhy)
			mustContain(t, lineWith(x.final(), "pinned"), "[ ]")
			mustContain(t, lineWith(x.final(), "pinned"), "can't "+verb)
			mustNotShow(t, x.final(), "apply")
		})
	}

	t.Run("nothing reaches the applier for it", func(t *testing.T) {
		x := open(t, lockedData()).press(append(onPinned, 'd', applyKey, 'y', 'p', applyKey, 'y', 'm', applyKey, 'y')...).run()

		mustApply(t, x)
	})

	t.Run("space and a have nothing to choose on it", func(t *testing.T) {
		spaced := open(t, lockedData()).press(append(onPinned, space)...).run()
		auto := open(t, lockedData()).press(append(onPinned, 'a')...).run()

		mustEqual(t, "space said", statusText(spaced.final()), "nothing to select here: "+lockedWhy)
		mustContain(t, statusText(auto.final()), "reap chose nothing")
		mustNotShow(t, spaced.final(), "apply")
		mustNotShow(t, auto.final(), "apply")
	})

	t.Run("the hint under it says how to get at it", func(t *testing.T) {
		x := open(t, lockedData()).press(onPinned...).run()

		mustShow(t, x.final(), "pinned · "+lockedWhy)
		mustContain(t, lineWith(x.final(), "pinned"), "locked")
	})

	t.Run("d on its group takes the row beside it and names the locked one", func(t *testing.T) {
		x := open(t, lockedData()).press(down, down, 'd', right, applyKey, 'y').run()

		mustEqual(t, "said", x.saw("1 deleted · 1 cannot be deleted: pinned: it is locked"), true)
		mustApply(t, x, []string{"delete daily"})
	})

	t.Run("d on everything selected deletes all but the locked row and the pruned one", func(t *testing.T) {
		selectAll := []term.Key{'g', 'g', 'v', term.KeyEnd} // flat, then mark from the first line to the last

		marked := open(t, lockedData()).press(append(selectAll, 'd')...).run()
		applied := open(t, lockedData()).press(append(selectAll, 'd', applyKey, 'y')...).run()

		mustContain(t, statusText(marked.final()), "⚠ 6 marked for delete: cannot be restored · 2 cannot be deleted")
		mustContain(t, lineWith(marked.final(), "pinned"), "[ ]")
		mustContain(t, lineWith(marked.final(), "pinned"), "can't delete")
		mustNotShow(t, applied.frame(doIt), "pinned")
		mustApply(t, applied, []string{"delete srv", "delete acme:lint", "delete acme:fmt", "delete old-notes", "delete daily", "delete quiet"})
	})

	t.Run("p and m on everything selected leave it out too", func(t *testing.T) {
		for _, key := range []term.Key{'p', 'm'} {
			x := open(t, lockedData()).press('g', 'g', 'v', term.KeyEnd, key, applyKey, 'y').run()

			for _, call := range x.a.calls {
				for _, pk := range call {
					if pk.Row.Raw().Name == "pinned" {
						t.Errorf("%c on a selection handed the applier %s for the locked row", key, pk.Verb)
					}
				}
			}
			mustEqual(t, "applies", len(x.a.calls), 1)
		}
	})

	t.Run("u unlocks it, and that is what is applied", func(t *testing.T) {
		x := open(t, lockedData()).press(append(onPinned, 'u', applyKey, 'y')...).run()

		mustEqual(t, "receipt", x.saw(doIt, "undo     pinned"), true)
		mustApply(t, x, []string{"unlock pinned"})
	})
}

func TestLockedRowOnItsCard(t *testing.T) {
	card := func(t *testing.T, keys ...term.Key) *session {
		return open(t, lockedData()).press(append(append([]term.Key(nil), onPinned...), enter)...).press(keys...).run()
	}

	t.Run("it stands on lock and says who locked it", func(t *testing.T) {
		x := card(t)

		mustShow(t, x.final(), "pinned · skill · personal · claude-code", "locked: reap never suggests touching it", "You protected it with reap keep.")
	})

	for name, key := range map[string]term.Key{"d": 'd', "p": 'p', "m": 'm'} {
		t.Run(name+" stands on the grey option, which says it is locked", func(t *testing.T) {
			verb := map[term.Key]string{'d': "delete", 'p': "prune", 'm': "mute"}[key]

			x := card(t, key, applyKey, 'y')

			mustEqual(t, "said why", x.saw("pinned · skill", verb+" is not available: "+lockedWhy), true)
			mustNotShow(t, x.final(), "apply") // shift+enter had nothing to apply and went back to the list
			mustApply(t, x)
		})
	}

	t.Run("d does not ring: nothing was marked", func(t *testing.T) {
		x := card(t, 'd')

		mustEqual(t, "bells", x.s.Beeps, 0)
	})

	t.Run("the arrows can stand on the grey options and change nothing", func(t *testing.T) {
		x := card(t, right, right, right, applyKey, 'y') // from lock: mute, prune, delete

		for _, verb := range []string{"mute", "prune", "delete"} {
			mustEqual(t, "stood on "+verb, x.saw("pinned · skill", verb+" is not available: "+lockedWhy), true)
		}
		mustApply(t, x)
		mustEqual(t, "bells", x.s.Beeps, 0)
	})

	t.Run("space and a choose nothing", func(t *testing.T) {
		x := card(t, space, 'a', applyKey, 'y')

		mustNotShow(t, x.final(), "apply")
		mustApply(t, x)
	})

	t.Run("u unlocks: reap may suggest touching it again", func(t *testing.T) {
		x := card(t, 'u')

		mustShow(t, x.final(), "reap may suggest touching it again", oneChange)
	})

	t.Run("nothing, on the options row, is the unlock too", func(t *testing.T) {
		x := card(t, left, applyKey, 'y') // from lock to nothing

		mustApply(t, x, []string{"unlock pinned"})
	})
}

func TestRowLockedInThisSessionIsLockedAtOnce(t *testing.T) {
	lockSrv := []term.Key{right, down, 'l', applyKey, 'y'} // srv, which reap suggests pruning, locked and applied

	t.Run("it shows as locked and stays in the list", func(t *testing.T) {
		x := play(t, lockSrv...)

		mustApply(t, x, []string{"lock srv"})
		mustContain(t, lineWith(x.final(), "srv"), "locked")
	})

	for name, key := range map[string]term.Key{"p": 'p', "d": 'd', "m": 'm'} {
		t.Run(name+" on it is refused from then on", func(t *testing.T) {
			x := play(t, append(lockSrv, key, applyKey, 'y')...)

			mustApply(t, x, []string{"lock srv"})
		})
	}

	own := map[string]func(*session) *session{
		"space": func(x *session) *session { return x.press(space) },
		"a":     func(x *session) *session { return x.press('a') },
	}
	for name, choose := range own {
		t.Run(name+" on it chooses nothing: reap's own choice is refused like any other", func(t *testing.T) {

			x := choose(open(t, flowData()).press(lockSrv...)).press(applyKey, 'y').run()

			mustApply(t, x, []string{"lock srv"})
		})
	}
}

// A click takes best(r), as space does: with no choice to take, it sets nothing.
func TestReapHasNoChoiceOfItsOwnForALockedRow(t *testing.T) {
	// Arrange
	x := open(t, flowData())
	var srv *Row
	for _, r := range x.m.rows {
		if r.Name == "srv" {
			srv = r
		}
	}
	if srv == nil || best(srv) == "" {
		t.Fatalf("the fixture's srv should start with a suggestion, got %v", srv)
	}

	// Act
	srv.Kept = true

	// Assert
	mustEqual(t, "reap's choice for a locked row", best(srv), "")
}
