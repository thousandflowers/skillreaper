package reinvest

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"strconv"
)

// Grouping the sessions into kinds of work, finding the gaps, asking Claude,
// reading transcripts and reap's report: the paths the other tests leave out.

var errLogicCovBoom = errors.New("boom")

// logicCovLogged sends the app's run log to a buffer the test can read.
func logicCovLogged(a *app) *bytes.Buffer {
	var log bytes.Buffer
	a.log = &log
	return &log
}

// logicCovNotADir is a path under a regular file: nothing can be written there.
func logicCovNotADir(t *testing.T) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "file")
	writeFile(t, file, "x")
	return file
}

func logicCovFixedNow(a *app) time.Time {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	return now
}

func logicCovAskFails(a *app) {
	a.ask = func(string, string, askOptions) (any, error) { return nil, errLogicCovBoom }
}

// --- groups.go ---------------------------------------------------------------

func TestLogicCov_rememberSessions_keeps_the_first_place_and_the_last_record(t *testing.T) {
	a := fakeApp(t)
	writeFile(t, filepath.Join(a.stateDir, memoryFile), `[
		{"id": "x", "prompts": ["first x"], "ts": "1"},
		{"id": "y", "prompts": ["only y"], "ts": "3"},
		{"id": "x", "prompts": ["second x"], "ts": "2"},
		{"id": "cur", "prompts": ["remembered"], "ts": "0"},
		{"id": "bad", "prompts": ["a", 5]},
		5]`)
	current := []session{{ID: "cur", Prompts: []string{"on disk"}, TS: "9", Skills: map[string]bool{}}}
	merged, err := a.rememberSessions(current)
	if err != nil {
		t.Fatal(err)
	}
	var ids, prompts []string
	for _, s := range merged {
		ids, prompts = append(ids, s.ID), append(prompts, s.Prompts[0])
	}
	equalStrings(t, ids, []string{"cur", "x", "y"})
	equalStrings(t, prompts, []string{"on disk", "second x", "only y"})

	saved := js(t, readFile(t, filepath.Join(a.stateDir, memoryFile))).([]any)
	var savedIDs []string
	for _, r := range saved {
		id, _ := r.(*obj).str("id")
		savedIDs = append(savedIDs, id)
	}
	equalStrings(t, savedIDs, []string{"cur", "y", "x"}) // newest first
}

func TestLogicCov_rememberSessions_keeps_at_most_memorySessions(t *testing.T) {
	a := fakeApp(t)
	a.memorySessions = 1
	current := []session{{ID: "old", Prompts: []string{"p"}, TS: "1"}, {ID: "new", Prompts: []string{"p"}, TS: "2"}}
	if _, err := a.rememberSessions(current); err != nil {
		t.Fatal(err)
	}
	saved := js(t, readFile(t, filepath.Join(a.stateDir, memoryFile))).([]any)
	if len(saved) != 1 || saved[0].(*obj).val("id") != "new" {
		t.Errorf("saved %s", dumpJSON(saved))
	}
}

func TestLogicCov_rememberSessions_reports_a_failed_write(t *testing.T) {
	a := fakeApp(t)
	a.stateDir = logicCovNotADir(t)
	if _, err := a.rememberSessions(nil); err == nil {
		t.Error("a memory that could not be written was not reported")
	}
}

func TestLogicCov_loadGroups_errors_stop_the_run(t *testing.T) {
	sessions := []session{{ID: "s1", Prompts: []string{"a"}}, {ID: "s2", Prompts: []string{"b"}}}

	t.Run("grouping fails", func(t *testing.T) {
		a := fakeApp(t)
		a.groupAll = func([]session) ([]*obj, bool, error) { return nil, false, errLogicCovBoom }
		if _, err := a.loadGroups(sessions, true); !errors.Is(err, errLogicCovBoom) {
			t.Errorf("got %v", err)
		}
	})
	t.Run("placing new sessions fails", func(t *testing.T) {
		a := fakeApp(t)
		logicCovFixedNow(a)
		saved := objOf("at", unixSeconds(a), "groups", []any{objOf("name", "A", "task", "t", sessionIDsKey, []any{"s1"})},
			"seen", []any{"s1"})
		writeFile(t, a.cachePath(groupsFile), dumpJSON(saved))
		logicCovAskFails(a)
		if _, err := a.loadGroups(sessions, false); !errors.Is(err, errLogicCovBoom) {
			t.Errorf("got %v", err)
		}
	})
	t.Run("the groups cannot be saved", func(t *testing.T) {
		a := fakeApp(t)
		a.cacheDir = logicCovNotADir(t)
		a.groupAll = func([]session) ([]*obj, bool, error) { return []*obj{objOf("name", "A")}, true, nil }
		if _, err := a.loadGroups(sessions, false); err == nil {
			t.Error("groups that could not be saved were not reported")
		}
	})
}

