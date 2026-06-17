// Package usage extracts invocation evidence from Claude Code session
// transcripts (~/.claude/projects/**/*.jsonl). It stream-parses each
// line, looking for tool_use blocks (Skill, Task/Agent, mcp__*) and
// <command-name> tags in user messages. Skill tool_use blocks are matched
// to their tool_result so an errored invocation is tracked separately from
// a successful use.
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

	// Errors counts invocations that resulted in an error, and LastAttempt
	// records the most recent attempt (success or error). Together with Uses
	// they distinguish a "broken-cold" item (errored, never succeeded) from a
	// plain "cold" one (never invoked).
	Errors      map[scan.Category]map[string]int
	LastAttempt map[scan.Category]map[string]time.Time

	// SkillProjects maps a skill invocation key to the projects that fired it
	// and how often. A skill that is cold globally but heavily used in one
	// project shows up here as concentrated in a single bucket. The project
	// key is the ~/.claude/projects/<dir> segment (Claude Code's encoded cwd).
	SkillProjects map[string]map[string]int

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
		Errors: map[scan.Category]map[string]int{
			scan.CatSkill: {}, scan.CatAgent: {}, scan.CatMCP: {},
		},
		LastAttempt: map[scan.Category]map[string]time.Time{
			scan.CatSkill: {}, scan.CatAgent: {}, scan.CatMCP: {},
		},
		SkillProjects: map[string]map[string]int{},
	}
}

// recordSkillProject attributes one successful skill firing to a project.
func (s *Stats) recordSkillProject(key, project string) {
	if key == "" || project == "" {
		return
	}
	if s.SkillProjects[key] == nil {
		s.SkillProjects[key] = map[string]int{}
	}
	s.SkillProjects[key][project]++
}

func (s *Stats) record(cat scan.Category, key string, ts time.Time) {
	if key == "" {
		return
	}
	s.Uses[cat][key]++
	if ts.After(s.Last[cat][key]) {
		s.Last[cat][key] = ts
	}
	if ts.After(s.LastAttempt[cat][key]) {
		s.LastAttempt[cat][key] = ts
	}
}

func (s *Stats) recordError(cat scan.Category, key string, ts time.Time) {
	if key == "" {
		return
	}
	s.Errors[cat][key]++
	if ts.After(s.LastAttempt[cat][key]) {
		s.LastAttempt[cat][key] = ts
	}
}

// transcript entry shapes (only the fields we need).
type entry struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
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
	// tool_use carries an ID; the matching tool_result carries ToolUseID.
	// IsError / Content let us tell an errored Skill invocation from a clean one.
	ID        string          `json:"id"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
}

// pendingSkill links a Skill tool_use to the skill it invoked until its
// tool_result arrives, so an errored invocation is not counted as a use.
type pendingSkill struct {
	key string
	ts  time.Time
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
		parseFile(path, projectFor(projectsDir, path), st)
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
// projectFor returns the project bucket for a transcript: the first path
// segment under projectsDir (Claude Code's encoded cwd directory). Empty when
// the file is not under projectsDir.
func projectFor(projectsDir, path string) string {
	rel, err := filepath.Rel(projectsDir, path)
	if err != nil {
		return ""
	}
	parts := strings.Split(rel, string(os.PathSeparator))
	if len(parts) < 2 {
		return "" // file directly in projectsDir, no project subdir
	}
	return parts[0]
}

func parseFile(path, project string, st *Stats) {
	f, err := os.Open(path)
	if err != nil {
		st.MalformedLines++
		return
	}
	defer f.Close()

	// recSkill records a successful skill firing and its project bucket.
	recSkill := func(key string, ts time.Time) {
		st.record(scan.CatSkill, key, ts)
		st.recordSkillProject(key, project)
	}

	declared := map[string]int{}
	used := map[string]bool{}
	pending := map[string]pendingSkill{}
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
		hasToolResult := bytes.Contains(line, []byte(`"tool_result"`))
		hasCommand := bytes.Contains(line, []byte(`<command-name>`))

		if hasCommand {
			for _, m := range commandNameRe.FindAllSubmatch(line, -1) {
				recSkill(string(m[1]), ts)
			}
		}
		if !hasToolUse && !hasToolResult {
			continue
		}
		var blocks []contentBlock
		if err := json.Unmarshal(e.Message.Content, &blocks); err != nil {
			continue
		}
		recordBlocks(st, blocks, ts, project, pending, used)
	}
	if sc.Err() != nil {
		st.MalformedLines++
	}

	// A Skill invocation with no matching tool_result (session ended, or the
	// result was dropped) is counted as a use: absence of a result is not
	// evidence of an error.
	for _, ps := range pending {
		recSkill(ps.key, ps.ts)
	}

	for name, weight := range declared {
		if !used[name] {
			st.DeadToolChars += weight
		}
	}
}

// recordBlocks processes one message's content blocks for skill/agent/mcp
// usage. It is shared by the JSONL parser and the SQLite parser so both count
// identically. ts dates every block in the message. pending correlates a Skill
// tool_use with its tool_result (so an errored invocation is not counted as a
// use). project (""-ok) attributes a successful skill firing to a repo. used
// (nil-ok) records every tool name seen, for JSONL dead-tool weight.
func recordBlocks(st *Stats, blocks []contentBlock, ts time.Time, project string, pending map[string]pendingSkill, used map[string]bool) {
	rec := func(key string, t time.Time) {
		st.record(scan.CatSkill, key, t)
		st.recordSkillProject(key, project)
	}
	for _, b := range blocks {
		switch b.Type {
		case "tool_use":
			if used != nil {
				used[b.Name] = true
				if strings.HasPrefix(b.Name, "mcp__") {
					rest := b.Name[len("mcp__"):]
					if i := strings.Index(rest, "__"); i > 0 {
						used[rest[:i]] = true
					} else {
						used[rest] = true
					}
				}
			}
			switch {
			case b.Name == "Skill":
				var in struct {
					Skill string `json:"skill"`
				}
				if json.Unmarshal(b.Input, &in) == nil && in.Skill != "" {
					// Defer counting until the result is known, so an errored
					// invocation is not mistaken for a successful use. Blocks
					// with no id (older transcripts, tests) count immediately.
					if b.ID == "" {
						rec(in.Skill, ts)
					} else {
						pending[b.ID] = pendingSkill{key: in.Skill, ts: ts}
					}
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
		case "tool_result":
			ps, ok := pending[b.ToolUseID]
			if !ok {
				continue
			}
			delete(pending, b.ToolUseID)
			// is_error is the authoritative signal that the tool itself failed.
			// A substring scan for "error" in the content misfires on successful
			// output that merely mentions the word (e.g. a linter reporting
			// "no errors found"), wrongly marking a working skill as broken.
			if b.IsError {
				st.recordError(scan.CatSkill, ps.key, ps.ts)
			} else {
				rec(ps.key, ps.ts)
			}
		}
	}
}
