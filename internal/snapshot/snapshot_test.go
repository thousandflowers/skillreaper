package snapshot

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/evidence"
	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/scan"
)

func row(cat, name, verdict string, uses int) report.Row {
	return report.Row{
		Item:    scan.Item{Category: scan.Category(cat), Name: name},
		Verdict: verdict,
		Uses:    uses,
	}
}

func rep(dead int, rows ...report.Row) *report.Report {
	return &report.Report{Rows: rows, DeadTokensPerSession: dead}
}

func TestCompareClassifiesEveryKindOfChange(t *testing.T) {
	from := rep(200,
		row("skill", "stays", "KEEP", 3),
		row("skill", "leaves", "REAP", 0),
		row("skill", "turns", "REAP", 0),
	)
	to := rep(150,
		row("skill", "stays", "KEEP", 3),
		row("skill", "turns", "KEEP", 4),
		row("agent", "arrives", "REAP", 0),
	)

	d := Compare(from, to, "a.json", "b.json", nil)

	got := map[string]string{}
	for _, c := range d.Changes {
		got[c.Name] = c.Kind
	}
	for name, want := range map[string]string{
		"leaves":  Disappeared,
		"turns":   VerdictMoved,
		"arrives": Appeared,
	} {
		if got[name] != want {
			t.Errorf("%s: kind = %q, want %q", name, got[name], want)
		}
	}
	if _, ok := got["stays"]; ok {
		t.Error("an unchanged item must not appear in the diff")
	}
	if d.DeadTokenDelta() != -50 {
		t.Errorf("DeadTokenDelta = %d, want -50", d.DeadTokenDelta())
	}
}

func TestCompareFlagsAnItemBackAfterAPrune(t *testing.T) {
	from := rep(0, row("skill", "kept", "KEEP", 1))
	to := rep(0,
		row("skill", "kept", "KEEP", 1),
		row("skill", "reinstalled", "REAP", 0),
		row("skill", "brand-new", "REAP", 0),
	)
	pruned := map[string]bool{"skill:reinstalled": true}

	d := Compare(from, to, "a.json", "b.json", pruned)

	back := d.Returned()
	if len(back) != 1 || back[0].Name != "reinstalled" {
		t.Fatalf("Returned() = %+v, want just reinstalled", back)
	}
	// The finding leads, so it survives a reader who stops after one line.
	if d.Changes[0].Name != "reinstalled" {
		t.Errorf("a returned item must sort first, got %q", d.Changes[0].Name)
	}
}

func TestUtilizationHandlesAnEmptyStack(t *testing.T) {
	d := Compare(rep(0), rep(0), "a", "b", nil)
	if d.UtilizationFrom() != 0 || d.UtilizationTo() != 0 {
		t.Error("an empty stack has no utilization; reporting 100% would read as perfect")
	}
}

func TestSaveListLoadRoundTrip(t *testing.T) {
	t.Setenv(evidence.StateDirEnv, t.TempDir())
	claudeDir := t.TempDir()
	r := rep(99, row("skill", "one", "REAP", 0))

	first, err := Save(claudeDir, r, time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Save(claudeDir, r, time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	saved, err := List(claudeDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 2 {
		t.Fatalf("List = %d snapshots, want 2", len(saved))
	}
	if saved[0] != first {
		t.Error("List must return oldest first: diff with no arguments relies on the order")
	}

	back, err := Load(first)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Rows) != 1 || back.Rows[0].Name != "one" || back.DeadTokensPerSession != 99 {
		t.Errorf("round trip lost data: %+v", back)
	}
}

func TestLoadRejectsAFileThatIsNotASnapshot(t *testing.T) {
	path := t.TempDir() + "/notes.json"
	if err := os.WriteFile(path, []byte("not json at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected a non-snapshot file to be rejected")
	}
}

func TestRenderTextLeadsWithTheReturn(t *testing.T) {
	d := Compare(
		rep(0),
		rep(0, row("skill", "reinstalled", "REAP", 0)),
		"a.json", "b.json",
		map[string]bool{"skill:reinstalled": true},
	)
	var buf bytes.Buffer
	RenderText(&buf, d)

	out := buf.String()
	if !strings.Contains(out, "back after a prune") {
		t.Errorf("the return must be stated, got:\n%s", out)
	}
	if strings.Index(out, "back after a prune") > strings.Index(out, "dead tokens") {
		t.Error("the return must come before the totals")
	}
}
