package menu

// check_verbs of the prototype's test_menu.py (F074, F254, F016): what the menu
// offers is what can be carried out, and every refusal says why in one line.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/scan"
)

// verbRows is the cast of check_verbs: one row for each way a verb can be refused.
type verbRows struct {
	skill, agent, server           *Row // three kinds of row inside one plugin
	mine, linked, shared, near     *Row
	folder, prose, toml, bare, was *Row
}

func newVerbRows() verbRows {
	inPlugin := []opt{source(fooPlugin), notRemovable}
	with := func(extra ...opt) []opt { return append(slices.Clone(inPlugin), extra...) }
	skill := row("ps", inPlugin...)
	return verbRows{
		skill:  skill,
		agent:  row("pa", with(category(scan.CatAgent))...),
		server: row("pm", with(category(scan.CatMCP))...),
		mine:   row("mine"),
		linked: row("via", notRemovable, func(r *report.Row) { r.ViaLink, r.RootDir = "/x/via", "/h/.codex" }),
		shared: row("sh", notRemovable, func(r *report.Row) { r.Path, r.RootDir = "/h/.agents/skills/sh/SKILL.md", "/h/.codex" }),
		near:   row("near", with(func(r *report.Row) { r.ViaLink, r.RootDir = "/h/.agents/skills/near", "/h/.claude" })...),
		folder: row("fl", notRemovable, func(r *report.Row) { r.ViaLink, r.FolderLink, r.RootDir = "/x/fl", true, "/h/.codex" }),
		prose:  row("rules", category(scan.CatProse), notRemovable),
		toml:   row("t", category(scan.CatMCP), notRemovable),
		bare:   row("nodesc", with(noDescription)...),
		was:    derive(skill, func(r *report.Row) { r.Name = "ps-muted" }, reason(reasonMuted), noDescription),
	}
}

func TestSuggestionDidNotChange(t *testing.T) {
	v := newVerbRows()
	cases := []struct {
		name string
		row  *Row
		want string
	}{
		{"own items prune", v.mine, vPrune},
		{"plugin skills mute", v.skill, vMute},
		{"plugin agents mute", v.agent, vMute},
		{"a plugin's server is its plugin's to turn off", v.server, suggestPlugin},
		{"a link is the user's to choose", v.linked, suggestNone},
		{"a shared folder is the user's to choose", v.shared, suggestNone},
		{"prose is only weighed", v.prose, suggestRO},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mustEqual(t, "suggestion", suggestion(c.row), c.want)
		})
	}
}

func TestMenuOffersWhatCanBeCarriedOut(t *testing.T) {
	v := newVerbRows()

	t.Run("F074: reap's own choice is something it can do", func(t *testing.T) {
		mustEqual(t, "plugin skill", best(v.skill), vMute)
		mustEqual(t, "own skill", best(v.mine), vPrune)
		mustEqual(t, "plugin server", best(v.server), vPrune)
		mustEqual(t, "linked skill", best(v.linked), vPrune)
		mustEqual(t, "shared skill", best(v.shared), vMute)
	})

	t.Run("F254: prune, delete and lock fit every row reap can edit", func(t *testing.T) {
		for _, r := range []*Row{v.mine, v.skill, v.agent, v.server, v.linked, v.shared, v.near, v.was} {
			for _, verb := range []string{vPrune, vDelete, vLock} {
				if !can(r, verb) {
					t.Errorf("%s cannot take %s: %s", r.Name, verb, whyNot(r, verb))
				}
			}
		}
	})

	t.Run("the card offers a plugin skill all five", func(t *testing.T) {
		want := []option{{choiceNothing, true}, {vLock, true}, {vMute, true}, {vPrune, true}, {vDelete, true}}

		if got := cardOptions(v.skill); !slices.Equal(got, want) {
			t.Errorf("options = %v, want %v", got, want)
		}
	})

	t.Run("the card greys mute on a server", func(t *testing.T) {
		want := []option{{choiceNothing, true}, {vLock, true}, {vMute, false}, {vPrune, true}, {vDelete, true}}

		if got := cardOptions(v.server); !slices.Equal(got, want) {
			t.Errorf("options = %v, want %v", got, want)
		}
	})
}

