package main

import (
	"fmt"
	"io"
	"sort"

	"github.com/thousandflowers/skillreaper/internal/prune"
)

// listPruned prints what is still in quarantine, newest first, with the id
// restore takes. The id is otherwise only ever shown at the moment of pruning.
func listPruned(claudeDir string, stdout, stderr io.Writer) int {
	entries, err := prune.LoadManifest(claudeDir)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	var pruned []prune.Entry
	for _, e := range entries {
		if !e.Restored {
			pruned = append(pruned, e)
		}
	}
	if len(pruned) == 0 {
		fmt.Fprintln(stdout, "Nothing to restore.")
		return 0
	}
	sort.SliceStable(pruned, func(i, j int) bool { return pruned[i].Timestamp.After(pruned[j].Timestamp) })
	for _, e := range pruned {
		fmt.Fprintf(stdout, "  %s  %s  %-5s  %s\n", e.ID, e.Timestamp.Local().Format("2006-01-02 15:04"), e.Category, e.Name)
	}
	fmt.Fprintln(stdout, "\nreap restore <id> brings one back, reap restore --all brings back all of them.")
	return 0
}
