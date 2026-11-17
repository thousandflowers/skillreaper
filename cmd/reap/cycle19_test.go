package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func nowStamp() string { return time.Now().Add(-time.Hour).UTC().Format(time.RFC3339) }

// A selector that names a platform is about that platform's item: with none
// there, the answer is "not found", not a word about another tool's copy.
func TestASelectorThatMatchesNothingIsNotBlamedOnAnotherPlatform(t *testing.T) {
	deleteHome(t)
	for _, verb := range []string{"prune", "mute", "delete"} {
		var out, errOut bytes.Buffer

		run([]string{verb, "codex:skill:mine", "--yes"}, strings.NewReader(""), &out, &errOut)

		if said := out.String() + errOut.String(); !strings.Contains(said, "found: codex:skill:mine") || strings.Contains(said, "by hand") {
			t.Errorf("%s:\n%s", verb, said)
		}
	}
}

// The money after a prune or a mute is the moved rows' own platforms': a tool
// with no session on record has no monthly cost to claim.
func TestTheSavingOfAToolWithNoSessionsClaimsNoMoney(t *testing.T) {
	home := deleteHome(t)
	mustWriteFile(t, filepath.Join(home, ".claude", "projects", "p", "s.jsonl"),
		`{"type":"user","sessionId":"s","timestamp":"`+nowStamp()+`","message":{"content":"hi"}}`+"\n")
	for _, verb := range []string{"mute", "prune"} {
		var out, errOut bytes.Buffer

		run([]string{verb, "codex:skill:dup", "--yes"}, strings.NewReader(""), &out, &errOut)

		if said := out.String(); !strings.Contains(said, "tok/session") || strings.Contains(said, "$") {
			t.Errorf("%s:\n%s%s", verb, said, errOut.String())
		}
	}
	var out, errOut bytes.Buffer
	run([]string{"prune", "mine", "--yes"}, strings.NewReader(""), &out, &errOut)
	if !strings.Contains(out.String(), "$") {
		t.Errorf("a Claude Code skill, with a session on record, has its money line:\n%s%s", out.String(), errOut.String())
	}
}

// A file reap could not read is "not found" by name, and the answer points at
// the warning that says why.
func TestNotFoundPointsAtTheWarnings(t *testing.T) {
	home := deleteHome(t)
	locked := putSkill(t, home, "zzlocked")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })
	if _, err := os.ReadFile(locked); err == nil {
		t.Skip("this user reads the file all the same")
	}
	for _, verb := range []string{"prune", "why"} {
		var out, errOut bytes.Buffer

		run([]string{verb, "zzlocked"}, strings.NewReader(""), &out, &errOut)

		if said := out.String() + errOut.String(); !strings.Contains(said, "run reap to see what it could not read or judge") {
			t.Errorf("%s:\n%s", verb, said)
		}
	}
}

// Run through npx there is no `skillreaper` on the PATH either: a hint names
// the command that works there.
func TestHintsUnderNpxNameNpx(t *testing.T) {
	npx := "/h/.npm/_npx/ab12/node_modules/skillreaper/bin/skillreaper.js"
	if got := shownName("skillreaper", npx); got != "npx skillreaper" {
		t.Errorf("under npx the command is shown as %q", got)
	}
	if got := shownName("skillreaper", "/usr/local/lib/node_modules/skillreaper/bin/skillreaper.js"); got != "skillreaper" {
		t.Errorf("installed for good, the command is shown as %q", got)
	}
	rewrite := hintRewriterAs("skillreaper", "npx skillreaper", nil)
	if got := string(rewrite([]byte("To prune: reap prune   (reversible via reap restore --all)\n"))); got != "To prune: npx skillreaper prune   (reversible via npx skillreaper restore --all)\n" {
		t.Errorf("hint: %q", got)
	}
}

// --mute-min-tokens 0 turns MUTE off, and the help says so.
func TestHelpSaysWhatZeroMuteMinTokensDoes(t *testing.T) {
	var out, errOut bytes.Buffer
	run([]string{"--help"}, strings.NewReader(""), &out, &errOut)
	said := out.String() + errOut.String()
	at := strings.Index(said, "mute-min-tokens")
	if at < 0 || !strings.Contains(said[at:min(len(said), at+200)], "0 disables") {
		t.Errorf("help does not say what 0 does:\n%s", said[max(at, 0):min(len(said), max(at, 0)+200)])
	}
}
