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

	defOpts := VerdictOpts{MinSessions: 10, GraceDays: 14, MinTokens: 3, WindowDays: 30, Cutoff: cutoff}

	cases := []struct {
		name                    string
		uses, sessions, tokens  int
		installedAt             time.Time
		opts                    VerdictOpts
		wantVerdict, wantReason string
	}{
		{"used", 5, 50, 100, older, defOpts, VerdictKeep, ReasonUsed},
		{"unused with evidence", 0, 50, 100, older, defOpts, VerdictReap, ReasonUnused},
		{"no sessions yet", 0, 0, 100, older, defOpts, VerdictReview, ReasonNeedsData},
		{"reap above min-sessions", 0, 3, 100, older, VerdictOpts{MinSessions: 3, GraceDays: 14, MinTokens: 3, WindowDays: 30, Cutoff: cutoff}, VerdictReap, ReasonUnused},
		{"tiny weight", 0, 50, 1, older, defOpts, VerdictKeep, ReasonTiny},
		{"installed recently (grace)", 0, 50, 100, newer, defOpts, VerdictReview, ReasonGrace},
		{"unknown install date", 0, 50, 100, time.Time{}, defOpts, VerdictReap, ReasonUnused},
		{"proportional scaling", 0, 2, 100, time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC), VerdictOpts{MinSessions: 10, GraceDays: 14, MinTokens: 3, WindowDays: 30, Cutoff: cutoff}, VerdictReview, ReasonNeedsData},
		{"mute heavy rare", 2, 100, 100, older, VerdictOpts{MinSessions: 10, GraceDays: 14, MinTokens: 3, WindowDays: 30, Cutoff: cutoff, Mutable: true, MuteThreshold: 0.20, MuteMinTokens: 50}, VerdictMute, ReasonHeavyRare},
		{"used often not muted", 50, 100, 100, older, VerdictOpts{MinSessions: 10, GraceDays: 14, MinTokens: 3, WindowDays: 30, Cutoff: cutoff, Mutable: true, MuteThreshold: 0.20, MuteMinTokens: 50}, VerdictKeep, ReasonUsed},
		{"broken cold", 0, 50, 100, older, VerdictOpts{MinSessions: 10, GraceDays: 14, MinTokens: 3, WindowDays: 30, Cutoff: cutoff, ErrorCount: 2}, VerdictReap, ReasonBroken},
	}
	for _, c := range cases {
		gotV, gotR := Verdict(c.uses, c.sessions, c.tokens, c.installedAt, c.opts)
		if gotV != c.wantVerdict {
			t.Errorf("%s: verdict got %s, want %s", c.name, gotV, c.wantVerdict)
		}
		if gotR != c.wantReason {
			t.Errorf("%s: reason got %s, want %s", c.name, gotR, c.wantReason)
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

// An item from a platform whose transcripts we could not parse must never be
// REAP'd on missing evidence — it is held at REVIEW(no-transcript) — while a
// covered platform's dead item is still REAP'd normally.
func TestEvidenceBlindNotReaped(t *testing.T) {
	st := usage.NewStats(30)
	st.Sessions = 60 // ample Claude Code evidence

	items := []scan.Item{
		{Category: scan.CatSkill, Name: "oc-only", Platform: "opencode", Source: "personal", DescChars: 370, Removable: true},
		{Category: scan.CatSkill, Name: "cc-dead", Platform: "claude-code", Source: "personal", DescChars: 370, Removable: true},
	}
	r := Build(items, st, nil, Opts{
		MinSessions:   10,
		Cutoff:        time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
		EvidenceBlind: map[string]bool{"opencode": true},
	})

	byName := map[string]Row{}
	for _, row := range r.Rows {
		byName[row.Name] = row
	}
	if v := byName["oc-only"].Verdict; v != VerdictReview {
		t.Errorf("evidence-blind item: verdict = %s, want REVIEW", v)
	}
	if rsn := byName["oc-only"].Reason; rsn != ReasonNoEvidence {
		t.Errorf("evidence-blind item: reason = %s, want %s", rsn, ReasonNoEvidence)
	}
	if v := byName["cc-dead"].Verdict; v != VerdictReap {
		t.Errorf("covered dead item: verdict = %s, want REAP", v)
	}
	if r.DeadCount != 1 {
		t.Errorf("DeadCount = %d, want 1 (blind item excluded)", r.DeadCount)
	}
}

// A skill named in a CLAUDE.md is held at KEEP(claude-md-ref) even when it
// would otherwise be REAP'd, while an unreferenced dead skill is still REAP'd.
func TestBuildClaudeMDProtection(t *testing.T) {
	st := usage.NewStats(30)
	st.Sessions = 60

	items := []scan.Item{
		{Category: scan.CatSkill, Name: "referenced", Source: "personal", DescChars: 370, Removable: true},
		{Category: scan.CatSkill, Name: "unreferenced", Source: "personal", DescChars: 370, Removable: true},
	}
	r := Build(items, st, nil, Opts{
		MinSessions:   10,
		Cutoff:        time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
		ClaudeMDLines: []string{"I rely on the referenced skill daily."},
	})

	byName := map[string]Row{}
	for _, row := range r.Rows {
		byName[row.Name] = row
	}
	if v, rsn := byName["referenced"].Verdict, byName["referenced"].Reason; v != VerdictKeep || rsn != ReasonClaudeMDRef {
		t.Errorf("referenced = %s(%s), want KEEP(%s)", v, rsn, ReasonClaudeMDRef)
	}
	if v := byName["unreferenced"].Verdict; v != VerdictReap {
		t.Errorf("unreferenced = %s, want REAP", v)
	}
}

// A heavy skill fired in too few sessions becomes MUTE(heavy-rare); the
// recoverable tokens are tallied on the report.
func TestBuildMute(t *testing.T) {
	st := usage.NewStats(30)
	st.Sessions = 100
	st.Uses[scan.CatSkill]["heavy"] = 2 // fired in ~2% of sessions

	items := []scan.Item{
		{Category: scan.CatSkill, Name: "heavy", Source: "personal", DescChars: 370, Removable: true},
	}
	r := Build(items, st, nil, Opts{
		MinSessions:   10,
		Cutoff:        time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
		MuteThreshold: 0.20,
		MuteMinTokens: 50,
	})
	if v, rsn := r.Rows[0].Verdict, r.Rows[0].Reason; v != VerdictMute || rsn != ReasonHeavyRare {
		t.Errorf("heavy rare skill = %s(%s), want MUTE(%s)", v, rsn, ReasonHeavyRare)
	}
	if r.MuteCount != 1 || r.MuteTokensPerSession == 0 {
		t.Errorf("MuteCount=%d MuteTok=%d, want 1 and >0", r.MuteCount, r.MuteTokensPerSession)
	}
}

// A skill that only ever errored is REAP(broken), distinct from a cold skill.
func TestBuildBrokenCold(t *testing.T) {
	st := usage.NewStats(30)
	st.Sessions = 60
	st.Errors[scan.CatSkill]["brokenskill"] = 3
	st.LastAttempt[scan.CatSkill]["brokenskill"] = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	items := []scan.Item{
		{Category: scan.CatSkill, Name: "brokenskill", Source: "personal", DescChars: 370, Removable: true},
	}
	r := Build(items, st, nil, Opts{MinSessions: 10, Cutoff: time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)})
	row := r.Rows[0]
	if row.Verdict != VerdictReap || row.Reason != ReasonBroken {
		t.Errorf("broken skill = %s(%s), want REAP(%s)", row.Verdict, row.Reason, ReasonBroken)
	}
	if row.ErrorCount != 3 {
		t.Errorf("ErrorCount = %d, want 3", row.ErrorCount)
	}
}

func TestBuildManifest(t *testing.T) {
	st := usage.NewStats(30)
	st.Sessions = 50
	st.Uses[scan.CatSkill]["myskill"] = 5
	st.Last[scan.CatSkill]["myskill"] = time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)
	st.SkillProjects["myskill"] = map[string]int{"repo-a": 5}

	items := []scan.Item{
		{Category: scan.CatSkill, Name: "myskill", Source: "personal", Path: "/x/.claude/skills/myskill/SKILL.md", DescChars: 100, Removable: true, ToolSurface: 2},
		{Category: scan.CatHook, Name: "SessionStart#0", Description: "echo hi"},
	}
	r := Build(items, st, nil, Opts{MinSessions: 10, Cutoff: time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)})

	m, ok := BuildManifest(r, "myskill", "/x/.claude", "1.2.3")
	if !ok {
		t.Fatal("manifest not built for known skill")
	}
	if m.Skill != "myskill" || m.ClaudeCodeVersion != "1.2.3" {
		t.Errorf("manifest = %+v", m)
	}
	if m.ToolSurface != "2 tool(s)" {
		t.Errorf("tool surface = %q, want \"2 tool(s)\"", m.ToolSurface)
	}
	if m.UsageWindow.Uses != 5 || m.UsageWindow.Projects != 1 {
		t.Errorf("usage window = %+v", m.UsageWindow)
	}
	if len(m.Hooks) != 1 || m.Hooks[0] != "echo hi" {
		t.Errorf("hooks = %v", m.Hooks)
	}
	if !strings.Contains(m.RestorePath, "muted") {
		t.Errorf("restore path = %s", m.RestorePath)
	}
	if _, ok := BuildManifest(r, "nope", "/x/.claude", ""); ok {
		t.Error("expected not-found for unknown skill")
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
	for _, want := range []string{"| dead-skill |", "| REAP |", "unused |", "# skillreaper report"} {
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
