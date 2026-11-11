package reinvest

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The run as a whole, which the prototype's tests never drove: these have no
// counterpart in test_reinvest.py. Claude, reap and the network are replaced;
// the transcripts, caches and reports are real files in scratch folders.

const reapReport = `{"DeadTokensPerSession": 7688, "Rows": [
 {"Category": "skill", "Name": "ecc:deployment-patterns", "Description": "Deployment workflows and rollback", "Tokens": 150, "Uses": 0, "Verdict": "REAP"},
 {"Category": "skill", "Name": "tdd", "Description": "Test first", "Tokens": 40, "Uses": 9, "Verdict": "KEEP"},
 {"Category": "skill", "Name": "old-notes", "Description": "Takes notes", "Tokens": 60, "Uses": 0, "Verdict": "REAP"},
 {"Category": "mcp", "Name": "docs", "Tokens": 0, "WeightUnknown": true},
 {"Category": "mcp", "Name": "weighed", "Tokens": 300},
 {"Category": "hook", "Name": "x"}, {"Category": "skill"}]}`

func userEntry(ts, text string) string {
	return dumpJSON(objOf("type", "user", "timestamp", ts, "cwd", "/tmp/project", "entrypoint", "cli",
		"message", objOf("role", "user", "content", text)))
}

// assistantEntry is one turn that ran Bash and, with skill, a skill.
func assistantEntry(ts, skill string) string {
	tools := `{"type": "tool_use", "name": "Bash"}`
	if skill != "" {
		tools += `, {"type": "tool_use", "name": "Skill", "input": {"skill": "` + skill + `"}}`
	}
	return `{"type": "assistant", "timestamp": "` + ts + `", "message": {"usage": {"input_tokens": 1000, "output_tokens": 200, ` +
		`"cache_creation_input_tokens": 50, "cache_read_input_tokens": 99999}, "content": [` + tools + `, {"type": "text", "text": "done"}]}}`
}

// writeTranscripts leaves four release sessions a person wrote, and around
// them everything that must not count: an SDK run, a subagent, a session
// still open, and one with nothing typed.
func writeTranscripts(t *testing.T, a *app, now time.Time) {
	t.Helper()
	at := func(ago time.Duration) string { return now.Add(-ago).UTC().Format("2006-01-02T15:04:05.000Z") }
	project := filepath.Join(a.projectsDir, "-tmp-project")
	for i, id := range []string{"rel-1", "rel-2", "rel-3", "rel-4"} {
		ago := time.Duration(i+1) * 26 * time.Hour
		skill, command := "", "<command-name>/not-a-skill</command-name>"
		if i == 0 { // one session in four uses a skill: not enough for the skill to cover this work
			skill, command = "ecc:tdd", "<command-name>/tdd</command-name>"
		}
		lines := []string{
			userEntry(at(ago), "please cut the release for this tool <system-reminder>ignore me</system-reminder> now"),
			assistantEntry(at(ago-time.Minute), skill),
			`{"type": "user", "timestamp": "` + at(ago-2*time.Minute) + `", "message": {"content": [{"type": "tool_result", "is_error": true}]}}`,
			userEntry(at(ago-3*time.Minute), "[Request interrupted by user]"),
			userEntry(at(ago-4*time.Minute), command),
			userEntry(at(ago-5*time.Minute), "<local-command-stdout>noise that is long enough</local-command-stdout>"),
			userEntry(at(ago-6*time.Minute), "short"),
			`{"type": "atis-latch"}`, `not json at all`, `[1, 2]`,
		}
		writeFile(t, filepath.Join(project, id+".jsonl"), strings.Join(lines, "\n")+"\n")
	}
	sdk := strings.Replace(userEntry(at(30*time.Hour), "an automated run that asks for a release"), `"cli"`, `"sdk-py"`, 1)
	writeFile(t, filepath.Join(project, "sdk.jsonl"), sdk+"\n")
	writeFile(t, filepath.Join(project, "x", "subagents", "agent.jsonl"), userEntry(at(30*time.Hour), "a subagent asking for a release")+"\n")
	writeFile(t, filepath.Join(project, "open.jsonl"), userEntry(at(time.Minute), "the session that is running reinvest")+"\n")
	writeFile(t, filepath.Join(project, "empty.jsonl"), assistantEntry(at(30*time.Hour), "")+"\n")
}

