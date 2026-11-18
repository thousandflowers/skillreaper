package reinvest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A report from another version or of another shape is not "no skills
// installed": reinvest went on with it, sent the sessions to be grouped and
// checked a new skill against an empty list.
func TestCycle24_a_report_without_rows_is_not_an_empty_stack(t *testing.T) {
	for _, out := range []string{`{"rows":[{"name":"x"}],"version":"9"}`, `[]`, `"x"`} {
		if _, err := parseReapState(out); err == nil || !strings.Contains(err.Error(), "did not answer with a report") {
			t.Errorf("%s: %v", out, err)
		}
	}
	if _, err := parseReapState(`{"Rows": null, "DeadTokensPerSession": 0}`); err != nil {
		t.Errorf("a stack with nothing in it: %v", err)
	}
}

// The files reinvest keeps are its own: a link in their place is not written
// through, or the file it leads to is overwritten and, for some, made private.
func TestCycle24_no_cache_file_is_written_through_a_link(t *testing.T) {
	for name, write := range map[string]func(string, string) error{"private": writePrivate, "shared": writeShared} {
		dir := t.TempDir()
		target := filepath.Join(dir, "precious.txt")
		if err := os.WriteFile(target, []byte("precious"), 0o644); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(dir, "groups.json")
		if err := os.Symlink(target, link); err != nil {
			t.Skip(err)
		}
		if err := write(link, "{}"); err == nil || !strings.Contains(err.Error(), "is a link") {
			t.Errorf("%s: written through a link: %v", name, err)
		}
		info, _ := os.Stat(target)
		if b, _ := os.ReadFile(target); string(b) != "precious" || info.Mode().Perm() != 0o644 {
			t.Errorf("%s: the file the link leads to holds %q, mode %v", name, b, info.Mode().Perm())
		}
	}
}

// An answer that speaks of none of the kinds of work asked about is no
// answer: it read as "no installed skill covers any of them".
func TestCycle24_a_cover_answer_about_nothing_asked_is_no_answer(t *testing.T) {
	kinds, installed := []kind{{Name: "release", Task: "ship a release"}}, []skill{{Name: "used-c", Desc: "ships releases"}}
	for answer, want := range map[string]bool{`{}`: false, `{"covers": {"0": "used-c"}}`: false, `{"1": "used-c"}`: false, `{"0": null}`: true, `{"0": "used-c"}`: true} {
		a := fakeApp(t)
		a.runProcess = func(procSpec) procResult { return procResult{stdout: answer} }
		if _, answered, err := a.coveringSkills(kinds, installed); err != nil || answered != want {
			t.Errorf("%s: answered %v, err %v", answer, answered, err)
		}
	}
}

// A value of the wrong type is not the answer: {"assign": "x"} marked every
// new session as placed, and {"skills": "x"} was kept for a week as "no results".
func TestCycle24_a_wrongly_typed_answer_is_no_answer(t *testing.T) {
	a := fakeApp(t)
	a.runProcess = func(procSpec) procResult { return procResult{stdout: `{"assign": "x"}`} }
	if _, answered, err := a.assignSessions(nil, []session{{ID: "s1", Prompts: []string{"p"}}}); err != nil || answered {
		t.Errorf("assign as text: answered %v, err %v", answered, err)
	}
	for _, body := range []string{`{"skills": "x"}`, `{"skills": {}}`} {
		b := fakeApp(t)
		asked := 0
		b.skillsSh = func(string) (any, error) {
			asked++
			return js(t, body), nil
		}
		b.searchPage("release notes")
		b.searchPage("release notes")
		if asked != 2 {
			t.Errorf("%s: asked %d times, want 2: it was cached as a page of results", body, asked)
		}
	}
}

// A rejection that gives no reason says so: the report had a name, a colon
// and nothing.
func TestCycle24_a_rejection_with_no_reason_says_so(t *testing.T) {
	if got := joinReasons([]string{""}); got != noReasonGiven {
		t.Errorf("no reason: %q", got)
	}
	if got := joinReasons([]string{"too broad"}); got != "too broad" {
		t.Errorf("a reason: %q", got)
	}
}

// A mute that gave no answer in time may have done its work. A stop while it
// is being undone still names it: it was listed as "not muted".
func TestCycle24_a_stop_while_an_unsure_mute_is_undone_names_it(t *testing.T) {
	tempHome(t)
	a := fakeApp(t)
	stepsRecorded(a, func(cmd []string) error {
		switch {
		case sameWords(cmd, []string{reapName, "--yes", "mute", "second"}):
			return noAnswer(120)
		case sameWords(cmd, []string{reapName, "unmute", "second"}):
			runStopHook()
		}
		return nil
	})
	a.applyOne(writtenSwap("first", "second"))
	said := termText(a)
	if !strings.Contains(said, "?  second: its mute was cut short") || strings.Contains(said, "not muted: second") {
		t.Errorf("the stop does not name the mute that may have been done:\n%s", said)
	}
	runStopHook()
}

// The picker cuts text by the cells it takes: a name in wide characters ran
// past the window's edge and wrapped onto the next row.
func TestCycle24_the_picker_cuts_wide_text_at_the_window(t *testing.T) {
	wide := strings.Repeat("新しいリリース作業の自動化", 6)
	swaps := []swap{{Cands: []candidate{{Kind: kindSkill, Name: wide, Gap: wide, Why: wide}}, OK: true}}
	const width = 60
	for _, d := range frame(swaps, 0, 24, width) {
		used := 0
		for _, r := range d.text {
			used++
			if r > 0x2e80 {
				used++
			}
		}
		if d.x+used > width {
			t.Errorf("row %d ends at cell %d of %d: %q", d.y, d.x+used, width, d.text)
		}
	}
}
