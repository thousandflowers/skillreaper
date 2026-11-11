package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/thousandflowers/skillreaper/internal/prune"
	"github.com/thousandflowers/skillreaper/internal/scan"
)

// cmdDelete removes one item for good. Unlike prune it keeps nothing, so it takes
// exactly one item, by the names prune takes, and never a kept item.
func cmdDelete(opts options, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: reap delete <name>")
		return 2
	}
	keepGoingWithoutReader()
	if !requireClaudeDir(opts, stderr) {
		return 1
	}
	r, err := gather(opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if !keepListReadable(opts, stderr) {
		return 1
	}
	rows, err := findItem(r, args[0])
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if len(rows) == 0 {
		fmt.Fprintln(stderr, pruneNameMiss(r, args[0]))
		return 1
	}
	row := rows[0]
	if refuseKept(row, stderr) {
		return 1
	}

	fmt.Fprintf(stdout, "\n  %-6s  %s\n  %s\n", row.Category, row.Name, row.Path)
	if row.PrunedCopyID != "" {
		fmt.Fprintf(stdout, "  A copy is in quarantine (id %s). It stays there: reap restore %s can still bring that one back.\n", row.PrunedCopyID, row.PrunedCopyID)
	}
	if row.LinkTarget != "" {
		fmt.Fprintf(stdout, "  This is a link: the link goes, and what it points to stays at %s\n", row.LinkTarget)
	}
	printChosenNote(stdout, row)
	if !opts.yes && !confirm(stdin, stderr, fmt.Sprintf("\nDelete %s for good? This cannot be restored, not even with reap restore. [y/N] ", row.Name)) {
		fmt.Fprintln(stdout, "aborted")
		return exitNotDone
	}

	lock, ok := lockState(opts.claudeDir, stateLockWait, stderr)
	if !ok {
		return 1
	}
	defer lock.Release()
	if row.Category == scan.CatMCP {
		err = prune.DeleteMCP(opts.claudeDir, row.Path, serverScope(row), row.Name)
	} else {
		err = prune.DeleteItem(row.Item)
	}
	switch {
	case errors.Is(err, prune.ErrAlreadyGone):
		fmt.Fprintf(stdout, "%s was already gone; nothing was deleted\n", row.Name)
		return 0
	case err != nil:
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "deleted %s for good\n", row.Name)
	return 0
}
