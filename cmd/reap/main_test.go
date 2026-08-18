package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/banner"
	"github.com/thousandflowers/skillreaper/internal/hook"
	"github.com/thousandflowers/skillreaper/internal/platform"
	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/scan"
)

func TestStateCommandsRequireClaudeDir(t *testing.T) {
	// With no Claude dir resolved, filepath.Join("", ...) yields a relative
	// path that writes into the cwd. State-mutating commands must fail loudly
	// instead of polluting the working directory.
	dir := t.TempDir()
	t.Chdir(dir)
	var out, errb bytes.Buffer
	if code := cmdInstallHook(options{claudeDir: ""}, &out, &errb); code == 0 {
		t.Error("install-hook with no claude dir should fail, not write to cwd")
	}
	if !strings.Contains(strings.ToLower(errb.String()), "claude") {
		t.Errorf("expected a clear error mentioning the claude dir, got %q", errb.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); !os.IsNotExist(err) {
		t.Error("install-hook polluted the cwd with settings.json")
	}

	out.Reset()
	errb.Reset()
	if code := cmdPrune(options{claudeDir: ""}, strings.NewReader(""), &out, &errb); code == 0 {
		t.Error("prune with no claude dir should fail, not write to cwd")
	}
	if _, err := os.Stat(filepath.Join(dir, "reaped")); !os.IsNotExist(err) {
		t.Error("prune polluted the cwd with reaped/")
	}
}

// buildFixture creates a minimal but complete fake installation:
// a used skill, a dead skill, a dead MCP server, and one transcript.
func buildFixture(t *testing.T) (claudeDir, claudeJSON string) {
	t.Helper()
	root := t.TempDir()
	claudeDir = filepath.Join(root, ".claude")
	claudeJSON = filepath.Join(root, ".claude.json")

	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write(filepath.Join(claudeDir, "skills", "usedskill", "SKILL.md"),
		"---\nname: usedskill\ndescription: I am used\n---\nbody")
	write(filepath.Join(claudeDir, "skills", "deadskill", "SKILL.md"),
		"---\nname: deadskill\ndescription: I am never used\n---\nbody")
	write(claudeJSON, `{"mcpServers":{"deadsrv":{"command":"uvx","args":["deadsrv"]}}}`)
	write(filepath.Join(claudeDir, "CLAUDE.md"), "global prose")

	// One transcript with a usedskill invocation.
	write(filepath.Join(claudeDir, "projects", "p1", "s1.jsonl"),
		`{"type":"assistant","timestamp":"2026-06-09T10:00:00Z","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"usedskill"}}]}}`+"\n")
	return claudeDir, claudeJSON
}

func TestMuteNamedAgent(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	agent := filepath.Join(claudeDir, "agents", "myagent.md")
	if err := os.MkdirAll(filepath.Dir(agent), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agent, []byte("---\nname: myagent\ndescription: an agent description\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"mute", "--claude-dir", claudeDir, "--claude-json", claudeJSON, "myagent"},
		strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("mute agent exit = %d, stderr: %s", code, errOut.String())
	}
	b, _ := os.ReadFile(agent)
	if strings.Contains(string(b), "description:") {
		t.Errorf("named agent was not muted: %s", b)
	}
}

func TestMuteBareAbortsOnDecline(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	dead := filepath.Join(claudeDir, "skills", "deadskill", "SKILL.md")
	var out, errOut bytes.Buffer
	// Bare `reap mute` must prompt; answering no leaves files untouched.
	code := run([]string{"mute", "--claude-dir", claudeDir, "--claude-json", claudeJSON, "--min-sessions", "1"},
		strings.NewReader("n\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errOut.String())
	}
	b, _ := os.ReadFile(dead)
	if !strings.Contains(string(b), "description:") {
		t.Errorf("declined bulk mute still stripped the description: %s", b)
	}
	if !strings.Contains(out.String(), "aborted") {
		t.Errorf("expected an abort message, got: %s", out.String())
	}
}

func TestMuteBareProceedsOnYes(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	dead := filepath.Join(claudeDir, "skills", "deadskill", "SKILL.md")
	var out, errOut bytes.Buffer
	code := run([]string{"mute", "--claude-dir", claudeDir, "--claude-json", claudeJSON, "--min-sessions", "1"},
		strings.NewReader("y\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errOut.String())
	}
	b, _ := os.ReadFile(dead)
	if strings.Contains(string(b), "description:") {
		t.Errorf("confirmed bulk mute did not strip the description: %s", b)
	}
}

func TestRunReport(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	var out, errOut bytes.Buffer

	code := run([]string{
		"--claude-dir", claudeDir, "--claude-json", claudeJSON,
		"--days", "30", "--min-sessions", "1",
	}, strings.NewReader(""), &out, &errOut)

	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errOut.String())
	}
	for _, want := range []string{"usedskill", "deadskill", "deadsrv", "REAP", "KEEP"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestRunReportJSON(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	var out, errOut bytes.Buffer

	code := run([]string{
		"--claude-dir", claudeDir, "--claude-json", claudeJSON,
		"--min-sessions", "1", "--json",
	}, strings.NewReader(""), &out, &errOut)

	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}
}

func TestRunReportOverlongTranscriptHoldsAbsenceDecisionsAtReview(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	transcript := filepath.Join(claudeDir, "projects", "p1", "s1.jsonl")
	overlong := `{"type":"assistant","timestamp":"2026-06-09T10:01:00Z","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"missedskill"}}],"pad":"` +
		strings.Repeat("x", 10*1024*1024+1) + `"}}`
	f, err := os.OpenFile(transcript, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(overlong + "\n"); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := run([]string{
		"--claude-dir", claudeDir, "--claude-json", claudeJSON,
		"--min-sessions", "1", "--json",
	}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errOut.String())
	}

	var r report.Report
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}
	if r.MalformedLines != 1 {
		t.Errorf("MalformedLines = %d, want 1", r.MalformedLines)
	}
	byName := map[string]report.Row{}
	for _, row := range r.Rows {
		byName[row.Name] = row
	}
	if row := byName["usedskill"]; row.Verdict != report.VerdictKeep {
		t.Errorf("usedskill verdict = %s(%s), want KEEP from positive evidence", row.Verdict, row.Reason)
	}
	if row := byName["deadskill"]; row.Verdict != report.VerdictReview || row.Reason != report.ReasonNoEvidence {
		t.Errorf("deadskill verdict = %s(%s), want REVIEW(%s)", row.Verdict, row.Reason, report.ReasonNoEvidence)
	}
	foundIncomplete := false
	for _, w := range r.Warnings {
		if strings.Contains(w.Msg, "incomplete") {
			foundIncomplete = true
			break
		}
	}
	if !foundIncomplete {
		t.Errorf("expected incomplete-evidence warning, got %+v", r.Warnings)
	}
}

func TestFindItemRejectsAmbiguousSuffixMatch(t *testing.T) {
	r := &report.Report{Rows: []report.Row{
		{Item: scan.Item{Category: scan.CatSkill, Name: "one:plan", Removable: true}},
		{Item: scan.Item{Category: scan.CatSkill, Name: "two:plan", Removable: true}},
	}}
	if _, ok := findItem(r, "plan"); ok {
		t.Fatal("ambiguous suffix match should not select an arbitrary item")
	}
}

func TestFindItemAllowsUniqueSuffixMatch(t *testing.T) {
	r := &report.Report{Rows: []report.Row{
		{Item: scan.Item{Category: scan.CatSkill, Name: "one:plan", Removable: true}},
		{Item: scan.Item{Category: scan.CatSkill, Name: "two:build", Removable: true}},
	}}
	row, ok := findItem(r, "plan")
	if !ok || row.Name != "one:plan" {
		t.Fatalf("unique suffix match = %+v, %v; want one:plan", row, ok)
	}
}

func TestRunMissingDir(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"--claude-dir", "/nonexistent/nope"},
		strings.NewReader(""), &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "no Claude Code installation") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestRunPruneAndRestore(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	var out, errOut bytes.Buffer

	code := run([]string{
		"prune", "--claude-dir", claudeDir, "--claude-json", claudeJSON,
		"--min-sessions", "1", "--yes",
	}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("prune exit = %d, stderr: %s", code, errOut.String())
	}

	// deadskill moved to quarantine; usedskill untouched.
	if _, err := os.Stat(filepath.Join(claudeDir, "skills", "deadskill")); !os.IsNotExist(err) {
		t.Error("deadskill should be quarantined")
	}
	if _, err := os.Stat(filepath.Join(claudeDir, "skills", "usedskill", "SKILL.md")); err != nil {
		t.Error("usedskill should survive")
	}

	// deadsrv removed from config.
	b, _ := os.ReadFile(claudeJSON)
	if strings.Contains(string(b), "deadsrv") {
		t.Error("deadsrv should be removed from config")
	}

	// Restore everything.
	out.Reset()
	code = run([]string{"restore", "--all", "--claude-dir", claudeDir},
		strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("restore exit = %d, stderr: %s", code, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(claudeDir, "skills", "deadskill", "SKILL.md")); err != nil {
		t.Error("deadskill not restored")
	}
	b, _ = os.ReadFile(claudeJSON)
	if !strings.Contains(string(b), "deadsrv") {
		t.Error("deadsrv not restored")
	}
}

func TestRunPruneJSONEmitsValidJSON(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	var out, errOut bytes.Buffer

	// No --yes: the plan, and nothing touched. A prompt here would be
	// unanswerable on a stream headed for jq, so there must not be one.
	code := run([]string{
		"prune", "--claude-dir", claudeDir, "--claude-json", claudeJSON,
		"--min-sessions", "1", "--json",
	}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errOut.String())
	}

	var plan struct {
		Candidates []struct {
			Category string `json:"category"`
			Name     string `json:"name"`
			Tokens   int    `json:"tokens"`
		} `json:"candidates"`
		TotalTokens int  `json:"total_tokens"`
		Applied     bool `json:"applied"`
		Pruned      []struct {
			ID string `json:"id"`
		} `json:"pruned"`
	}
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatalf("prune --json did not emit JSON: %v\noutput:\n%s", err, out.String())
	}
	if len(plan.Candidates) == 0 {
		t.Error("plan lists no candidates")
	}
	if plan.Applied {
		t.Error("applied must be false without --yes")
	}
	if len(plan.Pruned) != 0 {
		t.Error("nothing may be pruned without --yes")
	}
	if plan.TotalTokens <= 0 {
		t.Error("total_tokens should sum the candidates")
	}
	if strings.Contains(out.String(), "[Y/n]") || strings.Contains(out.String(), "🧹") {
		t.Error("the human plan leaked into the JSON stream")
	}
	if _, err := os.Stat(filepath.Join(claudeDir, "skills", "deadskill", "SKILL.md")); err != nil {
		t.Error("a plan must not move anything")
	}

	// With --yes it acts, and still emits one JSON document.
	out.Reset()
	code = run([]string{
		"prune", "--claude-dir", claudeDir, "--claude-json", claudeJSON,
		"--min-sessions", "1", "--json", "--yes",
	}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errOut.String())
	}
	plan.Applied = false
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatalf("prune --json --yes did not emit JSON: %v\noutput:\n%s", err, out.String())
	}
	if !plan.Applied {
		t.Error("applied must be true after --yes")
	}
	if len(plan.Pruned) == 0 {
		t.Error("pruned should list what moved")
	}
	if _, err := os.Stat(filepath.Join(claudeDir, "skills", "deadskill")); !os.IsNotExist(err) {
		t.Error("deadskill should be quarantined")
	}
}

