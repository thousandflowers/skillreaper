package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

func writeLines(t *testing.T, path string, lines ...any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var out []byte
	for _, l := range lines {
		if s, ok := l.(string); ok {
			out = append(out, s...)
		} else {
			b, err := json.Marshal(l)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, b...)
		}
		out = append(out, '\n')
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

func userLine(id string) map[string]any {
	return map[string]any{"type": "user", "sessionId": id, "message": map[string]any{"content": "hi"}}
}

func toolLine(name string, input map[string]any) map[string]any {
	return map[string]any{"type": "assistant", "message": map[string]any{
		"content": []map[string]any{{"type": "tool_use", "name": name, "input": input}},
	}}
}

func parseAll(t *testing.T, dir string) *Stats {
	t.Helper()
	st, err := Parse(dir, time.Time{}, 30)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestSessionsCountDistinctTopLevelSessions(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		writeLines(t, filepath.Join(dir, "p", "s"+strconv.Itoa(i)+".jsonl"), userLine("s"+strconv.Itoa(i)))
	}
	// A subagent of s0, fired under it: its use counts, it is not a session.
	writeLines(t, filepath.Join(dir, "p", "s0", "subagents", "a.jsonl"),
		map[string]any{"type": "user", "isSidechain": true, "sessionId": "s0", "message": map[string]any{"content": "x"}},
		toolLine("Skill", map[string]any{"skill": "fromsub"}))
	writeLines(t, filepath.Join(dir, "p", "same-id-again.jsonl"), userLine("s1"))
	writeLines(t, filepath.Join(dir, "p", "summary-only.jsonl"), map[string]any{"type": "summary"})
	writeLines(t, filepath.Join(dir, "p", "empty.jsonl"))

	st := parseAll(t, dir)
	if st.Sessions != 3 {
		t.Errorf("Sessions = %d, want 3", st.Sessions)
	}
	if st.Uses[scan.CatSkill]["fromsub"] != 1 {
		t.Errorf("a subagent's use was lost: %v", st.Uses[scan.CatSkill])
	}
}

func TestAMissingRootIsNoSessionsNotLostEvidence(t *testing.T) {
	st := parseAll(t, filepath.Join(t.TempDir(), "absent"))
	if st.IncompleteEvidence || st.Sessions != 0 {
		t.Errorf("incomplete=%v sessions=%d", st.IncompleteEvidence, st.Sessions)
	}
}

func TestLinesThatPartlyDecode(t *testing.T) {
	floaty := toolLine("Skill", map[string]any{"skill": "floaty"})
	floaty["message"].(map[string]any)["usage"] = map[string]any{"input_tokens": 12.5}
	for _, c := range []struct {
		name       string
		line       any
		skill      string
		incomplete bool
	}{
		{"a float in usage loses only usage", floaty, "floaty", false},
		{"an odd block loses only itself", map[string]any{"type": "assistant", "message": map[string]any{"content": []any{
			map[string]any{"type": "x_custom", "id": 5},
			map[string]any{"type": "tool_use", "name": "Skill", "input": map[string]any{"skill": "sib"}},
		}}}, "sib", false},
		{"a cut line with a use marks the evidence incomplete",
			`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Skill"`, "", true},
	} {
		dir := t.TempDir()
		writeLines(t, filepath.Join(dir, "p", "a.jsonl"), userLine("a"), c.line)
		st := parseAll(t, dir)
		if c.skill != "" && st.Uses[scan.CatSkill][c.skill] != 1 {
			t.Errorf("%s: uses = %v", c.name, st.Uses[scan.CatSkill])
		}
		if st.IncompleteEvidence != c.incomplete {
			t.Errorf("%s: incomplete = %v", c.name, st.IncompleteEvidence)
		}
	}
}

func TestSkillAndCommandKeys(t *testing.T) {
	for in, want := range map[string]string{"/foo": "foo", "foo --flag x": "foo", "  /foo  ": "foo", "": ""} {
		if got := skillKey(in); got != want {
			t.Errorf("skillKey(%q) = %q, want %q", in, got, want)
		}
	}
	for text, want := range map[string]int{
		"<command-message>x</command-message>\n<command-name>/x</command-name>": 1,
		"<command-name>/x</command-name>":                                       1,
		"why did <command-name>/x</command-name> appear?":                       0,
	} {
		if got := len(commandNames(text)); got != want {
			t.Errorf("commandNames(%q) = %d, want %d", text, got, want)
		}
	}
}

func TestMCPResourceToolsCountForTheServer(t *testing.T) {
	dir := t.TempDir()
	writeLines(t, filepath.Join(dir, "p", "a.jsonl"), userLine("a"),
		toolLine("ListMcpResourcesTool", map[string]any{"server": "res.only"}),
		toolLine("ReadMcpResourceTool", map[string]any{"server": "resread", "uri": "x://y"}))
	st := parseAll(t, dir)
	if st.Uses[scan.CatMCP]["res_only"] != 1 || st.Uses[scan.CatMCP]["resread"] != 1 {
		t.Errorf("uses = %v", st.Uses[scan.CatMCP])
	}
}

func TestNonRegularTranscriptsAndLinkedProjects(t *testing.T) {
	dir := t.TempDir()
	writeLines(t, filepath.Join(dir, "p", "a.jsonl"), userLine("a"))
	// Would hang the parse forever if it were opened.
	if err := mkfifo(filepath.Join(dir, "p", "ff.jsonl")); err != nil {
		t.Skip(err)
	}
	moved := t.TempDir()
	writeLines(t, filepath.Join(moved, "b.jsonl"), userLine("b"), toolLine("Skill", map[string]any{"skill": "linked"}))
	if err := os.Symlink(moved, filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	st := parseAll(t, dir)
	if st.Sessions != 2 || st.Uses[scan.CatSkill]["linked"] != 1 {
		t.Errorf("sessions=%d uses=%v", st.Sessions, st.Uses[scan.CatSkill])
	}
	if st.SkillProjects["linked"]["linked"] != 1 {
		t.Errorf("linked project bucket: %v", st.SkillProjects)
	}
}

func TestMergeIntoKeepsTheMalformedBreakdown(t *testing.T) {
	src := NewStats(30)
	src.UnreadableFiles, src.TruncatedReads, src.OversizedLines, src.UnparsableLines = 1, 2, 3, 4
	dst := NewStats(30)
	MergeInto(dst, src)
	if dst.UnreadableFiles != 1 || dst.TruncatedReads != 2 || dst.OversizedLines != 3 || dst.UnparsableLines != 4 {
		t.Errorf("%+v", dst)
	}
}

func TestParseCodex(t *testing.T) {
	dir := t.TempDir()
	call := func(name, args, id string) map[string]any {
		return map[string]any{"type": "response_item", "payload": map[string]any{"type": "function_call", "name": name, "arguments": args, "call_id": id}}
	}
	writeLines(t, filepath.Join(dir, "2026", "rollout-1.jsonl"),
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "c1"}},
		call("exec_command", `{"cmd":"cat ~/.codex/skills/cx/SKILL.md"}`, "a"),
		call("mcp__srv__ping", `{}`, "b"),
		map[string]any{"type": "event_msg", "payload": map[string]any{"type": "mcp_tool_call_end", "call_id": "b", "invocation": map[string]any{"server": "srv"}}},
		map[string]any{"type": "event_msg", "payload": map[string]any{"type": "mcp_tool_call_end", "call_id": "c", "invocation": map[string]any{"server": "other"}}})
	writeLines(t, filepath.Join(dir, "2026", "not-a-rollout.jsonl"), map[string]any{"role": "user"})

	st, err := ParseCodex(dir, time.Time{}, 30, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Sessions != 1 || !st.PositiveOnly {
		t.Errorf("sessions=%d positiveOnly=%v", st.Sessions, st.PositiveOnly)
	}
	if st.Uses[scan.CatSkill]["cx"] != 1 {
		t.Errorf("skills = %v", st.Uses[scan.CatSkill])
	}
	// Two records of one call count once.
	if st.Uses[scan.CatMCP]["srv"] != 1 || st.Uses[scan.CatMCP]["other"] != 1 {
		t.Errorf("mcp = %v", st.Uses[scan.CatMCP])
	}
}
