package reinvest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A step that got no answer says what `claude -p` said and where the log is:
// "got no answer" alone hid a login that had run out.
func TestCycle19_a_failed_step_says_why(t *testing.T) {
	for stdout, want := range map[string]string{"": "exit 3: boom", "some prose": "an answer that could not be read"} {
		a := fakeApp(t)
		a.runProcess = func(procSpec) procResult {
			if stdout == "" {
				return procResult{code: 3, stderr: "warming up\nboom\x1b[2J"}
			}
			return procResult{stdout: stdout}
		}
		if answer, err := a.askClaude("p", "b", askOptions{}); answer != nil || err != nil {
			t.Fatalf("answer %v, err %v", answer, err)
		}

		a.failed("grouping your work got no answer")

		said := termText(a)
		if !strings.Contains(said, want) || !strings.Contains(said, a.cachePath(logFile)) || strings.Contains(said, "\x1b[2J") {
			t.Errorf("the failed step reads:\n%q", said)
		}
	}
}

// A written skill whose link was deleted is still in skills-reinvest. Offered
// again with the same text, it is linked again; it used to be shown in full,
// accepted, and then refused, at every run.
func TestCycle19_a_written_skill_left_without_its_link_is_linked_again(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	c := candidate{Slug: "release-helper", Doc: "---\nname: release-helper\ndescription: d\n---\nbody\n"}
	if err := create(c); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(skillsDir(), c.Slug)
	if err := os.Rename(link, filepath.Join(t.TempDir(), "deleted")); err != nil { // what reap delete does to the link
		t.Fatal(err)
	}

	if err := create(c); err != nil {
		t.Fatalf("the same skill again: %v", err)
	}
	if !pointsAt(link, filepath.Join(writtenDir(), c.Slug)) {
		t.Error("the link is not back")
	}

	// With another text there, nothing is overwritten and the way out is said.
	if err := os.Rename(link, filepath.Join(t.TempDir(), "deleted")); err != nil {
		t.Fatal(err)
	}
	c.Doc += "more\n"
	if err := create(c); err == nil || !strings.Contains(err.Error(), "move it away") {
		t.Errorf("another text over the one left there: %v", err)
	}
}

// The mute line of --dry-run is the command that would undo-ably run: the
// item's path, and the config folder the run was pointed at.
func TestCycle19_the_dry_run_mute_line_names_the_folder(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/tmp/a profile")
	lines := strings.Join(dryRunLines(swap{Cands: []candidate{{Kind: kindSkill, Name: "new", Installed: true}}, Mute: []skill{{Name: "old-three", Path: "/tmp/a profile/skills/old-three/SKILL.md"}}}), "\n")

	if !strings.Contains(lines, `CLAUDE_CONFIG_DIR='/tmp/a profile' reap mute '/tmp/a profile/skills/old-three/SKILL.md'`) {
		t.Errorf("dry run:\n%s", lines)
	}
}

// The reason on a failed step is that step's own. A call that failed and then
// answered leaves none behind; a message on stdout is read when stderr is empty.
func TestCycle20_the_reason_of_a_failed_step_is_its_own(t *testing.T) {
	a := fakeApp(t)
	calls := 0
	a.runProcess = func(procSpec) procResult {
		calls++
		if calls == 1 {
			return procResult{code: 1, stderr: "rate limit: try later"}
		}
		return procResult{stdout: `{"ok": true}`}
	}
	if answer, _ := a.askClaude("p", "b", askOptions{}); answer == nil {
		t.Fatal("the retry gave no answer")
	}
	a.failed("1 of 1 skills not written: no usable answer")
	if said := termText(a); strings.Contains(said, "rate limit") {
		t.Errorf("a reason from a call that then answered:\n%s", said)
	}

	b := fakeApp(t)
	b.runProcess = func(procSpec) procResult {
		return procResult{code: 1, stdout: "Invalid API key · Please run /login\n", stderr: " \n"}
	}
	_, _ = b.askClaude("p", "b", askOptions{})
	b.failed("grouping your work got no answer")
	if said := termText(b); !strings.Contains(said, "exit 1: Invalid API key") {
		t.Errorf("what claude said on stdout is not shown:\n%s", said)
	}

	c := fakeApp(t)
	c.runProcess = func(procSpec) procResult { return procResult{code: 2} }
	_, _ = c.askClaude("p", "b", askOptions{})
	c.failed("grouping your work got no answer")
	if said := termText(c); !strings.Contains(said, "claude -p: exit 2 ·") {
		t.Errorf("an exit with nothing said:\n%s", said)
	}
}

