//go:build darwin || linux

package reinvest

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// The verifier read files. Whatever is in the installed folder that is not a
// plain file (a link to a folder or to a file, a pipe) is not what it read:
// a link can point anywhere once the check is over.
func TestUnchecked_anything_but_a_plain_file_is_never_what_was_checked(t *testing.T) {
	home := tempHome(t)
	c := candidate{Kind: kindSkill, Slug: "release", Doc: renderTree(map[string]string{skillFile: "---\nname: release\n---\n", "notes.md": "same text"})}
	root := filepath.Join(home, ".claude", "skills", "release")
	writeFile(t, filepath.Join(root, skillFile), "---\nname: release\n---\n")
	elsewhere := t.TempDir()
	writeFile(t, filepath.Join(elsewhere, "inner", "payload.md"), "anything")
	writeFile(t, filepath.Join(elsewhere, "notes.md"), "same text")
	for link, target := range map[string]string{
		"dir-link": filepath.Join(elsewhere, "inner"),
		"notes.md": filepath.Join(elsewhere, "notes.md"), // the checked text, behind a link
		"dangling": filepath.Join(elsewhere, "gone"),
	} {
		if err := os.Symlink(target, filepath.Join(root, link)); err != nil {
			t.Fatal(err)
		}
	}
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := uncheckedFiles(c); !sameWords(got, []string{"dangling", "dir-link", "notes.md", "pipe"}) {
		t.Errorf("unchecked %v, want every link and the pipe", got)
	}
}