func TestRunPruneQuietPrintsNothing(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	var out, errOut bytes.Buffer

	// Without --yes: silent, and a no-op.
	code := run([]string{
		"prune", "--claude-dir", claudeDir, "--claude-json", claudeJSON,
		"--min-sessions", "1", "--quiet",
	}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errOut.String())
	}
	if out.Len() != 0 {
		t.Errorf("--quiet wrote %d bytes to stdout:\n%s", out.Len(), out.String())
	}
	if _, err := os.Stat(filepath.Join(claudeDir, "skills", "deadskill", "SKILL.md")); err != nil {
		t.Error("--quiet without --yes must not move anything")
	}

	// With --yes: still silent, but it acts.
	code = run([]string{
		"prune", "--claude-dir", claudeDir, "--claude-json", claudeJSON,
		"--min-sessions", "1", "--quiet", "--yes",
	}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errOut.String())
	}
	if out.Len() != 0 {
		t.Errorf("--quiet --yes wrote %d bytes to stdout:\n%s", out.Len(), out.String())
	}
	if _, err := os.Stat(filepath.Join(claudeDir, "skills", "deadskill")); !os.IsNotExist(err) {
		t.Error("deadskill should be quarantined")
	}
}

func TestRunPruneInteractiveAbort(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	var out, errOut bytes.Buffer

	code := run([]string{
		"prune", "--claude-dir", claudeDir, "--claude-json", claudeJSON,
		"--min-sessions", "1",
	}, strings.NewReader("n\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "aborted") {
		t.Error("empty selection should abort")
	}
	if _, err := os.Stat(filepath.Join(claudeDir, "skills", "deadskill", "SKILL.md")); err != nil {
		t.Error("abort must not touch files")
	}
}

func TestRunHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}} {
		t.Run(args[0], func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := run(args, strings.NewReader(""), &out, &errOut); code != 0 {
				t.Errorf("exit = %d, want 0 (help is not a usage error)", code)
			}
		})
	}
}

