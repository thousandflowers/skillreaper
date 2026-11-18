package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/evidence"
	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/scan"
	"github.com/thousandflowers/skillreaper/internal/usage"
)

// What has no date of its own (a server in a config), and what came back (an
// item restored, or unmuted), is dated by when reap first found it there:
// judged at once on sessions it was not in, it came out REAP.
func TestItemsAreDatedByWhenReapFirstFoundThem(t *testing.T) {
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	stack := func() []scan.Item {
		return []scan.Item{
			{Category: scan.CatSkill, Name: "kept", Platform: "claude-code", Source: "personal", Path: "/s/kept/SKILL.md", InstalledAt: old},
			{Category: scan.CatMCP, Name: "srv", Platform: "claude-code", Source: "user-config", Path: "/h/.claude.json"},
		}
	}
	d := evidence.New()
	items := stack()
	if !stampFirstSeen(d, items, nil, now) || !items[1].InstalledAt.IsZero() || !items[0].InstalledAt.Equal(old) {
		t.Fatalf("the first look dates nothing: %+v", items)
	}
	later := now.AddDate(0, 0, 3)
	items = append(stack(), scan.Item{Category: scan.CatMCP, Name: "added", Platform: "claude-code", Source: "user-config", Path: "/h/.claude.json"})
	stampFirstSeen(d, items, nil, later)
	if !items[2].InstalledAt.Equal(later) || !items[1].InstalledAt.IsZero() {
		t.Errorf("a server added later: %v; one there from the start: %v", items[2].InstalledAt, items[1].InstalledAt)
	}
	// pruned (gone from the stack), then restored: new again
	stampFirstSeen(d, stack()[1:], nil, later)
	back := stack()
	stampFirstSeen(d, back, nil, later.AddDate(0, 0, 1))
	if !back[0].InstalledAt.Equal(later.AddDate(0, 0, 1)) {
		t.Errorf("a skill that came back: %v", back[0].InstalledAt)
	}
	// muted: dated by the last look that found it muted
	muted := stack()
	stampFirstSeen(d, muted, map[string]bool{"/s/kept/SKILL.md": true}, later.AddDate(0, 0, 9))
	if !muted[0].InstalledAt.Equal(later.AddDate(0, 0, 9)) {
		t.Errorf("a muted skill: %v", muted[0].InstalledAt)
	}
	// a look at another tool's stack forgets nothing of this one
	stampFirstSeen(d, []scan.Item{{Category: scan.CatSkill, Name: "x", Platform: "codex", Source: "personal"}}, nil, later)
	again := stack()
	stampFirstSeen(d, again, nil, later.AddDate(0, 0, 20))
	if again[0].InstalledAt.After(later.AddDate(0, 0, 9)) {
		t.Errorf("forgotten by a look elsewhere: %v", again[0].InstalledAt)
	}
}

// A transcript read only in part is not folded into the record of past uses:
// marked as read there, it was never read again, and what it held was lost.
func TestAPartlyReadTranscriptIsNotRecordedAsRead(t *testing.T) {
	d := evidence.New()
	partial, whole := usage.NewStats(30), usage.NewStats(30)
	partial.IncompleteEvidence = true
	if folded(d, "a", "claude-code", partial) || d.HasSource("a") {
		t.Error("a partial read was recorded")
	}
	if !folded(d, "b", "claude-code", whole) || !d.HasSource("b") {
		t.Error("a whole read was not recorded")
	}
}

// A plan made on a few days of history says so: the warning was in the
// report only, and the prune plan read like any other.
func TestAPlanOnAShortHistorySaysSo(t *testing.T) {
	if note := shortHistoryNote(&report.Report{WindowDays: 30, MonthlyBasisDays: 3}); !strings.Contains(note, "3 days") || !strings.Contains(note, "30") {
		t.Errorf("3 days of 30: %q", note)
	}
	for _, r := range []*report.Report{{WindowDays: 30, MonthlyBasisDays: 29}, {WindowDays: 30}, {WindowDays: 30, MonthlyBasisDays: 15}} {
		if note := shortHistoryNote(r); note != "" {
			t.Errorf("%d of %d: %q", r.MonthlyBasisDays, r.WindowDays, note)
		}
	}
}

// With a mute list that cannot be read reap does not know what it muted, and
// a muted skill has no use because of the mute: prune stops, as mute does.
func TestPruneStopsOnAMuteListItCannotRead(t *testing.T) {
	home := deleteHome(t)
	mustWriteFile(t, filepath.Join(home, ".claude", "reaped", "muted", "state.json"), `{"entries": [`)
	for _, args := range [][]string{{"prune", "--yes"}, {"prune", "mine", "--yes"}, {"delete", "mine", "--yes"}} {
		var out, errOut bytes.Buffer
		if code := run(args, strings.NewReader(""), &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "Nothing was changed") {
			t.Errorf("%v: exit %d\n%s%s", args, code, out.String(), errOut.String())
		}
	}
}
