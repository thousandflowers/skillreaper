package report

import (
	"bytes"
	"strings"
	"testing"
)

func TestComputeGap(t *testing.T) {
	r := fixtureReport()
	g := r.Gap
	if g == nil {
		t.Fatal("Gap is nil")
	}

	// fixtureReport inventory (skill/agent/mcp only):
	//   skills: used-skill (uses 4, ~28 tok), dead-skill (uses 0, ~100 tok),
	//           ecc:plan (uses 2 via bare "plan", ~14 tok)
	//   mcp:    deadsrv (uses 0, token weight unknown)
	// prose is excluded.
	if g.Loaded != 4 {
		t.Errorf("Loaded = %d, want 4", g.Loaded)
	}
	if g.Fired != 2 {
		t.Errorf("Fired = %d, want 2", g.Fired)
	}
	// MCP tokens excluded: 28 + 100 + 14 = 142 loaded, 28 + 14 = 42 fired.
	if g.LoadedTok != 142 {
		t.Errorf("LoadedTok = %d, want 142", g.LoadedTok)
	}
	if g.FiredTok != 42 {
		t.Errorf("FiredTok = %d, want 42", g.FiredTok)
	}

	byCat := map[string]GapCat{}
	for _, gc := range g.PerCat {
		byCat[string(gc.Category)] = gc
	}
	if byCat["skill"].Loaded != 3 || byCat["skill"].Fired != 2 {
		t.Errorf("skill = %+v, want Loaded 3 Fired 2", byCat["skill"])
	}
	if byCat["mcp"].Loaded != 1 || byCat["mcp"].Fired != 0 {
		t.Errorf("mcp = %+v, want Loaded 1 Fired 0", byCat["mcp"])
	}
	if byCat["mcp"].LoadedTok != 0 {
		t.Errorf("mcp LoadedTok = %d, want 0 (unknown)", byCat["mcp"].LoadedTok)
	}
	// prose/hook never appear in PerCat.
	if _, ok := byCat["prose"]; ok {
		t.Error("prose must not appear in Gap")
	}
}

func TestRenderGap(t *testing.T) {
	var buf bytes.Buffer
	RenderGap(&buf, fixtureReport(), false)
	out := buf.String()
	for _, want := range []string{"loaded vs fired", "skills", "mcp", "total", "60 sessions"} {
		if !strings.Contains(out, want) {
			t.Errorf("gap view missing %q", want)
		}
	}
	// MCP token weight is unknown and must render as "?".
	if !strings.Contains(out, "?") {
		t.Error("gap view must mark MCP tokens as ?")
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("color disabled but ANSI codes present")
	}
}

func TestRenderGapNoSessions(t *testing.T) {
	r := fixtureReport()
	r.Sessions = 0
	var buf bytes.Buffer
	RenderGap(&buf, r, false) // must not panic on divide-by-zero
	if !strings.Contains(buf.String(), "n/a") {
		t.Error("expected n/a utilization when no sessions")
	}
}

func TestRenderTextHasGapLine(t *testing.T) {
	var buf bytes.Buffer
	RenderText(&buf, fixtureReport(), false)
	out := buf.String()
	// fixtureReport: 2 of 4 fired → 50% utilization.
	if !strings.Contains(out, "utilization") {
		t.Error("report text missing utilization line")
	}
	if !strings.Contains(out, "50%") {
		t.Errorf("expected 50%% utilization, got:\n%s", out)
	}
	if !strings.Contains(out, "2/4 items fired") {
		t.Error("report text missing fired/loaded counts")
	}
}

func TestRenderGapMarkdown(t *testing.T) {
	var buf bytes.Buffer
	RenderGapMarkdown(&buf, fixtureReport())
	out := buf.String()
	for _, want := range []string{"# loaded vs fired", "| skills |", "| total |", "| Category |"} {
		if !strings.Contains(out, want) {
			t.Errorf("gap markdown missing %q", want)
		}
	}
}

func TestRenderGapJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderGapJSON(&buf, fixtureReport()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `"Loaded": 4`) {
		t.Errorf("gap json missing Loaded: %s", out)
	}
	if !strings.Contains(out, `"Fired": 2`) {
		t.Errorf("gap json missing Fired: %s", out)
	}
}
