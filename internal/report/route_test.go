package report

import (
	"bytes"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

func routeReport() *Report {
	return &Report{
		Sessions: 100,
		Rows: []Row{
			{Item: scan.Item{Category: scan.CatSkill, Name: "frequent"}, Verdict: VerdictKeep, Uses: 50, Tokens: 40},
			{Item: scan.Item{Category: scan.CatSkill, Name: "ecc:rare"}, Verdict: VerdictKeep, Uses: 2, Tokens: 30},
			{Item: scan.Item{Category: scan.CatSkill, Name: "ecc:alsorare"}, Verdict: VerdictReview, Uses: 1, Tokens: 20},
			{Item: scan.Item{Category: scan.CatSkill, Name: "personal-rare"}, Verdict: VerdictKeep, Uses: 3, Tokens: 25},
			{Item: scan.Item{Category: scan.CatSkill, Name: "cold"}, Verdict: VerdictReview, Uses: 0, Tokens: 15},
			{Item: scan.Item{Category: scan.CatSkill, Name: "deadweight"}, Verdict: VerdictReap, Uses: 0, Tokens: 99},
			// non-skill rows are ignored entirely
			{Item: scan.Item{Category: scan.CatMCP, Name: "somemcp"}, Verdict: VerdictReap, Uses: 0, Tokens: 500},
		},
		SkillProjects: map[string]map[string]int{
			"personal-rare": {"-Users-x-Desktop-myrepo": 3},
		},
	}
}

func TestBuildRoutePlanTiers(t *testing.T) {
	plan := BuildRoutePlan(routeReport(), 0.10)

	// REAP skill and the MCP row are excluded; 5 skills survive.
	if plan.TotalSkills != 5 {
		t.Fatalf("TotalSkills: want 5 (REAP + non-skill excluded), got %d", plan.TotalSkills)
	}

	exposed := map[string]RouteSkill{}
	for _, s := range plan.Exposed {
		exposed[s.Name] = s
	}
	if exposed["frequent"].Reason != routeReasonFrequent {
		t.Errorf("frequent skill should be exposed/frequent, got %q", exposed["frequent"].Reason)
	}
	if exposed["cold"].Reason != routeReasonNoEvidence {
		t.Errorf("never-fired skill should be exposed/no-evidence, got %q", exposed["cold"].Reason)
	}
	if _, routed := indexCategorySkill(plan, "deadweight"); routed {
		t.Error("REAP skill must not appear in any router")
	}

	// Token accounting: exposed = frequent(40) + cold(15) = 55.
	if plan.ExposedTok != 55 {
		t.Errorf("ExposedTok: want 55, got %d", plan.ExposedTok)
	}
	// routed = ecc:rare(30) + ecc:alsorare(20) + personal-rare(25) = 75.
	if plan.RoutedTok != 75 {
		t.Errorf("RoutedTok: want 75, got %d", plan.RoutedTok)
	}
}

func TestBuildRoutePlanCategories(t *testing.T) {
	plan := BuildRoutePlan(routeReport(), 0.10)

	var ecc, proj *RouteCategory
	for i := range plan.Categories {
		c := &plan.Categories[i]
		switch {
		case c.Name == "ecc" && c.Source == "namespace":
			ecc = c
		case c.Source == "project":
			proj = c
		}
	}
	if ecc == nil {
		t.Fatal("expected an 'ecc' namespace router")
	}
	if len(ecc.Skills) != 2 {
		t.Errorf("ecc router should hold 2 skills, got %d", len(ecc.Skills))
	}
	if proj == nil {
		t.Fatal("expected a project router for personal-rare")
	}
	if proj.Name != "/Users/x/Desktop/myrepo" {
		t.Errorf("project label: got %q", proj.Name)
	}
	// Heaviest router first: ecc(50) before project(25).
	if plan.Categories[0].Name != "ecc" {
		t.Errorf("heaviest router should sort first, got %q", plan.Categories[0].Name)
	}
}

func TestBuildRoutePlanBelowFloorAndDefaults(t *testing.T) {
	plan := BuildRoutePlan(routeReport(), 0) // 0 → default threshold
	if plan.ExposeThreshold != RouteDefaultExposeThreshold {
		t.Errorf("non-positive threshold should fall back to default, got %v", plan.ExposeThreshold)
	}
	if !plan.BelowFloor {
		t.Error("5 skills is below the advice floor; BelowFloor should be true")
	}
}

func TestRenderRoutePlanRuns(t *testing.T) {
	plan := BuildRoutePlan(routeReport(), 0.10)
	for _, render := range []func(){
		func() { RenderRoutePlan(&bytes.Buffer{}, plan, false) },
		func() { RenderRoutePlanMarkdown(&bytes.Buffer{}, plan) },
		func() {
			if err := RenderRoutePlanJSON(&bytes.Buffer{}, plan); err != nil {
				t.Fatal(err)
			}
		},
	} {
		render()
	}
}

// indexCategorySkill reports whether name appears in any routed category.
func indexCategorySkill(plan *RoutePlan, name string) (RouteSkill, bool) {
	for _, c := range plan.Categories {
		for _, s := range c.Skills {
			if s.Name == name {
				return s, true
			}
		}
	}
	return RouteSkill{}, false
}
