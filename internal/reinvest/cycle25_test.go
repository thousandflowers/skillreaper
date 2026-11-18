package reinvest

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// With --online, a search that could not be made is not a search that found
// nothing: a 403 and a host that does not answer read "0 candidates" and the
// cause was in the log only.
func TestCycle25_a_search_that_failed_is_said(t *testing.T) {
	a, _ := pipelineApp(t, verdict{Keep: true, Risk: noRisk, Fit: 5, Reason: "fits", Use: "x"})
	a.skillsSh = func(string) (any, error) { return nil, errors.New("HTTP Error 403: Forbidden") }
	if err := a.run(options{days: defaultDays, max: defaultSwaps, online: true}); err != nil {
		t.Fatal(err)
	}
	said := termText(a)
	if !strings.Contains(said, "✗") || !strings.Contains(said, "could not be made") || !strings.Contains(said, "HTTP Error 403") {
		t.Errorf("a failed search is not said:\n%s", said)
	}
	b, _ := pipelineApp(t, verdict{Keep: true, Risk: noRisk, Fit: 5, Reason: "fits", Use: "x"})
	b.skillsSh = func(string) (any, error) { return js(t, `{"skills": []}`), nil }
	if err := b.run(options{days: defaultDays, max: defaultSwaps, online: true}); err != nil {
		t.Fatal(err)
	}
	if said := termText(b); strings.Contains(said, "could not be made") {
		t.Errorf("a search that found nothing is called failed:\n%s", said)
	}
}

// A candidate kept with neither a use nor a reason has words under it all the
// same: the line under "for <the work>" was blank.
func TestCycle25_a_kept_candidate_always_says_why(t *testing.T) {
	c := candidate{ID: "a/b", Name: "b", Doc: "doc"}
	passing, _ := sortVerdicts([]candidate{c}, []string{"k"}, map[string]verdict{"k": {Keep: true, Risk: noRisk, Fit: 4}}, nil)
	if len(passing) != 1 || passing[0].Why != noReasonGiven {
		t.Errorf("why: %+v", passing)
	}
	passing, _ = sortVerdicts([]candidate{c}, []string{"k"}, map[string]verdict{"k": {Keep: true, Risk: noRisk, Fit: 4, Reason: "fits the work"}}, nil)
	if len(passing) != 1 || passing[0].Why != "fits the work" {
		t.Errorf("why from the reason: %+v", passing)
	}
}

// An answer that is JSON and not the object asked for is said, as the calls
// before it say theirs.
func TestCycle25_a_cover_answer_of_another_shape_says_why(t *testing.T) {
	a := fakeApp(t)
	a.runProcess = func(procSpec) procResult { return procResult{stdout: `["used-c"]`} }
	if _, answered, err := a.coveringSkills([]kind{{Name: "r", Task: "ship"}}, []skill{{Name: "used-c", Desc: "ships"}}); err != nil || answered {
		t.Fatalf("answered %v, err %v", answered, err)
	}
	a.failed("checking your installed skills got no answer")
	if said := termText(a); !strings.Contains(said, "claude -p: ") {
		t.Errorf("no reason is given:\n%s", said)
	}
}

// A mute that gave no answer and could not be undone may still be in place:
// a stop during the rest of the rollback listed it as "not muted".
func TestCycle25_a_stop_in_the_rollback_names_a_mute_that_could_not_be_undone(t *testing.T) {
	tempHome(t)
	a := fakeApp(t)
	stepsRecorded(a, func(cmd []string) error {
		switch {
		case sameWords(cmd, []string{reapName, "--yes", "mute", "second"}):
			return noAnswer(120)
		case sameWords(cmd, []string{reapName, "unmute", "second"}):
			return errors.New("exit 1")
		case sameWords(cmd, []string{reapName, "unmute", "first"}):
			runStopHook()
		}
		return nil
	})
	a.applyOne(writtenSwap("first", "second", "third"))
	said := termText(a)
	if !strings.Contains(said, "?  second: its mute was cut short") || strings.Contains(said, "not muted: second") || !strings.Contains(said, "not muted: third") {
		t.Errorf("the stop does not name the mute that may be in place:\n%s", said)
	}
	runStopHook()
}

// "Rows" is a list, or null for a stack with nothing in it.
func TestCycle25_rows_of_another_type_are_no_report(t *testing.T) {
	for _, out := range []string{`{"Rows": "x"}`, `{"Rows": {}}`, `{"Rows": 3}`} {
		if _, err := parseReapState(out); err == nil || !strings.Contains(err.Error(), "did not answer with a report") {
			t.Errorf("%s: %v", out, err)
		}
	}
	if _, err := parseReapState(`{"Rows": []}`); err != nil {
		t.Errorf("an empty list: %v", err)
	}
}

// A projects folder that cannot be read is not "0 sessions".
func TestCycle25_an_unreadable_projects_folder_is_said(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	a, _ := pipelineApp(t, verdict{Keep: true, Risk: noRisk, Fit: 5, Reason: "fits", Use: "x"})
	if err := os.Chmod(a.projectsDir, 0); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { _ = os.Chmod(a.projectsDir, 0o755) })
	_ = a.run(options{days: defaultDays, max: defaultSwaps})
	if said := termText(a); !strings.Contains(said, "✗") || !strings.Contains(said, "could not be read") {
		t.Errorf("an unreadable projects folder is not said:\n%s", said)
	}
}
