package report

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

func sourceRow(source string, tokens int, removable bool) Row {
	return Row{
		Item:    scan.Item{Category: scan.CatSkill, Source: source, Removable: removable},
		Verdict: VerdictReap,
		Tokens:  tokens,
	}
}

// The shape this was built for: one source dominating a long list.
func TestSourceTotalsGroupsAndOrdersByWeight(t *testing.T) {
	var rows []Row
	for i := 0; i < 5; i++ {
		rows = append(rows, sourceRow("plugin:big@mkt", 100, false))
	}
	rows = append(rows,
		sourceRow("personal", 10, true),
		sourceRow("personal", 5, true),
		sourceRow("user-config", 1, true),
	)

	got := SourceTotals(rows)
	if len(got) != 3 {
		t.Fatalf("want 3 sources, got %d: %+v", len(got), got)
	}
	if got[0].Source != "plugin:big@mkt" || got[0].Items != 5 || got[0].Tokens != 500 {
		t.Errorf("heaviest source wrong: %+v", got[0])
	}
	if got[0].Removable != 0 {
		t.Errorf("plugin items are not removable: got %d", got[0].Removable)
	}
	if got[1].Source != "personal" || got[1].Items != 2 || got[1].Removable != 2 || got[1].Tokens != 15 {
		t.Errorf("second source wrong: %+v", got[1])
	}
}

// Every dead item has to land in some bucket, or the totals stop matching the
// count printed above them.
func TestSourceTotalsCountsItemsWithNoSource(t *testing.T) {
	got := SourceTotals([]Row{sourceRow("", 7, true)})
	if len(got) != 1 || got[0].Source != "unknown" || got[0].Items != 1 || got[0].Tokens != 7 {
		t.Errorf("got %+v", got)
	}
}

// Ties break on name so two runs on an unchanged stack print the same thing.
func TestSourceTotalsIsStableOnTies(t *testing.T) {
	rows := []Row{sourceRow("zebra", 1, true), sourceRow("alpha", 1, true)}
	first := SourceTotals(rows)
	if first[0].Source != "alpha" {
		t.Errorf("ties should order by name, got %q first", first[0].Source)
	}
	if second := SourceTotals(rows); second[0].Source != first[0].Source {
		t.Error("two calls disagreed on order")
	}
}

func TestRenderSourceTotals(t *testing.T) {
	rows := []Row{
		sourceRow("plugin:big@mkt", 100, false),
		sourceRow("personal", 5, true),
	}
	var b bytes.Buffer
	RenderSourceTotals(&b, SourceTotals(rows), len(rows), func(n int) string { return fmt.Sprint(n) })
	out := b.String()

	// A source reap cannot touch has to say so, or the summary points at the
	// biggest number and offers no way to act on it.
	if !strings.Contains(out, "disable via /plugin") {
		t.Errorf("unremovable source needs its escape route:\n%s", out)
	}
	if !strings.Contains(out, "1 prunable here") {
		t.Errorf("removable source needs its count:\n%s", out)
	}
	// One item is a common case, and "1 items" is the bug this repo just fixed
	// elsewhere.
	if strings.Contains(out, "1 items") {
		t.Errorf("singular is wrong:\n%s", out)
	}
}

// Below two sources there is nothing to group, and printing a one-row summary
// above the list it summarises is noise.
func TestRenderSourceTotalsSilentBelowTwo(t *testing.T) {
	var b bytes.Buffer
	RenderSourceTotals(&b, SourceTotals([]Row{sourceRow("personal", 1, true)}), 1,
		func(n int) string { return fmt.Sprint(n) })
	if b.Len() != 0 {
		t.Errorf("want no output for a single source, got:\n%s", b.String())
	}
}
