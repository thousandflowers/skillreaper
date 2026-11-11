package reinvest

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Checks for the fixes to the README's known issues.

func entryLine(ts string) string {
	return dumpJSON(objOf("type", "user", "timestamp", ts, "cwd", "/tmp", "entrypoint", "cli",
		"message", objOf("role", "user", "content", "please fix the release workflow")))
}

// Known issue 7: counts moved because mtime moves when Claude Code appends metadata to old transcripts.
func TestSessionWindow_window_uses_last_entry_not_mtime(t *testing.T) {
	a := fakeApp(t)
	now := time.Now()
	iso := func(ago time.Duration) string { return now.Add(-ago).UTC().Format("2006-01-02T15:04:05Z") }
	old := filepath.Join(a.projectsDir, "p", "old.jsonl")
	writeFile(t, old, entryLine(iso(24*time.Hour))+"\n"+dumpJSON(objOf("type", "atis-latch"))+"\n")
	writeFile(t, filepath.Join(a.projectsDir, "p", "live.jsonl"), entryLine(iso(time.Minute))+"\n")
	if err := os.Chtimes(old, now, now); err != nil { // touched a minute ago, like the batched metadata appends
		t.Fatal(err)
	}
	var got []string
	a.readTranscripts(120, func(tr transcript) {
		got = append(got, strings.TrimSuffix(filepath.Base(tr.path), transcriptExt))
	})
	equalStrings(t, got, []string{"old"})
}

// Known issue 6: weights now match reap's formula (checked against all 301 installed skills).
func TestReapTokens_formula(t *testing.T) {
	doc := "---\nname: x\ndescription: \"Sorts files\"\n---\nbody"
	if got, want := reapTokens("file-organizer", doc), tokensFor("file-organizerSorts files"); got != want {
		t.Errorf("got %d, want %d", got, want)
	}
}