func TestRunVersion(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "subcommand", args: []string{"version"}},
		{name: "long flag", args: []string{"--version"}},
		{name: "short flag", args: []string{"-v"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := run(tt.args, strings.NewReader(""), &out, &errOut); code != 0 {
				t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
			}
			if got, want := out.String(), "reap "+version()+"\n"; got != want {
				t.Errorf("version output = %q, want %q", got, want)
			}
			if got := errOut.String(); got != "" {
				t.Errorf("stderr = %q, want empty", got)
			}
		})
	}
}

func TestRunKeep(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	var out, errOut bytes.Buffer

	code := run([]string{
		"keep", "--claude-dir", claudeDir, "--claude-json", claudeJSON, "skill:deadskill",
	}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("keep exit = %d, stderr: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "marked as keep") {
		t.Errorf("output = %q", out.String())
	}

	out.Reset()
	code = run([]string{
		"--claude-dir", claudeDir, "--claude-json", claudeJSON,
		"--min-sessions", "1",
	}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("report exit = %d", code)
	}
	if !strings.Contains(out.String(), "KEEP · keep") {
		t.Errorf("report should show KEEP · keep, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "KEEP") {
		t.Errorf("deadskill should have KEEP verdict")
	}
}

func TestRunKeepList(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	var discard bytes.Buffer
	_ = run([]string{
		"keep", "--claude-dir", claudeDir, "--claude-json", claudeJSON, "skill:deadskill",
	}, strings.NewReader(""), &discard, &discard)

	var out, errOut bytes.Buffer
	code := run([]string{
		"keep", "--list", "--claude-dir", claudeDir, "--claude-json", claudeJSON,
	}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("keep --list exit = %d", code)
	}
	if !strings.Contains(out.String(), "skill:deadskill") {
		t.Errorf("list output = %q", out.String())
	}
}

func TestRunPruneSkipsKept(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)

	var discard bytes.Buffer
	_ = run([]string{
		"keep", "--claude-dir", claudeDir, "--claude-json", claudeJSON, "skill:deadskill",
	}, strings.NewReader(""), &discard, &discard)

	var out, errOut bytes.Buffer
	_ = run([]string{
		"prune", "--claude-dir", claudeDir, "--claude-json", claudeJSON,
		"--min-sessions", "1", "--yes",
	}, strings.NewReader(""), &out, &errOut)
	if strings.Contains(out.String(), "deadskill") {
		t.Errorf("kept skill deadskill should not be pruned, got: %s", out.String())
	}
}

func TestRunMuteUnmute(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	skill := filepath.Join(claudeDir, "skills", "usedskill", "SKILL.md")
	var out, errOut bytes.Buffer

	code := run([]string{
		"mute", "--claude-dir", claudeDir, "--claude-json", claudeJSON, "usedskill",
	}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("mute exit = %d, stderr: %s", code, errOut.String())
	}
	b, _ := os.ReadFile(skill)
	if strings.Contains(string(b), "description:") {
		t.Errorf("muted skill still has a description: %s", b)
	}

	out.Reset()
	code = run([]string{"unmute", "--claude-dir", claudeDir, "usedskill"},
		strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("unmute exit = %d, stderr: %s", code, errOut.String())
	}
	b, _ = os.ReadFile(skill)
	if !strings.Contains(string(b), "description:") {
		t.Errorf("unmute did not restore the description: %s", b)
	}
}

// TestRunMuteFlagOrdering guards the parser bug where flags placed after the
// subcommand's positional argument (or a flag placed before the subcommand)
// were silently dropped, so mute ran against the default ~/.claude instead of
// the fixture and reported "no skill found".
func TestRunMuteFlagOrdering(t *testing.T) {
	hasDesc := func(t *testing.T, path string) bool {
		t.Helper()
		b, _ := os.ReadFile(path)
		return strings.Contains(string(b), "description:")
	}

	t.Run("flags after positional", func(t *testing.T) {
		claudeDir, claudeJSON := buildFixture(t)
		skill := filepath.Join(claudeDir, "skills", "usedskill", "SKILL.md")
		var out, errOut bytes.Buffer
		code := run([]string{
			"mute", "usedskill",
			"--claude-dir", claudeDir, "--claude-json", claudeJSON, "--no-nudge",
		}, strings.NewReader(""), &out, &errOut)
		if code != 0 {
			t.Fatalf("mute exit = %d, stderr: %s", code, errOut.String())
		}
		if hasDesc(t, skill) {
			t.Errorf("description not stripped — flags after positional were dropped")
		}
	})

	t.Run("flag before subcommand", func(t *testing.T) {
		claudeDir, claudeJSON := buildFixture(t)
		skill := filepath.Join(claudeDir, "skills", "deadskill", "SKILL.md")
		var out, errOut bytes.Buffer
		code := run([]string{
			"--claude-dir", claudeDir, "--claude-json", claudeJSON, "--no-nudge",
			"mute", "deadskill",
		}, strings.NewReader(""), &out, &errOut)
		if code != 0 {
			t.Fatalf("mute exit = %d, stderr: %s", code, errOut.String())
		}
		if hasDesc(t, skill) {
			t.Errorf("description not stripped — leading flag hid the subcommand")
		}
	})
}

func TestRunInstallUninstallHook(t *testing.T) {
	claudeDir, _ := buildFixture(t)
	settings := filepath.Join(claudeDir, "settings.json")
	var out, errOut bytes.Buffer

	if code := run([]string{"install-hook", "--claude-dir", claudeDir},
		strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("install-hook exit = %d, stderr: %s", code, errOut.String())
	}
	b, _ := os.ReadFile(settings)
	if !strings.Contains(string(b), "SessionStart") {
		t.Errorf("settings.json missing the hook: %s", b)
	}

	out.Reset()
	if code := run([]string{"uninstall-hook", "--claude-dir", claudeDir},
		strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("uninstall-hook exit = %d", code)
	}
	b, _ = os.ReadFile(settings)
	if strings.Contains(string(b), "skillreaper-weekly-nudge") {
		t.Errorf("uninstall left the nudge hook: %s", b)
	}
}

func TestRunInstallHookDryRun(t *testing.T) {
	claudeDir, _ := buildFixture(t)
	var out, errOut bytes.Buffer

	if code := run([]string{"install-hook", "--dry-run", "--claude-dir", claudeDir},
		strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "dry-run") {
		t.Errorf("expected dry-run output, got: %s", out.String())
	}
	if _, err := os.Stat(filepath.Join(claudeDir, "settings.json")); !os.IsNotExist(err) {
		t.Error("dry-run must not write settings.json")
	}
}

func TestRunByProjectAndManifest(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	var out, errOut bytes.Buffer

	if code := run([]string{
		"by-project", "--claude-dir", claudeDir, "--claude-json", claudeJSON, "--min-sessions", "1",
	}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("by-project exit = %d, stderr: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "skills by project") {
		t.Errorf("by-project output: %s", out.String())
	}

	out.Reset()
	if code := run([]string{
		"manifest", "--claude-dir", claudeDir, "--claude-json", claudeJSON,
		"--claude-version", "9.9", "usedskill",
	}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("manifest exit = %d, stderr: %s", code, errOut.String())
	}
	for _, want := range []string{"skillreaper manifest", "usedskill", "9.9", "Tool surface"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("manifest output missing %q: %s", want, out.String())
		}
	}
}

func TestRunWhy(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	var out, errOut bytes.Buffer

	// REAP item, text output.
	code := run([]string{
		"why", "--claude-dir", claudeDir, "--claude-json", claudeJSON,
		"--min-sessions", "1", "skill:deadskill",
	}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("why exit = %d, stderr: %s", code, errOut.String())
	}
	for _, want := range []string{"REAP", "verdict", "zero uses", "reap prune"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("why text missing %q: %s", want, out.String())
		}
	}

	// Bare name, JSON output.
	out.Reset()
	code = run([]string{
		"why", "--claude-dir", claudeDir, "--claude-json", claudeJSON,
		"--min-sessions", "1", "--json", "deadskill",
	}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("why --json exit = %d, stderr: %s", code, errOut.String())
	}
	var e map[string]any
	if err := json.Unmarshal(out.Bytes(), &e); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if e["verdict"] != "REAP" {
		t.Errorf("verdict = %v, want REAP", e["verdict"])
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Error("JSON output must not contain ANSI color codes")
	}

	// Used item → KEEP.
	out.Reset()
	code = run([]string{
		"why", "--claude-dir", claudeDir, "--claude-json", claudeJSON,
		"--min-sessions", "1", "usedskill",
	}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("why usedskill exit = %d", code)
	}
	if !strings.Contains(out.String(), "KEEP") {
		t.Errorf("usedskill should be KEEP: %s", out.String())
	}

	// Unknown item → error exit.
	out.Reset()
	code = run([]string{
		"why", "--claude-dir", claudeDir, "--claude-json", claudeJSON, "nope-nope",
	}, strings.NewReader(""), &out, &errOut)
	if code != 1 {
		t.Errorf("unknown item exit = %d, want 1", code)
	}
}

