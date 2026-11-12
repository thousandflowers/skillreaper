package main

import (
	"strings"
	"testing"
)

// Rewritten to the name that was run, a command column keeps its width: the
// descriptions after it stay in one column.
func TestRewrittenUsageKeepsItsColumns(t *testing.T) {
	rw := hintRewriter("skillreaper", nil)
	in := "  reap prune [<name>]       quarantine one item\n" +
		"  reap mute [<name>]        strip descriptions\n" +
		"                            and more on the next line\n" +
		"run `reap prune` to act\n"
	out := string(rw([]byte(in)))
	lines := strings.Split(out, "\n")
	col := func(l, word string) int { return strings.Index(l, word) }
	if a, b, c := col(lines[0], "quarantine"), col(lines[1], "strip"), col(lines[2], "and more"); a != b || b != c {
		t.Errorf("columns %d %d %d:\n%s", a, b, c, out)
	}
	if !strings.HasPrefix(lines[0], "  skillreaper prune") || lines[3] != "run `skillreaper prune` to act" {
		t.Errorf("not rewritten:\n%s", out)
	}
}
