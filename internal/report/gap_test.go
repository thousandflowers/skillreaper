package report

import "testing"

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
