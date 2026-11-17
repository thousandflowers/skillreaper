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
	for name, text := range map[string]string{skillFile: c.Doc, "my-notes.txt": "mine"} {
		if err := os.WriteFile(filepath.Join(home, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
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
	if _, err := os.Stat(filepath.Join(home, "my-notes.txt")); err != nil {
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
