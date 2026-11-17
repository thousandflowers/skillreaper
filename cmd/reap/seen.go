package main

import (
	"fmt"
	"os"

	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/scan"
)

// stamps is what each item's file was when reap read the stack, by path. A
// delete cannot be undone, so it is checked against this first: the item there
// when the answer comes may be another one of the same name (a pull, a
// reinstall), which the person was never shown.
type stamps map[string]os.FileInfo

// stampsOf reads the files of the rows that are files. An MCP server is a key
// in a config other programs write all the time; it has no stamp.
func stampsOf(rows []report.Row) stamps {
	seen := stamps{}
	for _, row := range rows {
		if row.Category == scan.CatMCP || row.Path == "" {
			continue
		}
		if info, err := os.Lstat(row.Path); err == nil {
			seen[row.Path] = info
		}
	}
	return seen
}

// changed is an error when the item's file is another one, or was written,
// since it was read. A file that is gone is not: the delete says that itself.
func (s stamps) changed(row report.Row) error {
	was, known := s[row.Path]
	if !known {
		return nil
	}
	now, err := os.Lstat(row.Path)
	if err != nil {
		return nil
	}
	if os.SameFile(was, now) && was.ModTime().Equal(now.ModTime()) && was.Size() == now.Size() {
		return nil
	}
	return fmt.Errorf("%s changed since reap read it, and a delete cannot be undone: nothing was deleted. Look at it again, then delete it", row.Name)
}
