package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/menu"
)

// swapThenYes answers the delete question with y, after putting another folder
// of the same name where the skill was: what a pull or a reinstall does while
// the question waits.
type swapThenYes struct {
	t      *testing.T
	folder string
	done   bool
}

func (s *swapThenYes) Read(p []byte) (int, error) {
	if s.done {
		return 0, io.EOF
	}
	s.done = true
	swapFolder(s.t, s.folder)
	return copy(p, "y\n"), nil
}

func swapFolder(t *testing.T, folder string) {
	t.Helper()
	if err := os.Rename(folder, filepath.Join(t.TempDir(), "the-one-reap-read")); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(folder, "SKILL.md"), "---\nname: mine\ndescription: another skill altogether\n---\nnew work, never shown\n")
}

// Delete cannot be undone, so it is for the item reap read and showed. With
// another one in its place by the time the answer comes, nothing is deleted.
func TestDeleteLeavesAnItemThatChangedSinceItWasRead(t *testing.T) {
	home := deleteHome(t)
	folder := filepath.Join(home, ".claude", "skills", "mine")
	var out, errOut bytes.Buffer

	code := run([]string{"delete", "mine"}, &swapThenYes{t: t, folder: folder}, &out, &errOut)

	if code == 0 || !strings.Contains(errOut.String(), "changed since reap read it") {
		t.Errorf("exit %d:\n%s%s", code, out.String(), errOut.String())
	}
	if b, err := os.ReadFile(filepath.Join(folder, "SKILL.md")); err != nil || !strings.Contains(string(b), "never shown") {
		t.Errorf("the folder put there meanwhile is gone or changed: %v", err)
	}
}

// The same from the menu, where the wait is as long as the menu stays open.
func TestMenuDeleteLeavesAnItemThatChangedSinceItWasRead(t *testing.T) {
	home := deleteHome(t)
	dir := filepath.Join(home, ".claude")
	rows := reportRows(t)
	row := menuRow(t, rows, "skill", "mine")
	applier := &menuApplier{dir: dir, seen: stampsOf(rows)}
	swapFolder(t, filepath.Join(dir, "skills", "mine"))

	out := applier.Apply([]menu.Pick{{Row: row, Verb: menu.Delete}})

	if out[0].Err == nil || !strings.Contains(out[0].Err.Error(), "changed since reap read it") {
		t.Errorf("delete of a changed item: %v", out[0].Err)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "mine", "SKILL.md")); err != nil {
		t.Errorf("the folder put there meanwhile is gone: %v", err)
	}
	// Another row, untouched, is still deleted.
	other := menuRow(t, rows, "skill", "copied")
	if out := applier.Apply([]menu.Pick{{Row: other, Verb: menu.Delete}}); out[0].Err != nil {
		t.Errorf("delete of an unchanged item: %v", out[0].Err)
	}
}
