// Package usage extracts invocation evidence from Claude Code session
// transcripts (~/.claude/projects/**/*.jsonl). It stream-parses each
// line, looking for tool_use blocks (Skill, Task/Agent, mcp__*) and
// <command-name> tags in user messages.
package usage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// maxLineBytes bounds a single transcript line; tool results with
// embedded file contents can be huge.
const maxLineBytes = 10 * 1024 * 1024

var commandNameRe = regexp.MustCompile(`<command-name>/?([^<\s]+)</command-name>`)

// Stats aggregates invocation counts per category and invocation key.
type Stats struct {
	Sessions       int
	FilesScanned   int
	MalformedLines int
	WindowDays     int
	Uses           map[scan.Category]map[string]int
	Last           map[scan.Category]map[string]time.Time

	// DeadToolChars is the total estimated prompt-injection size of all
	// tools declared in init blocks that were never invoked during the
	// session. This captures the "dead weight" of MCP server schemas,
	// skill descriptions, and agent definitions that the model receives
	// every session but never uses.
	DeadToolChars int
}

// NewStats returns an empty Stats with initialized maps.
func NewStats(windowDays int) *Stats {
	return &Stats{
		WindowDays: windowDays,
		Uses: map[scan.Category]map[string]int{
			scan.CatSkill: {}, scan.CatAgent: {}, scan.CatMCP: {},
		},
		Last: map[scan.Category]map[string]time.Time{
			scan.CatSkill: {}, scan.CatAgent: {}, scan.CatMCP: {},
		},
	}
}

func (s *Stats) record(cat scan.Category, key string, ts time.Time) {
	if key == "" {
		return
	}
	s.Uses[cat][key]++
	if ts.After(s.Last[cat][key]) {
		s.Last[cat][key] = ts
	}
}

// transcript entry shapes (only the fields we need).
type entry struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Message   struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	Init *initPayload `json:"init,omitempty"`
}

type initPayload struct {
	Model string     `json:"model"`
	Tools []toolDecl `json:"tools"`
}

type toolDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type contentBlock struct {
	Type  string          `json:"type"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// Parse scans every .jsonl transcript under projectsDir whose mtime is
// at or after cutoff. windowDays is carried into Stats for reporting.
func Parse(projectsDir string, cutoff time.Time, windowDays int) (*Stats, error) {
	st := NewStats(windowDays)
	err := filepath.WalkDir(projectsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil || info.ModTime().Before(cutoff) {
			return nil
		}
		st.FilesScanned++
		st.Sessions++
		parseFile(path, st)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return st, nil
}

// parseFile reads one transcript. Unreadable files or lines count as
// malformed rather than aborting the whole scan.
//
// It tracks declared tools from the init block and cross-references
// them with actual tool_use invocations to compute dead-tool weight
// (tools the model sees every session but never invokes).
func parseFile(path string, st *Stats) {
	f, err := os.Open(path)
	if err != nil {
		st.MalformedLines++
		return
	}
	defer f.Close()

	declared := map[string]int{}
	used := map[string]bool{}
	parsedInit := false

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 256*1024), maxLineBytes)
	for sc.Scan() {
		line := sc.Bytes()

		var e entry
		if err := json.Unmarshal(line, &e); err != nil {
			if bytes.Contains(line, []byte(`"tool_use"`)) || bytes.Contains(line, []byte(`<command-name>`)) {
				st.MalformedLines++
			}
			continue
		}
		ts, _ := time.Parse(time.RFC3339, e.Timestamp)

		if !parsedInit && e.Type == "init" && e.Init != nil {
			parsedInit = true
			for _, t := range e.Init.Tools {
				w := len(t.Name) + len(t.Description) + len(t.InputSchema)
				if w < len(t.Name) {
					w = len(t.Name)
				}
				declared[t.Name] = w
			}
		}

		hasToolUse := e.Type == "assistant" && bytes.Contains(line, []byte(`"tool_use"`))
		hasCommand := bytes.Contains(line, []byte(`<command-name>`))

		if hasCommand {
			for _, m := range commandNameRe.FindAllSubmatch(line, -1) {
				st.record(scan.CatSkill, string(m[1]), ts)
			}
		}
		if !hasToolUse {
			continue
		}
		var blocks []contentBlock
		if err := json.Unmarshal(e.Message.Content, &blocks); err != nil {
			continue
		}
		for _, b := range blocks {
			if b.Type != "tool_use" {
				continue
			}
			used[b.Name] = true
			if strings.HasPrefix(b.Name, "mcp__") {
				rest := b.Name[len("mcp__"):]
				if i := strings.Index(rest, "__"); i > 0 {
					used[rest[:i]] = true
				} else {
					used[rest] = true
				}
			}

			switch {
			case b.Name == "Skill":
				var in struct {
					Skill string `json:"skill"`
				}
				if json.Unmarshal(b.Input, &in) == nil {
					st.record(scan.CatSkill, in.Skill, ts)
				}
			case b.Name == "Task" || b.Name == "Agent":
				var in struct {
					SubagentType string `json:"subagent_type"`
				}
				if json.Unmarshal(b.Input, &in) == nil {
					st.record(scan.CatAgent, in.SubagentType, ts)
				}
			case strings.HasPrefix(b.Name, "mcp__"):
				rest := b.Name[len("mcp__"):]
				if i := strings.Index(rest, "__"); i > 0 {
					st.record(scan.CatMCP, rest[:i], ts)
				} else {
					st.record(scan.CatMCP, rest, ts)
				}
			}
		}
	}
	if sc.Err() != nil {
		st.MalformedLines++
	}

	for name, weight := range declared {
		if !used[name] {
			st.DeadToolChars += weight
		}
	}
}
