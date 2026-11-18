package report

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// A use is filed under the name as it can be printed. An item whose name holds
// a character that is rewritten for printing is looked up under that form too:
// it read as never used, in every session it was used in.
func TestAnItemIsFoundUnderTheNameItsUsesAreFiledUnder(t *testing.T) {
	name := "bidi\u200fname"
	for _, cat := range []scan.Category{scan.CatSkill, scan.CatAgent, scan.CatMCP} {
		if keys := evidenceKeys(scan.Item{Category: cat, Name: name}); !slices.Contains(keys, scan.SafeText(name)) {
			t.Errorf("%s: %q is not among %q", cat, scan.SafeText(name), keys)
		}
	}
}

// One item fired out of many is "<1%" in every view: the report said 0% where
// the agent view said <1%.
func TestUtilisationBelowOnePercentIsNotZero(t *testing.T) {
	if _, text := utilPct(1, 152, 10); text != "<1%" {
		t.Errorf("utilisation of 1 in 152: %s", text)
	}
	if _, text := utilPct(0, 152, 10); text != "0%" {
		t.Errorf("utilisation of 0 in 152: %s", text)
	}
}

// One day is a day and one session a session, wherever they are counted.
func TestOneDayAndOneSessionAreSingular(t *testing.T) {
	r := &Report{WindowDays: 1, Sessions: 1, MonthlyBasisDays: 0}
	views := map[string]func(*bytes.Buffer){
		"report":     func(b *bytes.Buffer) { RenderText(b, r, false) },
		"agent":      func(b *bytes.Buffer) { RenderAgent(b, r, 0) },
		"gap":        func(b *bytes.Buffer) { RenderGap(b, r, false) },
		"by-project": func(b *bytes.Buffer) { RenderByProject(b, r, false) },
		"markdown":   func(b *bytes.Buffer) { RenderMarkdown(b, r) },
	}
	for name, render := range views {
		var b bytes.Buffer
		render(&b)
		if said := b.String(); strings.Contains(said, "1 days") || strings.Contains(said, "1 sessions") || strings.Contains(said, "last 1d · 1 sessions") {
			t.Errorf("%s:\n%s", name, said)
		}
	}
	if got := windowOf(1, 1); got != "last 1 day · 1 session" {
		t.Errorf("windowOf(1, 1) = %q", got)
	}
}

// The muted line says how many of the muted never fired when not all of them
// did: the three figures above it only close with that number.
func TestMutedLineSaysHowManyNeverFired(t *testing.T) {
	r := &Report{
		DeadCount: 0, MutedCount: 3,
		Gap: &Gap{Loaded: 3, Fired: 1},
		Rows: []Row{
			{Item: scan.Item{Category: scan.CatSkill, Name: "a"}, Verdict: VerdictInfo, Reason: ReasonMuted, Uses: 2},
			{Item: scan.Item{Category: scan.CatSkill, Name: "b"}, Verdict: VerdictInfo, Reason: ReasonMuted},
			{Item: scan.Item{Category: scan.CatSkill, Name: "c"}, Verdict: VerdictInfo, Reason: ReasonMuted},
		},
	}
	if got := NewHeadline(r).MutedLine(); !strings.Contains(got, "3 muted, 2 of them never fired") {
		t.Errorf("muted line: %s", got)
	}
	r.Rows[0].Uses = 0
	if got := NewHeadline(r).MutedLine(); got != "3 muted · reap unmute to list" {
		t.Errorf("muted line with none fired: %s", got)
	}
}

// "rare" has its sentence in reap why, like every other reason.
func TestWhyExplainsRare(t *testing.T) {
	if got := summaryFor(Row{Verdict: VerdictReview, Reason: ReasonRare}); got == ReasonRare || !strings.Contains(got, "before this window") {
		t.Errorf("reason line: %q", got)
	}
}

// The masthead is cut nowhere: what does not fit beside the window goes on a
// line of its own. Cut, it lost the words saying that warnings follow.
func TestMastheadKeepsItsCaveatsWhole(t *testing.T) {
	lines := mastheadLines("skillreaper · last 30 days · 20 sessions", []string{"1 oversized record skipped", "1 warning below"}, 60)
	if joined := strings.Join(lines, "\n"); len(lines) != 2 || !strings.Contains(joined, "1 warning below") || strings.Contains(joined, "…") {
		t.Errorf("masthead: %q", lines)
	}
	if lines := mastheadLines("skillreaper · last 30 days · 20 sessions", []string{"1 warning below"}, 78); len(lines) != 1 {
		t.Errorf("a masthead that fits is one line: %q", lines)
	}
}

