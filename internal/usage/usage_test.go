package usage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	past := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}

	st, err := Parse(dir, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), 30)
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

func TestParseOverlongLineMarksEvidenceIncomplete(t *testing.T) {
	dir := t.TempDir()
	overlong := `{"type":"assistant","timestamp":"2026-06-01T10:01:00Z","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"hidden-skill"}}],"pad":"` +
		strings.Repeat("x", maxLineBytes) + `"}}`
	writeTranscript(t, filepath.Join(dir, "proj-a", "s1.jsonl"),
		`{"type":"assistant","timestamp":"2026-06-01T10:00:00Z","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"seen-skill"}}]}}`,
		overlong,
	)

	st, err := Parse(dir, time.Time{}, 30)
	if err != nil {
		t.Fatal(err)
	}
	if !st.IncompleteEvidence {
		t.Fatal("overlong transcript line should mark evidence incomplete")
	}
	if st.MalformedLines != 1 {
		t.Errorf("MalformedLines = %d, want 1", st.MalformedLines)
	}
	if got := st.Uses[scan.CatSkill]["seen-skill"]; got != 1 {
		t.Errorf("seen-skill uses = %d, want 1", got)
	}
	if got := st.Uses[scan.CatSkill]["hidden-skill"]; got != 0 {
		t.Errorf("hidden-skill uses = %d, want 0 because the overlong record was not parsed", got)
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

	st, err := Parse(dir, time.Time{}, 30)
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

func TestParseSuccessfulResultMentioningErrorIsNotBroken(t *testing.T) {
	dir := t.TempDir()
	writeTranscript(t, filepath.Join(dir, "p", "s.jsonl"),
		// Succeeds (is_error absent/false) but the output text contains the word
		// "error". This must count as a use, not an error: a linter/review skill
		// reporting "no errors found" is working correctly.
		`{"type":"assistant","timestamp":"2026-06-01T10:00:00Z","message":{"content":[{"type":"tool_use","id":"t1","name":"Skill","input":{"skill":"lintskill"}}]}}`,
		`{"type":"user","timestamp":"2026-06-01T10:00:01Z","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"no error found in your code"}]}}`,
	)

	st, err := Parse(dir, time.Time{}, 30)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Uses[scan.CatSkill]["lintskill"]; got != 1 {
		t.Errorf("lintskill uses = %d, want 1 (success that merely mentions 'error' is still a use)", got)
	}
	if got := st.Errors[scan.CatSkill]["lintskill"]; got != 0 {
		t.Errorf("lintskill errors = %d, want 0 (is_error was not set)", got)
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

	st, err := Parse(dir, time.Time{}, 30)
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

func TestNewStatsInit(t *testing.T) {
	st := NewStats(30)
	if st.WindowDays != 30 {
		t.Errorf("WindowDays = %d", st.WindowDays)
	}
	if st.Uses == nil || st.Errors == nil || st.Last == nil {
		t.Error("maps should be initialized")
	}
}

func TestRecord(t *testing.T) {
	st := NewStats(30)
	now := time.Now()
	st.record(scan.CatSkill, "test-skill", now)
	if st.Uses[scan.CatSkill]["test-skill"] != 1 {
		t.Error("record did not increment use count")
	}
	if !st.Last[scan.CatSkill]["test-skill"].Equal(now) {
		t.Error("record did not update Last")
	}
	// Second call with later timestamp.
	later := now.Add(time.Hour)
	st.record(scan.CatSkill, "test-skill", later)
	if st.Uses[scan.CatSkill]["test-skill"] != 2 {
		t.Error("record did not increment again")
	}
	if !st.Last[scan.CatSkill]["test-skill"].Equal(later) {
		t.Error("Last should be the later timestamp")
	}
}

func TestRecordEmptyKey(t *testing.T) {
	st := NewStats(30)
	st.record(scan.CatSkill, "", time.Now())
	if len(st.Uses[scan.CatSkill]) != 0 {
		t.Error("empty key should not be recorded")
	}
}

func TestRecordError(t *testing.T) {
	st := NewStats(30)
	now := time.Now()
	st.recordError(scan.CatMCP, "broken-mcp", now)
	if st.Errors[scan.CatMCP]["broken-mcp"] != 1 {
		t.Error("recordError did not increment error count")
	}
	if !st.LastAttempt[scan.CatMCP]["broken-mcp"].Equal(now) {
		t.Error("recordError did not update LastAttempt")
	}
}

func TestRecordSkillProjectEmptyKey(t *testing.T) {
	st := NewStats(30)
	st.recordSkillProject("", "repo")
	if len(st.SkillProjects) != 0 {
		t.Error("empty skill key should not create project entry")
	}
	st.recordSkillProject("skill", "")
	if _, ok := st.SkillProjects["skill"]; ok {
		t.Error("empty project should not create entry")
	}
}

func TestProjectFor(t *testing.T) {
	cases := []struct {
		path, want string
	}{
		{"/root/proj-a/s1.jsonl", "proj-a"},
		{"/root/s1.jsonl", ""},
		{"/root//s1.jsonl", ""},
	}
	for _, c := range cases {
		if got := projectFor("/root", c.path); got != c.want {
			t.Errorf("projectFor(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestParseUnreadableSubdirMarksEvidenceIncomplete(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-based failure injection does not work as root")
	}
	dir := t.TempDir()

	// Accessible transcript that should still be parsed.
	writeTranscript(t, filepath.Join(dir, "proj-a", "s1.jsonl"),
		`{"type":"assistant","timestamp":"2026-06-01T10:00:00Z","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"seen-skill"}}]}}`,
	)

	// A subdirectory whose contents cannot be read: evidence under it is lost.
	unreadable := filepath.Join(dir, "proj-b")
	writeTranscript(t, filepath.Join(unreadable, "s2.jsonl"),
		`{"type":"assistant","timestamp":"2026-06-01T10:00:00Z","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"hidden-skill"}}]}}`,
	)
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(unreadable, 0o755)

	st, err := Parse(dir, time.Time{}, 30)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !st.IncompleteEvidence {
		t.Fatal("unreadable subdirectory should mark evidence incomplete")
	}
	if got := st.Uses[scan.CatSkill]["seen-skill"]; got != 1 {
		t.Errorf("accessible transcript should still be parsed; seen-skill uses = %d, want 1", got)
	}
}

// Real transcripts carry three shapes of MCP tool name. Pin the server segment
// the parser extracts from each, since report.aliasKeys is written against
// exactly these keys.
func TestParseMCPToolPrefixForms(t *testing.T) {
	dir := t.TempDir()
	writeTranscript(t, filepath.Join(dir, "proj", "s.jsonl"),
		// Plain: a server from the user or project config.
		`{"type":"assistant","timestamp":"2026-06-01T10:00:00Z","message":{"content":[{"type":"tool_use","name":"mcp__headroom__headroom_stats","input":{}}]}}`,
		// Plugin-shipped: the server name is prefixed with plugin_<plugin>_.
		`{"type":"assistant","timestamp":"2026-06-01T10:01:00Z","message":{"content":[{"type":"tool_use","name":"mcp__plugin_ecc_playwright__browser_navigate","input":{}}]}}`,
		// Plugin-shipped with a hyphen in the plugin name.
		`{"type":"assistant","timestamp":"2026-06-01T10:02:00Z","message":{"content":[{"type":"tool_use","name":"mcp__plugin_superpowers-chrome_chrome__use_browser","input":{}}]}}`,
		// Remote claude.ai connector: no config row exists for these, so the key
		// is recorded but matches nothing. Pinned so the shape is not lost.
		`{"type":"assistant","timestamp":"2026-06-01T10:03:00Z","message":{"content":[{"type":"tool_use","name":"mcp__claude_ai_Figma__get_metadata","input":{}}]}}`,
	)

	st, err := Parse(dir, time.Time{}, 30)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]int{
		"headroom":                         1,
		"plugin_ecc_playwright":            1,
		"plugin_superpowers-chrome_chrome": 1,
		"claude_ai_Figma":                  1,
	}
	for key, n := range want {
		if got := st.Uses[scan.CatMCP][key]; got != n {
			t.Errorf("Uses[mcp][%q] = %d, want %d", key, got, n)
		}
	}
}

// Issue #33. bufio.Scanner stops for good at a line over its limit, so every
// record after a 12MB tool result is silently dropped and the loss is reported
// as "1 unreadable line". The existing overlong test puts the long line last,
// which is why it never caught this.
func TestParseResumesAfterAnOverlongLine(t *testing.T) {
	dir := t.TempDir()
	overlong := `{"type":"assistant","timestamp":"2026-06-01T10:01:00Z","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"hidden-skill"}}],"pad":"` +
		strings.Repeat("x", maxLineBytes) + `"}}`
	writeTranscript(t, filepath.Join(dir, "proj-a", "s1.jsonl"),
		`{"type":"assistant","timestamp":"2026-06-01T10:00:00Z","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"before-skill"}}]}}`,
		overlong,
		`{"type":"assistant","timestamp":"2026-06-01T10:02:00Z","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"after-skill"}}]}}`,
	)

	st, err := Parse(dir, time.Time{}, 30)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Uses[scan.CatSkill]["before-skill"]; got != 1 {
		t.Errorf("before-skill uses = %d, want 1", got)
	}
	if got := st.Uses[scan.CatSkill]["after-skill"]; got != 1 {
		t.Errorf("after-skill uses = %d, want 1: everything past the overlong line was dropped", got)
	}
	if got := st.Uses[scan.CatSkill]["hidden-skill"]; got != 0 {
		t.Errorf("hidden-skill uses = %d, want 0: that record really is unparseable", got)
	}
	if !st.IncompleteEvidence {
		t.Error("the overlong line should still mark the evidence incomplete")
	}
}

// The provider's own accounting rides along on assistant messages. It is the
// only measured counterpart to the character-ratio estimate, and the only view
// of the cache split: resident context is billed once as a cache creation and
// again on every later turn as a cache read.
func TestParseSumsMeasuredUsage(t *testing.T) {
	dir := t.TempDir()
	writeTranscript(t, filepath.Join(dir, "proj-a", "s1.jsonl"),
		`{"type":"assistant","timestamp":"2026-06-01T10:00:00Z","message":{"content":[],"usage":{"input_tokens":2,"output_tokens":100,"cache_creation_input_tokens":613,"cache_read_input_tokens":159929}}}`,
		// A message that fired nothing still costs a cache read, and skipping
		// it would undercount exactly the turns this is meant to expose.
		`{"type":"assistant","timestamp":"2026-06-01T10:01:00Z","message":{"content":[],"usage":{"input_tokens":3,"output_tokens":50,"cache_creation_input_tokens":0,"cache_read_input_tokens":160000}}}`,
		// No usage block at all: older transcripts and other harnesses.
		`{"type":"assistant","timestamp":"2026-06-01T10:02:00Z","message":{"content":[]}}`,
	)
	writeTranscript(t, filepath.Join(dir, "proj-b", "s2.jsonl"),
		`{"type":"assistant","timestamp":"2026-06-02T11:00:00Z","message":{"content":[],"usage":{"input_tokens":10,"output_tokens":7,"cache_creation_input_tokens":1,"cache_read_input_tokens":5}}}`,
	)

	st, err := Parse(dir, time.Time{}, 30)
	if err != nil {
		t.Fatal(err)
	}
	got := st.Measured
	want := Measured{Messages: 3, Input: 15, Output: 157, CacheCreation: 614, CacheRead: 319934}
	if got != want {
		t.Errorf("measured totals across files:\n got %+v\nwant %+v", got, want)
	}
}

func TestMeasuredMergeAdds(t *testing.T) {
	a := Measured{Messages: 1, Input: 2, Output: 3, CacheCreation: 4, CacheRead: 5}
	b := Measured{Messages: 10, Input: 20, Output: 30, CacheCreation: 40, CacheRead: 50}
	a.Merge(b)
	if want := (Measured{11, 22, 33, 44, 55}); a != want {
		t.Errorf("got %+v, want %+v", a, want)
	}
}

// The per-file cache path merges through MergeInto rather than Merge, and a
// miss there would silently zero the totals on every warm run.
func TestMergeIntoCarriesMeasured(t *testing.T) {
	dst, src := NewStats(30), NewStats(30)
	src.Measured = Measured{Messages: 2, Input: 5, CacheRead: 900}
	MergeInto(dst, src)
	if dst.Measured.CacheRead != 900 || dst.Measured.Messages != 2 {
		t.Errorf("MergeInto dropped the measured totals: %+v", dst.Measured)
	}
}

// An MCP server's per-session weight is what Claude Code recorded injecting:
// the deferred tool names and the server's instructions. A later session wins,
// because a server's tool list changes with its version.
func TestParseMCPWeight(t *testing.T) {
	dir := t.TempDir()
	writeTranscript(t, filepath.Join(dir, "p", "old.jsonl"),
		`{"type":"attachment","timestamp":"2026-06-01T10:00:00Z","attachment":{"type":"deferred_tools_delta","addedNames":["mcp__blender__a","WebFetch"]}}`,
	)
	writeTranscript(t, filepath.Join(dir, "p", "new.jsonl"),
		`{"type":"attachment","timestamp":"2026-06-02T10:00:00Z","attachment":{"type":"deferred_tools_delta","addedNames":["mcp__blender__a","mcp__blender__bb","mcp__claude_ai_Figma__x"]}}`,
		`{"type":"attachment","timestamp":"2026-06-02T10:00:01Z","attachment":{"type":"mcp_instructions_delta","addedNames":["claude.ai Figma"],"addedBlocks":["0123456789"]}}`,
	)
	st, err := Parse(dir, time.Time{}, 30)
	if err != nil {
		t.Fatal(err)
	}
	// Each name is one line of the injected list: name plus newline.
	want := map[string]int{
		"blender":         len("mcp__blender__a\n") + len("mcp__blender__bb\n"),
		"claude_ai_Figma": len("mcp__claude_ai_Figma__x\n") + 10,
	}
	if len(st.MCPWeight) != len(want) {
		t.Fatalf("MCPWeight = %v, want keys %v", st.MCPWeight, want)
	}
	for k, chars := range want {
		if got := st.MCPWeight[k].Chars; got != chars {
			t.Errorf("MCPWeight[%q] = %d, want %d", k, got, chars)
		}
	}
}

func TestMCPServerKey(t *testing.T) {
	for in, want := range map[string]string{
		"claude.ai Figma":     "claude_ai_Figma",
		"plugin:ecc:context7": "plugin_ecc_context7",
		"caveman-shrink":      "caveman-shrink",
	} {
		if got := mcpServerKey(in); got != want {
			t.Errorf("mcpServerKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// A skill's per-session weight is its line in the listing Claude Code
// injected: past a budget it keeps only the name, so reading the SKILL.md
// overstated it. A later listing in the session adds skills; a later session
// replaces the measurement.
func TestParseSkillWeight(t *testing.T) {
	dir := t.TempDir()
	listing := func(ts string, initial bool, content string, names ...string) string {
		b, _ := json.Marshal(map[string]any{"type": "attachment", "timestamp": ts, "attachment": map[string]any{
			"type": "skill_listing", "isInitial": initial, "content": content, "names": names}})
		return string(b)
	}
	writeTranscript(t, filepath.Join(dir, "p", "s.jsonl"),
		listing("2026-06-02T10:00:00Z", true, "- long: does a thing\n  over two lines\n- ecc:plan\n- ecc:x: y",
			"long", "ecc:plan", "ecc:x"),
		listing("2026-06-02T10:05:00Z", false, "- late: added", "late"),
	)
	st, err := Parse(dir, time.Time{}, 30)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{
		"long":     len("- long: does a thing\n  over two lines\n"),
		"ecc:plan": len("- ecc:plan\n"),
		"ecc:x":    len("- ecc:x: y\n"),
		"late":     len("- late: added\n"),
	}
	for k, chars := range want {
		if got := st.SkillWeight[k].Chars; got != chars {
			t.Errorf("SkillWeight[%q] = %d, want %d", k, got, chars)
		}
	}
}

// Only a message the user sent is an invocation. Transcripts also hold the tag
// as data: a file read back in a tool_result, source written by the assistant,
// the raw API request log.
func TestParseCountsCommandNameOnlyInUserMessages(t *testing.T) {
	dir := t.TempDir()
	writeTranscript(t, filepath.Join(dir, "proj", "s.jsonl"),
		`{"type":"user","timestamp":"2026-06-01T10:00:00Z","message":{"content":"<command-message>typed</command-message>\n<command-name>/typed</command-name>"}}`,
		`{"type":"user","timestamp":"2026-06-01T10:00:01Z","message":{"content":[{"type":"text","text":"<command-name>/typedblock</command-name>"}]}}`,
		`{"type":"user","timestamp":"2026-06-01T10:01:00Z","message":{"content":[{"type":"tool_result","tool_use_id":"t","content":"<command-name>/inresult</command-name>"}]}}`,
		`{"type":"assistant","timestamp":"2026-06-01T10:02:00Z","message":{"content":[{"type":"text","text":"<command-name>/saidbyassistant</command-name>"}]}}`,
		`{"type":"api-request-blob","timestamp":"2026-06-01T10:03:00Z","message":{"content":[{"type":"text","text":"<command-name>/inblob</command-name>"}]}}`,
	)
	st, err := Parse(dir, time.Time{}, 30)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]int{"typed": 1, "typedblock": 1, "inresult": 0, "saidbyassistant": 0, "inblob": 0} {
		if got := st.Uses[scan.CatSkill][name]; got != want {
			t.Errorf("%s uses = %d, want %d", name, got, want)
		}
	}
}

// A skill asked for in words is recorded apart from its uses. Only what the user
// wrote counts: a subagent's prompt, pasted text and tool output do not.

// "N uses in 30 days" goes by each event's own time. A resumed session keeps old
// lines in a file modified today: they stay out of the count and stay available
// to the durable record. An event with no time has only its file's date.
func TestWindowGoesByEachEventsOwnTime(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "p")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	line := func(skill, ts string) string {
		field := ""
		if ts != "" {
			field = `"timestamp":"` + ts + `",`
		}
		return `{"type":"assistant","sessionId":"s",` + field + `"message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"` + skill + `"}}]}}` + "\n"
	}
	old := now.AddDate(0, 0, -60).Format(time.RFC3339)
	recent := now.AddDate(0, 0, -2).Format(time.RFC3339)
	body := line("oldfire", old) + line("both", old) + line("both", recent) + line("undated", "")
	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var observed *Stats
	st, err := ParseObserving(dir, now.AddDate(0, 0, -30), 30, func(string) bool { return true },
		func(_ string, one *Stats) { observed = one })
	if err != nil {
		t.Fatal(err)
	}
	uses := st.Uses[scan.CatSkill]
	if uses["oldfire"] != 0 || !st.Last[scan.CatSkill]["oldfire"].IsZero() {
		t.Errorf("oldfire counted: %d, last %v", uses["oldfire"], st.Last[scan.CatSkill]["oldfire"])
	}
	if uses["both"] != 1 || st.Last[scan.CatSkill]["both"].Format(time.RFC3339) != recent {
		t.Errorf("both = %d, last %v", uses["both"], st.Last[scan.CatSkill]["both"])
	}
	if uses["undated"] != 1 {
		t.Errorf("undated = %d, want it counted on its file's date", uses["undated"])
	}
	if len(st.SkillProjects["oldfire"]) != 0 {
		t.Errorf("oldfire attributed to a project: %v", st.SkillProjects["oldfire"])
	}
	olderUses, olderLast := observed.OlderUses()
	if olderUses[scan.CatSkill]["oldfire"] != 1 || olderUses[scan.CatSkill]["both"] != 1 {
		t.Errorf("history lost older events: %v", olderUses)
	}
	if olderLast[scan.CatSkill]["oldfire"].Format(time.RFC3339) != old {
		t.Errorf("older date = %v", olderLast[scan.CatSkill]["oldfire"])
	}
}

// A call that was refused or interrupted never ran, in the wordings real
// transcripts carry; one that ran and failed is an error.
func TestRefusalsAreNeitherUsesNorErrors(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "p")
	result := func(id, content string, isError bool) string {
		c, _ := json.Marshal(content)
		return fmt.Sprintf(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":%q,"is_error":%v,"content":%s}]}}`, id, isError, c)
	}
	call := func(id, skill string) string {
		return fmt.Sprintf(`{"type":"assistant","timestamp":"2026-06-01T10:00:00Z","message":{"content":[{"type":"tool_use","id":%q,"name":"Skill","input":{"skill":%q}}]}}`, id, skill)
	}
	refusals := []string{
		"The user doesn't want to proceed with this tool use. The tool use was rejected (eg. if it was a file edit, the new_string was NOT written to the file)",
		"Permission to use Skill has been denied.",
		"Permission for this action was denied by the Claude Code auto mode classifier. Reason: [x].",
		"[Request interrupted by user for tool use]",
		"PreToolUse:Skill hook error: [x]",
	}
	var lines []string
	for i, text := range refusals {
		id := fmt.Sprintf("r%d", i)
		lines = append(lines, call(id, "refused"), result(id, text, true))
	}
	lines = append(lines, call("e", "failed"), result("e", "Skill failed: no such file", true))
	lines = append(lines, call("o", "fine"), result("o", "The user doesn't want to proceed is what this skill prints", false))
	writeTranscript(t, filepath.Join(dir, "s.jsonl"), lines...)
	st, err := Parse(dir, time.Time{}, 30)
	if err != nil {
		t.Fatal(err)
	}
	if st.Uses[scan.CatSkill]["refused"] != 0 || st.Errors[scan.CatSkill]["refused"] != 0 {
		t.Errorf("refused: uses %d errors %d", st.Uses[scan.CatSkill]["refused"], st.Errors[scan.CatSkill]["refused"])
	}
	if st.Errors[scan.CatSkill]["failed"] != 1 {
		t.Errorf("failed: errors %d", st.Errors[scan.CatSkill]["failed"])
	}
	if st.Uses[scan.CatSkill]["fine"] != 1 {
		t.Errorf("fine: uses %d", st.Uses[scan.CatSkill]["fine"])
	}
}

// MCP calls the same way: refused is no use, failed and unanswered are.
func TestRefusedMCPCallsAreNotUses(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "p")
	call := func(id, server string) string {
		return fmt.Sprintf(`{"type":"assistant","timestamp":"2026-06-01T10:00:00Z","message":{"content":[{"type":"tool_use","id":%q,"name":"mcp__%s__do","input":{}}]}}`, id, server)
	}
	result := func(id, content string, isError bool) string {
		c, _ := json.Marshal(content)
		return fmt.Sprintf(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":%q,"is_error":%v,"content":%s}]}}`, id, isError, c)
	}
	writeTranscript(t, filepath.Join(dir, "s.jsonl"),
		call("a", "refused"), result("a", "The user doesn't want to proceed with this tool use. The tool use was rejected", true),
		call("b", "failed"), result("b", "connection reset", true),
		call("c", "fine"), result("c", "ok", false),
		call("d", "unanswered"),
	)
	st, err := Parse(dir, time.Time{}, 30)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"refused": 0, "failed": 1, "fine": 1, "unanswered": 1}
	for server, n := range want {
		if got := st.Uses[scan.CatMCP][server]; got != n {
			t.Errorf("%s uses = %d, want %d", server, got, n)
		}
	}
	if _, scored := st.MCPPayload["mcp__refused__do"]; scored {
		t.Error("a refused call's text was scored as its payload")
	}
}
