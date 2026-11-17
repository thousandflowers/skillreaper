package main

import (
	"os"

	"github.com/thousandflowers/skillreaper/internal/prune"
	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/scan"
)

// stamp is one item as reap read it: its file, and what a delete of it takes
// (for a skill, the folder the file is in).
type stamp struct {
	file, target os.FileInfo
}

// stamps is what each item was when reap read the stack, by path. A delete
// cannot be undone, so it is checked against this first: the item there when
// the answer comes may be another one of the same name (a pull, a reinstall),
// which the person was never shown.
type stamps map[string]stamp

// stampsOf reads the rows that are files. An MCP server is a key in a config
// other programs write all the time; it has no stamp.
func stampsOf(rows []report.Row) stamps {
	seen := stamps{}
	for _, row := range rows {
		if row.Category == scan.CatMCP || row.Path == "" {
			continue
		}
		file, err := os.Lstat(row.Path)
		if err != nil {
			continue
		}
		target, _ := os.Lstat(prune.Target(row.Item))
		seen[row.Path] = stamp{file: file, target: target}
	}
	return seen
}

// changed says whether the item is another one, or was written, since it was
// read: its file, or the folder a delete would take. An item that is gone
// altogether is not: the delete says that itself.
func (s stamps) changed(row report.Row) bool {
	was, known := s[row.Path]
	if !known {
		return false
	}
	target, err := os.Lstat(prune.Target(row.Item))
	if err != nil {
		return false // nothing there to delete
	}
	now, err := os.Lstat(row.Path)
	same := err == nil && os.SameFile(was.file, now) && was.file.ModTime().Equal(now.ModTime()) && was.file.Size() == now.Size()
	return !same || (was.target != nil && !os.SameFile(was.target, target))
}

// refresh reads an item again after reap itself rewrote it (a mute, an
// unmute): that change is not somebody else's.
func (s stamps) refresh(row report.Row) {
	if _, known := s[row.Path]; !known {
		return
	}
	for path, st := range stampsOf([]report.Row{row}) {
		s[path] = st
	}
}