func TestLogicCov_median_is_Pythons_int_of_statistics_median(t *testing.T) {
	cases := []struct {
		in   []int
		want int
	}{{[]int{3, 1, 2}, 2}, {[]int{5}, 5}, {[]int{10, 1, 3, 2}, 2}, {[]int{4, 6}, 5}, {[]int{7, 0, 7}, 7}}
	for _, c := range cases {
		in := append([]int{}, c.in...)
		if got := median(c.in); got != c.want {
			t.Errorf("median(%v) = %d, want %d", c.in, got, c.want)
		}
		if !equalInts(in, c.in) {
			t.Errorf("median reordered its input: %v", c.in)
		}
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestLogicCov_kindsOfWork_adds_up_each_group(t *testing.T) {
	skilled := func(id, ts string, tokens, errs, interrupts int, skills ...string) session {
		s := session{ID: id, Prompts: []string{"prompt of " + id}, TS: ts, Tokens: tokens, Errors: errs, Interrupts: interrupts,
			Skills: map[string]bool{}}
		for _, name := range skills {
			s.Skills[name] = true
		}
		return s
	}
	sessions := []session{
		skilled("a", "2026-01-05T10:00:00Z", 100, 1, 0, "rel"),
		skilled("b", "2026-01-06T10:00:00Z", 300, 0, 1, "rel", "other"),
		skilled("c", "2026-01-13T10:00:00Z", 200, 0, 0),
		skilled("d", "2026-01-20T10:00:00Z", 900, 2, 0),
		skilled("e", "bad date", 0, 0, 0, "lint"),
	}
	groups := []*obj{
		jsObj(t, `{"name": "Gone", "task": "t", "session_ids": ["zz"]}`), // no session left: no kind
		jsObj(t, `{"name": "Releases", "task": "Cut a release.", "delegable": true, "session_ids": ["a", "b", "c", "d"],
			"queries": ["release"], "mcp_queries": ["github"]}`),
		jsObj(t, `{"name": 5, "task": null, "delegable": "yes", "session_ids": ["c", "d", "e"]}`),
	}
	kinds := kindsOfWork(groups, sessions)
	if len(kinds) != 2 {
		t.Fatalf("%d kinds: %+v", len(kinds), kinds)
	}
	rel := kinds[0]
	if rel.Name != "Releases" || !rel.Delegable || rel.Sessions != 4 || rel.Weeks != 3 || rel.Tokens != 1500 ||
		rel.MedianTokens != 250 || rel.Friction != 1+interruptWeight+2 || rel.CoveredBy != "rel" {
		t.Errorf("releases: %+v", rel)
	}
	equalStrings(t, rel.IDs, []string{"a", "b", "c", "d"})
	equalStrings(t, rel.MCPQueries, []string{"github"})
	other := kinds[1]
	if other.Name != "5" || other.Task != "None" || other.Delegable || other.CoveredBy != "" || other.Weeks != 3 {
		t.Errorf("a kind with odd values: %+v", other) // a skill in one session of three covers nothing
	}
}

func logicCovKind(name string, sessions, friction, medianTokens int) kind {
	return kind{Name: name, Task: "do " + name, Delegable: true, Sessions: sessions, Friction: friction, MedianTokens: medianTokens}
}

func TestLogicCov_findGaps_orders_by_score_and_keeps_six(t *testing.T) {
	a := fakeApp(t)
	open := []kind{
		logicCovKind("plain", 3, 0, 0),         // 3
		logicCovKind("painful", 3, 3, 0),       // 3 * 2 = 6
		logicCovKind("frequent", 5, 0, 0),      // 5
		logicCovKind("heavy", 3, 0, 200_000),   // 3 * 2 = 6, after "painful" on the tie
		logicCovKind("tie-plain", 3, 0, 0),     // 3, after "plain"
		logicCovKind("small", 4, 0, 0),         // 4
		logicCovKind("covered-by-ai", 9, 9, 0), // named as covered below
		logicCovKind("lightest", 3, 0, 1),      // 3.000015, before both plain ones
	}
	ruled := []kind{
		{Name: "rare", Task: "t", Delegable: true, Sessions: 2},
		{Name: "chat", Task: "t", Sessions: 9},
		{Name: "no-task", Delegable: true, Sessions: 9},
		{Name: "used", Task: "t", Delegable: true, Sessions: 9, CoveredBy: "x"},
	}
	calls := answering(t, a, js(t, `{"6": "Release-Tool"}`))
	gaps, _, err := a.findGaps(append(append([]kind{}, ruled...), open...), []skill{{Name: "release-tool", Desc: "cuts releases"}})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, g := range gaps {
		names = append(names, g.Name)
	}
	equalStrings(t, names, []string{"painful", "heavy", "frequent", "small", "lightest", "plain"})
	body := (*calls)[0].body
	if len(*calls) != 1 || (*calls)[0].prompt != coverPrompt || !strings.Contains(body, "[7] do lightest") || strings.Contains(body, "[8]") {
		t.Errorf("the kinds the rules ruled out were sent: %+v", *calls)
	}
}

func TestLogicCov_findGaps_and_coveringSkills_errors(t *testing.T) {
	a := fakeApp(t)
	logicCovAskFails(a)
	if _, _, err := a.findGaps([]kind{logicCovKind("x", 3, 0, 0)}, nil); !errors.Is(err, errLogicCovBoom) {
		t.Errorf("findGaps: %v", err)
	}
	a = fakeApp(t)
	names, answered, err := a.coveringSkills(nil, nil)
	if names != nil || !answered || err != nil {
		t.Errorf("no kinds: %v %v %v (and Claude must not be asked)", names, answered, err)
	}
}

func TestLogicCov_coveringSkills_matches_the_answer_to_an_installed_name(t *testing.T) {
	a := fakeApp(t)
	answering(t, a, js(t, `{"0": "plugin:Release-Tool", "1": 5, "2": "", "3": "not-installed"}`))
	kinds := []kind{logicCovKind("a", 3, 0, 0), logicCovKind("b", 3, 0, 0), logicCovKind("c", 3, 0, 0), logicCovKind("d", 3, 0, 0)}
	names, answered, err := a.coveringSkills(kinds, []skill{{Name: "release-tool"}, {Name: "5"}})
	if err != nil || !answered {
		t.Fatal(answered, err)
	}
	equalStrings(t, names, []string{"release-tool", "5", "", ""}) // str(5) is "5", as Python reads a number
}

// --- llm.go ------------------------------------------------------------------

func TestLogicCov_askClaude_without_claude_stops_the_run(t *testing.T) {
	a := fakeApp(t)
	a.lookPath = func(string) (string, error) { return "", errLogicCovBoom }
	if _, err := a.askClaude("p", "b", askOptions{}); !errors.Is(err, errNoClaude) {
		t.Errorf("got %v", err)
	}
}

func TestLogicCov_askClaude_a_failed_start_is_no_answer_and_no_retry(t *testing.T) {
	a := fakeApp(t)
	log := logicCovLogged(a)
	runs := 0
	a.runProcess = func(procSpec) procResult { runs++; return procResult{err: errLogicCovBoom} }
	answer, err := a.askClaude("p", "b", askOptions{})
	if answer != nil || err != nil || runs != 1 {
		t.Errorf("answer %v, err %v, %d runs", answer, err, runs)
	}
	if !strings.Contains(log.String(), "claude -p: boom") {
		t.Errorf("log: %q", log.String())
	}
}

func TestLogicCov_askClaude_a_failing_exit_is_retried_then_given_up(t *testing.T) {
	a := fakeApp(t)
	log := logicCovLogged(a)
	var specs []procSpec
	a.runProcess = func(spec procSpec) procResult {
		specs = append(specs, spec)
		return procResult{code: 3, stdout: `{"ok": true}`, stderr: strings.Repeat("x", 500) + "TAIL"}
	}
	answer, err := a.askClaude("p", "body", askOptions{think: true, effort: "high"})
	if answer != nil || err != nil || len(specs) != askTries {
		t.Fatalf("answer %v, err %v, %d runs", answer, err, len(specs))
	}
	if !hasPrefixWords(specs[0].argv[len(specs[0].argv)-4:], "--model", model, "--effort", "high") {
		t.Errorf("argv %q", specs[0].argv)
	}
	if specs[0].env[0] != "MAX_THINKING_TOKENS="+thinkTokens || specs[0].stdin != "body" {
		t.Errorf("spec %+v", specs[0])
	}
	line := strings.SplitN(log.String(), "\n", 2)[0]
	if line != "claude -p exit 3: "+strings.Repeat("x", stderrTail-4)+"TAIL" {
		t.Errorf("log keeps more or less than the tail: %q", line)
	}
}

func TestLogicCov_comparedWith_the_document_adds_but_cannot_replace(t *testing.T) {
	var installed []skill
	for i := 0; i < comparedByTask; i++ {
		installed = append(installed, skill{Name: "rel-" + string(rune('a'+i)), Desc: "release"})
	}
	installed = append(installed, skill{Name: "draw", Desc: "figma frames"})
	got := comparedWith("release the tool", "figma figma figma", installed)
	if len(got) != comparedByTask+1 || got[comparedByTask].Name != "draw" {
		t.Errorf("compared with %+v", got)
	}
	for _, s := range got[:comparedByTask] {
		if s.Desc != "release" {
			t.Errorf("the document pushed out a skill the task picked: %+v", got)
		}
	}
}

func TestLogicCov_members_iterates_as_Python(t *testing.T) {
	if got := members("0١2"); len(got) != 3 || got[1] != "١" {
		t.Errorf("a string: %#v", got)
	}
	if got := members(jsObj(t, `{"2": 0, "0": 1}`)); len(got) != 2 || got[0] != "2" || got[1] != "0" {
		t.Errorf("an object: %#v", got)
	}
	list := []any{int64(1)}
	if got := members(list); len(got) != 1 || got[0] != int64(1) {
		t.Errorf("a list: %#v", got)
	}
	if got := members(int64(5)); got != nil {
		t.Errorf("a number: %#v", got)
	}
}

func TestLogicCov_newGroups_takes_each_position_once(t *testing.T) {
	sessions := []session{{ID: "s0"}, {ID: "s1"}, {ID: "s2"}}
	answer := js(t, `[5, "x",
		{"name": "a", "sessions": "10"},
		{"name": "b", "sessions": {"1": true, "2": true}},
		{"name": "c", "sessions": [0, 9, "x", -1, true]},
		{"name": "d"}]`)
	groups := newGroups(answer, sessions, map[int]bool{})
	if len(groups) != 2 {
		t.Fatalf("groups %s", dumpJSON(groupsAsList(groups)))
	}
	if got := dumpJSON(groupsAsList(groups)); got != `[{"name": "a", "session_ids": ["s0", "s1"]}, {"name": "b", "session_ids": ["s2"]}]` {
		t.Errorf("groups %s", got)
	}
}

func groupsAsList(groups []*obj) []any {
	out := make([]any, len(groups))
	for i, g := range groups {
		out[i] = g
	}
	return out
}

func TestLogicCov_groupSessions_errors_and_non_lists(t *testing.T) {
	a := fakeApp(t)
	logicCovAskFails(a)
	if _, _, err := a.groupSessions([]session{{ID: "a", Prompts: []string{"p"}}}); !errors.Is(err, errLogicCovBoom) {
		t.Errorf("got %v", err)
	}
	a = fakeApp(t)
	answering(t, a, js(t, `{"groups": []}`))
	groups, answered, err := a.groupSessions([]session{{ID: "a", Prompts: []string{"p"}}})
	if groups != nil || answered || err != nil {
		t.Errorf("an object for a list is no answer: %v %v %v", groups, answered, err)
	}
}

func TestLogicCov_assignSessions_places_by_group_number(t *testing.T) {
	a := fakeApp(t)
	calls := answering(t, a, js(t, `{"assign": {"0": true, "1": 0, "x": 0, "5": 0, "2": 7, "0١": 1}, "new": "bad"}`))
	groups := []*obj{jsObj(t, `{"task": "first", "session_ids": ["old"]}`), jsObj(t, `{"name": "Second", "task": "t2"}`)}
	fresh := []session{{ID: "n0", Prompts: []string{"p0"}}, {ID: "n1", Prompts: []string{"p1"}}, {ID: "n2", Prompts: []string{"p2"}}}
	placed, answered, err := a.assignSessions(groups, fresh)
	if err != nil || !answered {
		t.Fatal(answered, err)
	}
	// true is group 1, as isinstance(True, int) is in Python; "0١" is position 1 already taken by "1"
	if got := dumpJSON(groupsAsList(placed)); got != `[{"task": "first", "session_ids": ["old", "n1"]}, {"name": "Second", "task": "t2", "session_ids": ["n0"]}]` {
		t.Errorf("placed %s", got)
	}
	if !strings.Contains((*calls)[0].body, "[G0] : first\n[G1] Second: t2") {
		t.Errorf("a group with no name is not shown with an empty one:\n%s", (*calls)[0].body)
	}
}

func TestLogicCov_assignSessions_error(t *testing.T) {
	a := fakeApp(t)
	logicCovAskFails(a)
	if _, _, err := a.assignSessions(nil, []session{{ID: "n", Prompts: []string{"p"}}}); !errors.Is(err, errLogicCovBoom) {
		t.Errorf("got %v", err)
	}
}

// --- sessions.go ---------------------------------------------------------------

func TestLogicCov_userText_content_that_is_neither_text_nor_blocks(t *testing.T) {
	for _, entry := range []string{
		`{"type": "user", "message": {"content": 5}}`,
		`{"type": "user", "message": {"content": [5, {"type": "image"}]}}`,
		`{"type": "user", "message": {"content": "   "}}`,
	} {
		if text, ok := userText(jsObj(t, entry)); ok {
			t.Errorf("%s read as %q", entry, text)
		}
	}
}

func TestLogicCov_parseTimestamp_and_week(t *testing.T) {
	for _, v := range []any{nil, int64(5), "", "yesterday", "2026-13-01T00:00:00Z"} {
		if _, ok := parseTimestamp(v); ok {
			t.Errorf("parseTimestamp(%#v) read a time", v)
		}
	}
	got, ok := parseTimestamp("2026-01-02 03:04:05.5+02:00")
	if !ok || !got.Equal(time.Date(2026, 1, 2, 1, 4, 5, 5e8, time.UTC)) {
		t.Errorf("a space instead of T: %v %v", got, ok)
	}
	weeks := map[string]string{
		"2026-01-05T10:00:00Z": "2026-02", "2026-01-05": "2026-02", "2026-01-05T10:00:00": "2026-02",
		"2027-01-01": "2026-53", "nonsense": "?", "": "?",
	}
	for ts, want := range weeks {
		if got := week(ts); got != want {
			t.Errorf("week(%q) = %q, want %q", ts, got, want)
		}
	}
}

func TestLogicCov_lastActivity_without_any_timestamp(t *testing.T) {
	if _, ok := lastActivity([]*obj{jsObj(t, `{"type": "x"}`), jsObj(t, `{"timestamp": 5}`)}); ok {
		t.Error("a time was found where there is none")
	}
	got, ok := lastActivity([]*obj{jsObj(t, `{"timestamp": "2026-01-01T00:00:00Z"}`), jsObj(t, `{"timestamp": "bad"}`)})
	if !ok || got.Year() != 2026 {
		t.Errorf("the newest usable time was not found: %v", got)
	}
}

func TestLogicCov_lessByParts_orders_folder_by_folder(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"a/b", "a-b", true}, // ("a", "b") < ("a-b",), though "/" sorts after "-"
		{"a-b", "a/b", false},
		{"a/b", "a/b/c", true},
		{"a/b/c", "a/b", false},
		{"a/b", "a/b", false},
	}
	for _, c := range cases {
		if got := lessByParts(c.a, c.b); got != c.want {
			t.Errorf("lessByParts(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestLogicCov_transcriptPaths_without_a_projects_folder(t *testing.T) {
	a := fakeApp(t)
	log := logicCovLogged(a)
	a.projectsDir = filepath.Join(t.TempDir(), "missing")
	if got := a.transcriptPaths(); len(got) != 0 {
		t.Errorf("paths %q", got)
	}
	if !strings.Contains(log.String(), "transcripts:") {
		t.Errorf("the missing folder is not logged: %q", log.String())
	}
}

// A config folder that itself lives under a folder named "subagents" is not a
// subagent's: only the part below the projects folder says what a transcript is.
func TestTranscriptPaths_a_subagents_folder_above_the_projects_folder(t *testing.T) {
	a := fakeApp(t)
	a.projectsDir = filepath.Join(t.TempDir(), "subagents", ".claude", "projects")
	writeFile(t, filepath.Join(a.projectsDir, "p", "mine.jsonl"), "{}\n")
	writeFile(t, filepath.Join(a.projectsDir, "p", "x", "subagents", "agent.jsonl"), "{}\n")
	got := a.transcriptPaths()
	if len(got) != 1 || filepath.Base(got[0]) != "mine.jsonl" {
		t.Errorf("paths %q, want only mine.jsonl", got)
	}
}

func logicCovNeedsPermissions(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a user that file permissions apply to")
	}
}

func TestLogicCov_readSessions_skips_what_cannot_be_read(t *testing.T) {
	logicCovNeedsPermissions(t)
	a := fakeApp(t)
	log := logicCovLogged(a)
	now := time.Now()
	a.now = func() time.Time { return now }
	ts := now.Add(-time.Hour).UTC().Format(time.RFC3339)
	entry := userEntry(ts, "please fix the failing build for me") + "\n"
	project := filepath.Join(a.projectsDir, "p")

	writeFile(t, filepath.Join(project, "good.jsonl"), entry)
	writeFile(t, filepath.Join(project, "old.jsonl"), entry) // its file is older than the window
	old := now.Add(-200 * day)
	if err := os.Chtimes(filepath.Join(project, "old.jsonl"), old, old); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(project, "locked.jsonl"), entry)
	if err := os.Chmod(filepath.Join(project, "locked.jsonl"), 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(project, "nowhere"), filepath.Join(project, "dead.jsonl")); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(project, "closed")
	writeFile(t, filepath.Join(locked, "hidden.jsonl"), entry)
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(locked, 0o755)
		_ = os.Chmod(filepath.Join(project, "locked.jsonl"), 0o644)
	})

	got := a.readSessions(30, nil)
	if len(got) != 1 || got[0].ID != "good" {
		t.Errorf("sessions %+v", got)
	}
	for _, name := range []string{"dead.jsonl", "locked.jsonl", "closed"} {
		if !strings.Contains(log.String(), name) {
			t.Errorf("%s is not in the log:\n%s", name, log.String())
		}
	}
}

