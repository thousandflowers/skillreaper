package report

import (
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/scan"
	"github.com/thousandflowers/skillreaper/internal/usage"
)

func verdictsOf(items []scan.Item, opts Opts) map[string]string {
	quiet := usage.NewStats(30)
	quiet.Sessions = 60
	opts.MinSessions, opts.Cutoff = 10, time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	opts.PerPlatform = map[string]*usage.Stats{"claude-code": quiet}
	got := map[string]string{}
	for _, row := range Build(items, quiet, nil, opts).Rows {
		got[row.Name] = row.Verdict + "(" + row.Reason + ")"
	}
	return got
}

// A skill is called by the name in its header. CLAUDE.md naming it by that
// name protects it: only the folder's name was looked for.
func TestCLAUDEMDProtectsASkillByTheNameInItsHeader(t *testing.T) {
	items := []scan.Item{{Category: scan.CatSkill, Name: "dirfoo", FrontmatterName: "ship-it", Platform: "claude-code", Source: "personal", DescChars: 370}}
	got := verdictsOf(items, Opts{ClaudeMDLines: []string{"Always run /ship-it before deploying."}})
	if got["dirfoo"] != "KEEP(claude-md-ref)" {
		t.Errorf("dirfoo: %s", got["dirfoo"])
	}
}

// What a project's own agents and hooks load is at work too: read from the
// user's folder only, a skill a project's agent preloads came out REAP.
func TestWhatAProjectLoadsIsKept(t *testing.T) {
	items := []scan.Item{
		{Category: scan.CatSkill, Name: "deploy-x", Platform: "claude-code", Source: "personal", DescChars: 370},
		{Category: scan.CatSkill, Name: "hooked", Platform: "claude-code", Source: "personal", DescChars: 370},
		{Category: scan.CatSkill, Name: "nobody", Platform: "claude-code", Source: "personal", DescChars: 370},
	}
	got := verdictsOf(items, Opts{ProjectLoaded: map[string]string{"deploy-x": ReasonAgentLoaded, "hooked": ReasonHookLoaded}})
	for name, want := range map[string]string{"deploy-x": "KEEP(agent-loaded)", "hooked": "KEEP(hook-loaded)", "nobody": "REAP(unused)"} {
		if got[name] != want {
			t.Errorf("%s: %s, want %s", name, got[name], want)
		}
	}
}

// A file that says what is at work (settings.json, an agent's file) and
// cannot be read holds the verdicts, as an unreadable CLAUDE.md does.
func TestAnUnreadableSourceOfProtectionHoldsTheVerdicts(t *testing.T) {
	items := []scan.Item{
		{Category: scan.CatSkill, Name: "a-skill", Platform: "claude-code", Source: "personal", DescChars: 370},
		{Category: scan.CatAgent, Name: "an-agent", Platform: "claude-code", Source: "personal", DescChars: 370},
		{Category: scan.CatMCP, Name: "srv", Platform: "claude-code", Source: "user-config"},
	}
	got := verdictsOf(items, Opts{ProtectionUnread: "settings.json could not be read"})
	for name, want := range map[string]string{"a-skill": "REVIEW(no-transcript)", "an-agent": "REVIEW(no-transcript)", "srv": "REAP(unused)"} {
		if got[name] != want {
			t.Errorf("%s: %s, want %s", name, got[name], want)
		}
	}
}
