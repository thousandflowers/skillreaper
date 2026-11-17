package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A byte order mark before the first line of a CLAUDE.md hid that line's list
// mark: "- review: ..." protected nothing, and reap why called it a plain word.
func TestAMemoryFileWithABOMIsReadFromItsFirstLine(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("\xef\xbb\xbf- review: reads the diff\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if lines := LoadClaudeMD(t.TempDir(), home, dir); !ClaudeMDReferences(lines, "review") {
		t.Errorf("not protected: %q", lines)
	}
}

// More ways a rules file writes a name, and a letter of any alphabet is part
// of a word: "/обзорный" named the skill "обзор".
func TestMoreWaysANameIsWrittenCycle25(t *testing.T) {
	for line, want := range map[string]bool{
		"- review (reads the diff)": true,
		"- review, deploy":          true,
		"- deploy, review, plan":    true,
		"Skills: review, deploy":    true,
		"Skills: deploy, review":    true,
		"the skill named review":    true,
		"use “review” here":         true,
		"use ‘review’ here":         true,
		"use «review» here":         true,
		"see [review][1]":           true,
		"<code>review</code>":       true,
		"we review, then we deploy": false,
		"always reviewé the diff":   false,
		"run /reviewé now":          false,
		"préreview the diff":        false,
	} {
		if got := ClaudeMDReferences([]string{line}, "review"); got != want {
			t.Errorf("%q: %v, want %v", line, got, want)
		}
	}
	if ClaudeMDReferences([]string{"run /обзорный now"}, "обзор") || ClaudeMDMentions([]string{"обзорный текст"}, "обзор") {
		t.Error("a longer Cyrillic word counted as the name")
	}
	if !ClaudeMDReferences([]string{"run /обзор now"}, "обзор") {
		t.Error("the Cyrillic name as a command is not protected")
	}
}

// A plugin's skill is mentioned when its short name is: reap why said "not
// referenced" of ecc:plan under "always plan the work".
func TestMentionsReadsAPluginsShortName(t *testing.T) {
	if !ClaudeMDMentions([]string{"always plan the work"}, "ecc:plan") {
		t.Error("the short name of a plugin's skill is not a mention")
	}
}

// One long line that repeats a name is read in time that grows with its
// length: 56 KB of "review " took 26 seconds. Twice the line now takes about
// twice the time; it took four times as long.
func TestALongLineOfOneNameIsReadInLinearTime(t *testing.T) {
	read := func(repeats int) time.Duration {
		line := strings.Repeat("review ", repeats)
		start := time.Now()
		ClaudeMDReferences([]string{line}, "review")
		return time.Since(start)
	}
	read(1000) // warm
	short, long := read(4000), read(16000)
	if long > 8*short+50*time.Millisecond { // four times the text: 4x when linear, 16x when quadratic
		t.Errorf("4000 repeats took %v, 16000 took %v", short, long)
	}
}