// A folder that was in skills-reinvest before this run is not this run's to
// remove: a swap that fails after linking it again takes the link away and
// leaves the folder, with whatever else is in it.
func TestCycle20_a_rollback_leaves_a_folder_this_run_did_not_make(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	c := candidate{Kind: kindSkill, Name: "fix-ui", Slug: "fix-ui", Created: true, Doc: "---\nname: fix-ui\ndescription: d\n---\nbody\n"}
	home := filepath.Join(writtenDir(), c.Slug)
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, skillFile), []byte(c.Doc), 0o644); err != nil {
		t.Fatal(err)
	}
	a := fakeApp(t)
	a.runStep = func(cmd []string) error {
		if len(cmd) > 2 && cmd[2] == "mute" {
			return errors.New("mute failed")
		}
		return nil
	}

	log, applied := a.carryOut(swap{Cands: []candidate{c}, Mute: []skill{{Name: "old", Path: "/x/old/SKILL.md"}}})

	said := strings.Join(log, "\n")
	if applied || !strings.Contains(said, "linked fix-ui again") {
		t.Errorf("applied %v:\n%s", applied, said)
	}
	if _, err := os.Stat(filepath.Join(home, skillFile)); err != nil {
		t.Errorf("the folder that was there before is gone: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(skillsDir(), c.Slug)); err == nil {
		t.Error("the link this run made is still there")
	}
}

// A server installed with nothing to mute is done: it is no longer a proposal.
func TestCycle20_an_installed_server_with_nothing_to_mute_is_applied(t *testing.T) {
	a := fakeApp(t)
	a.runStep = func([]string) error { return nil }
	a.remember = func(candidate) error { return nil }
	s := swap{Cands: []candidate{{Kind: kindMCP, Name: "fs", ID: "io.github.acme/fs\x1b[2J", Server: jsObj(t, `{"name": "io.github.acme/fs",
		"packages": [{"registryType": "npm", "identifier": "fs", "version": "1.0.0"}]}`)}}}

	log, applied := a.carryOut(s)

	if !applied || !strings.Contains(strings.Join(log, "\n"), "nothing muted yet") {
		t.Errorf("applied %v: %q", applied, log)
	}
}

// A registry name is printed like every other name from outside: without the
// control characters it may carry.
func TestCycle20_a_registry_name_is_printed_without_control_characters(t *testing.T) {
	c := candidate{Kind: kindMCP, Name: "evil", ID: "io.x/evil\x1b[2J\a", Server: jsObj(t, `{"name": "io.x/evil",
		"packages": [{"registryType": "npm", "identifier": "evil", "version": "1.0.0", "environmentVariables": [{"name": "K", "isRequired": true}]}]}`)}
	a := fakeApp(t)
	lines, _ := a.install(swap{Cands: []candidate{c}})
	for _, said := range []string{strings.Join(lines, "\n"), strings.Join(dryRunLines(swap{Cands: []candidate{c}}), "\n")} {
		if !strings.Contains(said, "needs manual setup") || strings.ContainsAny(said, "\x1b\a") {
			t.Errorf("printed: %q", said)
		}
	}
}