// An item never used has no date of last use in the JSON, not year 1.
func TestJSONGivesNoDateToAnItemNeverUsed(t *testing.T) {
	b, err := json.Marshal(Row{Item: scan.Item{Name: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "0001-01-01") {
		t.Errorf("row: %s", b)
	}
}

// The apm proposal counts the sessions of every project, and says so: beside a
// repo's name the number read as that repo's.
func TestAPMSaysWhoseSessionsItCounts(t *testing.T) {
	m := APMReport{Repo: "repo1", WindowDays: 30, Sessions: 21}
	var yaml, md bytes.Buffer
	RenderAPMYAML(&yaml, m)
	RenderAPMMarkdown(&md, m)
	for name, said := range map[string]string{"yaml": yaml.String(), "markdown": md.String()} {
		if !strings.Contains(said, "21 sessions in all projects") {
			t.Errorf("%s:\n%s", name, said)
		}
	}
}

// The route plan's opening keeps its words, with one skill as with six.
func TestRouteCountsItsSurvivors(t *testing.T) {
	if got := survivors(6); got != "6 skills survive" {
		t.Errorf("six: %q", got)
	}
	if got := survivors(1); got != "1 skill survives" {
		t.Errorf("one: %q", got)
	}
}

// Where a skill fired is looked up as its uses are: whatever the case the
// transcript wrote, and under the name in its header. apm told the person to
// drop a skill that had fired in the repo; manifest said "0 projects".
func TestProjectsOfASkillAreFoundAsItsUsesAre(t *testing.T) {
	r := &Report{SkillProjects: map[string]map[string]int{"foo": {"projx": 1}, "fm-name": {"projx": 2}}}
	cased := scan.Item{Category: scan.CatSkill, Name: "Foo"}
	aliased := scan.Item{Category: scan.CatSkill, Name: "folder-name", FrontmatterName: "fm-name"}

	if got := manifestProjects(r, cased); got != 1 {
		t.Errorf("manifest: Foo fired in %d projects", got)
	}
	if got := repoUses(r, cased, "projx") + repoUses(r, aliased, "projx"); got != 3 {
		t.Errorf("apm: %d uses in the repo, want 3", got)
	}
	if got := skillProjects(r, cased); got["projx"] != 1 {
		t.Errorf("route: Foo's projects are %v", got)
	}
}

// The counts that stood in a fixed plural.
func TestOneOfAThingIsSingularInTheRemainingViews(t *testing.T) {
	_, unit := Headline{KnowsFired: true, NeverFired: 1, Loaded: 1}.Figure()
	mustHave := func(got, want string) {
		t.Helper()
		if !strings.Contains(got, want) {
			t.Errorf("%q does not hold %q", got, want)
		}
	}
	mustHave(unit, "item never fired")
	_, unit = Headline{Reaped: 1}.Figure()
	mustHave(unit, "item marked REAP")
	var route, routeMD, why bytes.Buffer
	plan := &RoutePlan{Skipped: true, TotalSkills: 1, MinSkills: 5}
	RenderRoutePlan(&route, plan, false)
	RenderRoutePlanMarkdown(&routeMD, plan)
	mustHave(route.String(), "Only 1 skill survives a prune")
	mustHave(routeMD.String(), "Only 1 skill survives a prune")
	RenderWhy(&why, Explanation{Name: "x", Installed: "2026-11-16", InstalledAgeDays: 1, GraceDays: 1}, false)
	mustHave(why.String(), "(1 day ago)")
	mustHave(why.String(), "(grace: 1 day)")
	mustHave(summaryFor(Row{Verdict: VerdictKeep, Reason: ReasonUsed, Uses: 1}), "used 1 time in the window")
	mustHave(summaryFor(Row{Verdict: VerdictReap, Reason: ReasonBroken, ErrorCount: 1}), "(1 error)")
	mustHave(usageLine(ManifestUsage{Days: 30, Sessions: 1, Uses: 1, Errors: 0, Projects: 1, LastUsed: "never", Verdict: "KEEP"}), "1 session · 1 use · 0 errors · 1 project ·")
}

// A weight that was never measured is not "~0": the line after a prune of MCP
// servers alone says so, and with a measured item among them says "at least".
func TestValueFeedbackDoesNotCallAnUnmeasuredWeightZero(t *testing.T) {
	var none, some, all bytes.Buffer
	RenderValueFeedback(&none, "pruned", 2, 2, 0, 0, 3, false)
	RenderValueFeedback(&some, "pruned", 2, 1, 120, 1200, 3, false)
	RenderValueFeedback(&all, "pruned", 2, 0, 120, 1200, 3, false)
	if said := none.String(); !strings.Contains(said, "weight not measured") || strings.Contains(said, "~0") {
		t.Errorf("nothing measured: %q", said)
	}
	if said := some.String(); !strings.Contains(said, "saving at least ~120 tok/session") {
		t.Errorf("one of two measured: %q", said)
	}
	if said := all.String(); !strings.Contains(said, "saving ~120 tok/session") || strings.Contains(said, "at least") {
		t.Errorf("all measured: %q", said)
	}
	if got := ReclaimSaid("0", 1, 1); got != "weight not measured" {
		t.Errorf("header for one unmeasured item: %q", got)
	}
}

// What share calls dead is what the report calls dead: a skill that fired and
// is only heavy is not "dead context" in a line meant to be posted.
func TestShareCountsDeadContextAsTheReportDoes(t *testing.T) {
	r := &Report{DeadTokensPerSession: 115, MuteTokensPerSession: 218}
	if got := ShareCould(r); got != 115 {
		t.Errorf("share could cut %d, the report says 115 dead", got)
	}
}