func TestEveryRefusalSaysWhyInOneLine(t *testing.T) {
	v := newVerbRows()
	refused := []struct {
		name string
		row  *Row
		verb string
		why  string
	}{
		{"a server has nothing to mute", v.server, vMute, "nothing to mute"},
		{"nothing to strip without a description", v.bare, vMute, "no description"},
		{"muted once is muted", v.was, vMute, "muted already"},
		{"a link out of the folders reap may edit", v.linked, vMute, "outside"},
		{"a link that is the whole skills folder cannot be pruned", v.folder, vPrune, "whole"},
		{"nor deleted", v.folder, vDelete, "whole"},
		{"a server in a file reap cannot edit", v.toml, vPrune, "by hand"},
		{"prose cannot be pruned", v.prose, vPrune, "by hand"},
		{"nor muted", v.prose, vMute, "by hand"},
		{"nor deleted", v.prose, vDelete, "by hand"},
		{"plugin off needs a plugin", v.mine, vDisable, "not in a plugin"},
	}
	for _, c := range refused {
		t.Run(c.name, func(t *testing.T) {
			mustEqual(t, "can", can(c.row, c.verb), false)
			mustContain(t, whyNot(c.row, c.verb), c.why)
		})
	}

	allowed := []struct {
		name string
		row  *Row
		verb string
	}{
		{"a link into a folder reap knows can be muted", v.near, vMute},
		{"a skill in a shared folder can be muted", v.shared, vMute},
		{"prose can be locked", v.prose, vLock},
		{"a plugin row can go with its plugin", v.skill, vDisable},
	}
	for _, c := range allowed {
		t.Run(c.name, func(t *testing.T) {
			if !can(c.row, c.verb) {
				t.Errorf("refused: %s", whyNot(c.row, c.verb))
			}
		})
	}
}

func TestReadOnlyRowsShowABoxOnlyOnceLocked(t *testing.T) {
	v := newVerbRows()

	t.Run("no box on a row nothing can take away", func(t *testing.T) {
		mustEqual(t, "box", stateBox(v.prose, newPlan()), "   ")
	})
	t.Run("a locked one shows its letter", func(t *testing.T) {
		p := newPlan()
		p.set(v.prose, vLock)

		mustEqual(t, "box", stateBox(v.prose, p), "[l]")
	})
	t.Run("a linked row has a box like any other", func(t *testing.T) {
		mustEqual(t, "box", stateBox(v.linked, newPlan()), "[ ]")
	})
	t.Run("what counts as read-only", func(t *testing.T) {
		for r, want := range map[*Row]bool{v.prose: true, v.toml: true, v.linked: false, v.shared: false, v.skill: false} {
			mustEqual(t, "readOnly("+r.Name+")", readOnly(r), want)
		}
	})
}

func TestKeyOnAGroupTakesEveryRowThatCan(t *testing.T) {
	m := rulesMenu(t)
	v := newVerbRows()

	t.Run("p prunes what it can and names the one it cannot", func(t *testing.T) {
		p := newPlan()

		msg := m.actOn(p, []*Row{v.skill, v.server, v.mine, v.prose}, 'p')

		for _, r := range []*Row{v.skill, v.server, v.mine} {
			mustEqual(t, "choice on "+r.Name, p.get(r), vPrune)
		}
		mustEqual(t, "prose chosen", p.has(v.prose), false)
		mustContain(t, msg, "3 pruned")
		mustContain(t, msg, "1 cannot be pruned: rules: ")
	})

	t.Run("m mutes what it can, leaves a muted row as it is, names the server", func(t *testing.T) {
		p := newPlan()

		msg := m.actOn(p, []*Row{v.skill, v.server, v.was, v.mine}, 'm')

		mustEqual(t, "plugin skill", p.get(v.skill), vMute)
		mustEqual(t, "own skill", p.get(v.mine), vMute)
		mustEqual(t, "server chosen", p.has(v.server), false)
		mustContain(t, msg, "1 cannot be muted: pm: ")
	})

	t.Run("d deletes a muted row like any other", func(t *testing.T) {
		p := newPlan()

		msg := m.actOn(p, []*Row{v.was, v.skill}, 'd')

		if strings.Contains(msg, "cannot be deleted") {
			t.Errorf("message = %q", msg)
		}
		mustEqual(t, "muted row", p.get(v.was), vDelete)
		mustEqual(t, "plugin skill", p.get(v.skill), vDelete)
	})

	t.Run("more than one delete is said as a warning", func(t *testing.T) {
		msg := m.actOn(newPlan(), []*Row{v.was, v.skill}, 'd')

		mustEqual(t, "message", msg, "⚠ 2 marked for delete: cannot be restored")
	})

	t.Run("u on a row with nothing to take back says what u is for", func(t *testing.T) {
		msg := m.actOn(newPlan(), []*Row{v.mine}, 'u')

		mustContain(t, msg, "u takes back")
		mustEqual(t, "message", msg, "nothing to undo here: u takes back a mute, a lock or a prune")
	})

	t.Run("u takes back a mute", func(t *testing.T) {
		p := newPlan()

		msg := m.actOn(p, []*Row{v.was, v.mine}, 'u')

		mustEqual(t, "message", msg, "1 undone")
		mustEqual(t, "muted row", p.get(v.was), vUnmute)
		mustEqual(t, "own skill chosen", p.has(v.mine), false)
	})

	t.Run("a verb the row already is goes back to that", func(t *testing.T) {
		p := newPlan()
		p.set(v.was, vPrune)

		m.actOn(p, []*Row{v.was}, 'm')

		mustEqual(t, "muted row chosen", p.has(v.was), false)
	})
}