// Calls of one step run side by side. One that answers does not take away the
// reason of one that did not, and a reason does not outlive its step.
func TestCycle21_a_sibling_call_that_answers_leaves_the_reason(t *testing.T) {
	a := fakeApp(t)
	a.runProcess = func(spec procSpec) procResult {
		if strings.Contains(spec.stdin, "alpha") {
			return procResult{code: 1, stderr: "Error: not logged in"}
		}
		return procResult{stdout: `{"ok": true}`}
	}
	_, _ = a.askClaude("p", "alpha", askOptions{})
	if answer, _ := a.askClaude("p", "beta", askOptions{}); answer == nil {
		t.Fatal("beta gave no answer")
	}
	a.failed("1 of 2 skills not written: no usable answer")
	if said := termText(a); !strings.Contains(said, "not logged in") {
		t.Errorf("the reason of the call that failed is gone:\n%s", said)
	}

	b := fakeApp(t)
	b.runProcess = func(procSpec) procResult { return procResult{code: 1, stderr: "rate limit"} }
	_, _ = b.askClaude("p", "x", askOptions{})
	b.step("verifying 1 candidate", false) // the next step begins
	b.failed("1 of 1 skills not written")
	if said := termText(b); strings.Contains(said, "rate limit") {
		t.Errorf("a reason from the step before:\n%s", said)
	}
}

// What claude said is shown without the keys it may hold, and the log that
// keeps it is the person's alone to read.
func TestCycle21_the_reason_is_shown_without_secrets(t *testing.T) {
	a := fakeApp(t)
	a.runProcess = func(procSpec) procResult {
		return procResult{code: 1, stdout: `{"note": "the key is sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789"}`}
	}
	_, _ = a.askClaude("p", "b", askOptions{})
	a.failed("grouping your work got no answer")
	if said := termText(a); strings.Contains(said, "abcdefghijklmnopqrstuvwxyz0123456789") {
		t.Errorf("a key on the terminal:\n%s", said)
	}
	path := filepath.Join(t.TempDir(), "run.log")
	log, err := createLog(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = log.Close()
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("run.log mode: %v %v", info.Mode().Perm(), err)
	}
}

// A server that could not be noted down is not promised a later mute.
func TestCycle21_a_server_not_remembered_is_not_promised_a_mute(t *testing.T) {
	a := fakeApp(t)
	a.runStep = func([]string) error { return nil }
	a.remember = func(candidate) error { return errors.New("disk full") }
	s := swap{Cands: []candidate{{Kind: kindMCP, Name: "fs", Server: jsObj(t, `{"name": "io.github.acme/fs",
		"packages": [{"registryType": "npm", "identifier": "fs", "version": "1.0.0"}]}`)}}}

	log, _ := a.carryOut(s)

	if said := strings.Join(log, "\n"); strings.Contains(said, "reinvest offers the mute") || !strings.Contains(said, "will not offer a mute for it") {
		t.Errorf("log:\n%s", said)
	}
}

// Stopped after the new skill is in place, the text says how to finish by hand.
func TestCycle21_a_stop_after_the_install_says_how_to_finish(t *testing.T) {
	c := candidate{Kind: kindSkill, Name: "new", Slug: "new", Created: true}
	lines := strings.Join(stoppedHalfway(c, false, []string{"  ✓  created new in /x"}, nil, "", []skill{{Name: "old-a", Path: "/s/old-a/SKILL.md"}, {Name: "old-b", Path: "/s/old-b/SKILL.md"}}), "\n")
	if !strings.Contains(lines, "reap mute /s/old-a/SKILL.md") || !strings.Contains(lines, "reap mute /s/old-b/SKILL.md") {
		t.Errorf("stop text:\n%s", lines)
	}
}

