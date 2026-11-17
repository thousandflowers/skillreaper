package reinvest

import (
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
