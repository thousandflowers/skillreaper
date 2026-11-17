package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/thousandflowers/skillreaper/internal/prune"
	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/scan"
)

// stamp is one item as reap read it: its file, and what a delete of it takes
// (for a skill, the folder the file is in).
type stamp struct {
	file, target os.FileInfo
	// inside is every file of the folder a delete takes, with its size and
	// date: the delete takes them all, whoever put them there and when.
	inside string
}

// insideOf lists what is in a folder, for comparing later; empty for a file or
// a link. files is how many entries a delete would take with it.
func insideOf(target string) (listing string, files int) {
	info, err := os.Lstat(target)
	if err != nil || !info.IsDir() {
		return "", 0
	}
	var b strings.Builder
	_ = filepath.WalkDir(target, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == target {
			return nil // what cannot be read is not compared
		}
		rel, _ := filepath.Rel(target, path)
		size, when := int64(0), ""
		if in, statErr := d.Info(); statErr == nil && !d.IsDir() {
			size, when = in.Size(), in.ModTime().String()
			files++
		}
		fmt.Fprintf(&b, "%s\x00%d\x00%s\n", rel, size, when)
		return nil
	})
	return b.String(), files
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
		inside, _ := insideOf(prune.Target(row.Item))
		seen[row.Path] = stamp{file: file, target: target, inside: inside}
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
	if !same || (was.target != nil && !os.SameFile(was.target, target)) {
		return true
	}
	inside, _ := insideOf(prune.Target(row.Item))
	return inside != was.inside // a file added to the folder, or changed in it
}

// refresh reads an item again after reap itself changed it (a mute, an
// unmute, a prune): that change is not somebody else's.
func (s stamps) refresh(row report.Row, rows ...report.Row) {
	rows = append(rows, row)
	was, known := s[row.Path]
	if !known {
		return
	}
	// Every row that is this file: one file can be two rows, a tool's own
	// folder and a link to it in another tool's.
	for path, st := range s {
		if !os.SameFile(st.file, was.file) {
			continue
		}
		file, err := os.Lstat(path)
		if err != nil {
			delete(s, path) // gone with what reap did: nothing left to compare
			continue
		}
		st.file = file
		for _, row := range rows {
			if row.Path == path {
				st.inside, _ = insideOf(prune.Target(row.Item))
			}
		}
		s[path] = st
	}
}