// A folder left in skills-reinvest is linked again only when it is the skill
// and nothing else: a file beside SKILL.md was never shown, and a folder with
// no SKILL.md is not "another text".
func TestCycle21_a_left_folder_is_linked_only_when_it_is_the_skill_alone(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	c := candidate{Slug: "fix-ui", Doc: "---\nname: fix-ui\ndescription: d\n---\nbody\n"}
	home := filepath.Join(writtenDir(), c.Slug)
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := create(c); err == nil || !strings.Contains(err.Error(), "no SKILL.md") {
		t.Errorf("an empty folder: %v", err)
	}
	for name, text := range map[string]string{skillFile: c.Doc, "run.sh": "echo never shown"} {
		if err := os.WriteFile(filepath.Join(home, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := create(c); err == nil || !strings.Contains(err.Error(), "other files") {
		t.Errorf("a folder with a file beside the skill: %v", err)
	}
}

// "Nothing verified" is not said of candidates that were: with nothing unused
// to trade for them there is no swap, and the line says that.
func TestCycle22_no_swap_for_candidates_that_fit_says_why(t *testing.T) {
	a := fakeApp(t)
	if err := a.show(outcome{fit: 1}, false); err != nil {
		t.Fatal(err)
	}
	if said := termText(a); strings.Contains(said, "nothing verified") || !strings.Contains(said, "1 fit, but nothing unused") {
		t.Errorf("the line reads:\n%s", said)
	}
}

// A key is masked before the line is cut to its tail: cut first, a key that
// began before the cut was no longer a key to the mask.
func TestCycle22_a_key_cut_by_the_tail_is_still_masked(t *testing.T) {
	key := "sk-ant-api03-" + strings.Repeat("Ab3dEf9", 13)
	line := lastLine("request failed for " + key + " " + strings.Repeat("z", 100))
	if strings.Contains(line, "Ab3dEf9Ab3dEf9") {
		t.Errorf("part of the key is shown: %q", line)
	}
}

// What Finder leaves in a folder is not "other files"; anything else is, and
// is named.
func TestCycle22_a_left_folder_may_hold_what_finder_leaves(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	c := candidate{Slug: "fix-ui", Doc: "---\nname: fix-ui\ndescription: d\n---\nbody\n"}
	home := filepath.Join(writtenDir(), c.Slug)
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{skillFile: c.Doc, ".DS_Store": "x"} {
		if err := os.WriteFile(filepath.Join(home, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := create(c); err != nil {
		t.Errorf("a folder with a .DS_Store: %v", err)
	}
	if err := os.Remove(filepath.Join(skillsDir(), c.Slug)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "run.sh"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := create(c); err == nil || !strings.Contains(err.Error(), "run.sh") {
		t.Errorf("a folder with another file: %v", err)
	}
}

// Two skills of one name are two skills in the stop text too, and a swap that
// was being undone is not told how to finish.
func TestCycle22_the_stop_text_tells_skills_of_one_name_apart(t *testing.T) {
	c := candidate{Kind: kindSkill, Name: "new", Slug: "new", Created: true}
	a, b := skill{Name: "dup", Path: "/p/dup/SKILL.md"}, skill{Name: "dup", Path: "/s/dup/SKILL.md"}
	lines := strings.Join(stoppedHalfway(c, false, nil, []skill{a}, "", []skill{a, b}), "\n")
	if !strings.Contains(lines, "to finish: reap mute /s/dup/SKILL.md") {
		t.Errorf("the second copy is not in the text:\n%s", lines)
	}
	if kept := withoutFinish([]string{"  ·  not muted: x", "  →  to finish: reap mute x"}); len(kept) != 1 {
		t.Errorf("while undoing, the finish line stays: %q", kept)
	}
}

// A grouping answer that is a list and places no session is no answer: saved
// as one, it left a week of "0 kinds of work" with no call made.
func TestCycle22_a_grouping_that_places_nothing_is_no_answer(t *testing.T) {
	a := fakeApp(t)
	a.runProcess = func(procSpec) procResult {
		return procResult{stdout: `[{"name": "release work", "sessions": ["sess-0", "sess-1"], "delegable": true}]`}
	}
	sessions := []session{{ID: "a", Prompts: []string{"release it"}}, {ID: "b", Prompts: []string{"release again"}}}

	groups, answered, err := a.groupSessions(sessions)

	if err != nil || answered || len(groups) != 0 {
		t.Errorf("groups %d, answered %v, err %v", len(groups), answered, err)
	}
	a.failed("grouping your work got no answer")
	if said := termText(a); !strings.Contains(said, "claude -p: ") {
		t.Errorf("no reason for an answer of the wrong shape:\n%s", said)
	}
}
