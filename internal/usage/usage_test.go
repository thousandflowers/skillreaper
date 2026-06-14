package usage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

func writeTranscript(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParse(t *testing.T) {
	dir := t.TempDir()

	writeTranscript(t, filepath.Join(dir, "proj-a", "s1.jsonl"),
		`{"type":"assistant","timestamp":"2026-06-01T10:00:00Z","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"ecc:plan"}}]}}`,
		`{"type":"assistant","timestamp":"2026-06-01T10:05:00Z","message":{"content":[{"type":"tool_use","name":"mcp__blender__get_scene_info","input":{}}]}}`,
		`{"type":"assistant","timestamp":"2026-06-01T10:06:00Z","message":{"content":[{"type":"tool_use","name":"Task","input":{"subagent_type":"Explore","prompt":"x"}}]}}`,
		`this line is not json but mentions "tool_use" so it must count as malformed`,
		`{"type":"user","timestamp":"2026-06-01T10:07:00Z","message":{"content":"<command-name>/graphify</command-name>"}}`,
	)
	writeTranscript(t, filepath.Join(dir, "proj-b", "s2.jsonl"),
		`{"type":"assistant","timestamp":"2026-06-02T11:00:00Z","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"ecc:plan"}}]}}`,
		`{"type":"assistant","timestamp":"2026-06-02T11:01:00Z","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]}}`,
	)
	// Old session, excluded by mtime.
	old := filepath.Join(dir, "proj-c", "old.jsonl")
	writeTranscript(t, old,
		`{"type":"assistant","timestamp":"2025-01-01T00:00:00Z","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"ancient"}}]}}`,
	)
	past := time.Now().AddDate(0, -6, 0)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}

	cutoff := time.Now().AddDate(0, 0, -30)
	st, err := Parse(dir, cutoff, 30)
	if err != nil {
		t.Fatal(err)
	}

	if st.Sessions != 2 {
		t.Errorf("Sessions = %d, want 2", st.Sessions)
	}
	if st.MalformedLines != 1 {
		t.Errorf("MalformedLines = %d, want 1", st.MalformedLines)
	}
	if got := st.Uses[scan.CatSkill]["ecc:plan"]; got != 2 {
		t.Errorf("ecc:plan uses = %d, want 2", got)
	}
	if got := st.Uses[scan.CatSkill]["graphify"]; got != 1 {
		t.Errorf("graphify uses = %d, want 1", got)
	}
	if got := st.Uses[scan.CatMCP]["blender"]; got != 1 {
		t.Errorf("blender uses = %d, want 1", got)
	}
	if got := st.Uses[scan.CatAgent]["Explore"]; got != 1 {
		t.Errorf("Explore uses = %d, want 1", got)
	}
	if _, ok := st.Uses[scan.CatSkill]["ancient"]; ok {
		t.Error("old session should be excluded by mtime")
	}

	wantLast, _ := time.Parse(time.RFC3339, "2026-06-02T11:00:00Z")
	if !st.Last[scan.CatSkill]["ecc:plan"].Equal(wantLast) {
		t.Errorf("last use = %v, want %v", st.Last[scan.CatSkill]["ecc:plan"], wantLast)
	}
}

func TestParseErrorTracking(t *testing.T) {
	dir := t.TempDir()
	writeTranscript(t, filepath.Join(dir, "p", "s.jsonl"),
		// Invoked and errored: counts as an error, not a use (broken-cold).
		`{"type":"assistant","timestamp":"2026-06-01T10:00:00Z","message":{"content":[{"type":"tool_use","id":"t1","name":"Skill","input":{"skill":"brokenskill"}}]}}`,
		`{"type":"user","timestamp":"2026-06-01T10:00:01Z","message":{"content":[{"type":"tool_result","tool_use_id":"t1","is_error":true,"content":"boom"}]}}`,
		// Invoked and succeeded: counts as a use.
		`{"type":"assistant","timestamp":"2026-06-01T10:01:00Z","message":{"content":[{"type":"tool_use","id":"t2","name":"Skill","input":{"skill":"goodskill"}}]}}`,
		`{"type":"user","timestamp":"2026-06-01T10:01:01Z","message":{"content":[{"type":"tool_result","tool_use_id":"t2","content":"ok"}]}}`,
	)

	st, err := Parse(dir, time.Now().AddDate(0, 0, -30), 30)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Errors[scan.CatSkill]["brokenskill"]; got != 1 {
		t.Errorf("brokenskill errors = %d, want 1", got)
	}
	if got := st.Uses[scan.CatSkill]["brokenskill"]; got != 0 {
		t.Errorf("brokenskill uses = %d, want 0 (an error must not count as a use)", got)
	}
	if got := st.Uses[scan.CatSkill]["goodskill"]; got != 1 {
		t.Errorf("goodskill uses = %d, want 1", got)
	}
	if got := st.Errors[scan.CatSkill]["goodskill"]; got != 0 {
		t.Errorf("goodskill errors = %d, want 0", got)
	}
}

func TestParseSkillProjects(t *testing.T) {
	dir := t.TempDir()
	writeTranscript(t, filepath.Join(dir, "repo-a", "s1.jsonl"),
		`{"type":"assistant","timestamp":"2026-06-01T10:00:00Z","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"local-skill"}}]}}`,
	)
	writeTranscript(t, filepath.Join(dir, "repo-a", "s2.jsonl"),
		`{"type":"assistant","timestamp":"2026-06-02T10:00:00Z","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"local-skill"}}]}}`,
	)
	writeTranscript(t, filepath.Join(dir, "repo-b", "s3.jsonl"),
		`{"type":"assistant","timestamp":"2026-06-03T10:00:00Z","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"shared-skill"}}]}}`,
	)

	st, err := Parse(dir, time.Now().AddDate(0, 0, -30), 30)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.SkillProjects["local-skill"]["repo-a"]; got != 2 {
		t.Errorf("local-skill@repo-a = %d, want 2", got)
	}
	if n := len(st.SkillProjects["local-skill"]); n != 1 {
		t.Errorf("local-skill spans %d projects, want 1 (repo-local)", n)
	}
	if got := st.SkillProjects["shared-skill"]["repo-b"]; got != 1 {
		t.Errorf("shared-skill@repo-b = %d, want 1", got)
	}
}

func TestParseEmptyDir(t *testing.T) {
	st, err := Parse(t.TempDir(), time.Now().AddDate(0, 0, -30), 30)
	if err != nil {
		t.Fatal(err)
	}
	if st.Sessions != 0 {
		t.Errorf("Sessions = %d, want 0", st.Sessions)
	}
}
