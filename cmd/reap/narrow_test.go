package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/usage"
)

// A project behind a folder reap may not enter is not gone: dropped with no
// word, what its CLAUDE.md, agents and hooks name came out REAP.
func TestSessionProjectsTellsAClosedProjectFromOneThatIsGone(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root enters everything")
	}
	root := t.TempDir()
	open, behind, self := filepath.Join(root, "open"), filepath.Join(root, "closed", "p"), filepath.Join(root, "self")
	for _, dir := range []string{open, behind, self} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{filepath.Dir(behind), self} {
		if err := os.Chmod(dir, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	}
	st := usage.NewStats(30)
	st.ProjectDirs = map[string]bool{open: true, behind: true, self: true, filepath.Join(root, "gone"): true}

	dirs, closed := sessionProjects(st)
	if len(dirs) != 1 || dirs[0] != open {
		t.Errorf("projects to read: %v", dirs)
	}
	if len(closed) != 2 {
		t.Fatalf("closed projects: %v", closed)
	}
	for _, w := range closed {
		if (w.Path != behind && w.Path != self) || !strings.Contains(w.Msg, "REVIEW") {
			t.Errorf("warning %+v", w)
		}
	}
}
