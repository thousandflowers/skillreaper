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
