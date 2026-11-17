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
	// real is the file behind the path, through a link that is the file
	// itself: two rows are one file when this is the same.
	real os.FileInfo
	// inside is every file of the folder a delete takes, with its size and
	// date: the delete takes them all, whoever put them there and when.
	inside string
	// takes is that folder (or the file, for an item that is one).
	takes string
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
		takes := prune.Target(row.Item)
		target, _ := os.Lstat(takes)
		inside, _ := insideOf(takes)
		real, _ := os.Stat(row.Path)
		seen[row.Path] = stamp{file: file, real: real, target: target, inside: inside, takes: takes}
	}
	return seen
}

// changed says whether the item is another one, or was written, since it was
// read: its file, or the folder a delete would take. An item that is gone
// altogether is not: the delete says that itself.
func (s stamps) changed(row report.Row) bool { return s.changedAt(row.Path) }

func (s stamps) changedAt(path string) bool {
	was, known := s[path]
	if !known {
		return false
	}
	target, err := os.Lstat(was.takes)
	if err != nil {
		return false // nothing there to delete
	}
	now, err := os.Lstat(path)
	same := err == nil && os.SameFile(was.file, now) && was.file.ModTime().Equal(now.ModTime()) && was.file.Size() == now.Size()
	if !same || (was.target != nil && !os.SameFile(was.target, target)) {
		return true
	}
	inside, _ := insideOf(was.takes)
	return inside != was.inside // a file added to the folder, or changed in it
}

// untouched is taken before reap itself writes an item (a mute, an unmute, a
// prune): every row that is that file, and that nobody changed since it was
// read. One file can be two rows, a tool's own folder and a link to it in
// another tool's. A row somebody else changed is left out, so reap's write
// does not take their change for its own.
func (s stamps) untouched(row report.Row) []string {
	was, known := s[row.Path]
	if !known {
		return nil
	}
	var paths []string
	for path, st := range s {
		if os.SameFile(st.real, was.real) && !s.changedAt(path) {
			paths = append(paths, path)
		}
	}
	return paths
}

// refresh reads those rows again after reap's own write: that change is not
// somebody else's.
func (s stamps) refresh(untouched []string) {
	for _, path := range untouched {
		st := s[path]
		file, err := os.Lstat(path)
		if err != nil {
			delete(s, path) // gone with what reap did: nothing left to compare
			continue
		}
		// What a delete takes too: the folder's listing holds the file reap
		// rewrote, and an item that is one file is that file, written anew.
		st.file = file
		st.real, _ = os.Stat(path)
		st.target, _ = os.Lstat(st.takes)
		st.inside, _ = insideOf(st.takes)
		s[path] = st
	}
}