func TestLogicCov_addEntry_a_skill_call_naming_no_skill(t *testing.T) {
	e := jsObj(t, `{"type": "assistant", "message": {"usage": {"input_tokens": "12", "output_tokens": null},
		"content": [{"type": "tool_use", "name": "Skill", "input": {"skill": "", "name": 5}}, {"type": "text"}]}}`)
	s := addEntry(session{Skills: map[string]bool{}}, e, nil)
	if !s.Skills[unknownSkill] || s.Tools != 1 || s.Tokens != 12 {
		t.Errorf("session %+v", s)
	}
	if got := invokedSkill(jsObj(t, `{"command": "/ecc:tdd"}`)); got != "/ecc:tdd" {
		t.Errorf("an older Claude Code's key: %q", got)
	}
}

// --- reap.go -------------------------------------------------------------------

func TestLogicCov_loadReapState_explains_every_failure(t *testing.T) {
	cases := []struct {
		name    string
		command func() ([]string, error)
		result  procResult
		want    string
	}{
		{"not found", func() ([]string, error) { return nil, errLogicCovBoom }, procResult{},
			"could not find the reap binary to ask (set REAP to its path): boom"},
		{"found nothing", func() ([]string, error) { return nil, nil }, procResult{},
			"could not find the reap binary"},
		{"timed out", nil, procResult{timedOut: true}, "`/bin/reap --json` gave no answer in 10m0s"},
		{"did not start", nil, procResult{err: errLogicCovBoom}, "could not run reap (set REAP to its path): boom"},
		{"failed", nil, procResult{code: 2, stdout: "panic", stderr: "  " + strings.Repeat("e", 300) + "END\n"},
			"`/bin/reap --json` exited 2: " + strings.Repeat("e", reapTailSize-3) + "END"},
		{"bad JSON, exit 0", nil, procResult{stdout: "not json"}, "did not answer with JSON"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := fakeApp(t)
			a.reapCommand = c.command
			if a.reapCommand == nil {
				a.reapCommand = func() ([]string, error) { return []string{"/bin/reap"}, nil }
			}
			a.runProcess = func(procSpec) procResult { return c.result }
			_, err := a.loadReapState()
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("got %v, want %q", err, c.want)
			}
			if c.result.err != nil && !errors.Is(err, errLogicCovBoom) {
				t.Errorf("the cause is not kept: %v", err)
			}
		})
	}
}

