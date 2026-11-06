package usage

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// ParseCodex reads Codex CLI rollouts (~/.codex/sessions/**/rollout-*.jsonl),
// whose lines are {timestamp, type, payload} and not Claude Code's shape.
//
// What was checked against real rollouts: session_meta, and response_item with
// payload.type function_call, name and a JSON string of arguments. The MCP event
// and tool naming come from Codex's protocol and were not seen in real data. A
// skill can also reach Codex with no call on record, so the result is
// PositiveOnly: a fire is proof, and no fire is not.
func ParseCodex(dir string, cutoff time.Time, windowDays int, needs func(id string) bool, observe func(id string, one *Stats)) (*Stats, error) {
	st, err := parseTree(dir, cutoff, windowDays, needs, observe, parseCodexFile)
	if st != nil {
		st.PositiveOnly = true
	}
	return st, err
}

type codexLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type codexPayload struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Name       string `json:"name"`
	Arguments  string `json:"arguments"`
	CallID     string `json:"call_id"`
	Invocation struct {
		Server string `json:"server"`
	} `json:"invocation"`
}

var (
	// A read of an installed item's file, inside a shell call's arguments.
	codexSkillReadRe = regexp.MustCompile(`skills/([^/\s"'\\]+)/SKILL\.md`)
	codexAgentReadRe = regexp.MustCompile(`agents/([^/\s"'\\]+)\.md`)
)

func parseCodexFile(path, _ string, st *Stats) string {
	f, err := os.Open(path)
	if err != nil {
		st.MalformedLines++
		st.UnreadableFiles++
		return ""
	}
	defer f.Close()

	sessionID := ""
	counted := map[string]bool{} // call ids, as a call can appear as two records
	br := bufio.NewReaderSize(f, 256*1024)
	for {
		line, tooLong, rerr := readDelim(br, '\n', maxLineBytes)
		if rerr != nil {
			if rerr != io.EOF {
				st.MalformedLines++
				st.TruncatedReads++
				st.IncompleteEvidence = true
			}
			return sessionID
		}
		if tooLong {
			st.MalformedLines++
			st.OversizedLines++
			st.IncompleteEvidence = true
			continue
		}
		var l codexLine
		var p codexPayload
		if len(line) == 0 || json.Unmarshal(line, &l) != nil || !decodeLenient(l.Payload, &p) {
			continue
		}
		ts, _ := time.Parse(time.RFC3339, l.Timestamp)
		switch {
		case l.Type == "session_meta" && sessionID == "":
			sessionID = "codex:" + p.ID
		case l.Type == "response_item" && p.Type == "function_call":
			recordCodexCall(st, p, ts, counted)
		case l.Type == "event_msg" && p.Type == "mcp_tool_call_end":
			if !counted[p.CallID] || p.CallID == "" {
				counted[p.CallID] = true
				st.record(scan.CatMCP, mcpServerKey(p.Invocation.Server), ts)
			}
		}
	}
}

func recordCodexCall(st *Stats, p codexPayload, ts time.Time, counted map[string]bool) {
	if strings.HasPrefix(p.Name, "mcp__") {
		counted[p.CallID] = true
		recordBlocks(st, []contentBlock{{Type: "tool_use", Name: p.Name}}, ts, "", nil, nil, nil)
		return
	}
	for _, m := range codexSkillReadRe.FindAllStringSubmatch(p.Arguments, -1) {
		st.record(scan.CatSkill, m[1], ts)
	}
	for _, m := range codexAgentReadRe.FindAllStringSubmatch(p.Arguments, -1) {
		st.record(scan.CatAgent, m[1], ts)
	}
}