func TestPluginsAKeyCanName(t *testing.T) {
	v := newVerbRows()
	other := derive(v.skill, source("plugin:bar@m"))

	t.Run("o names every plugin in a mixed group, never an id that is not one", func(t *testing.T) {
		got := pluginsIn([]*Row{v.skill, v.mine, other, row("bad", source("plugin:$(x)"))})

		if !slices.Equal(got, []string{"plugin:bar@m", fooPlugin}) {
			t.Errorf("plugins = %v", got)
		}
	})

	t.Run("one plugin is found from any of its rows", func(t *testing.T) {
		src, id, ok := pluginTarget([]*Row{v.skill, v.agent})

		mustEqual(t, "found", ok, true)
		mustEqual(t, "source", src, fooPlugin)
		mustEqual(t, "id", id, "foo@m")
	})

	t.Run("an own row is in no plugin", func(t *testing.T) {
		_, _, ok := pluginTarget([]*Row{v.mine})

		mustEqual(t, "found", ok, false)
	})

	t.Run("a plugin row and an own row are not one plugin", func(t *testing.T) {
		_, _, ok := pluginTarget([]*Row{v.skill, v.mine})

		mustEqual(t, "found", ok, false)
	})
}

func TestPluginRowIsCountedLikeAnyOther(t *testing.T) {
	m, _, _ := newMenu(t, Data{Report: &report.Report{}})
	skill := newVerbRows().skill
	l := newList(m)
	l.p.set(skill, vPrune)

	t.Run("its saving is its weight, on the row and on its group", func(t *testing.T) {
		mustEqual(t, "saving", saving(skill, vPrune), 100)
		mustEqual(t, "group saving", m.groupStats([]*Row{skill}, l.p).saved, 100)
	})

	t.Run("once applied it is out of the plan and its saving is kept", func(t *testing.T) {
		m.settle([]Outcome{{Pick: Pick{skill, vPrune}}})
		l.consume()

		mustEqual(t, "applied as", m.applied[skill], vPrune)
		mustEqual(t, "rows applied", len(m.applied), 1)
		mustEqual(t, "choices left", l.p.size(), 0)
	})
}