func TestRunGap(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	var out, errOut bytes.Buffer

	code := run([]string{
		"gap",
		"--claude-dir", claudeDir, "--claude-json", claudeJSON,
		"--days", "30", "--min-sessions", "1",
	}, strings.NewReader(""), &out, &errOut)

	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errOut.String())
	}
	for _, want := range []string{"loaded vs fired", "skills", "total"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("gap output missing %q", want)
		}
	}
}

// buildStarCtaFixture extends buildFixture with a heavy dead skill so
// DeadTokensPerSession ≥ MinStarCtaTokens (200), triggering the star-CTA.
func buildStarCtaFixture(t *testing.T) (claudeDir, claudeJSON string) {
	t.Helper()
	claudeDir, claudeJSON = buildFixture(t)
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// DescChars = len("heavyskill") + len(desc) => need ≥ 740 for ≥200 tok.
	// 20× "this is a very heavy skill description " = 720; +deadskill's ~7
	// tok gives 205 total → above MinStarCtaTokens.
	desc := strings.Repeat("this is a very heavy skill description ", 20)
	write(filepath.Join(claudeDir, "skills", "heavyskill", "SKILL.md"),
		"---\nname: heavyskill\ndescription: "+desc+"\n---\nbody")
	return claudeDir, claudeJSON
}

