package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/scan"
	"github.com/thousandflowers/skillreaper/internal/usage"
)

func TestVerdict(t *testing.T) {
	cutoff := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name                       string
		uses, sessions, minSession int
		installedAt                time.Time
		want                       string
	}{
		{"used", 5, 50, 10, older, VerdictKeep},
		{"unused with evidence", 0, 50, 10, older, VerdictReap},
		{"no sessions yet", 0, 0, 10, older, VerdictReview},
		{"few sessions still reap", 0, 3, 10, older, VerdictReap},
		{"installed recently", 0, 50, 10, newer, VerdictReview},
		{"unknown install date", 0, 50, 10, time.Time{}, VerdictReap},
	}
	for _, c := range cases {
		if got := Verdict(c.uses, c.sessions, c.minSession, c.installedAt, cutoff); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

func fixtureReport() *Report {
	st := usage.NewStats(30)
	st.Sessions = 60
	st.Uses[scan.CatSkill]["used-skill"] = 4
	st.Last[scan.CatSkill]["used-skill"] = time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)
	// Namespaced skill invoked via bare slash command.
	st.Uses[scan.CatSkill]["plan"] = 2

	items := []scan.Item{
		{Category: scan.CatSkill, Name: "used-skill", Source: "personal", DescChars: 100, Removable: true},
		{Category: scan.CatSkill, Name: "dead-skill", Source: "personal", DescChars: 370, Removable: true},
		{Category: scan.CatSkill, Name: "ecc:plan", Source: "plugin:ecc@ecc", DescChars: 50},
		{Category: scan.CatMCP, Name: "deadsrv", Source: "user-config", Removable: true},
		{Category: scan.CatProse, Name: "~/.claude/CLAUDE.md", Source: "global", DescChars: 740},
	}
	return Build(items, st, nil, Opts{
		MinSessions:  10,
		PricePerMTok: 3.0,
		Cutoff:       time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
	})
}

func TestBuild(t *testing.T) {
	r := fixtureReport()

	byName := map[string]Row{}
	for _, row := range r.Rows {
		byName[row.Name] = row
	}

	if v := byName["used-skill"].Verdict; v != VerdictKeep {
		t.Errorf("used-skill = %s", v)
	}
	if v := byName["dead-skill"].Verdict; v != VerdictReap {
		t.Errorf("dead-skill = %s", v)
	}
	if v := byName["ecc:plan"].Verdict; v != VerdictKeep {
		t.Errorf("ecc:plan should match bare slash-command usage, got %s", v)
	}
	if v := byName["deadsrv"].Verdict; v != VerdictReap {
		t.Errorf("deadsrv = %s", v)
	}
	if v := byName["~/.claude/CLAUDE.md"].Verdict; v != VerdictInfo {
		t.Errorf("prose verdict = %s", v)
	}

	// dead-skill: 370 chars -> 100 tokens. deadsrv has no DescChars.
	if r.DeadTokensPerSession != 100 {
		t.Errorf("DeadTokensPerSession = %d, want 100", r.DeadTokensPerSession)
	}
	if r.DeadCount != 2 {
		t.Errorf("DeadCount = %d, want 2", r.DeadCount)
	}
	if r.SessionsPerMonth != 60 {
		t.Errorf("SessionsPerMonth = %d, want 60", r.SessionsPerMonth)
	}
	// 100 tok * 60 sessions * $3/MTok = $0.018
	if r.MoneyPerMonth < 0.017 || r.MoneyPerMonth > 0.019 {
		t.Errorf("MoneyPerMonth = %f", r.MoneyPerMonth)
	}

	// REAP rows sort before KEEP within a category.
	if r.Rows[0].Name != "dead-skill" {
		t.Errorf("first row = %s, want dead-skill", r.Rows[0].Name)
	}
}

func TestRenderText(t *testing.T) {
	var buf bytes.Buffer
	RenderText(&buf, fixtureReport(), false)
	out := buf.String()
	for _, want := range []string{"dead-skill", "REAP", "KEEP", "never used", "60 sessions"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q", want)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("color disabled but ANSI codes present")
	}
}

func TestRenderMarkdown(t *testing.T) {
	var buf bytes.Buffer
	RenderMarkdown(&buf, fixtureReport())
	out := buf.String()
	for _, want := range []string{"| dead-skill |", "| REAP |", "# skillreaper report"} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown output missing %q", want)
		}
	}
}

func TestRenderJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderJSON(&buf, fixtureReport()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"DeadCount": 2`) {
		t.Error("json output missing DeadCount")
	}
}