const writtenBody = "## When\nWhen asked to release.\n\n## Steps\n" +
	"1. Read the commits since the last tag.\n2. Bump the version.\n3. Write the changelog.\n4. Tag.\n5. Watch CI until it is green.\n\n" +
	"## Rules\n- Never push without asking.\n- Reply in the language of the request.\n"

// pipelineApp is an app in which Claude answers each question the pipeline asks.
func pipelineApp(t *testing.T, verified verdict) (*app, *[]asked) {
	t.Helper()
	a := fakeApp(t)
	writeTranscripts(t, a, a.now())
	a.reapCommand = func() ([]string, error) { return []string{"/scratch/reap"}, nil }
	a.runProcess = func(spec procSpec) procResult {
		if !sameWords(spec.argv, []string{"/scratch/reap", "--json", "--no-nudge"}) {
			t.Errorf("ran %v", spec.argv)
		}
		return procResult{stdout: reapReport}
	}
	var calls []asked
	a.ask = func(prompt, body string, opts askOptions) (any, error) {
		calls = append(calls, asked{prompt, body, opts})
		switch prompt {
		case groupPrompt:
			return js(t, `[{"name": "Software Releases", "sessions": [0, 1, 2, 3], "delegable": true, "task": "Cut a release: bump, changelog, tag, CI.",
				"queries": ["release manager"], "mcp_queries": ["github"]}]`), nil
		case coverPrompt:
			return js(t, `{"0": null}`), nil
		case writePrompt:
			return objOf("name", "ship-release", "description", releaseDesc, "body", writtenBody), nil
		}
		t.Errorf("unexpected question: %.60s", prompt)
		return nil, nil
	}
	verifying(a, always(verified))
	return a, &calls
}

func TestRun_whole_pipeline_writes_a_skill_and_proposes_the_swap(t *testing.T) {
	a, calls := pipelineApp(t, verdict{Keep: true, Risk: noRisk, Fit: 4, Reason: "fits", Use: "Cuts your releases."})
	if err := a.run(options{days: defaultDays, max: defaultSwaps, dryRun: true}); err != nil {
		t.Fatal(err)
	}
	out := termText(a)
	for _, part := range []string{"✓ 4 sessions read", "✓ 1 kinds of work · 1 with no skill", "✓ 1 skills written", "✓ 1 fit",
		"reinvest · ~7,688 dead tok/session per reap", "mute      old-notes", "create    ship-release   ~", "✎ new · written from your sessions",
		"for Software Releases (4 sessions)", "Cuts your releases.", "details: " + a.reportPath()} {
		if !strings.Contains(out, part) {
			t.Errorf("the terminal does not show %q:\n%s", part, out)
		}
	}
	equalStrings(t, prompts(*calls), []string{groupPrompt, coverPrompt, writePrompt})
	grouping := (*calls)[0]
	if grouping.opts.think || grouping.opts.effort != effortMid {
		t.Errorf("grouping was asked with %+v", grouping.opts)
	}
	if want := "[0] please cut the release for this tool now"; !strings.HasPrefix(grouping.body, want) {
		t.Errorf("the grouping body starts %q", firstRunes(grouping.body, 80))
	}
	if strings.Count(grouping.body, "\n") != 3 || strings.Contains(grouping.body, "automated") || strings.Contains(grouping.body, "subagent") ||
		strings.Contains(grouping.body, "running reinvest") {
		t.Errorf("sessions that are not the person's were sent:\n%s", grouping.body)
	}
	report := readFile(t, a.reportPath())
	for _, part := range []string{"- Software Releases: 4 sessions, ", "## Gaps\n\n- Software Releases: Cut a release",
		"- ship-release (✎ new · written from your sessions, ~", "fit 4/5) for Software Releases (4 sessions): Cuts your releases."} {
		if !strings.Contains(report, part) {
			t.Errorf("the report lacks %q:\n%s", part, report)
		}
	}
	verifiedList := js(t, readFile(t, a.cachePath(verifiedFile))).([]any)
	if first := verifiedList[0].(*obj); first.val("id") != "new:ship-release" || first.val("gap") != "Software Releases" {
		t.Errorf("verified.json holds %s", dumpJSON(first))
	}
	proposals := jsObj(t, readFile(t, a.cachePath(swapsFile)))
	if want := js(t, `[{"install": "ship-release", "mute": ["old-notes"]}]`); !jsonEqual(proposals.val("swaps"), want) {
		t.Errorf("swaps.json holds %s", dumpJSON(proposals.val("swaps")))
	}
	memory := js(t, readFile(t, filepath.Join(a.stateDir, memoryFile))).([]any)
	first := memory[0].(*obj)
	want := js(t, `{"tools": 2, "errors": 1, "interrupts": 1, "tokens": 1250, "skills": ["tdd"]}`).(*obj)
	for _, key := range want.keyList() {
		if !jsonEqual(first.val(key), want.val(key)) {
			t.Errorf("a remembered session has %s = %s, want %s", key, dumpJSON(first.val(key)), dumpJSON(want.val(key)))
		}
	}
	if len(memory) != 4 || !exists(a.cachePath(logFile)) || !exists(a.cachePath(groupsFile)) || !exists(a.cachePath(writtenFile)) {
		t.Errorf("%d sessions remembered; cache holds %v", len(memory), mustReadDir(t, a.cacheDir))
	}
}

func mustReadDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestRun_a_second_run_asks_only_what_is_not_cached(t *testing.T) {
	a, calls := pipelineApp(t, verdict{Keep: true, Risk: noRisk, Fit: 4, Reason: "fits", Use: "Cuts your releases."})
	for range 2 {
		if err := a.run(options{days: defaultDays, max: defaultSwaps}); err != nil {
			t.Fatal(err)
		}
	}
	// groups, the written skill and its verdicts come from the cache; only coverage is asked again
	equalStrings(t, prompts(*calls), []string{groupPrompt, coverPrompt, writePrompt, coverPrompt})
}

func TestRun_a_refused_skill_is_reported_and_nothing_is_proposed(t *testing.T) {
	a, _ := pipelineApp(t, verdict{Keep: false, Reason: securityPrefix + "uploads your .env"})
	if err := a.run(options{days: defaultDays, max: defaultSwaps}); err != nil {
		t.Fatal(err)
	}
	out := termText(a)
	for _, part := range []string{"refused for security: ship-release: security: uploads your .env", "no swaps: nothing verified for your gaps", "details: "} {
		if !strings.Contains(out, part) {
			t.Errorf("the terminal does not show %q:\n%s", part, out)
		}
	}
	if !strings.Contains(readFile(t, a.cachePath(deniedFile)), "uploads your .env") {
		t.Error("the flagged document was not remembered")
	}
	if got := jsObj(t, readFile(t, a.cachePath(swapsFile))).list("swaps"); len(got) != 0 {
		t.Errorf("proposals were left: %v", got)
	}
}

