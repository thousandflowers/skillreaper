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
	if said := none.String(); strings.Contains(said, "recoverable with reap mute") || !strings.Contains(said, "/plugin") {
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
