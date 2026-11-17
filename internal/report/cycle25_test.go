package report

import (
	"strings"
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/clock"
	"github.com/thousandflowers/skillreaper/internal/scan"
	"github.com/thousandflowers/skillreaper/internal/usage"
)

// OpenCode reads ~/.claude/skills too. When its history cannot be read (no
// sqlite3 on PATH) a skill there that only OpenCode uses has no use anywhere
// reap can see: it came out REAP, and into the prune plan.
func TestBuildHoldsASkillWhoseOtherReaderCannotBeRead(t *testing.T) {
	quiet := usage.NewStats(30)
	quiet.Sessions = 60
	items := []scan.Item{
		{Category: scan.CatSkill, Name: "shared-one", Platform: "claude-code", Source: "personal", DescChars: 370},
		{Category: scan.CatSkill, Name: "p:in-plugin", Platform: "claude-code", Source: "plugin:p@m", DescChars: 370},
		{Category: scan.CatAgent, Name: "an-agent", Platform: "claude-code", Source: "personal", DescChars: 370},
	}
	opts := Opts{
		MinSessions: 10, Cutoff: time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
		PerPlatform:        map[string]*usage.Stats{"claude-code": quiet},
		SkillFolderReaders: map[string][]string{"claude-code": {"opencode"}},
		PlatformNames:      map[string]string{"opencode": "OpenCode"},
		HistoryUnread:      map[string]string{"opencode": "reading its SQLite history needs the sqlite3 CLI, which was not found in PATH"},
	}
	verdicts := func(r *Report) map[string]string {
		got := map[string]string{}
		for _, row := range r.Rows {
			got[row.Name] = row.Verdict + "(" + row.Reason + ")"
			if row.Name == "shared-one" && row.Verdict == VerdictReview && (!strings.Contains(row.EvidenceNote, "OpenCode") || !strings.Contains(row.EvidenceNote, "sqlite3")) {
				t.Errorf("the note does not say whose history: %q", row.EvidenceNote)
			}
		}
		return got
	}
	got := verdicts(Build(items, quiet, nil, opts))
	for name, want := range map[string]string{"shared-one": "REVIEW(no-transcript)", "p:in-plugin": "REAP(unused)", "an-agent": "REAP(unused)"} {
		if got[name] != want {
			t.Errorf("unreadable reader: %s is %s, want %s", name, got[name], want)
		}
	}
	opts.HistoryUnread = nil
	if got := verdicts(Build(items, quiet, nil, opts)); got["shared-one"] != "REAP(unused)" {
		t.Errorf("a reader that was read: shared-one is %s", got["shared-one"])
	}
}

// "Monthly figures are extrapolated from N days of history": N is the history
// of the tools the figures come from. A second tool with one session today,
// and nothing dead in it, made it "from 3 days" for money scaled from 28.
func TestTheMonthlyBasisIsOfTheToolsTheFigureComesFrom(t *testing.T) {
	now := clock.Now() // the days of history are counted back from it
	claude, codex := usage.NewStats(30), usage.NewStats(30)
	claude.Sessions, codex.Sessions = 30, 1
	claude.ObserveCorpus(now.AddDate(0, 0, -28), now)
	codex.ObserveCorpus(now, now)
	merged := usage.NewStats(30)
	merged.Sessions = 31
	items := []scan.Item{{Category: scan.CatSkill, Name: "nobody", Platform: "claude-code", Source: "personal", DescChars: 370}}

	r := Build(items, merged, nil, Opts{
		MinSessions: 10, Cutoff: now.AddDate(0, 0, -30),
		PerPlatform: map[string]*usage.Stats{"claude-code": claude, "codex": codex},
	})

	if r.DeadTokensPerSession == 0 {
		t.Fatal("the fixture has nothing dead")
	}
	if r.MonthlyBasisDays < 20 {
		t.Errorf("basis %d days: that is the tool with nothing dead in it", r.MonthlyBasisDays)
	}
}
