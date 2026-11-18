package report

import (
	"math"
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/clock"
	"github.com/thousandflowers/skillreaper/internal/scan"
	"github.com/thousandflowers/skillreaper/internal/usage"
)

func skillItem(platformID, name string, chars int) scan.Item {
	return scan.Item{Category: scan.CatSkill, Name: name, Platform: platformID, DescChars: chars, Removable: true}
}

// An item is judged on its own platform's evidence: another platform's use does
// not protect it, and another platform's sessions do not condemn it.
func TestBuildJudgesEachPlatformOnItsOwnEvidence(t *testing.T) {
	claude := usage.NewStats(30)
	claude.Sessions = 12
	claude.Uses[scan.CatSkill]["shared"] = 1
	codex := usage.NewStats(30)
	codex.Sessions = 12
	merged := usage.NewStats(30)
	merged.Sessions = 24
	merged.Uses[scan.CatSkill]["shared"] = 1

	r := Build([]scan.Item{
		skillItem("claude-code", "shared", 4000),
		skillItem("codex", "shared", 4000),
		skillItem("gemini", "shared", 4000),
	}, merged, nil, Opts{
		MinSessions: 5, WindowDays: 30, Cutoff: clock.Now().AddDate(0, 0, -30),
		PerPlatform:   map[string]*usage.Stats{"claude-code": claude, "codex": codex},
		EvidenceBlind: map[string]bool{"gemini": true},
		BlindReason:   map[string]string{"gemini": "format not parsed"},
	})
	got := map[string]Row{}
	for _, row := range r.Rows {
		got[row.Platform] = row
	}
	for _, c := range []struct {
		platform, verdict, reason string
		uses                      int
	}{
		{"claude-code", VerdictKeep, ReasonUsed, 1},
		{"codex", VerdictReap, ReasonUnused, 0},
		{"gemini", VerdictReview, ReasonNoEvidence, 0},
	} {
		row := got[c.platform]
		if row.Verdict != c.verdict || row.Reason != c.reason || row.Uses != c.uses {
			t.Errorf("%s: %s(%s) uses=%d, want %s(%s) uses=%d", c.platform, row.Verdict, row.Reason, row.Uses, c.verdict, c.reason, c.uses)
		}
	}
	if got["gemini"].EvidenceNote != "format not parsed" {
		t.Errorf("EvidenceNote = %q", got["gemini"].EvidenceNote)
	}
}

// A token costs money only in sessions of the platform that loads it.
func TestMonthlyFiguresChargeEachPlatformItsOwnSessions(t *testing.T) {
	claude := usage.NewStats(30)
	claude.Sessions = 30
	codex := usage.NewStats(30)
	codex.Sessions = 10
	merged := usage.NewStats(30)
	merged.Sessions = 40

	r := Build([]scan.Item{
		skillItem("claude-code", "a", 4000),
		skillItem("codex", "b", 8000),
	}, merged, nil, Opts{
		MinSessions: 5, WindowDays: 30, Cutoff: clock.Now().AddDate(0, 0, -30), PricePerMTok: 1,
		PerPlatform: map[string]*usage.Stats{"claude-code": claude, "codex": codex},
	})
	var a, b int
	for _, row := range r.Rows {
		if row.Name == "a" {
			a = row.Tokens
		} else {
			b = row.Tokens
		}
	}
	wantAvg := int(math.Round(float64(a*30+b*10) / 40))
	if r.DeadTokensPerSession != wantAvg {
		t.Errorf("DeadTokensPerSession = %d, want the session-weighted %d", r.DeadTokensPerSession, wantAvg)
	}
	if r.SessionsPerMonth != 40 {
		t.Errorf("SessionsPerMonth = %d, want 40", r.SessionsPerMonth)
	}
	if want := float64(a*30+b*10) / 1e6; math.Abs(r.MoneyPerMonth-want) > 1e-9 {
		t.Errorf("MoneyPerMonth = %v, want %v", r.MoneyPerMonth, want)
	}
	if r.PlatformSessions["codex"] != 10 {
		t.Errorf("PlatformSessions = %v", r.PlatformSessions)
	}
}