func TestReceiptSaysWhatComesOfIt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	v := newVerbRows()
	far := derive(v.mine, func(r *report.Row) { r.LinkTarget = "/shared/skills/mine" })
	sharedHome := []string{"/h/.agents/skills"}

	t.Run("a plugin's update can bring back what was pruned or deleted from it", func(t *testing.T) {
		got := updateWarning(picks(v.skill, vPrune, v.server, vDelete, v.mine, vPrune, v.agent, vMute))

		mustEqual(t, "warning", got, "⚠ 2 plugin items may return with an update")
	})
	t.Run("a mute is not undone by an update's files", func(t *testing.T) {
		mustEqual(t, "warning", updateWarning(picks(v.mine, vPrune, v.skill, vMute)), "")
	})
	t.Run("a mute through a link reaches the folder it leads to", func(t *testing.T) {
		if got := sharedFolders(picks(v.near, vMute)); !slices.Equal(got, sharedHome) {
			t.Errorf("folders = %v", got)
		}
	})
	t.Run("a mute in a shared folder reaches that folder", func(t *testing.T) {
		if got := sharedFolders(picks(v.shared, vMute)); !slices.Equal(got, sharedHome) {
			t.Errorf("folders = %v", got)
		}
	})
	t.Run("a prune takes a shared file away, but only the link of a linked one", func(t *testing.T) {
		got := removedForOthers(picks(v.shared, vPrune, v.linked, vPrune, v.mine, vDelete))

		if !slices.Equal(got, sharedHome) {
			t.Errorf("folders = %v", got)
		}
	})
	t.Run("F189: a mute reaches where the skill's own link leads", func(t *testing.T) {
		if got := sharedFolders(picks(far, vMute)); !slices.Equal(got, []string{"/shared/skills"}) {
			t.Errorf("folders = %v", got)
		}
	})
	t.Run("F189: a prune of that skill reaches nowhere else", func(t *testing.T) {
		if got := sharedFolders(picks(far, vPrune)); len(got) != 0 {
			t.Errorf("folders = %v", got)
		}
	})
	t.Run("a folder under home is said from home, once", func(t *testing.T) {
		home, _ := os.UserHomeDir()
		a := derive(v.mine, func(r *report.Row) { r.LinkTarget = filepath.Join(home, ".agents", "skills", "a") })
		b := derive(v.mine, func(r *report.Row) { r.LinkTarget = filepath.Join(home, ".agents", "skills", "b") })

		got := sharedFolders(picks(a, vMute, b, vMute))

		if !slices.Equal(got, []string{"~/.agents/skills"}) {
			t.Errorf("folders = %v", got)
		}
	})
}

func TestRowTagSaysWhatIsTrueOfTheRow(t *testing.T) {
	v := newVerbRows()
	muteLost := derive(v.mine, func(r *report.Row) { r.MuteLost = true })
	pruneLost := derive(v.skill, func(r *report.Row) { r.PruneLost = true })
	cases := []struct {
		name   string
		row    *Row
		choice string
		want   string
	}{
		{"F027: a mute an update undid", muteLost, "", "mute lost"},
		{"being muted stays true until apply", derive(v.mine, reason(reasonMuted)), vPrune, "muted"},
		{"prose", v.prose, "", "read-only"},
		{"a linked row has nothing to say", v.linked, "", ""},
		{"a delete cannot be undone, so it is said", v.mine, vDelete, "⚠ delete"},
		{"locked by hand", derive(v.mine, func(r *report.Row) { r.Kept = true }), "", "locked"},
		{"a prune an update undid", pruneLost, "", "prune lost"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mustEqual(t, "tag", rowTag(c.row, c.choice, ""), c.want)
		})
	}

	t.Run("F027: a row whose mute or prune was lost is a normal row", func(t *testing.T) {
		mustEqual(t, "best after a lost mute", best(muteLost), vPrune)
		mustEqual(t, "best after a lost prune", best(pruneLost), vMute)
		mustEqual(t, "suggestion after a lost prune", suggestion(pruneLost), vMute)
	})
}

func TestGroupBoxReadsMixedOnlyWhenItIs(t *testing.T) {
	kept, dead := row("k", verdict(report.VerdictKeep)), row("r")

	t.Run("F032: an untouched row reap has no quarrel with does not make it mixed", func(t *testing.T) {
		p := newPlan()
		p.set(dead, vPrune)

		mustEqual(t, "box", groupBox([]*Row{kept, dead}, p), "[p]")
	})
	t.Run("F032: a lock beside an untouched unused row is mixed", func(t *testing.T) {
		p := newPlan()
		p.set(kept, vLock)

		mustEqual(t, "box", groupBox([]*Row{kept, dead}, p), "[-]")
	})
	t.Run("nothing chosen, nothing unused: an empty box", func(t *testing.T) {
		mustEqual(t, "box", groupBox([]*Row{kept}, newPlan()), "[ ]")
	})
}
