package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// What "recoverable with reap mute" counts is what reap mute takes: a heavy,
// rarely used skill inside a plugin is not, and the same goes for the weekly
// reminder's "N items to mute".
func TestMuteFiguresCountWhatMuteTakes(t *testing.T) {
	inPlugin := Row{Item: scan.Item{Category: scan.CatSkill, Name: "ecc:a", Path: "/p/a/SKILL.md", Source: "plugin:ecc@m"}, Verdict: VerdictMute, Tokens: 108}
	own := Row{Item: scan.Item{Category: scan.CatSkill, Name: "b", Path: "/s/b/SKILL.md", Removable: true}, Verdict: VerdictMute, Tokens: 100}

	if n, _ := Mutable([]Row{inPlugin, inPlugin}); n != 0 {
		t.Errorf("%d plugin skills counted as mutable", n)
	}
	var none, one bytes.Buffer
	renderMuteSavings(&none, &Report{MuteCount: 2, MuteTokensPerSession: 216, Rows: []Row{inPlugin, inPlugin}}, 100, painter(false))
	if said := none.String(); strings.Contains(said, "recoverable with reap mute") || !strings.Contains(said, "reap why") {
		t.Errorf("only plugin skills to mute:\n%s", said)
	}
	renderMuteSavings(&one, &Report{MuteCount: 2, MuteTokensPerSession: 208, Rows: []Row{inPlugin, own}}, 100, painter(false))
	if said := one.String(); !strings.Contains(said, "1 heavy low-use skill,") || !strings.Contains(said, "recoverable with reap mute") {
		t.Errorf("one of two is reap's to mute:\n%s", said)
	}
}

// More counts that stood in a fixed plural.
func TestOneOfAThingIsSingularCycle21(t *testing.T) {
	var route, agent bytes.Buffer
	RenderRoutePlanMarkdown(&route, &RoutePlan{TotalSkills: 1, Sessions: 3, BelowFloor: true})
	if said := route.String(); !strings.Contains(said, "1 skill is below") {
		t.Errorf("route markdown:\n%s", said)
	}
	RenderGapAgent(&agent, &Report{Sessions: 3, WindowDays: 30, MCPPayload: []PayloadRow{{}}})
	if said := agent.String(); strings.Contains(said, "1 tools") {
		t.Errorf("gap --agent:\n%s", said)
	}
	if got := toolSurface(1); got != "1 tool" {
		t.Errorf("manifest tool surface: %q", got)
	}
	if got := graceLeft(1, 14); !strings.Contains(got, "1 day left in the 14-day grace period") {
		t.Errorf("why: %q", got)
	}
}

// The plan's table by source does not print "~0 tok" for weight that was
// never measured.
func TestSourceTableMarksUnmeasuredWeight(t *testing.T) {
	rows := []Row{
		{Item: scan.Item{Name: "a", Source: "personal", Removable: true}, Tokens: 67},
		{Item: scan.Item{Name: "srv", Source: "user-config", Removable: true}, WeightUnknown: true},
	}
	var b bytes.Buffer
	RenderSourceTotals(&b, SourceTotals(rows), 2)
	if said := b.String(); strings.Contains(said, "~0 tok") || !strings.Contains(said, "?") {
		t.Errorf("table:\n%s", said)
	}
}

// What the report says can be done about a MUTE row is what reap does: a
// skill reached through a link outside the folders reap edits is not told
// "run: reap mute", which refuses it; a plugin's is, since naming it works.
func TestWhyDoesNotSendAMuteThatIsRefused(t *testing.T) {
	linked := Row{Item: scan.Item{Category: scan.CatSkill, Name: "sh-heavy", Path: "/s/sh-heavy/SKILL.md", ViaLink: "/shared/sh-heavy"}, Verdict: VerdictMute}
	if got := recommendationFor(linked, 12, ExplainInput{}); strings.Contains(got, "run: reap mute") || !strings.Contains(got, "link") {
		t.Errorf("a linked skill: %q", got)
	}
	inPlugin := Row{Item: scan.Item{Category: scan.CatSkill, Name: "pl:pheavy", Path: "/p/SKILL.md", Source: "plugin:pl@mk"}, Verdict: VerdictMute}
	if got := recommendationFor(inPlugin, 12, ExplainInput{}); !strings.Contains(got, "run: reap mute") || !strings.Contains(got, "update") {
		t.Errorf("a plugin's skill: %q", got)
	}
	var b bytes.Buffer
	renderMuteSavings(&b, &Report{MuteCount: 2, Rows: []Row{linked, inPlugin}}, 100, painter(false))
	if said := b.String(); strings.Contains(said, "cannot mute") || strings.Contains(said, "inside plugins") || !strings.Contains(said, "reap why") {
		t.Errorf("gap line:\n%s", said)
	}
}

// The DO block does not call an unmeasured weight "~0 tok", as the prune it
// points at does not.
func TestDoBlockDoesNotCallAnUnmeasuredWeightZero(t *testing.T) {
	srv := Row{Item: scan.Item{Category: scan.CatMCP, Name: "beta", Removable: true}, Verdict: VerdictReap, WeightUnknown: true}
	dead := Row{Item: scan.Item{Category: scan.CatSkill, Name: "d", Path: "/s/d/SKILL.md", Removable: true}, Verdict: VerdictReap, Tokens: 962}
	var only, mixed bytes.Buffer
	renderActions(&only, &Report{Rows: []Row{srv}}, 100, painter(false))
	if said := only.String(); strings.Contains(said, "~0 tok") || !strings.Contains(said, "weight not measured") {
		t.Errorf("only an unmeasured item:\n%s", said)
	}
	renderActions(&mixed, &Report{Rows: []Row{srv, dead}}, 100, painter(false))
	if said := mixed.String(); !strings.Contains(said, "at least ~962 tok/session reclaimed") {
		t.Errorf("one of two measured:\n%s", said)
	}
}

// A name in the route plan's Markdown cannot break its table.
func TestRouteMarkdownEscapesNames(t *testing.T) {
	var b bytes.Buffer
	RenderRoutePlanMarkdown(&b, &RoutePlan{TotalSkills: 200, Sessions: 3, Exposed: []RouteSkill{{Name: "a|b<i>"}}, Categories: []RouteCategory{{Name: "c|d", Source: "project", Skills: []RouteSkill{{Name: "e|f"}}}}})
	if said := b.String(); strings.Contains(said, "| a|b<i> |") || strings.Contains(said, "| e|f |") {
		t.Errorf("route markdown:\n%s", said)
	}
}

// In reap why --json a weight that was never measured is told apart from 0.
func TestWhyJSONMarksAnUnmeasuredWeight(t *testing.T) {
	e := BuildExplanation(Row{Item: scan.Item{Category: scan.CatMCP, Name: "srv"}, Verdict: VerdictReap, WeightUnknown: true}, 12, ExplainInput{})
	if !e.WeightUnknown {
		t.Error("weight_unknown is not set")
	}
}

// A prose file has no description: reap why gives its weight as the file's.
func TestWhyDoesNotCallAProseFileADescription(t *testing.T) {
	var b bytes.Buffer
	RenderWhy(&b, Explanation{Name: "prose:CLAUDE.md", Category: string(scan.CatProse), TokenWeight: 9, DescChars: 31}, false)
	if said := b.String(); strings.Contains(said, "description") || !strings.Contains(said, "31 chars") {
		t.Errorf("why:\n%s", said)
	}
}