func TestReapTokens_manual_only_skill_weighs_nothing(t *testing.T) {
	doc := "---\ndescription: a long description\ndisable-model-invocation: true\n---\n"
	if got := reapTokens("ab", doc); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestReapTokens_block_scalar_counts_as_reap_reads_it(t *testing.T) {
	// "ab" + "long text\n" = 12 bytes -> 4 tokens: the whole folded description, as reap now reads it
	if got := reapTokens("ab", "---\ndescription: >\n  long text\n---\n"); got != 4 {
		t.Errorf("got %d, want 4", got)
	}
}

// Known issues 4 and 5.
func TestVerification_kept_only_if_every_run_keeps(t *testing.T) {
	if combine([]verdict{{Keep: true, Fit: 4}, {Keep: false, Reason: "no"}}).Keep {
		t.Error("kept although one run rejected")
	}
	if got := combine([]verdict{{Keep: true, Fit: 4}, {Keep: true, Fit: 2}}).Fit; got != 2 {
		t.Errorf("fit %d, want 2", got)
	}
}

func TestVerification_best_fit_not_first(t *testing.T) {
	passing := []candidate{{Gap: "g", Name: "first", Fit: 2, Cost: intPtr(50)}, {Gap: "g", Name: "better", Fit: 5, Cost: intPtr(90)}}
	equalStrings(t, candNames(bestPerGap(passing, 3)), []string{"better"})
}

func TestVerification_verify_rejects_a_reported_risk_even_if_kept(t *testing.T) {
	a := fakeApp(t)
	alwaysAnswering(a, jsObj(t, `{"keep": true, "fit": 5, "risk": "sends your files out"}`))
	got, _ := a.verifyOne(verifyRequest{Task: "t", Doc: "doc"})
	if got.Keep || got.Reason != "security: sends your files out" {
		t.Errorf("got %+v", got)
	}
}

func TestVerification_verify_without_a_security_answer_is_not_kept(t *testing.T) {
	a := fakeApp(t)
	alwaysAnswering(a, jsObj(t, `{"keep": true, "fit": 5}`))
	if got, _ := a.verifyOne(verifyRequest{Task: "t", Doc: "doc"}); got != failedVerdict {
		t.Errorf("got %+v", got)
	}
}

func TestVerification_verify_keeps_when_no_risk(t *testing.T) {
	a := fakeApp(t)
	alwaysAnswering(a, jsObj(t, `{"keep": true, "fit": 5, "risk": "none"}`))
	if got, _ := a.verifyOne(verifyRequest{Task: "t", Doc: "doc"}); !got.Keep {
		t.Errorf("got %+v", got)
	}
}

var plainCand = candidate{Task: "t", ID: "a/b/c", Doc: "d", Name: "c", Kind: kindSkill, Cost: intPtr(10)}

func TestVerification_verdicts_not_reused_when_the_check_changes(t *testing.T) {
	a := fakeApp(t)
	calls := verifying(a, always(verdict{Keep: true, Fit: 3, Use: "u"}))
	if _, _, err := a.verifyAll([]candidate{plainCand}, nil, nil); err != nil {
		t.Fatal(err)
	}
	a.verifyPrompt = "a stricter check"
	if _, _, err := a.verifyAll([]candidate{plainCand}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2*verifyRuns {
		t.Errorf("%d calls, want %d", len(*calls), 2*verifyRuns)
	}
}

func TestVerification_verdicts_cached(t *testing.T) {
	a := fakeApp(t)
	calls := verifying(a, always(verdict{Keep: true, Fit: 3, Use: "u"}))
	for range 2 {
		if _, _, err := a.verifyAll([]candidate{plainCand}, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if len(*calls) != verifyRuns {
		t.Errorf("%d calls, want %d", len(*calls), verifyRuns)
	}
}

// Known issues 2 and 3.
func TestMcpChecks_command_runners_rejected(t *testing.T) {
	for _, doc := range []string{"tools: run_command, list_apps", "It can execute commands or AppleScript", "an applescript runner"} {
		if len(risks(doc, true)) == 0 {
			t.Errorf("not flagged: %s", doc)
		}
	}
	if got := risks("tools: get_file, list_frames", true); len(got) != 0 {
		t.Errorf("flagged: %v", got)
	}
}

func TestMcpChecks_unknown_weight_mutes_nothing(t *testing.T) {
	mcp := candidate{Gap: "g", Name: "srv", Fit: 4, Cost: nil}
	pool := []skill{{Name: "old", Tokens: 300}}
	if built := build([]candidate{mcp}, []candidate{mcp}, pool); len(built) != 1 || len(built[0].Mute) != 0 {
		t.Errorf("got %+v", built)
	}
	if got := pair(nil, pool); len(got) != 0 {
		t.Errorf("got %+v", got)
	}
}

// Known issue 1: a failure mid-swap leaves nothing half-done.
func applySwap() swap {
	c := candidate{Kind: kindSkill, Name: "new", Source: "o/r", Slug: "new"}
	return swap{Cands: []candidate{c}, Mute: []skill{{Name: "a"}, {Name: "b"}}}
}

func TestApply_failed_mute_rolls_back(t *testing.T) {
	tempHome(t)
	a := fakeApp(t)
	calls := stepsRecorded(a, func(cmd []string) error {
		if hasPrefixWords(cmd, "reap", "--yes", "mute", "b") {
			return errors.New("no skill or agent found: b")
		}
		return nil
	})
	a.isInstalled = func(candidate) bool { return true }
	a.unchecked = func(candidate) []string { return nil }
	log := a.applyOne(applySwap())
	if !containsCmd(*calls, "reap", "unmute", "a") {
		t.Errorf("a was not unmuted: %v", *calls)
	}
	last := (*calls)[len(*calls)-1]
	if !hasPrefixWords(last, append(append([]string{}, skillsCLI...), "remove", "new")...) {
		t.Errorf("the last step was %v, not the uninstall", last)
	}
	if !strings.HasSuffix(log[len(log)-1], "nothing changed") {
		t.Errorf("log ends with %q", log[len(log)-1])
	}
}

func TestApply_install_not_for_claude_code_mutes_nothing(t *testing.T) {
	tempHome(t)
	a := fakeApp(t)
	calls := stepsRecorded(a, nil)
	a.isInstalled = func(candidate) bool { return false }
	a.applyOne(applySwap())
	if len(*calls) != 1 {
		t.Errorf("%d steps, want 1: %v", len(*calls), *calls)
	}
}

func TestApply_skills_add_targets_claude_code_only(t *testing.T) {
	cmd, ok := installCommand(applySwap().cand())
	if !ok || !containsString(cmd, "claude-code") {
		t.Errorf("got %v", cmd)
	}
}

// An MCP swap mutes nothing at install; once reap has weighed the server, the next run offers the mute.
func mcpSwap(t *testing.T) swap {
	server := jsObj(t, `{"name": "io.github.x/docs", "remotes": [{"type": "streamable-http", "url": "https://x"}]}`)
	c := candidate{Kind: kindMCP, Name: "io.github.x/docs", Gap: "g", Why: "w", Server: server}
	return swap{Cands: []candidate{c}}
}

func weights(pairs map[string]int, unweighed ...string) map[string]*int {
	out := map[string]*int{}
	for name, n := range pairs {
		out[name] = intPtr(n)
	}
	for _, name := range unweighed {
		out[name] = nil
	}
	return out
}

func settled(t *testing.T, a *app, w map[string]*int, pool []skill) []swap {
	t.Helper()
	out, err := a.settle(w, pool)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func installedMCP(t *testing.T) *app {
	t.Helper()
	tempHome(t)
	a := fakeApp(t)
	stepsRecorded(a, nil)
	a.applyOne(mcpSwap(t))
	return a
}

func TestWeighLater_installed_mcp_is_remembered_then_offered_once_weighed(t *testing.T) {
	a := installedMCP(t)
	pool := []skill{{Name: "old", Tokens: 120}}
	if got := settled(t, a, weights(nil, "docs"), pool); len(got) != 0 { // not weighed yet: nothing to offer, kept
		t.Errorf("offered before it was weighed: %+v", got)
	}
	got := settled(t, a, weights(map[string]int{"docs": 118}), pool)
	if len(got) != 1 || len(got[0].Mute) != 1 || got[0].Mute[0].Name != "old" || !got[0].Cands[0].Installed {
		t.Errorf("got %+v", got)
	}
}

func TestWeighLater_uninstalled_server_is_forgotten(t *testing.T) {
	a := installedMCP(t)
	pool := []skill{{Name: "old", Tokens: 120}}
	if got := settled(t, a, weights(nil), pool); len(got) != 0 {
		t.Errorf("got %+v", got)
	}
	if got := settled(t, a, weights(map[string]int{"docs": 118}), pool); len(got) != 0 {
		t.Errorf("a removed server came back: %+v", got)
	}
}

func TestWeighLater_accepting_mutes_without_installing_then_forgets(t *testing.T) {
	a := installedMCP(t)
	pool := []skill{{Name: "old", Tokens: 120}}
	s := settled(t, a, weights(map[string]int{"docs": 118}), pool)[0]
	calls := stepsRecorded(a, nil)
	a.applyOne(s)
	if len(*calls) != 1 || !sameWords((*calls)[0], []string{"reap", "--yes", "mute", "old"}) {
		t.Errorf("steps: %v", *calls)
	}
	if got := settled(t, a, weights(map[string]int{"docs": 118}), pool); len(got) != 0 {
		t.Errorf("offered again: %+v", got)
	}
}

func TestWeighLater_failed_mute_of_settled_swap_keeps_server(t *testing.T) {
	a := installedMCP(t)
	s := settled(t, a, weights(map[string]int{"docs": 300}), []skill{{Name: "a", Tokens: 150}, {Name: "b", Tokens: 150}})[0]
	calls := stepsRecorded(a, func(cmd []string) error {
		if sameWords(cmd, []string{"reap", "--yes", "mute", "b"}) {
			return errors.New("boom")
		}
		return nil
	})
	a.applyOne(s)
	for _, cmd := range *calls {
		if containsString(cmd, "remove") {
			t.Errorf("the server was removed: %v", cmd)
		}
	}
	if got := settled(t, a, weights(map[string]int{"docs": 300}), []skill{{Name: "a", Tokens: 150}}); len(got) != 1 {
		t.Errorf("the server was forgotten: %+v", got)
	}
}

// reap's text report points at the proposals reinvest leaves in swaps.json.
func TestReapHint_cache_is_where_reap_reads(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the prototype checks this on macOS only")
	}
	home := tempHome(t)
	if got, want := defaultCacheDir(), filepath.Join(home, "Library", "Caches", "skillreaper", "reinvest"); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	base, err := os.UserCacheDir()
	if err != nil || filepath.Join(base, "skillreaper", "reinvest") != defaultCacheDir() {
		t.Errorf("reap looks in %s (%v), reinvest writes in %s", base, err, defaultCacheDir())
	}
}

func TestReapHint_proposals_saved_then_applied_ones_dropped(t *testing.T) {
	a := fakeApp(t)
	mk := func(n string) swap {
		return swap{Cands: []candidate{{Name: n}}, Mute: []skill{{Name: "old-" + n}}}
	}
	first, second := mk("a"), mk("b")
	if err := a.saveProposals([]swap{first, second}); err != nil {
		t.Fatal(err)
	}
	installs := func() []string {
		var out []string
		for _, x := range jsObj(t, readFile(t, a.cachePath(swapsFile))).list("swaps") {
			name, _ := x.(*obj).str("install")
			out = append(out, name)
		}
		return out
	}
	saved := jsObj(t, readFile(t, a.cachePath(swapsFile)))
	equalStrings(t, installs(), []string{"a", "b"})
	// the prototype's command named reinvest.py; the port's names the subcommand
	if command, _ := saved.str("command"); !strings.Contains(command, "reinvest") {
		t.Errorf("command %q", command)
	}
	at, _ := saved.str("at")
	if _, err := time.Parse(time.RFC3339, at); err != nil {
		t.Errorf("at %q is not a time reap can read: %v", at, err)
	}
	if err := a.dropProposals([]swap{first}); err != nil {
		t.Fatal(err)
	}
	equalStrings(t, installs(), []string{"b"})
}

// Groups are rebuilt from scratch weekly; in between only new sessions are placed, so proposals stay put.
func sess(ids ...string) []session {
	out := make([]session, len(ids))
	for i, id := range ids {
		out[i] = session{ID: id, Prompts: []string{"prompt " + id}, Skills: map[string]bool{}}
	}
	return out
}

const fullGrouping = `[{"name": "releasing", "task": "t", "sessions": [0, 1]}]`

func grouped(t *testing.T, a *app, regroup bool, ids ...string) []*obj {
	t.Helper()
	groups, err := a.loadGroups(sess(ids...), regroup)
	if err != nil {
		t.Fatal(err)
	}
	return groups
}

func prompts(calls []asked) []string {
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = c.prompt
	}
	return out
}

func TestIncrementalGroups_first_run_groups_everything(t *testing.T) {
	a := fakeApp(t)
	calls := answering(t, a, js(t, fullGrouping))
	groups := grouped(t, a, false, "a", "b")
	equalStrings(t, sessionIDsOf(groups[0]), []string{"a", "b"})
	equalStrings(t, prompts(*calls), []string{groupPrompt})
}

func TestIncrementalGroups_new_session_is_placed_not_regrouped(t *testing.T) {
	a := fakeApp(t)
	calls := answering(t, a, js(t, fullGrouping), js(t, `{"assign": {"0": 0}, "new": []}`))
	grouped(t, a, false, "a", "b")
	groups := grouped(t, a, false, "a", "b", "c")
	equalStrings(t, sessionIDsOf(groups[0]), []string{"a", "b", "c"})
	equalStrings(t, prompts(*calls), []string{groupPrompt, assignPrompt})
}

func TestIncrementalGroups_new_kind_of_work_becomes_a_group(t *testing.T) {
	a := fakeApp(t)
	answering(t, a, js(t, fullGrouping), js(t, `{"assign": {}, "new": [{"name": "blender", "task": "t", "sessions": [0, 1]}]}`))
	grouped(t, a, false, "a", "b")
	groups := grouped(t, a, false, "a", "b", "c", "d")
	if len(groups) != 2 {
		t.Fatalf("%d groups, want 2", len(groups))
	}
	equalStrings(t, sessionIDsOf(groups[0]), []string{"a", "b"})
	equalStrings(t, sessionIDsOf(groups[1]), []string{"c", "d"})
}

func TestIncrementalGroups_nothing_new_asks_nothing(t *testing.T) {
	a := fakeApp(t)
	calls := answering(t, a, js(t, fullGrouping))
	grouped(t, a, false, "a", "b")
	grouped(t, a, false, "a", "b")
	if len(*calls) != 1 {
		t.Errorf("%d calls, want 1", len(*calls))
	}
}

func TestIncrementalGroups_left_out_session_not_sent_again(t *testing.T) {
	a := fakeApp(t)
	calls := answering(t, a, js(t, fullGrouping), js(t, `{"assign": {}, "new": []}`))
	grouped(t, a, false, "a", "b")
	grouped(t, a, false, "a", "b", "chitchat")
	grouped(t, a, false, "a", "b", "chitchat")
	if len(*calls) != 2 {
		t.Errorf("%d calls, want 2", len(*calls))
	}
}

func TestIncrementalGroups_failed_call_retries_next_run(t *testing.T) {
	a := fakeApp(t)
	answering(t, a, js(t, fullGrouping), nil, js(t, `{"assign": {"0": 0}, "new": []}`))
	grouped(t, a, false, "a", "b")
	grouped(t, a, false, "a", "b", "c")
	groups := grouped(t, a, false, "a", "b", "c")
	equalStrings(t, sessionIDsOf(groups[0]), []string{"a", "b", "c"})
}

func TestIncrementalGroups_week_old_or_old_format_regroups(t *testing.T) {
	a := fakeApp(t)
	writeFile(t, a.cachePath(groupsFile), `[{"name": "x", "session_ids": ["a"]}]`)
	calls := answering(t, a, js(t, fullGrouping), js(t, fullGrouping))
	grouped(t, a, false, "a", "b")
	saved := jsObj(t, readFile(t, a.cachePath(groupsFile)))
	at, _ := number(saved.val("at"))
	writeFile(t, a.cachePath(groupsFile), dumpJSON(saved.with("at", at-regroupEvery.Seconds()-1)))
	grouped(t, a, false, "a", "b")
	equalStrings(t, prompts(*calls), []string{groupPrompt, groupPrompt})
}

func TestIncrementalGroups_regroup_flag_forces_it(t *testing.T) {
	a := fakeApp(t)
	calls := answering(t, a, js(t, fullGrouping), js(t, fullGrouping))
	grouped(t, a, false, "a", "b")
	grouped(t, a, true, "a", "b")
	equalStrings(t, prompts(*calls), []string{groupPrompt, groupPrompt})
}

// An object answer holding a list was cut down to the inner list, so every placement looked like no answer.
func askedWith(t *testing.T, stdout string) any {
	t.Helper()
	a := fakeApp(t)
	claudePrints(a, stdout, 0)
	got, err := a.askClaude("p", "b", askOptions{think: true})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestAskJson_outer_object_wins(t *testing.T) {
	want := js(t, `{"assign": {"0": 1}, "new": [{"name": "x"}]}`)
	if got := askedWith(t, `ok: {"assign": {"0": 1}, "new": [{"name": "x"}]}`); !jsonEqual(got, want) {
		t.Errorf("got %s", dumpJSON(got))
	}
}

func TestAskJson_outer_list_wins(t *testing.T) {
	want := js(t, `[{"name": "x", "sessions": [1]}]`)
	if got := askedWith(t, `[{"name": "x", "sessions": [1]}]`); !jsonEqual(got, want) {
		t.Errorf("got %s", dumpJSON(got))
	}
}

// Known issue 8: a 429 waits and retries instead of disabling skills.sh for the rest of the run.
func TestSkillsShRateLimit_429_then_ok(t *testing.T) {
	a := fakeApp(t)
	tries := 0
	a.httpGet = func(string, time.Duration) ([]byte, error) {
		tries++
		if tries == 1 {
			return nil, &httpError{code: 429, status: "429 slow down", retryAfter: "1"}
		}
		return []byte(`{"ok": 1}`), nil
	}
	var slept []time.Duration
	a.sleep = func(d time.Duration) { slept = append(slept, d) }
	got, err := a.skillsShJSON("u")
	if err != nil || !jsonEqual(got, js(t, `{"ok": 1}`)) {
		t.Errorf("got %v, %v", got, err)
	}
	if len(slept) != 1 || slept[0] != time.Second {
		t.Errorf("slept %v, want once for 1s", slept)
	}
}
