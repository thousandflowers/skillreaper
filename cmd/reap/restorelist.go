package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/thousandflowers/skillreaper/internal/mute"
	"github.com/thousandflowers/skillreaper/internal/prune"
)

// showPruned prints what is still in quarantine, newest first, with the id
// restore takes, and returns how many items that is. The id is otherwise only
// ever shown at the moment of pruning.
func showPruned(claudeDir string, stdout io.Writer) (int, error) {
	entries, err := prune.LoadManifest(claudeDir)
	if err != nil {
		return 0, err
	}
	var pruned []prune.Entry
	for _, e := range entries {
		if !e.Restored {
			pruned = append(pruned, e)
		}
	}
	if len(pruned) == 0 {
		fmt.Fprintln(stdout, "Nothing to restore.")
		return 0, nil
	}
	sort.SliceStable(pruned, func(i, j int) bool { return pruned[i].Timestamp.After(pruned[j].Timestamp) })
	for _, e := range pruned {
		fmt.Fprintf(stdout, "  %s  %s  %-5s  %s\n", e.ID, e.Timestamp.Local().Format("2006-01-02 15:04"), e.Category, e.Name)
	}
	return len(pruned), nil
}

// listMuted prints the skills whose description is stripped. Their weight drops
// to almost nothing, so the report alone gives no sign they were ever muted.
func listMuted(claudeDir string, stdout, stderr io.Writer) int {
	names, err := mute.List(claudeDir)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if len(names) == 0 {
		fmt.Fprintln(stdout, "Nothing is muted.")
		return 0
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(stdout, "  %s\n", name)
	}
	fmt.Fprintln(stdout, "\nreap unmute <name> brings one description back, reap unmute --all brings back all of them.")
	return 0
}

// resolveMuted finds a muted skill by its full name or its short one, as mute
// does. A name that matches nothing is returned as given, for Unmute to refuse.
func resolveMuted(names []string, name string) (string, error) {
	var matches []string
	for _, n := range names {
		if n == name {
			return n, nil
		}
		if strings.HasSuffix(n, ":"+name) {
			matches = append(matches, n)
		}
	}
	switch len(matches) {
	case 0:
		return name, nil
	case 1:
		return matches[0], nil
	}
	sort.Strings(matches)
	return "", fmt.Errorf("%s is ambiguous, use the full name: %s", name, strings.Join(matches, ", "))
}