// reap exits non-zero only for a scan that failed or a report it could not
// write: what it printed then is no report, however well it parses.
func TestLogicCov_loadReapState_reads_a_report_only_from_a_run_that_worked(t *testing.T) {
	a := fakeApp(t)
	a.reapCommand = func() ([]string, error) { return []string{"/bin/reap"}, nil }
	code := 1
	a.runProcess = func(procSpec) procResult {
		return procResult{code: code, stdout: `{"DeadTokensPerSession": 42, "Rows": [5, {"Category": "skill"},
			{"Name": "s", "Category": "skill", "Tokens": 10, "Uses": 0, "Verdict": "REAP"},
			{"Name": "m", "Category": "mcp", "WeightUnknown": true}, {"Name": "w", "Category": "mcp", "Tokens": "7"}]}`}
	}
	if _, err := a.loadReapState(); err == nil || !strings.Contains(err.Error(), "exited 1") {
		t.Fatalf("a run that failed was read as a report: %v", err)
	}
	code = 0
	state, err := a.loadReapState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Dead != 42 || len(state.Installed) != 1 || len(state.Reap) != 1 || len(state.Used) != 0 ||
		state.MCP["m"] != nil || state.MCP["w"] == nil || *state.MCP["w"] != 7 {
		t.Errorf("state %+v", state)
	}
}

