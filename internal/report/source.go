package report

import (
	"fmt"
	"io"
	"sort"
)

// SourceTotal is the dead weight a single source accounts for.
//
// Removable is counted separately because most of the weight usually is not:
// plugin items can only be turned off with /plugin in the host agent, so a
// source can dominate the report and still be untouchable by reap prune. A
// summary that hid that would point at the biggest number and offer no way to
// act on it.
type SourceTotal struct {
	Source    string `json:"source"`
	Items     int    `json:"items"`
	Removable int    `json:"removable"`
	Tokens    int    `json:"tokens"`
}

// SourceTotals groups rows by Source, heaviest first.
//
// It exists because a count of dead items reads as a count of decisions, and
// it is not one. Measured on the stack this was written for: 367 dead items,
// but 329 of them arrived from a single plugin and there were nine distinct
// sources in total. Nine decisions, not 367 — and the tool could already
// compute that, it just never showed it.
//
// Source is populated on every scanned item, so this is grouping, not new
// measurement.
func SourceTotals(rows []Row) []SourceTotal {
	byName := map[string]*SourceTotal{}
	for _, r := range rows {
		name := r.Source
		if name == "" {
			// An item with no recorded provenance still has to be counted, or
			// the totals stop adding up to the number printed above them.
			name = "unknown"
		}
		t := byName[name]
		if t == nil {
			t = &SourceTotal{Source: name}
			byName[name] = t
		}
		t.Items++
		t.Tokens += r.Tokens
		if r.Removable {
			t.Removable++
		}
	}

	out := make([]SourceTotal, 0, len(byName))
	for _, t := range byName {
		out = append(out, *t)
	}
	// Heaviest first: the point of the grouping is that one source usually
	// dominates, and that source should be the first thing read. Ties break on
	// name so the output is stable between runs.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Items != out[j].Items {
			return out[i].Items > out[j].Items
		}
		return out[i].Source < out[j].Source
	})
	return out
}

// RenderSourceTotals prints the by-source breakdown that turns a list of dead
// items into the handful of decisions behind it. The column is sized from the
// longest source name, because plugin coordinates run long and a fixed width
// silently ragged the one line that matters most.
func RenderSourceTotals(w io.Writer, totals []SourceTotal, deadItems int, humanTok func(int) string) {
	if len(totals) < 2 {
		return
	}
	width := 0
	for _, t := range totals {
		if len(t.Source) > width {
			width = len(t.Source)
		}
	}
	fmt.Fprintf(w, "  by source — %d unused %s came from %d places:\n",
		deadItems, plural(deadItems, "item"), len(totals))
	for _, t := range totals {
		note := fmt.Sprintf("%d prunable here", t.Removable)
		if t.Removable == 0 {
			// reap can see these and cannot touch them: they are registered by
			// a plugin, and the host agent owns that registration.
			note = "disable via /plugin"
		}
		fmt.Fprintf(w, "    %-*s  %4d %-6s ~%-8s %s\n",
			width, t.Source, t.Items, plural(t.Items, "item"), humanTok(t.Tokens)+" tok", note)
	}
	fmt.Fprintln(w)
}
