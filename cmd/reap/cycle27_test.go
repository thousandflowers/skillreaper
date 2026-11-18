package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/evidence"
	"github.com/thousandflowers/skillreaper/internal/prune"
	"github.com/thousandflowers/skillreaper/internal/usage"
)

// Each entry of "is in quarantine N times" reads as an id, where it was, and
// the project when it had one: the two ran together and ended in "))".
func TestAQuarantinedEntryIsNamedWithItsProject(t *testing.T) {
	plain := prune.Entry{ID: "003", ConfigPath: "/h/.claude.json"}
	scoped := prune.Entry{ID: "004", ConfigPath: "/h/.claude.json", JSONScope: "/work/my proj"}
	if got := quarantined(plain); got != "003 (/h/.claude.json)" {
		t.Errorf("no project: %q", got)
	}
	if got := quarantined(scoped); got != "004 (/h/.claude.json, project /work/my proj)" {
		t.Errorf("a project: %q", got)
	}
}

// One cause, one warning. A history read in part is said as incomplete; the
// read's own error then said it again.
func TestAPartialReadIsWarnedOfOnce(t *testing.T) {
	failed := errors.New("sqlite3 query output exceeded 52428800 bytes")
	partial, whole := usage.NewStats(30), usage.NewStats(30)
	partial.Sessions, partial.IncompleteEvidence = 6, true
	whole.Sessions = 6
	for name, c := range map[string]struct {
		parsed *usage.Stats
		err    error
		want   bool
	}{
		"read whole":                   {whole, nil, false},
		"failed with sessions in hand": {whole, failed, true},
		"failed, and said as partial":  {partial, failed, false},
		"failed with nothing":          {nil, failed, false},
		"no sqlite3":                   {nil, usage.ErrNoSQLite, false},
	} {
		if got := readFailureIsNews(c.parsed, c.err); got != c.want {
			t.Errorf("%s: %v", name, got)
		}
	}
}

// A folder whose only transcript could not be read is not "no session
// transcripts were found": the line before it said one was over the limit.
func TestATranscriptOverTheLimitIsNotNoTranscripts(t *testing.T) {
	home := deleteHome(t)
	projects := filepath.Join(home, ".claude", "projects", "p")
	if err := os.Rename(filepath.Join(projects, "s.jsonl"), filepath.Join(home, "s.jsonl.away")); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","sessionId":"big","timestamp":"` + nowStamp() + `","message":{"content":"` + strings.Repeat("x", 11<<20) + `"}}` + "\n"
	mustWriteFile(t, filepath.Join(projects, "big.jsonl"), line)
	var out, errOut bytes.Buffer
	run([]string{"--no-cache"}, strings.NewReader(""), &out, &errOut)
	said := strings.Join(strings.Fields(out.String()+errOut.String()), " ") // the warnings are wrapped
	if !strings.Contains(said, "Claude Code usage evidence is incomplete") || strings.Contains(said, "Claude Code usage is not counted") {
		t.Errorf("warnings:\n%s", said[max(0, len(said)-1500):])
	}
}

// A record of past uses that cannot be read is said, set aside and not
// written over; what it knew is not known, so nothing is condemned this run.
func TestAnUnreadableRecordOfPastUsesIsSaidAndKept(t *testing.T) {
	home := deleteHome(t)
	path := evidence.Path(filepath.Join(home, ".claude"))
	mustWriteFile(t, path, `{"format":1,"sources":{"cut`)
	var out, errOut bytes.Buffer
	run([]string{"--no-cache"}, strings.NewReader(""), &out, &errOut)
	said := strings.Join(strings.Fields(out.String()+errOut.String()), " ")
	if !strings.Contains(said, "record of past uses") || !strings.Contains(said, "could not be read") {
		t.Errorf("no warning:\n%s", said[max(0, len(said)-900):])
	}
	if b, err := os.ReadFile(path + ".unreadable"); err != nil || !strings.Contains(string(b), `"cut`) {
		t.Errorf("the unreadable record was not kept aside: %v", err)
	}
}

// OpenCode loads ~/.claude/skills, not another folder: a second config
// folder's skills are not held on its account. And with --claude-dir on the
// usual folder, OpenCode is not read: its history counts as not read.
func TestWhichToolsReadTheFolder(t *testing.T) {
	home := t.TempDir()
	usual := filepath.Join(home, ".claude")
	if got := folderReaders(usual, home); len(got["claude-code"]) == 0 {
		t.Errorf("the usual folder has no other reader: %v", got)
	}
	if got := folderReaders(filepath.Join(home, ".claude-work"), home); len(got) != 0 {
		t.Errorf("another folder has readers: %v", got)
	}
}

// A CLAUDE.md that is there and cannot be read may name skills: it is said,
// and nothing is condemned. It stopped protecting them without a word.
func TestAnUnreadableCLAUDEMDIsSaid(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	home := deleteHome(t)
	memory := filepath.Join(home, ".claude", "CLAUDE.md")
	mustWriteFile(t, memory, "Use the `mine` skill.\n")
	if err := os.Chmod(memory, 0); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { _ = os.Chmod(memory, 0o644) })
	var out, errOut bytes.Buffer
	run([]string{"--no-cache"}, strings.NewReader(""), &out, &errOut)
	said := strings.Join(strings.Fields(out.String()+errOut.String()), " ")
	if !strings.Contains(said, "CLAUDE.md") || !strings.Contains(said, "could not be read") {
		t.Errorf("no warning:\n%s", said[max(0, len(said)-900):])
	}
}