// "with no skill" counts every kind of work with no skill, and says when only
// the first few are taken up. It used to print the cap (6) as if it were the count.
func TestGaps_the_count_is_not_the_cap(t *testing.T) {
	a := fakeApp(t)
	answering(t, a, js(t, `{"0": null}`)) // no skill covers them: an answer has to speak of a kind
	kinds := make([]kind, maxGaps+2)
	for i := range kinds {
		kinds[i] = kind{Name: "k" + strconv.Itoa(i), Task: "t", Delegable: true, Sessions: 9}
	}
	gaps, open, err := a.findGaps(kinds, []skill{{Name: "x", Desc: "y"}})
	if err != nil || len(gaps) != maxGaps || open != maxGaps+2 {
		t.Fatalf("got %d gaps of %d open (%v)", len(gaps), open, err)
	}
	for _, c := range []struct {
		kinds, open, taken int
		want               string
	}{
		{8, 8, 6, "8 kinds of work · 8 with no skill · the first 6 taken up"},
		{8, 3, 3, "8 kinds of work · 3 with no skill"},
		{0, 0, 0, "0 kinds of work · 0 with no skill"},
	} {
		if got := gapsLine(c.kinds, c.open, c.taken); got != c.want {
			t.Errorf("gapsLine(%d, %d, %d) = %q, want %q", c.kinds, c.open, c.taken, got, c.want)
		}
	}
}