func TestHistoryDays(t *testing.T) {
	now := clock.Now()
	for _, c := range []struct {
		name              string
		window, retention int
		oldest            time.Time
		want              int
	}{
		{"the window when nothing shortens it", 30, 30, now.AddDate(0, 0, -60), 30},
		{"retention below the window", 30, 3, time.Time{}, 3},
		{"a young corpus", 30, 30, now.AddDate(0, 0, -10).Add(time.Hour), 10},
		{"never below the floor", 30, 30, now.Add(-time.Hour), minExtrapolationDays},
		{"the floor never exceeds retention", 30, 2, now.Add(-time.Hour), 2},
		{"no retention known", 30, 0, time.Time{}, 30},
	} {
		st := usage.NewStats(c.window)
		st.OldestTranscript = c.oldest
		if got := historyDays(st, c.window, c.retention); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}

func TestLookupUsesEveryNameAnItemCanFireUnder(t *testing.T) {
	for _, c := range []struct {
		name string
		it   scan.Item
		key  string
	}{
		{"plugin skill, bare", scan.Item{Category: scan.CatSkill, Name: "pl:foo"}, "foo"},
		{"frontmatter name", scan.Item{Category: scan.CatSkill, Name: "folder", FrontmatterName: "real"}, "real"},
		{"plugin frontmatter name", scan.Item{Category: scan.CatSkill, Name: "pl:folder", FrontmatterName: "real"}, "pl:real"},
		{"case differs", scan.Item{Category: scan.CatSkill, Name: "casefoo"}, "CaseFoo"},
		{"plugin agent, bare", scan.Item{Category: scan.CatAgent, Name: "pl:ag"}, "ag"},
		{"server with a dot", scan.Item{Category: scan.CatMCP, Name: "my.server"}, "my_server"},
		{"plugin with a dot", scan.Item{Category: scan.CatMCP, Name: "srv", Source: "plugin:my.plug@mk"}, "plugin_my_plug_srv"},
	} {
		st := usage.NewStats(30)
		st.Uses[c.it.Category][c.key] = 2
		if got, _ := lookupUses(canonicalStats(st, nil), c.it); got != 2 {
			t.Errorf("%s: uses = %d, want 2", c.name, got)
		}
	}
}

func TestLookupUsesCountsAKeyOnce(t *testing.T) {
	st := usage.NewStats(30)
	st.Uses[scan.CatMCP]["plain"] = 3
	it := scan.Item{Category: scan.CatMCP, Name: "plain"}
	if got, _ := lookupUses(canonicalStats(st, nil), it); got != 3 {
		t.Errorf("uses = %d, want 3: a name equal to its own key was counted twice", got)
	}
}

// The longest configured name owns a call, and the parser's first guess moves.
func TestCreditToolCallsToTheLongestServer(t *testing.T) {
	st := usage.NewStats(30)
	st.Uses[scan.CatMCP]["a"] = 2
	st.ToolCalls = map[string]int{"a__b__x": 2}
	c := canonicalStats(st, []string{"a", "a__b"})
	if c.Uses[scan.CatMCP]["a__b"] != 2 || c.Uses[scan.CatMCP]["a"] != 0 {
		t.Errorf("uses = %v", c.Uses[scan.CatMCP])
	}
	if st.Uses[scan.CatMCP]["a"] != 2 {
		t.Error("the input stats were changed")
	}

	oc := usage.NewStats(30)
	oc.ToolSep = "_"
	oc.ToolCalls = map[string]int{"srv_ping": 1, "bash": 4}
	c = canonicalStats(oc, []string{"srv"})
	if c.Uses[scan.CatMCP]["srv"] != 1 || len(c.Uses[scan.CatMCP]) != 1 {
		t.Errorf("uses = %v", c.Uses[scan.CatMCP])
	}
}

func TestManifestCarriesNoSecretsOrHomePaths(t *testing.T) {
	r := &Report{Rows: []Row{
		{Item: scan.Item{Category: scan.CatSkill, Name: "s", Platform: "claude-code", RootDir: "/Users/me/.claude", Path: "/Users/me/.claude/skills/s/SKILL.md"}},
		{Item: scan.Item{Category: scan.CatHook, Name: "Stop#0", Description: "/Users/me/x.sh --api-key SEKRET"}},
	}}
	m, ok := BuildManifest(r, "s", "/Users/me/.claude", "")
	if !ok {
		t.Fatal("not found")
	}
	if m.InstallPath != "skills/s/SKILL.md" {
		t.Errorf("InstallPath = %q", m.InstallPath)
	}
	if len(m.Hooks) != 1 || m.Hooks[0] != "Stop (1)" {
		t.Errorf("Hooks = %v", m.Hooks)
	}
}
