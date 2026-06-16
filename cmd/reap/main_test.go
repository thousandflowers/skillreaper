package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/hook"
	"github.com/thousandflowers/skillreaper/internal/report"
)

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

func TestRunVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"version"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "reap") {
		t.Errorf("version output = %q", out.String())
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