func TestRun_online_searches_skills_sh_and_reads_whole_folders(t *testing.T) {
	a, calls := pipelineApp(t, verdict{Keep: true, Risk: noRisk, Fit: 5, Reason: "fits", Use: "Runs a release end to end."})
	skillDoc := "---\nname: release-conductor\ndescription: Prepares a versioned release of a command-line project, from bump to tag.\n---\nSteps.\n"
	var urls []string
	a.skillsSh = func(url string) (any, error) {
		urls = append(urls, url)
		if strings.Contains(url, "/api/search?") {
			return js(t, `{"skills": [{"id": "o/r/release-conductor", "source": "o/r", "skillId": "release-conductor", "name": "release-conductor", "installs": 500},
				{"id": "o/r/tdd", "source": "o/r", "skillId": "tdd", "name": "tdd", "installs": 9000},
				{"id": "o/r/rare", "source": "o/r", "skillId": "rare", "name": "rare", "installs": 3}]}`), nil
		}
		return filesAnswer("SKILL.md", skillDoc, "scripts/tag.sh", "git tag \"$1\"\n"), nil
	}
	if err := a.run(options{days: defaultDays, max: defaultSwaps, online: true}); err != nil {
		t.Fatal(err)
	}
	out := termText(a)
	for _, part := range []string{"✓ 1 candidates", "install   release-conductor   ~", "skill · o/r", "mute      old-notes"} {
		if !strings.Contains(out, part) {
			t.Errorf("the terminal does not show %q:\n%s", part, out)
		}
	}
	equalStrings(t, prompts(*calls), []string{groupPrompt, coverPrompt})
	if !containsString(urls, "https://skills.sh/api/search?q=release+manager&limit=12") ||
		!containsString(urls, "https://skills.sh/api/download/o/r/release-conductor") {
		t.Errorf("skills.sh was asked %q", urls)
	}
	for _, url := range urls {
		if strings.Contains(url, "/tdd") || strings.Contains(url, "/rare") {
			t.Errorf("fetched a skill already installed or too little used: %s", url)
		}
	}
}

func TestRun_stops_with_a_plain_message(t *testing.T) {
	a := fakeApp(t)
	a.reapCommand = func() ([]string, error) { return []string{"reap"}, nil }
	a.runProcess = func(procSpec) procResult {
		return procResult{stdout: "not json", code: 2, stderr: "reap: unknown flag"}
	}
	if err := a.run(options{days: defaultDays}); err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("a reap that fails gave %v", err)
	}
	a, _ = pipelineApp(t, verdict{})
	a.ask = a.askClaude
	a.lookPath = func(string) (string, error) { return "", os.ErrNotExist }
	if err := a.run(options{days: defaultDays}); err != errNoClaude {
		t.Errorf("a missing claude gave %v", err)
	}
	if exists(a.cachePath(groupsFile)) {
		t.Error("groups were saved although Claude was never asked")
	}
}