func TestTryShowStarCta_ShowsOnSufficientSavings(t *testing.T) {
	claudeDir, _ := buildStarCtaFixture(t)
	var buf bytes.Buffer
	r := &report.Report{DeadTokensPerSession: 250}

	tryShowStarCta(options{claudeDir: claudeDir}, &buf, r, true)
	if !strings.Contains(buf.String(), "github.com/thousandflowers/skillreaper") {
		t.Error("CTA should show with color=true and DeadTokensPerSession ≥ 200")
	}
}

func TestTryShowStarCta_NoColorSuppresses(t *testing.T) {
	var buf bytes.Buffer
	r := &report.Report{DeadTokensPerSession: 250}

	tryShowStarCta(options{}, &buf, r, false)
	if strings.Contains(buf.String(), "github.com/thousandflowers/skillreaper") {
		t.Error("CTA should not show with color=false")
	}
}

func TestTryShowStarCta_NoNudgeFlagSuppresses(t *testing.T) {
	var buf bytes.Buffer
	r := &report.Report{DeadTokensPerSession: 250}

	tryShowStarCta(options{noNudge: true}, &buf, r, true)
	if strings.Contains(buf.String(), "github.com/thousandflowers/skillreaper") {
		t.Error("CTA should not show with --no-nudge")
	}
}

func TestTryShowStarCta_JSONModeSuppresses(t *testing.T) {
	var buf bytes.Buffer
	r := &report.Report{DeadTokensPerSession: 250}

	tryShowStarCta(options{asJSON: true}, &buf, r, true)
	if strings.Contains(buf.String(), "github.com/thousandflowers/skillreaper") {
		t.Error("CTA should not show in JSON mode")
	}
}

func TestTryShowStarCta_MarkdownModeSuppresses(t *testing.T) {
	var buf bytes.Buffer
	r := &report.Report{DeadTokensPerSession: 250}

	tryShowStarCta(options{asMarkdown: true}, &buf, r, true)
	if strings.Contains(buf.String(), "github.com/thousandflowers/skillreaper") {
		t.Error("CTA should not show in Markdown mode")
	}
}

func TestTryShowStarCta_InsufficientTokensSuppresses(t *testing.T) {
	claudeDir, _ := buildStarCtaFixture(t)
	var buf bytes.Buffer
	r := &report.Report{DeadTokensPerSession: 10} // below MinStarCtaTokens

	tryShowStarCta(options{claudeDir: claudeDir}, &buf, r, true)
	if strings.Contains(buf.String(), "github.com/thousandflowers/skillreaper") {
		t.Error("CTA should not show with insufficient dead tokens")
	}
}

func TestTryShowStarCta_ThrottleSuppresses(t *testing.T) {
	claudeDir, _ := buildStarCtaFixture(t)
	var buf bytes.Buffer
	r := &report.Report{DeadTokensPerSession: 250}

	// Seed nudge state with a recent CTA (now = throttled).
	st := hook.NudgeState{LastStarCtaAt: time.Now(), StarCtaCount: 1}
	if err := hook.SaveNudgeState(claudeDir, st); err != nil {
		t.Fatal(err)
	}

	tryShowStarCta(options{claudeDir: claudeDir}, &buf, r, true)
	if strings.Contains(buf.String(), "github.com/thousandflowers/skillreaper") {
		t.Error("CTA should be throttled when LastStarCtaAt is within 30 days")
	}
}

func TestTryShowStarCta_EnvVarSuppresses(t *testing.T) {
	t.Setenv("SKILLREAPER_NO_NUDGE", "1")
	var buf bytes.Buffer
	r := &report.Report{DeadTokensPerSession: 250}

	tryShowStarCta(options{}, &buf, r, true)
	if strings.Contains(buf.String(), "github.com/thousandflowers/skillreaper") {
		t.Error("CTA should not show with SKILLREAPER_NO_NUDGE env var")
	}
}

func TestTryShowStarCta_PersistsStateOnShow(t *testing.T) {
	claudeDir, _ := buildStarCtaFixture(t)
	var buf bytes.Buffer
	r := &report.Report{DeadTokensPerSession: 250}

	tryShowStarCta(options{claudeDir: claudeDir}, &buf, r, true)

	st, err := hook.LoadNudgeState(claudeDir)
	if err != nil {
		t.Fatal(err)
	}
	if st.LastStarCtaAt.IsZero() {
		t.Error("LastStarCtaAt should be set after CTA is shown")
	}
	if st.StarCtaCount != 1 {
		t.Errorf("StarCtaCount = %d, want 1", st.StarCtaCount)
	}
}

