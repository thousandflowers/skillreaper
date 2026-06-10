package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	}, strings.NewReader("\n"), &out, &errOut)
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
