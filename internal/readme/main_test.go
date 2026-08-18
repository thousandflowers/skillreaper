package main

import (
	"strings"
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/report"
)

func TestFormatting(t *testing.T) {
	if got, want := spaced(790400), "790 400"; got != want {
		t.Errorf("spaced = %q, want %q", got, want)
	}
	if got, want := commas(19760), "19,760"; got != want {
		t.Errorf("commas = %q, want %q", got, want)
	}
	if got, want := roundTo(693805, 1000), 694000; got != want {
		t.Errorf("roundTo = %d, want %d", got, want)
	}
	// Truncation, not rounding: the page must agree with what reap prints for
	// the same run, and reap truncates.
	if got, want := pct(11, 380), "2%"; got != want {
		t.Errorf("pct = %q, want %q", got, want)
	}
	if got, want := pct(1, 500), "<1%"; got != want {
		t.Errorf("pct = %q, want %q", got, want)
	}
}

func TestReplaceRewritesOnlyItsOwnBlock(t *testing.T) {
	doc := "keep me\n<!-- readme-mine-table:start -->\nold\n<!-- readme-mine-table:end -->\nkeep me too\n"

	got, err := replace(doc, "mine-table", "new")
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if strings.Contains(got, "old") || !strings.Contains(got, "new") {
		t.Errorf("block not swapped: %q", got)
	}
	if !strings.Contains(got, "keep me\n") || !strings.Contains(got, "keep me too") {
		t.Errorf("text outside the markers was disturbed: %q", got)
	}

	again, err := replace(got, "mine-table", "new")
	if err != nil {
		t.Fatalf("replace twice: %v", err)
	}
	if again != got {
		t.Error("not idempotent")
	}
}

func TestReplaceFailsClosed(t *testing.T) {
	if _, err := replace("no markers here", "mine-table", "new"); err == nil {
		t.Error("missing markers should be an error, not a silent no-op")
	}
	doc := "<!-- readme-mine-table:start -->\nx\n<!-- readme-mine-table:end -->"
	if _, err := replace(doc, "mine-table", "  \n"); err == nil {
		t.Error("an empty body should be refused")
	}
}

func TestBlocksUseTheDocumentedDerivations(t *testing.T) {
	r := &report.Report{
		GeneratedAt:          time.Date(2026, 8, 18, 15, 0, 0, 0, time.UTC),
		WindowDays:           30,
		Sessions:             40,
		DeadCount:            368,
		DeadTokensPerSession: 19760,
		SessionsPerMonth:     40,
		MoneyPerMonth:        2.3712,
		Gap:                  &report.Gap{Loaded: 380, Fired: 11},
	}
	all := ""
	for _, b := range blocks(r) {
		all += b.body + "\n"
	}
	for _, want := range []string{
		"380 items loaded, 11 ever fired — 2% utilization", // Gap fields
		"12 kept · 11 actually fire",                       // loaded - dead
		"~790 000 tok/month",                               // dead tok x sessions/month
		"19 760 × 40 × $3.00 ÷ 1e6",                        // price recovered from the money field
		"~$2.37/month, ~$28/year",
		"40 sessions over 30 days, 2026-08-18", // the run has a date
	} {
		if !strings.Contains(all, want) {
			t.Errorf("rendered blocks missing %q", want)
		}
	}
}