func TestRun_arguments(t *testing.T) {
	cases := []struct {
		args []string
		code int
		ok   bool
		want options
	}{
		{nil, exitOK, true, options{days: 120, max: 3}},
		{[]string{"--dry-run", "--regroup", "--online", "--mcp", "--days", "30", "--max=5"}, exitOK, true,
			options{days: 30, max: 5, dryRun: true, regroup: true, online: true, mcp: true}},
		{[]string{"--no-mcp"}, exitOK, true, options{days: 120, max: 3, noMCP: true}},
		{[]string{"--days", "many"}, exitUsage, false, options{}},
		{[]string{"--nope"}, exitUsage, false, options{}},
		{[]string{"stray"}, exitUsage, false, options{}},
		{[]string{"--help"}, exitOK, false, options{}},
	}
	for _, c := range cases {
		var stdout, stderr bytes.Buffer
		opts, code, ok := parseArgs(c.args, &stdout, &stderr)
		if code != c.code || ok != c.ok || (ok && opts != c.want) {
			t.Errorf("%v: got %+v, %d, %v", c.args, opts, code, ok)
		}
		if !ok && stdout.Len()+stderr.Len() == 0 {
			t.Errorf("%v: stopped without a word", c.args)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--days"}, strings.NewReader(""), &stdout, &stderr); code != exitUsage || !strings.Contains(stderr.String(), "usage:") {
		t.Errorf("Run gave %d and %q", code, stderr.String())
	}
}

func TestRun_reap_is_the_running_binary_unless_REAP_names_another(t *testing.T) {
	t.Setenv(reapVariable, "")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := reapCommand(); err != nil || !sameWords(got, []string{self}) {
		t.Errorf("got %v, %v", got, err)
	}
	spaced := filepath.Join(t.TempDir(), "my builds", "reap")
	writeFile(t, spaced, "#!/bin/sh\n")
	if err := os.Chmod(spaced, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(reapVariable, spaced)
	if got, _ := reapCommand(); !sameWords(got, []string{spaced}) { // a program's path is taken whole
		t.Errorf("got %q", got)
	}
	t.Setenv(reapVariable, "reap -claude-dir /tmp/x")
	if got, _ := reapCommand(); !sameWords(got, []string{"reap", "-claude-dir", "/tmp/x"}) { // anything else may carry arguments
		t.Errorf("got %q", got)
	}
	a := fakeApp(t)
	var ran []string
	a.runProcess = func(spec procSpec) procResult {
		ran = spec.argv
		return procResult{}
	}
	if err := a.runInEmptyDir([]string{"reap", "mute", "old"}); err != nil {
		t.Fatal(err)
	}
	equalStrings(t, ran, []string{"reap", "-claude-dir", "/tmp/x", "mute", "old"})
}

func TestRun_reap_report_is_read_as_the_prototype_read_it(t *testing.T) {
	state, err := parseReapState(reapReport)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Installed) != 3 || len(state.Used) != 1 || state.Used[0].Name != "tdd" || len(state.Reap) != 2 || state.Dead != 7688 {
		t.Errorf("got %+v", state)
	}
	if w, listed := state.MCP["docs"]; !listed || w != nil {
		t.Errorf("a server not weighed yet reads as %v", w)
	}
	if w := state.MCP["weighed"]; w == nil || *w != 300 {
		t.Errorf("a weighed server reads as %v", w)
	}
	if _, err := parseReapState("reap: no such flag"); err == nil {
		t.Error("an answer that is not JSON was read")
	}
}

// TestHelperProcess is the program the next test runs: this test binary, told
// to behave like a command.
func TestHelperProcess(t *testing.T) {
	switch os.Getenv("REINVEST_HELPER") {
	case "echo":
		raw := make([]byte, 64)
		n, _ := os.Stdin.Read(raw)
		os.Stdout.WriteString("got:" + string(raw[:n]) + ":" + os.Getenv("MAX_THINKING_TOKENS"))
		os.Stderr.WriteString("warned")
		os.Exit(3)
	case "hang":
		time.Sleep(time.Minute)
	}
}

func TestRun_a_program_is_run_to_its_end_or_to_its_time_limit(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	spec := procSpec{argv: []string{self, "-test.run=TestHelperProcess"}, stdin: "hello", dir: t.TempDir(), timeout: time.Minute,
		env: []string{"REINVEST_HELPER=echo", "MAX_THINKING_TOKENS=0"}}
	res := runProcess(spec)
	if res.stdout != "got:hello:0" || res.stderr != "warned" || res.code != 3 || res.timedOut || res.err != nil {
		t.Errorf("got %+v", res)
	}
	spec.env, spec.timeout = []string{"REINVEST_HELPER=hang"}, 200*time.Millisecond
	if res := runProcess(spec); !res.timedOut {
		t.Errorf("a program past its time limit gave %+v", res)
	}
	spec.argv = []string{filepath.Join(t.TempDir(), "no-such-program")}
	if res := runProcess(spec); res.err == nil {
		t.Errorf("a program that is not there gave %+v", res)
	}
}

func TestRun_claude_is_called_lean_and_retried_once(t *testing.T) {
	a := fakeApp(t)
	var specs []procSpec
	outputs := []procResult{{code: 1, stderr: "overloaded"}, {stdout: `{"ok": true}`}}
	a.runProcess = func(spec procSpec) procResult {
		specs = append(specs, spec)
		return outputs[len(specs)-1]
	}
	got, err := a.askClaude("the prompt", "the body", askOptions{think: false, effort: effortMid})
	if err != nil || !jsonEqual(got, js(t, `{"ok": true}`)) {
		t.Fatalf("got %v, %v", got, err)
	}
	want := append([]string{claudeCLI, "-p", "the prompt"}, leanFlags...)
	want = append(want, "--model", "sonnet", "--effort", "medium")
	if len(specs) != 2 || !sameWords(specs[0].argv, want) || specs[0].stdin != "the body" || specs[0].dir != os.TempDir() ||
		!sameWords(specs[0].env, []string{"MAX_THINKING_TOKENS=0"}) || specs[0].timeout != 900*time.Second {
		t.Errorf("claude was run as %+v", specs)
	}
	a.runProcess = func(spec procSpec) procResult {
		specs = append(specs, spec)
		return procResult{stdout: `{"risk": "none", "keep": false, "reason": "thin"}`}
	}
	if _, err := a.verifyOne(verifyRequest{Task: "t", Doc: "d"}); err != nil {
		t.Fatal(err)
	}
	last := specs[len(specs)-1]
	if !sameWords(last.env, []string{"MAX_THINKING_TOKENS=16000"}) || !sameWords(last.argv[len(last.argv)-2:], []string{"--effort", "high"}) {
		t.Errorf("the verifier was run with %v %v", last.env, last.argv[len(last.argv)-4:])
	}
}

func TestRun_accepted_swaps_are_applied_only_after_a_yes(t *testing.T) {
	home := tempHome(t)
	s := writtenSwap("old")
	written := filepath.Join(home, ".claude", "skills-reinvest", "ship-release", skillFile)
	ask := func(answer string, dryRun bool, chosen ...swap) (*app, *[][]string) {
		a := fakeApp(t)
		a.stdin = strings.NewReader(answer)
		a.pick = func([]swap) ([]swap, error) { return chosen, nil }
		calls := stepsRecorded(a, nil)
		if err := a.saveProposals([]swap{s}); err != nil {
			t.Fatal(err)
		}
		if err := a.chooseAndApply([]swap{s}, dryRun); err != nil {
			t.Fatal(err)
		}
		return a, calls
	}
	a, calls := ask("", false)
	if !strings.Contains(termText(a), "nothing accepted · nothing changed") || len(*calls) != 0 {
		t.Errorf("nothing was chosen, yet: %q %v", termText(a), *calls)
	}
	for _, answer := range []string{"\n", "n\n", "yes\n", ""} { // only a plain y is a yes; no answer at all is a no
		a, calls = ask(answer, false, s)
		if !strings.Contains(termText(a), applyQuestion+"  nothing changed") || len(*calls) != 0 || exists(written) {
			t.Errorf("answer %q: %q %v", answer, termText(a), *calls)
		}
	}
	a, calls = ask("", true, s) // a dry run asks nothing and changes nothing
	if out := termText(a); strings.Contains(out, applyQuestion) || !strings.Contains(out, "  ·  reap mute old") ||
		!strings.Contains(out, "dry run · nothing installed or muted") || len(*calls) != 0 || exists(written) {
		t.Errorf("dry run: %q %v", out, *calls)
	}
	if got := jsObj(t, readFile(t, a.cachePath(swapsFile))).list("swaps"); len(got) != 1 {
		t.Errorf("a dry run dropped the proposal: %v", got)
	}
	a, calls = ask(" Y \nleft for the next reader", false, s)
	out := termText(a)
	for _, part := range []string{"  ship-release  in place of  old\n", "✓  created ship-release in ", "✓  muted old (reap unmute old undoes)"} {
		if !strings.Contains(out, part) {
			t.Errorf("the terminal does not show %q:\n%s", part, out)
		}
	}
	if !exists(written) || len(*calls) != 1 {
		t.Errorf("after a yes: written %v, steps %v", exists(written), *calls)
	}
	if got := jsObj(t, readFile(t, a.cachePath(swapsFile))).list("swaps"); len(got) != 0 {
		t.Errorf("an applied swap is still proposed: %v", got)
	}
	rest := make([]byte, 64)
	if n, _ := a.stdin.Read(rest); string(rest[:n]) != "left for the next reader" {
		t.Errorf("the answer took more than its line: %q is left", rest[:n])
	}
}
