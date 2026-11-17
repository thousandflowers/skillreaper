package usage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeSession(t *testing.T, path, id string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","sessionId":"` + id + `","timestamp":"` + time.Now().UTC().Format(time.RFC3339) + `","message":{"content":"hi"}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A project folder that is a link to a disk that is not there holds sessions
// reap cannot see: the evidence is incomplete. It was skipped without a word,
// and a skill used only there came out REAP.
func TestADeadProjectLinkMakesTheEvidenceIncomplete(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "projects")
	writeSession(t, filepath.Join(dir, "-work-here", "a.jsonl"), "a")
	if err := os.Symlink(filepath.Join(t.TempDir(), "unmounted", "-work-ext"), filepath.Join(dir, "-work-ext")); err != nil {
		t.Skip(err)
	}
	st, err := Parse(dir, time.Now().AddDate(0, 0, -30), 30)
	if err != nil {
		t.Fatal(err)
	}
	if !st.IncompleteEvidence || st.Sessions != 1 {
		t.Errorf("incomplete %v, sessions %d", st.IncompleteEvidence, st.Sessions)
	}
	if fp := Fingerprint([]string{dir}, 30); fp != "" {
		t.Errorf("a tree with a dead link has a fingerprint: %q", fp)
	}
}

// A projects folder that is itself a link is read: nothing under it was, and
// the report said no transcripts were found.
func TestAProjectsFolderThatIsALinkIsRead(t *testing.T) {
	base := t.TempDir()
	writeSession(t, filepath.Join(base, "moved", "-work-here", "a.jsonl"), "a")
	link := filepath.Join(base, "projects")
	if err := os.Symlink(filepath.Join(base, "moved"), link); err != nil {
		t.Skip(err)
	}
	st, err := Parse(link, time.Now().AddDate(0, 0, -30), 30)
	if err != nil || st.Sessions != 1 {
		t.Errorf("sessions %d, err %v", st.Sessions, err)
	}
}
