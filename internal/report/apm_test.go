package report

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

const apmCwd = "/tmp/myrepo"

func apmRepoKey() string { return encodeProject(apmCwd) } // "-tmp-myrepo"

func apmReport() *Report {
	rk := apmRepoKey()
	return &Report{
		WindowDays: 30,
		Sessions:   20,
		Rows: []Row{
			{Item: scan.Item{Category: scan.CatSkill, Name: "frontend-design", Source: "plugin:anthropics"}, Verdict: VerdictKeep, Uses: 5},
			{Item: scan.Item{Category: scan.CatSkill, Name: "graphify", Source: "personal"}, Verdict: VerdictKeep, Uses: 3},
			{Item: scan.Item{Category: scan.CatSkill, Name: "elsewhere", Source: "personal"}, Verdict: VerdictKeep, Uses: 9},
			{Item: scan.Item{Category: scan.CatSkill, Name: "dead", Source: "personal"}, Verdict: VerdictReap, Uses: 2},
			{Item: scan.Item{Category: scan.CatSkill, Name: "maybe", Source: "personal"}, Verdict: VerdictReview, Uses: 1},
			{Item: scan.Item{Category: scan.CatSkill, Name: "coldreview", Source: "personal"}, Verdict: VerdictReview, Uses: 0},
			// non-skill rows ignored entirely
			{Item: scan.Item{Category: scan.CatMCP, Name: "mcpthing"}, Verdict: VerdictKeep, Uses: 50},
		},
		SkillProjects: map[string]map[string]int{
			"frontend-design": {rk: 5},
			"graphify":        {rk: 3},
			"dead":            {rk: 2},
			"maybe":           {rk: 1},
			"elsewhere":       {"-other-repo": 9}, // fired, but not here
		},
	}
}

func TestBuildAPMProposeSelectsFiredHereByVerdict(t *testing.T) {
	lock := map[string]string{"frontend-design": "anthropics/skills/skills/frontend-design"}
	m := BuildAPM(apmReport(), apmCwd, lock, nil, "")

	if m.Diff {
		t.Error("propose mode should not be a diff")
	}
	got := map[string]APMDep{}
	for _, d := range m.Deps {
		got[d.Name] = d
	}
	if len(m.Deps) != 3 {
		t.Fatalf("want 3 deps (frontend-design, graphify, maybe), got %d: %v", len(m.Deps), depNames(m.Deps))
	}
	if _, ok := got["elsewhere"]; ok {
		t.Error("skill fired only in another repo must be excluded")
	}
	if _, ok := got["dead"]; ok {
		t.Error("REAP skill must be omitted")
	}
	if got["frontend-design"].Placeholder {
		t.Error("frontend-design has a lock coordinate; not a placeholder")
	}
	if got["frontend-design"].Coordinate != "anthropics/skills/skills/frontend-design" {
		t.Errorf("coordinate: got %q", got["frontend-design"].Coordinate)
	}
	if !got["graphify"].Placeholder {
		t.Error("graphify has no coordinate evidence; should be a placeholder")
	}
	// Sorted by uses desc.
	if m.Deps[0].Name != "frontend-design" {
		t.Errorf("most-used first: got %q", m.Deps[0].Name)
	}
}

func TestBuildAPMDiffAddDropAndReviewProtection(t *testing.T) {
	lock := map[string]string{"frontend-design": "anthropics/skills/skills/frontend-design"}
	declared := map[string]bool{
		"frontend-design": true, // declared AND fired → "declared"
		"removed-skill":   true, // declared, never fired here → drop
		"coldreview":      true, // declared, maps to a REVIEW skill → protected
	}
	m := BuildAPM(apmReport(), apmCwd, lock, declared, "/tmp/apm.yml")

	if !m.Diff {
		t.Fatal("expected diff mode")
	}
	status := map[string]string{}
	for _, d := range m.Deps {
		status[d.Name] = d.Status
	}
	if status["frontend-design"] != apmStatusDeclared {
		t.Errorf("frontend-design should be 'declared', got %q", status["frontend-design"])
	}
	if status["graphify"] != apmStatusAdd {
		t.Errorf("graphify should be 'add', got %q", status["graphify"])
	}

	drops := map[string]bool{}
	for _, d := range m.Drop {
		drops[d.Name] = true
	}
	if !drops["removed-skill"] {
		t.Error("declared-but-cold 'removed-skill' should be a drop candidate")
	}
	if drops["coldreview"] {
		t.Error("REVIEW skill must never be suggested for dropping")
	}
	if drops["frontend-design"] {
		t.Error("covered declared entry must not be dropped")
	}
}

func TestLoadAPMManifestHarvestsCoordinates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "apm.yml")
	content := `dependencies:
  apm:
    - "anthropics/skills/skills/frontend-design"
    - "github/awesome-copilot/plugins/context-engineering#v1.2.0"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	declared, lock, err := LoadAPMManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if !declared["frontend-design"] || !declared["context-engineering"] {
		t.Errorf("declared set missing entries: %v", declared)
	}
	if lock["frontend-design"] != "anthropics/skills/skills/frontend-design" {
		t.Errorf("lock coordinate: got %q", lock["frontend-design"])
	}
}

func TestLoadAPMManifestMissingFile(t *testing.T) {
	declared, lock, err := LoadAPMManifest(filepath.Join(t.TempDir(), "nope.yml"))
	if err != nil || declared != nil || lock != nil {
		t.Errorf("missing file should be (nil,nil,nil), got %v/%v/%v", declared, lock, err)
	}
}

func TestCoordLastSegment(t *testing.T) {
	cases := map[string]string{
		"anthropics/skills/skills/frontend-design":   "frontend-design",
		"owner/repo/path/name#v1.0.0":                "name",
		"github/awesome-copilot/agents/api.agent.md": "api.agent.md",
		"bare": "bare",
	}
	for in, want := range cases {
		if got := coordLastSegment(in); got != want {
			t.Errorf("coordLastSegment(%q): want %q, got %q", in, want, got)
		}
	}
}

func TestRenderAPMRuns(t *testing.T) {
	m := BuildAPM(apmReport(), apmCwd, map[string]string{"frontend-design": "anthropics/skills/skills/frontend-design"}, nil, "")
	var yaml bytes.Buffer
	RenderAPMYAML(&yaml, m)
	if !strings.Contains(yaml.String(), "dependencies:") || !strings.Contains(yaml.String(), "TODO(skillreaper)") {
		t.Errorf("yaml missing structure or placeholder comment:\n%s", yaml.String())
	}
	RenderAPMMarkdown(&bytes.Buffer{}, m)
	if err := RenderAPMJSON(&bytes.Buffer{}, m); err != nil {
		t.Fatal(err)
	}
}

func depNames(deps []APMDep) []string {
	var out []string
	for _, d := range deps {
		out = append(out, d.Name)
	}
	return out
}
