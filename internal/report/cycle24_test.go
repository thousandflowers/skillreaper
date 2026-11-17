package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// With two tools the dead-token figure is an average and the rows under it
// are not. The plain report said so; --agent and --md printed the same pair
// of numbers with no word.
func TestTheAverageOverToolsIsSaidInEveryFormat(t *testing.T) {
	r := &Report{Sessions: 23, WindowDays: 30, DeadTokensPerSession: 115, PlatformSessions: map[string]int{"claude-code": 12, "codex": 11}, Gap: &Gap{Loaded: 2}}
	one := &Report{Sessions: 12, WindowDays: 30, DeadTokensPerSession: 115, PlatformSessions: map[string]int{"claude-code": 12}, Gap: &Gap{Loaded: 2}}
	for name, render := range map[string]func(*bytes.Buffer, *Report){
		"agent": func(b *bytes.Buffer, r *Report) { RenderAgent(b, r, 10) },
		"md":    func(b *bytes.Buffer, r *Report) { RenderMarkdown(b, r) },
	} {
		var two, single bytes.Buffer
		render(&two, r)
		render(&single, one)
		if !strings.Contains(strings.ToLower(two.String()), "an average over the 23 sessions of 2 tools") {
			t.Errorf("%s, two tools:\n%s", name, two.String())
		}
		if strings.Contains(single.String(), "average over") {
			t.Errorf("%s, one tool:\n%s", name, single.String())
		}
	}
}

// A skill that runs only by hand has no description in the context: a mute
// of it saves nothing and takes the text its slash menu shows.
func TestAManualOnlySkillIsNotReapsToMute(t *testing.T) {
	row := Row{Item: scan.Item{Category: scan.CatSkill, Name: "byhand", Path: "/s/byhand/SKILL.md", Removable: true, ManualOnly: true}, Verdict: VerdictReap}
	if MuteEligible(row) {
		t.Error("eligible to mute")
	}
	row.ManualOnly = false
	if !MuteEligible(row) {
		t.Error("an ordinary unused skill is not eligible")
	}
}

// A category with nothing in it has no row in gap --agent, as in the plain
// view: "mcp 0 0 n/a" and a note about tool schemas, on a stack with no server.
func TestGapAgentLeavesOutEmptyCategories(t *testing.T) {
	r := &Report{Sessions: 3, WindowDays: 30, Rows: []Row{{Item: scan.Item{Category: scan.CatSkill, Name: "a"}, Tokens: 10, Uses: 1}}}
	r.Gap = computeGap(r.Rows)
	var b bytes.Buffer
	RenderGapAgent(&b, r)
	if said := b.String(); strings.Contains(said, "mcp") || strings.Contains(said, "REACH n/a means") || !strings.Contains(said, "skill") {
		t.Errorf("gap --agent:\n%s", said)
	}
}

// Two tools can each have a skill of one name: route told them apart nowhere.
func TestRouteTellsApartTwoSkillsOfOneName(t *testing.T) {
	r := &Report{Sessions: 10, PlatformSessions: map[string]int{"claude-code": 5, "codex": 5}, Rows: []Row{
		{Item: scan.Item{Category: scan.CatSkill, Name: "shared", Platform: "claude-code"}, Uses: 5, Tokens: 10},
		{Item: scan.Item{Category: scan.CatSkill, Name: "shared", Platform: "codex"}, Uses: 4, Tokens: 10},
		{Item: scan.Item{Category: scan.CatSkill, Name: "alone", Platform: "codex"}, Uses: 4, Tokens: 10},
	}}
	plan := BuildRoutePlan(r, 0)
	var text, md, js bytes.Buffer
	RenderRoutePlan(&text, plan, false)
	RenderRoutePlanMarkdown(&md, plan)
	if err := RenderRoutePlanJSON(&js, plan); err != nil {
		t.Fatal(err)
	}
	for name, said := range map[string]string{"text": text.String(), "markdown": md.String()} {
		if !strings.Contains(said, "shared (claude-code)") || !strings.Contains(said, "shared (codex)") || strings.Contains(said, "alone (") {
			t.Errorf("%s:\n%s", name, said)
		}
	}
	if said := js.String(); !strings.Contains(said, `"platform": "codex"`) || strings.Contains(said, "shared (") {
		t.Errorf("json:\n%s", said)
	}
}

// reap why speaks of CLAUDE.md for a skill only (nothing else is protected by
// it), and says when the name is in it as a plain word that protects nothing.
func TestWhySaysWhatCLAUDEMDDoesForThisItem(t *testing.T) {
	skill := Row{Item: scan.Item{Category: scan.CatSkill, Name: "review", Path: "/s/review/SKILL.md", Removable: true}, Verdict: VerdictReap, Reason: ReasonUnused}
	var plain, none, server bytes.Buffer
	RenderWhy(&plain, BuildExplanation(skill, 10, ExplainInput{ClaudeMDPlain: true}), false)
	if said := plain.String(); !strings.Contains(said, "a plain word") || !strings.Contains(said, "reap keep skill:review") || strings.Contains(said, "not referenced") {
		t.Errorf("a plain word:\n%s", said)
	}
	RenderWhy(&none, BuildExplanation(skill, 10, ExplainInput{}), false)
	if said := none.String(); !strings.Contains(said, "not referenced") {
		t.Errorf("not in it:\n%s", said)
	}
	mcp := Row{Item: scan.Item{Category: scan.CatMCP, Name: "review", Removable: true}, Verdict: VerdictReap, Reason: ReasonUnused}
	RenderWhy(&server, BuildExplanation(mcp, 10, ExplainInput{}), false)
	if said := server.String(); strings.Contains(said, "claude-md") {
		t.Errorf("a server:\n%s", said)
	}
}
