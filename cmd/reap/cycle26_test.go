package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/scan"
	"github.com/thousandflowers/skillreaper/internal/usage"
)

// A history read only in part is not read: a skill used in the part that was
// lost came out REAP. A database too large for one query, or one record over
// the limit, leaves sessions behind and an error with them.
func TestAHistoryReadInPartCountsAsUnread(t *testing.T) {
	partial := usage.NewStats(30)
	partial.Sessions, partial.IncompleteEvidence = 6, true
	whole := usage.NewStats(30)
	whole.Sessions = 6
	for name, c := range map[string]struct {
		parsed *usage.Stats
		err    error
		unread bool
	}{
		"part of it was lost":       {partial, nil, true},
		"the query failed half way": {whole, errors.New("sqlite3 query output exceeded 52428800 bytes"), true},
		"no sqlite3":                {nil, usage.ErrNoSQLite, true},
		"read whole":                {whole, nil, false},
		"nothing to read":           {nil, nil, false},
	} {
		why, unread := unreadHistory(c.parsed, c.err)
		if unread != c.unread || unread && why == "" {
			t.Errorf("%s: %q, %v", name, why, unread)
		}
	}
}

// Two projects can each have a server of one name. In quarantine they were
// told apart nowhere: the list and the error showed one path twice.
func TestRestoreTellsApartTwoProjectsServersOfOneName(t *testing.T) {
	home := deleteHome(t)
	for i := range 12 {
		mustWriteFile(t, filepath.Join(home, ".claude", "projects", "p", "q"+strings.Repeat("x", i)+".jsonl"),
			`{"type":"user","sessionId":"q`+strings.Repeat("x", i)+`","timestamp":"`+nowStamp()+`","message":{"content":"hi"}}`+"\n")
	}
	mustWriteFile(t, filepath.Join(home, ".claude.json"), `{"projects":{"/work/a":{"mcpServers":{"scoped":{"command":"x"}}},"/work/b":{"mcpServers":{"scoped":{"command":"y"}}}}}`)
	var pruned, list, named, errOut bytes.Buffer
	run([]string{"prune", "--yes"}, strings.NewReader(""), &pruned, &errOut)
	run([]string{"restore"}, strings.NewReader(""), &list, &errOut)
	if said := list.String(); !strings.Contains(said, "/work/a") || !strings.Contains(said, "/work/b") {
		t.Fatalf("the list:\n%s\nprune said:\n%s%s", said, pruned.String(), errOut.String())
	}
	errOut.Reset()
	run([]string{"restore", "scoped"}, strings.NewReader(""), &named, &errOut)
	if said := errOut.String(); !strings.Contains(said, "/work/a") || !strings.Contains(said, "/work/b") {
		t.Errorf("the error:\n%s", said)
	}
}

// The undo line is pasted: a path with a control character in it is not
// printed raw (it can colour the terminal, or split the command).
func TestTheUndoLineNeverPrintsAControlCharacter(t *testing.T) {
	row := report.Row{Item: scan.Item{Category: scan.CatSkill, Name: "ev?[31mil", Path: "/s/ev\x1b[31mil/SKILL.md"}, Tokens: 40}
	twin := report.Row{Item: scan.Item{Category: scan.CatAgent, Name: "ev?[31mil", Path: "/a/ev.md"}}
	var out, errOut bytes.Buffer
	(muteOutcome{muted: []report.Row{row}}).printSummary(options{noNudge: true}, &out, &errOut, &report.Report{Rows: []report.Row{row, twin}}, true)
	if said := out.String(); strings.Contains(said, "\x1b") || !strings.Contains(said, "Undo: reap unmute") {
		t.Errorf("the summary:\n%q", said)
	}
}