func TestRunReportJSONModeSkipsCta(t *testing.T) {
	claudeDir, claudeJSON := buildStarCtaFixture(t)
	var out, errOut bytes.Buffer
	code := run([]string{
		"--claude-dir", claudeDir, "--claude-json", claudeJSON,
		"--min-sessions", "1", "--json",
	}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if strings.Contains(out.String(), "github.com/thousandflowers/skillreaper") {
		t.Error("CTA should not appear in JSON output")
	}
}

func TestRunReportMDModeSkipsCta(t *testing.T) {
	claudeDir, claudeJSON := buildStarCtaFixture(t)
	var out, errOut bytes.Buffer
	code := run([]string{
		"--claude-dir", claudeDir, "--claude-json", claudeJSON,
		"--min-sessions", "1", "--md",
	}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if strings.Contains(out.String(), "github.com/thousandflowers/skillreaper") {
		t.Error("CTA should not appear in Markdown output")
	}
}

func TestPruneShowsValueFeedback(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	var out, errOut bytes.Buffer

	code := run([]string{
		"prune", "--claude-dir", claudeDir, "--claude-json", claudeJSON,
		"--min-sessions", "1", "--yes",
	}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("prune exit = %d, stderr: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "✓ pruned") {
		t.Errorf("prune output missing value feedback: %s", out.String())
	}
}

func TestMuteShowsValueFeedback(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	var out, errOut bytes.Buffer

	code := run([]string{
		"mute", "--claude-dir", claudeDir, "--claude-json", claudeJSON, "usedskill",
	}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("mute exit = %d, stderr: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "✓ muted") {
		t.Errorf("mute output missing value feedback: %s", out.String())
	}
}

func TestShareSubcommand(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	var out, errOut bytes.Buffer

	code := run([]string{
		"share", "--claude-dir", claudeDir, "--claude-json", claudeJSON,
		"--min-sessions", "1",
	}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("share exit = %d, stderr: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "skillreaper") {
		t.Errorf("share output missing skillreaper: %s", out.String())
	}
	if !strings.Contains(out.String(), "brew install") {
		t.Errorf("share output missing install instructions: %s", out.String())
	}
}

func TestShareSubcommandJSON(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	var out, errOut bytes.Buffer

	code := run([]string{
		"share", "--json", "--claude-dir", claudeDir, "--claude-json", claudeJSON,
		"--min-sessions", "1",
	}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("share --json exit = %d, stderr: %s", code, errOut.String())
	}
	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := decoded["message"]; !ok {
		t.Errorf("share json missing message key: %s", out.String())
	}
	if _, ok := decoded["install"]; !ok {
		t.Errorf("share json missing install key: %s", out.String())
	}
}

func TestShareSubcommandMarkdown(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	var out, errOut bytes.Buffer

	code := run([]string{
		"share", "--md", "--claude-dir", claudeDir, "--claude-json", claudeJSON,
		"--min-sessions", "1",
	}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("share --md exit = %d, stderr: %s", code, errOut.String())
	}
	if !strings.HasPrefix(out.String(), "```") {
		t.Errorf("share --md should be a code block, got: %s", out.String())
	}
}

func TestTryShowShareHint_Shows(t *testing.T) {
	claudeDir, _ := buildFixture(t)
	var buf bytes.Buffer

	tryShowShareHint(options{claudeDir: claudeDir}, &buf, true)
	if !strings.Contains(buf.String(), "reap share") {
		t.Error("share hint should show with color=true")
	}
}

func TestTryShowShareHint_Throttle(t *testing.T) {
	claudeDir, _ := buildFixture(t)
	var buf bytes.Buffer

	// Seed nudge state with a recent hint (now = throttled).
	st := hook.NudgeState{LastShareHintAt: time.Now(), ShareHintCount: 1}
	if err := hook.SaveNudgeState(claudeDir, st); err != nil {
		t.Fatal(err)
	}

	tryShowShareHint(options{claudeDir: claudeDir}, &buf, true)
	if strings.Contains(buf.String(), "reap share") {
		t.Error("share hint should be throttled when LastShareHintAt is within 30 days")
	}
}

func TestTryShowShareHint_NoNudgeFlag(t *testing.T) {
	var buf bytes.Buffer
	tryShowShareHint(options{noNudge: true}, &buf, true)
	if strings.Contains(buf.String(), "reap share") {
		t.Error("share hint should not show with --no-nudge")
	}
}

func TestTryShowShareHint_NoColor(t *testing.T) {
	var buf bytes.Buffer
	tryShowShareHint(options{}, &buf, false)
	if strings.Contains(buf.String(), "reap share") {
		t.Error("share hint should not show with color=false")
	}
}

func TestTryShowShareHint_EnvVar(t *testing.T) {
	t.Setenv("SKILLREAPER_NO_NUDGE", "1")
	var buf bytes.Buffer
	tryShowShareHint(options{}, &buf, true)
	if strings.Contains(buf.String(), "reap share") {
		t.Error("share hint should not show with SKILLREAPER_NO_NUDGE env var")
	}
}

func TestTryShowShareHint_PersistsState(t *testing.T) {
	claudeDir, _ := buildFixture(t)
	var buf bytes.Buffer

	tryShowShareHint(options{claudeDir: claudeDir}, &buf, true)

	st, err := hook.LoadNudgeState(claudeDir)
	if err != nil {
		t.Fatal(err)
	}
	if st.LastShareHintAt.IsZero() {
		t.Error("LastShareHintAt should be set after hint is shown")
	}
	if st.ShareHintCount != 1 {
		t.Errorf("ShareHintCount = %d, want 1", st.ShareHintCount)
	}
}

func TestRunGapJSON(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	var out, errOut bytes.Buffer

	code := run([]string{
		"gap",
		"--claude-dir", claudeDir, "--claude-json", claudeJSON,
		"--min-sessions", "1", "--json",
	}, strings.NewReader(""), &out, &errOut)

	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := decoded["Loaded"]; !ok {
		t.Errorf("gap json missing Loaded key: %s", out.String())
	}
}

// TestFooter_AlwaysPrintsExceptMachineFormats locks the footer's contract: it
// is a signature, not a nudge. It survives --no-nudge and a non-TTY writer
// (which is what colour=false means here), and it is absent from the three
// machine-readable modes. Breaking either half breaks a promise to users.
func TestFooter_AlwaysPrintsExceptMachineFormats(t *testing.T) {
	const url = "github.com/thousandflowers/skillreaper"
	claudeDir, claudeJSON := buildFixture(t)
	base := []string{"--claude-dir", claudeDir, "--claude-json", claudeJSON, "--min-sessions", "1"}

	for _, tc := range []struct {
		name string
		args []string
		want bool
	}{
		{"default", nil, true},
		{"no-nudge still signs", []string{"--no-nudge"}, true},
		{"json", []string{"--json"}, false},
		{"markdown", []string{"--md"}, false},
		{"quiet", []string{"--quiet"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := run(append(base, tc.args...), strings.NewReader(""), &out, &errOut); code != 0 {
				t.Fatalf("exit = %d: %s", code, errOut.String())
			}
			if got := strings.Contains(out.String(), url); got != tc.want {
				t.Errorf("footer URL present = %v, want %v\noutput: %s", got, tc.want, out.String())
			}
		})
	}
}

func TestUsageBannerListsEverySupportedPlatform(t *testing.T) {
	// The banner used to hand-type the platform list, so adding Gemini CLI
	// left it advertising six platforms out of seven. It is derived now, and
	// this fails if anyone types it back.
	var out, errb bytes.Buffer
	run([]string{"--help"}, strings.NewReader(""), &out, &errb)
	banner := out.String() + errb.String()
	for _, p := range platform.All() {
		if !strings.Contains(banner, p.Name) {
			t.Errorf("usage banner does not mention %q:\n%s", p.Name, banner)
		}
	}
}

func TestDedupeByPathKeepsServersSharingAConfigFile(t *testing.T) {
	// Every MCP server declared in ~/.claude.json carries that file as its
	// Path, so a path-only key would collapse them all into one and silently
	// erase most of the inventory.
	items := []scan.Item{
		{Category: scan.CatMCP, Platform: "claude-code", Path: "/c/.claude.json", Name: "serena"},
		{Category: scan.CatMCP, Platform: "claude-code", Path: "/c/.claude.json", Name: "inaz"},
		{Category: scan.CatProse, Platform: "claude-code", Path: "/c/CLAUDE.md", Name: "~/CLAUDE.md"},
		{Category: scan.CatProse, Platform: "claude-code", Path: "/c/CLAUDE.md", Name: "~/CLAUDE.md"},
	}
	got := dedupeByPath(items)
	if len(got) != 3 {
		t.Fatalf("dedupeByPath returned %d items, want 3: %+v", len(got), got)
	}
	mcp := 0
	for _, it := range got {
		if it.Category == scan.CatMCP {
			mcp++
		}
	}
	if mcp != 2 {
		t.Errorf("kept %d MCP servers, want both", mcp)
	}
}

// stubTerminal makes the banner's terminal probe report an interactive
// terminal of the given width for the duration of one test.
func stubTerminal(t *testing.T, tty bool, cols int) {
	t.Helper()
	original := banner.Detect
	banner.Detect = func(io.Writer) (bool, int, bool) { return tty, cols, tty }
	t.Cleanup(func() { banner.Detect = original })
}

// The usage text is the only place the wordmark belongs: the user is looking
// at the tool rather than running it.
func TestRunBannerOnTerminal(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}} {
		t.Run(args[0], func(t *testing.T) {
			stubTerminal(t, true, 80)
			var out, errOut bytes.Buffer

			run(args, strings.NewReader(""), &out, &errOut)

			if !strings.Contains(errOut.String(), banner.Mark) {
				t.Errorf("stderr = %q, want it to contain the wordmark", errOut.String())
			}
			if strings.Contains(out.String(), "----------/") {
				t.Error("wordmark leaked into stdout")
			}
		})
	}
}

func TestRunBannerSuppressed(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	base := []string{"--claude-dir", claudeDir, "--claude-json", claudeJSON, "--min-sessions", "1"}

	tests := []struct {
		name    string
		args    []string
		tty     bool
		noColor string
	}{
		{name: "bare invocation runs the scan", args: base, tty: true},
		{name: "stdout is not a terminal", args: base, tty: false},
		{name: "json", args: append(append([]string{}, base...), "--json"), tty: true},
		{name: "markdown", args: append(append([]string{}, base...), "--md"), tty: true},
		{name: "agent", args: append(append([]string{}, base...), "--agent"), tty: true},
		{name: "no-banner", args: append(append([]string{}, base...), "--no-banner"), tty: true},
		{name: "NO_COLOR", args: base, tty: true, noColor: "1"},
		{name: "narrow terminal", args: base, tty: true},
		{name: "version flag", args: []string{"--version"}, tty: true},
		{name: "version subcommand", args: []string{"version"}, tty: true},
		{name: "help with no-banner", args: []string{"--help", "--no-banner"}, tty: true},
		{name: "named command", args: append(append([]string{}, base...), "gap"), tty: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cols := 80
			if tt.name == "narrow terminal" {
				cols = 19
			}
			stubTerminal(t, tt.tty, cols)
			t.Setenv("NO_COLOR", tt.noColor)
			var out, errOut bytes.Buffer

			run(tt.args, strings.NewReader(""), &out, &errOut)

			if strings.Contains(errOut.String(), "----------/") {
				t.Errorf("wordmark printed anyway; stderr = %q", errOut.String())
			}
			if strings.Contains(out.String(), "----------/") {
				t.Errorf("wordmark reached stdout; stdout = %q", out.String())
			}
		})
	}
}

func TestRunVersionPrintsOnlyTheVersion(t *testing.T) {
	// A banner here would corrupt every pipe, grep and pasted bug report.
	stubTerminal(t, true, 80)
	for _, args := range [][]string{{"--version"}, {"-v"}, {"version"}} {
		t.Run(args[0], func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := run(args, strings.NewReader(""), &out, &errOut); code != 0 {
				t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
			}
			if got, want := out.String(), "reap "+version()+"\n"; got != want {
				t.Errorf("stdout = %q, want exactly %q", got, want)
			}
			if got := errOut.String(); got != "" {
				t.Errorf("stderr = %q, want empty", got)
			}
		})
	}
}

// Issue #29. On a machine with Claude Code installed, fillDefaults resolves
// opts.claudeDir so the state commands have somewhere to read and write. gather
// then read that same field as "the user passed --claude-dir" and scanned Claude
// Code alone, so every other detected platform was dropped and the six platforms
// promised in --help were never actually read. Every other test in this file
// passes --claude-dir, which is exactly why none of them caught it.
func TestGatherScansEveryDetectedPlatform(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)

	codexDir := t.TempDir()
	skill := filepath.Join(codexDir, "skills", "codexonly")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"),
		[]byte("---\nname: codexonly\ndescription: lives only on codex\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}

	orig := detectPlatforms
	t.Cleanup(func() { detectPlatforms = orig })
	detectPlatforms = func() []platform.Info {
		return []platform.Info{
			{
				ID: platform.ClaudeCode, Name: "Claude Code",
				ConfigDirAbs: claudeDir, ConfigFileAbs: claudeJSON,
				TranscriptType: "jsonl", HasTranscripts: true,
				TranscriptDirs: []string{filepath.Join(claudeDir, "projects")},
			},
			{
				ID: platform.CodexCLI, Name: "Codex CLI",
				ConfigDirAbs: codexDir, TranscriptType: "jsonl",
			},
		}
	}

	// Exactly what fillDefaults leaves behind when no --claude-dir was given:
	// claudeDir resolved for state, but never requested by the user.
	opts := options{claudeDir: claudeDir, claudeJSON: claudeJSON, days: 30}

	r, err := gather(opts)
	if err != nil {
		t.Fatal(err)
	}

	var sawCodex, sawClaude bool
	for _, row := range r.Rows {
		switch row.Name {
		case "codexonly":
			sawCodex = true
		case "deadskill":
			sawClaude = true
		}
	}
	if !sawClaude {
		t.Errorf("Claude Code skills missing from the report (%d rows)", len(r.Rows))
	}
	if !sawCodex {
		t.Errorf("codexonly missing: gather scanned Claude Code alone and dropped the other detected platform (%d rows)", len(r.Rows))
	}
}

// Issue #30. A platform is held at REVIEW only when it advertises transcripts
// and none could be parsed. Cursor and OpenClaw declare HasTranscripts: false,
// so they skipped that branch entirely and their items were judged with Uses: 0
// against the merged session count, which Claude Code dominates. The result is
// a REAP verdict built on evidence that cannot exist, and reap prune acts on it.
func TestPlatformWithoutTranscriptsIsNotReapedOnAnotherPlatformsSessions(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)

	blindDir := t.TempDir()
	skill := filepath.Join(blindDir, "skills", "blindskill")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"),
		[]byte("---\nname: blindskill\ndescription: on a platform we cannot observe\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}

	orig := detectPlatforms
	t.Cleanup(func() { detectPlatforms = orig })
	detectPlatforms = func() []platform.Info {
		return []platform.Info{
			{
				ID: platform.ClaudeCode, Name: "Claude Code",
				ConfigDirAbs: claudeDir, ConfigFileAbs: claudeJSON,
				TranscriptType: "jsonl", HasTranscripts: true,
				TranscriptDirs: []string{filepath.Join(claudeDir, "projects")},
			},
			{
				ID: platform.OpenClaw, Name: "OpenClaw",
				ConfigDirAbs: blindDir, TranscriptType: "none", HasTranscripts: false,
			},
		}
	}

	opts := options{claudeDir: claudeDir, claudeJSON: claudeJSON, days: 30, minSessions: 1}

	r, err := gather(opts)
	if err != nil {
		t.Fatal(err)
	}

	var found bool
	for _, row := range r.Rows {
		if row.Name != "blindskill" {
			continue
		}
		found = true
		if row.Verdict == "REAP" || row.Verdict == "MUTE" {
			t.Errorf("blindskill got %s/%q, but OpenClaw exposes no transcripts: the verdict rests on Claude Code's sessions, not on evidence about this item",
				row.Verdict, row.Reason)
		}
	}
	if !found {
		t.Fatal("blindskill never reached the report")
	}
}

// Issue #32. With the config directory unreadable, every scanner returns
// nothing and the banner prints "0 items never used · ~$0.00/month" with exit
// 0, which is exactly what a genuinely clean stack prints. The warnings say the
// opposite, but the banner is the loudest thing on the page and a wrapping
// script sees only the exit status.
func TestReportOnUnreadableDirDoesNotLookLikeACleanStack(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: chmod 000 grants access anyway")
	}
	root := t.TempDir()
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	var out, errOut bytes.Buffer
	code := run([]string{"--claude-dir", dir, "--no-nudge", "--no-banner"},
		strings.NewReader(""), &out, &errOut)

	if code == 0 {
		t.Errorf("exit = 0, so a caller cannot tell an unreadable directory from a clean stack")
	}
	if !strings.Contains(errOut.String(), "could not") && !strings.Contains(errOut.String(), "nothing") {
		t.Errorf("stderr does not say the inventory failed: %q", errOut.String())
	}
}
