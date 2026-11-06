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
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// maxLineBytes bounds a single transcript line; tool results with
// embedded file contents can be huge.
const maxLineBytes = 10 * 1024 * 1024

var commandNameRe = regexp.MustCompile(`<command-name>/?([^<\s]+)</command-name>`)

// commandNames returns the commands a user entry ran. Claude Code writes a
// command block at the very start of the entry; the same tags further into a
// message are text the user pasted.
func commandNames(text string) []string {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "<command-name>") && !strings.HasPrefix(t, "<command-message>") {
		return nil
	}
	var names []string
	for _, m := range commandNameRe.FindAllStringSubmatch(t, -1) {
		names = append(names, m[1])
	}
	return names
}

// Stats aggregates invocation counts per category and invocation key.
type Stats struct {
	Sessions     int
	FilesScanned int
	// MalformedLines is the total across every failure kind below, kept as a
	// single number because callers and the JSON output already depend on it.
	MalformedLines int
	// The breakdown. "2 unreadable lines" says nothing about whether a
	// transcript is corrupt, truncated, or merely holds one oversized record,
	// and those call for different responses from the reader.
	UnreadableFiles int // the file could not be opened at all
	TruncatedReads  int // a read failed part-way through a file
	OversizedLines  int // a record exceeded the line limit and was dropped
	UnparsableLines int // a line that should have been a record was not JSON
	WindowDays      int
	// IncompleteEvidence is set when a parser hit a bounded-read error after
	// it may already have seen earlier records. Callers may still use positive
	// evidence, but must not make absence-of-use REAP/MUTE decisions from it.
	IncompleteEvidence bool

	// OldestTranscript and NewestTranscript bracket every transcript seen on
	// disk, including the ones the cutoff discards. SkippedByCutoff counts
	// those. Recorded because the walk already stats every file: without them
	// there is no way to tell a corpus the retention sweep has been trimming
	// from one that is simply younger than the window.
	OldestTranscript time.Time
	NewestTranscript time.Time
	SkippedByCutoff  int
	// ModelUse counts assistant messages in the window by model id.
	ModelUse map[string]int `json:",omitempty"`
	// windowStart is the earliest event that counts as use. Zero counts all.
	windowStart time.Time
	older       olderUses

	// Measured is the provider's own token accounting, summed over every
	// assistant message in the window. It measures whole requests and can
	// never attribute tokens to one skill, so it does not replace the
	// per-item estimate — it is the ground truth the estimate can be checked
	// against, and the only way to see how loaded-but-unused context actually
	// bills. Zero when the transcripts carry no usage block.
	Measured Measured
	Uses     map[scan.Category]map[string]int
	Last     map[scan.Category]map[string]time.Time

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

	// MCPPayload accumulates payload-quality evidence per MCP tool key
	// ("mcp__server__tool"): how many results were measured and how much of
	// their content was useful vs noise. It powers the "fires but mostly
	// noise" axis — a tool that fires often yet returns mostly base64/boilerplate
	// every call. Only MCP tools are scored (heaviest, noisiest payloads).
	MCPPayload map[string]PayloadStat

	// MCPWeight is each MCP server's per-session context weight, keyed like
	// Uses[CatMCP] ("blender", "plugin_ecc_github", "claude_ai_Figma"). It is
	// what Claude Code recorded injecting: the deferred tool names and the
	// server's instructions. A server whose schemas went straight into the
	// system prompt (tool search off) leaves no record and stays unmeasured.
	MCPWeight map[string]Injected

	// SkillWeight is each skill's per-session weight, keyed by its invocation
	// name ("graphify", "ecc:plan"): its entry in the skill listing Claude Code
	// injected. Past a size budget the listing keeps only some descriptions and
	// names the rest, so the SKILL.md alone overstates most skills.
	SkillWeight map[string]Injected

	// ProjectPaths maps a project bucket to the cwd its transcripts recorded,
	// the only real path: the bucket name itself cannot be decoded.
	ProjectPaths map[string]string

	// ToolCalls counts tool names whose server cannot be told from the name
	// alone, because a server's name can contain the separator. They are matched
	// against the configured servers at join time. ToolSep is the separator.
	ToolCalls map[string]int
	ToolLast  map[string]time.Time
	ToolSep   string

	// PositiveOnly means a fire in this history is proof of use, but no fire is
	// not proof of disuse: the format was not confirmed for every way an item
	// can be used. Such a platform never yields REAP or MUTE.
	PositiveOnly bool
}

// ToolSepDefault is how Claude Code joins server and tool in mcp__server__tool.
const ToolSepDefault = "__"

func (s *Stats) recordTool(name string, ts time.Time) {
	if s.beforeWindow(ts) {
		return
	}
	if s.ToolCalls == nil {
		s.ToolCalls, s.ToolLast = map[string]int{}, map[string]time.Time{}
	}
	s.ToolCalls[name]++
	if ts.After(s.ToolLast[name]) {
		s.ToolLast[name] = ts
	}
}

// MCPServerKey is the form of a server's name that its tool names carry.
func MCPServerKey(name string) string { return mcpServerKey(name) }

// Injected is what one item put into the prompt in the latest session that
// loaded it. Latest, not average: tool lists and listings change over time.
type Injected struct {
	Chars int
	At    time.Time
}

// recordInjected keeps the most recent measurement of key in m, which it
// creates when nil, and returns.
func recordInjected(m map[string]Injected, key string, w Injected) map[string]Injected {
	if key == "" || w.Chars == 0 {
		return m
	}
	if old, ok := m[key]; ok && !w.At.After(old.At) {
		return m
	}
	if m == nil {
		m = map[string]Injected{}
	}
	m[key] = w
	return m
}

// MergeMCPWeight folds src's injected weights (servers and skills) into s,
// the newer one winning.
func (s *Stats) MergeMCPWeight(src *Stats) {
	for key, w := range src.MCPWeight {
		s.MCPWeight = recordInjected(s.MCPWeight, key, w)
	}
	for key, w := range src.SkillWeight {
		s.SkillWeight = recordInjected(s.SkillWeight, key, w)
	}
}

// mcpServerKey turns a server's display name ("claude.ai Figma",
// "plugin:ecc:context7") into the key its tool names carry
// ("claude_ai_Figma", "plugin_ecc_context7").
func mcpServerKey(name string) string {
	return mcpKeyUnsafe.ReplaceAllString(name, "_")
}

var mcpKeyUnsafe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// PayloadStat is the per-MCP-tool payload-quality accumulator. Calls is the
// number of tool_result payloads measured; TotalChars/NoiseChars sum bytes
// across them. Stored by value to match Stats' int-map style.
type PayloadStat struct {
	Calls      int
	TotalChars int
	NoiseChars int
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
		MCPPayload:    map[string]PayloadStat{},
		MCPWeight:     map[string]Injected{},
		SkillWeight:   map[string]Injected{},
	}
}

// recordPayload folds one measured MCP tool_result into the per-tool quality
// accumulator. toolKey is the full "mcp__server__tool" name. No-op on empty key.
func (s *Stats) recordPayload(toolKey string, total, noise int) {
	if toolKey == "" {
		return
	}
	p := s.MCPPayload[toolKey]
	p.Calls++
	p.TotalChars += total
	p.NoiseChars += noise
	s.MCPPayload[toolKey] = p
}

// recordSkillProject attributes one successful skill firing to a project.
func (s *Stats) recordSkillProject(key, project string) {
	if key == "" || project == "" {
		return
	}
	key, project = scan.SafeText(key), scan.SafeText(project)
	if s.SkillProjects[key] == nil {
		s.SkillProjects[key] = map[string]int{}
	}
	s.SkillProjects[key][project]++
}

// beforeWindow reports an event older than the window. A session resumed today
// keeps its old lines in a file modified today, so the file's date says nothing
// about them: "N uses in 30 days" goes by each event's own time. An event with
// no time at all has only its file's date to go on, and stays in.
func (s *Stats) beforeWindow(ts time.Time) bool {
	return !s.windowStart.IsZero() && !ts.IsZero() && ts.Before(s.windowStart)
}

// olderUses are the uses seen before the window. They do not count as use in the
// window, but they are real history and the durable record keeps them.
type olderUses struct {
	Uses map[scan.Category]map[string]int
	Last map[scan.Category]map[string]time.Time
}

func (o *olderUses) add(cat scan.Category, key string, ts time.Time) {
	if o.Uses == nil {
		o.Uses, o.Last = map[scan.Category]map[string]int{}, map[scan.Category]map[string]time.Time{}
	}
	if o.Uses[cat] == nil {
		o.Uses[cat], o.Last[cat] = map[string]int{}, map[string]time.Time{}
	}
	o.Uses[cat][key]++
	if ts.After(o.Last[cat][key]) {
		o.Last[cat][key] = ts
	}
}

// OlderUses returns the uses this Stats saw before its window, by category and
// name, and when they last happened.
func (s *Stats) OlderUses() (map[scan.Category]map[string]int, map[scan.Category]map[string]time.Time) {
	return s.older.Uses, s.older.Last
}

func (s *Stats) record(cat scan.Category, key string, ts time.Time) {
	if key == "" {
		return
	}
	key = scan.SafeText(key)
	if s.beforeWindow(ts) {
		s.older.add(cat, key, ts)
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
	key = scan.SafeText(key)
	if s.beforeWindow(ts) {
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
	// IsMeta marks text the harness added, IsSidechain a subagent's transcript,
	// where the "user" is Claude writing a prompt.
	IsMeta      bool   `json:"isMeta"`
	IsSidechain bool   `json:"isSidechain"`
	SessionID   string `json:"sessionId"`
	Cwd         string `json:"cwd"`
	// Decoded on their own, so one field of an unexpected type loses that field
	// and not the tool uses on the same line.
	Message    json.RawMessage `json:"message"`
	Init       json.RawMessage `json:"init,omitempty"`
	Attachment json.RawMessage `json:"attachment,omitempty"`
}

// message is the part of an entry's message this parser reads.
type message struct {
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
	Usage   json.RawMessage `json:"usage"`
}

// decodeLenient decodes raw into v when it can, and reports whether it did.
func decodeLenient(raw json.RawMessage, v any) bool {
	return len(raw) > 0 && json.Unmarshal(raw, v) == nil
}

// decodeBlocks decodes a message's blocks one at a time, so a block of an
// unknown shape does not take its siblings with it. A dropped block that held a
// tool use is evidence lost, and the caller is told.
func decodeBlocks(raw json.RawMessage) (blocks []contentBlock, lostToolUse bool) {
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return nil, bytes.Contains(raw, []byte(`"tool_use"`))
	}
	for _, p := range parts {
		var b contentBlock
		if json.Unmarshal(p, &b) != nil {
			lostToolUse = lostToolUse || bytes.Contains(p, []byte(`"tool_use"`))
			continue
		}
		blocks = append(blocks, b)
	}
	return blocks, lostToolUse
}

// attachment is the part of a Claude Code context attachment that says what
// MCP servers put into the prompt.
type attachment struct {
	Type        string   `json:"type"`
	AddedNames  []string `json:"addedNames"`
	AddedBlocks []string `json:"addedBlocks"`
	// skill_listing: the listing text as injected, and the skill names in it.
	// Names is raw so an unexpected shape loses the listing, not the line.
	Content string          `json:"content"`
	Names   json.RawMessage `json:"names"`
}

type initPayload struct {
	Model string     `json:"model"`
	Tools []toolDecl `json:"tools"`
}

// Measured is the provider's token accounting, summed across the window.
//
// CacheRead is the number that matters most here and the least intuitive: a
// prompt is billed in full once as CacheCreation, then re-read on every
// subsequent turn as CacheRead. Context that loads and never fires is paid for
// on each of those turns, which is why a small resident prompt shows up as a
// large read total.
type Measured struct {
	Messages      int64 `json:"messages"`
	Input         int64 `json:"input_tokens"`
	Output        int64 `json:"output_tokens"`
	CacheCreation int64 `json:"cache_creation_tokens"`
	CacheRead     int64 `json:"cache_read_tokens"`
}

// Add folds one message's accounting in.
func (m *Measured) Add(u usageBlock) {
	m.Messages++
	m.Input += u.InputTokens
	m.Output += u.OutputTokens
	m.CacheCreation += u.CacheCreationTokens
	m.CacheRead += u.CacheReadTokens
}

// Merge folds another window's totals in, for the per-file merge.
func (m *Measured) Merge(o Measured) {
	m.Messages += o.Messages
	m.Input += o.Input
	m.Output += o.Output
	m.CacheCreation += o.CacheCreation
	m.CacheRead += o.CacheRead
}

// usageBlock is the token accounting the provider attaches to every assistant
// message. It is the measured counterpart to cost.Tokens, which estimates from
// character counts, and it is the only place the cache split can be read: dead
// context is billed once as cache_creation when a session starts and again as
// cache_read on every later turn, which is the mechanism this tool asserts.
type usageBlock struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadTokens     int64 `json:"cache_read_input_tokens"`
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
//
// A mid-walk failure (e.g. a permission-denied subdirectory) does not abort
// the scan: the affected subtree is skipped and Stats.IncompleteEvidence is
// set so callers do not treat cold items as safe to reap or mute.
// observeTranscript widens the corpus bracket to include ts. Called for every
// transcript found, in or out of the window.
func (s *Stats) observeTranscript(ts time.Time) {
	if ts.IsZero() {
		return
	}
	if s.OldestTranscript.IsZero() || ts.Before(s.OldestTranscript) {
		s.OldestTranscript = ts
	}
	if ts.After(s.NewestTranscript) {
		s.NewestTranscript = ts
	}
}

// CorpusSpanDays is how many days separate the oldest and newest transcript on
// disk. Zero when fewer than two were found.
func (s *Stats) CorpusSpanDays() int {
	if s.OldestTranscript.IsZero() || s.NewestTranscript.IsZero() {
		return 0
	}
	return int(s.NewestTranscript.Sub(s.OldestTranscript).Hours() / 24)
}

func Parse(projectsDir string, cutoff time.Time, windowDays int) (*Stats, error) {
	return ParseObserving(projectsDir, cutoff, windowDays, nil, nil)
}

// TranscriptID identifies one transcript for the purpose of remembering that
// it has already been counted. Path alone is not enough: a session file grows
// as the session continues, so the same path holds different evidence at
// different times. Size and modification time distinguish those without
// reading the file.
func TranscriptID(path string, size, modUnix int64) string {
	return fmt.Sprintf("%s@%d@%d", path, size, modUnix)
}

// ParseObserving is Parse with a per-transcript callback, so a caller keeping a
// durable record can attribute what fired to the transcript it fired in.
//
// needs is asked first, before any work: a transcript already recorded is
// parsed straight into the aggregate, skipping the per-file allocation
// entirely. The aggregate needs every in-window file either way, so that
// allocation is pure overhead once the record holds the file — which, after
// the first run, is nearly all of them. Measured on a 356 MB corpus, doing it
// unconditionally cost 17%.
//
// Both callbacks may be nil, which is the plain Parse path.
func ParseObserving(projectsDir string, cutoff time.Time, windowDays int, needs func(id string) bool, observe func(id string, one *Stats)) (*Stats, error) {
	return parseTree(projectsDir, cutoff, windowDays, needs, observe, parseFile)
}

// transcriptRoot is one tree to walk. project names the bucket when the tree is
// a linked project folder; empty means take it from the path under the root.
type transcriptRoot struct {
	dir, project string
}

// transcriptRoots returns dir plus any project folder directly under it that is
// a symlink to a directory, such as a history moved to another disk. WalkDir does
// not descend into those, and what fired there would read as never fired.
func transcriptRoots(dir string) []transcriptRoot {
	roots := []transcriptRoot{{dir: dir}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return roots
	}
	seen := map[string]bool{}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		seen[real] = true
	}
	for _, e := range entries {
		if e.Type()&os.ModeSymlink == 0 {
			continue
		}
		target, err := filepath.EvalSymlinks(filepath.Join(dir, e.Name()))
		if err != nil || seen[target] {
			continue
		}
		if info, err := os.Stat(target); err == nil && info.IsDir() {
			seen[target] = true
			roots = append(roots, transcriptRoot{dir: target, project: e.Name()})
		}
	}
	return roots
}

// isSubagentPath reports a transcript written by a subagent inside a session.
func isSubagentPath(path string) bool {
	return strings.Contains(filepath.ToSlash(path), "/subagents/")
}

// transcriptInfo returns a file's stat when it can be read as a transcript. A
// FIFO would block the open forever. A symlink is judged by what it points to.
func transcriptInfo(path string, d fs.DirEntry) (fs.FileInfo, error, bool) {
	info, err := d.Info()
	if err != nil {
		return nil, err, false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if info, err = os.Stat(path); err != nil {
			return nil, err, false
		}
	}
	return info, nil, info.Mode().IsRegular()
}

// parseTree walks one transcript tree. parseOne reads a file into a Stats and
// returns its session id, or "" when the file holds no session of its own: a
// subagent's transcript, an empty file, or lines this parser cannot recognise.
// Each session counts once, however many files carry it.
func parseTree(dir string, cutoff time.Time, windowDays int, needs func(id string) bool, observe func(id string, one *Stats), parseOne func(path, project string, st *Stats) string) (*Stats, error) {
	st := NewStats(windowDays)
	st.windowStart = cutoff
	sessions := map[string]bool{}
	var walkErr error
	for _, root := range transcriptRoots(dir) {
		if err := walkTranscripts(root, st, sessions, cutoff, windowDays, needs, observe, parseOne); err != nil {
			walkErr = err
		}
	}
	st.Sessions = len(sessions)
	return st, walkErr
}

func walkTranscripts(root transcriptRoot, st *Stats, sessions map[string]bool, cutoff time.Time, windowDays int, needs func(id string) bool, observe func(id string, one *Stats), parseOne func(path, project string, st *Stats) string) error {
	projectOf := func(path string) string {
		if root.project != "" {
			return root.project
		}
		return projectFor(root.dir, path)
	}
	return filepath.WalkDir(root.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// No such folder is the state of a machine with no sessions yet, and
			// no evidence was lost.
			if path == root.dir && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			// Walk handed us a read/stat error for this path. Skip it but flag
			// that evidence is missing — a whole subtree may be unreadable.
			st.IncompleteEvidence = true
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		info, ierr, regular := transcriptInfo(path, d)
		if ierr != nil {
			st.IncompleteEvidence = true
			return nil
		}
		if !regular {
			return nil
		}
		// Bracket the whole corpus before the window is applied: the file the
		// cutoff is about to discard is the one that says how far back the
		// evidence actually reaches.
		st.observeTranscript(info.ModTime())
		id := TranscriptID(path, info.Size(), info.ModTime().Unix())
		if info.ModTime().Before(cutoff) {
			st.SkippedByCutoff++
			// Nothing in it counts for the window, but it is history, and the
			// retention sweep will delete it. Read once, for the durable record, so
			// a skill last used 40 days ago does not read as never used.
			if observe != nil && needs != nil && needs(id) {
				one := NewStats(windowDays)
				one.windowStart = cutoff
				parseOne(path, projectOf(path), one)
				observe(id, one)
			}
			return nil
		}
		st.FilesScanned++
		note := func(session string) {
			if session != "" {
				sessions[session] = true
			}
		}
		if observe == nil || (needs != nil && !needs(id)) {
			note(parseOne(path, projectOf(path), st))
			return nil
		}
		// Parse into a per-file Stats so the observer sees this transcript
		// alone, then fold it in. The aggregate is identical either way.
		one := NewStats(windowDays)
		one.windowStart = cutoff
		note(parseOne(path, projectOf(path), one))
		observe(id, one)
		MergeInto(st, one)
		return nil
	})
}

// MergeInto folds src into dst. Counts add, timestamps take the later, and
// incomplete evidence is sticky: a partial read anywhere means the whole
// result is partial.
func MergeInto(dst, src *Stats) {
	for model, n := range src.ModelUse {
		if dst.ModelUse == nil {
			dst.ModelUse = map[string]int{}
		}
		dst.ModelUse[model] += n
	}
	dst.MalformedLines += src.MalformedLines
	dst.UnreadableFiles += src.UnreadableFiles
	dst.TruncatedReads += src.TruncatedReads
	dst.OversizedLines += src.OversizedLines
	dst.UnparsableLines += src.UnparsableLines
	dst.PositiveOnly = dst.PositiveOnly || src.PositiveOnly
	if dst.ToolSep == "" {
		dst.ToolSep = src.ToolSep
	}
	for project, path := range src.ProjectPaths {
		if dst.ProjectPaths == nil {
			dst.ProjectPaths = map[string]string{}
		}
		dst.ProjectPaths[project] = path
	}
	for name, n := range src.ToolCalls {
		if dst.ToolCalls == nil {
			dst.ToolCalls, dst.ToolLast = map[string]int{}, map[string]time.Time{}
		}
		dst.ToolCalls[name] += n
		if t := src.ToolLast[name]; t.After(dst.ToolLast[name]) {
			dst.ToolLast[name] = t
		}
	}
	dst.IncompleteEvidence = dst.IncompleteEvidence || src.IncompleteEvidence
	dst.Measured.Merge(src.Measured)
	for cat, uses := range src.Uses {
		for key, n := range uses {
			dst.Uses[cat][key] += n
		}
	}
	for cat, lasts := range src.Last {
		for key, ts := range lasts {
			if ts.After(dst.Last[cat][key]) {
				dst.Last[cat][key] = ts
			}
		}
	}
	for cat, errs := range src.Errors {
		for key, n := range errs {
			dst.Errors[cat][key] += n
		}
	}
	for cat, lasts := range src.LastAttempt {
		for key, ts := range lasts {
			if ts.After(dst.LastAttempt[cat][key]) {
				dst.LastAttempt[cat][key] = ts
			}
		}
	}
	for key, projects := range src.SkillProjects {
		if dst.SkillProjects[key] == nil {
			dst.SkillProjects[key] = map[string]int{}
		}
		for proj, n := range projects {
			dst.SkillProjects[key][proj] += n
		}
	}
	dst.DeadToolChars += src.DeadToolChars
	for key, p := range src.MCPPayload {
		d := dst.MCPPayload[key]
		d.Calls += p.Calls
		d.TotalChars += p.TotalChars
		d.NoiseChars += p.NoiseChars
		dst.MCPPayload[key] = d
	}
	dst.MergeMCPWeight(src)
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

func parseFile(path, project string, st *Stats) string {
	f, err := os.Open(path)
	if err != nil {
		st.MalformedLines++
		st.UnreadableFiles++
		return ""
	}
	sessionID := ""
	subagent := isSubagentPath(path)
	defer f.Close()

	// recSkill records a successful skill firing and its project bucket.
	recSkill := func(key string, ts time.Time) {
		st.record(scan.CatSkill, key, ts)
		if !st.beforeWindow(ts) {
			st.recordSkillProject(key, project)
		}
	}

	declared := map[string]int{}
	used := map[string]bool{}
	pending := map[string]pendingSkill{}
	mcpPending := map[string]pendingMCP{}
	parsedInit := false
	// Injected MCP context in this session: tool names per server, and the
	// latest instructions per server (a re-add replaces, never adds).
	mcpTools := map[string]map[string]bool{}
	mcpInstr := map[string]int{}
	skillChars := map[string]int{}
	var lastTS time.Time

	br := bufio.NewReaderSize(f, 256*1024)
	for {
		line, tooLong, rerr := readDelim(br, '\n', maxLineBytes)
		if rerr != nil {
			if rerr != io.EOF {
				st.MalformedLines++
				st.TruncatedReads++
				st.IncompleteEvidence = true
			}
			break
		}
		if tooLong {
			// The record itself is lost, but the file keeps being read.
			st.MalformedLines++
			st.OversizedLines++
			st.IncompleteEvidence = true
			continue
		}
		if len(line) == 0 {
			continue
		}

		var e entry
		if err := json.Unmarshal(line, &e); err != nil {
			if bytes.Contains(line, []byte(`"tool_use"`)) || bytes.Contains(line, []byte(`<command-name>`)) {
				st.MalformedLines++
				st.UnparsableLines++
				// A use may be on this line, so a zero for its item proves nothing.
				st.IncompleteEvidence = true
			}
			continue
		}
		ts, _ := time.Parse(time.RFC3339, e.Timestamp)
		if ts.After(lastTS) {
			lastTS = ts
		}
		if (e.Type == "user" || e.Type == "assistant") && !e.IsSidechain && !subagent && sessionID == "" {
			sessionID = e.SessionID
			if sessionID == "" {
				sessionID = "file:" + path // older transcripts carry no id
			}
		}
		if e.Cwd != "" && project != "" && st.ProjectPaths[project] == "" {
			if st.ProjectPaths == nil {
				st.ProjectPaths = map[string]string{}
			}
			st.ProjectPaths[project] = scan.SafeText(e.Cwd)
		}
		if e.Type == "attachment" {
			var a attachment
			if decodeLenient(e.Attachment, &a) {
				recordInjectedMCP(&a, mcpTools, mcpInstr)
				if a.Type == "skill_listing" {
					recordSkillListing(&a, skillChars)
				}
			}
			continue
		}

		var msg message
		_ = decodeLenient(e.Message, &msg)

		// Counted before any type or content check: the accounting is attached
		// to the message regardless of what the message did, and skipping the
		// ones that fired no tool would undercount exactly the turns where
		// resident context was re-read for nothing.
		var u usageBlock
		if e.Type == "assistant" && msg.Model != "" && !st.beforeWindow(ts) {
			if st.ModelUse == nil {
				st.ModelUse = map[string]int{}
			}
			st.ModelUse[msg.Model]++
		}
		if string(msg.Usage) != "null" && decodeLenient(msg.Usage, &u) {
			st.Measured.Add(u)
		}

		var init initPayload
		if !parsedInit && e.Type == "init" && decodeLenient(e.Init, &init) {
			parsedInit = true
			for _, t := range init.Tools {
				w := len(t.Name) + len(t.Description) + len(t.InputSchema)
				if w < len(t.Name) {
					w = len(t.Name)
				}
				declared[t.Name] = w
			}
		}

		hasToolUse := e.Type == "assistant" && bytes.Contains(line, []byte(`"tool_use"`))
		hasToolResult := bytes.Contains(line, []byte(`"tool_result"`))

		if e.Type == "user" {
			text := userText(msg.Content)
			for _, name := range commandNames(text) {
				recSkill(name, ts)
			}
		}
		if !hasToolUse && !hasToolResult {
			continue
		}
		blocks, lost := decodeBlocks(msg.Content)
		if lost {
			st.IncompleteEvidence = true
		}
		recordBlocks(st, blocks, ts, project, pending, mcpPending, used)
	}

	// A Skill invocation with no matching tool_result (session ended, or the
	// result was dropped) is counted as a use: absence of a result is not
	// evidence of an error.
	for _, ps := range pending {
		recSkill(ps.key, ps.ts)
	}
	for _, call := range mcpPending {
		recordMCPCall(st, call.tool, call.ts)
	}

	for name, weight := range declared {
		if !used[name] {
			st.DeadToolChars += weight
		}
	}

	chars := map[string]int{}
	for key, names := range mcpTools {
		for name := range names {
			chars[key] += len(name) + 1 // one line of the injected list
		}
	}
	for key, n := range mcpInstr {
		chars[key] += n
	}
	for key, n := range chars {
		st.MCPWeight = recordInjected(st.MCPWeight, key, Injected{Chars: n, At: lastTS})
	}
	for key, n := range skillChars {
		st.SkillWeight = recordInjected(st.SkillWeight, key, Injected{Chars: n, At: lastTS})
	}
	return sessionID
}

// skillKey reduces what the Skill tool was given to the name alone: the slash
// path already drops the "/", and a caller may pass arguments after the name.
func skillKey(s string) string {
	fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(s), "/"))
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimPrefix(fields[0], "/")
}

// userText returns a user message's own text. The tags that mark a command also
// reach a user entry as data, in a tool_result that read a file holding them.
func userText(content json.RawMessage) string {
	var text string
	if json.Unmarshal(content, &text) == nil {
		return text
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		return ""
	}
	for _, b := range blocks {
		if b.Type == "text" {
			text += b.Text
		}
	}
	return text
}

// recordSkillListing measures each skill's entry in one injected listing.
// An entry starts with "- <name>" and runs until the next entry, so a
// description written over several lines is counted whole. The name is matched
// against the listing's own names, longest first, because names contain ':'.
func recordSkillListing(a *attachment, chars map[string]int) {
	var names []string
	if json.Unmarshal(a.Names, &names) != nil || len(names) == 0 {
		return
	}
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
	var entries []string
	for _, line := range strings.Split(a.Content, "\n") {
		if strings.HasPrefix(line, "- ") || len(entries) == 0 {
			entries = append(entries, line)
			continue
		}
		entries[len(entries)-1] += "\n" + line
	}
	for _, e := range entries {
		rest, ok := strings.CutPrefix(e, "- ")
		if !ok {
			continue
		}
		for _, n := range names {
			if after, ok := strings.CutPrefix(rest, n); ok && (after == "" || after[0] == ':') {
				chars[n] = len(e) + 1
				break
			}
		}
	}
}

// recordInjectedMCP notes what one attachment injected for MCP servers.
func recordInjectedMCP(a *attachment, tools map[string]map[string]bool, instr map[string]int) {
	switch a.Type {
	case "deferred_tools_delta":
		for _, name := range a.AddedNames {
			rest, ok := strings.CutPrefix(name, "mcp__")
			if !ok {
				continue
			}
			key, _, _ := strings.Cut(rest, "__")
			if tools[key] == nil {
				tools[key] = map[string]bool{}
			}
			tools[key][name] = true
		}
	case "mcp_instructions_delta":
		for i, name := range a.AddedNames {
			if i < len(a.AddedBlocks) {
				instr[mcpServerKey(name)] = len(a.AddedBlocks[i])
			}
		}
	}
}

// recordBlocks processes one message's content blocks for skill/agent/mcp
// usage. It is shared by the JSONL parser and the SQLite parser so both count
// identically. ts dates every block in the message. pending correlates a Skill
// tool_use with its tool_result (so an errored invocation is not counted as a
// use). project (""-ok) attributes a successful skill firing to a repo. used
// (nil-ok) records every tool name seen, for JSONL dead-tool weight.
func recordBlocks(st *Stats, blocks []contentBlock, ts time.Time, project string, pending map[string]pendingSkill, mcpPending map[string]pendingMCP, used map[string]bool) {
	rec := func(key string, t time.Time) {
		st.record(scan.CatSkill, key, t)
		if !st.beforeWindow(t) {
			st.recordSkillProject(key, project)
		}
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
						rec(skillKey(in.Skill), ts)
					} else {
						pending[b.ID] = pendingSkill{key: skillKey(in.Skill), ts: ts}
					}
				}
			case b.Name == "Task" || b.Name == "Agent":
				var in struct {
					SubagentType string `json:"subagent_type"`
				}
				if json.Unmarshal(b.Input, &in) == nil {
					st.record(scan.CatAgent, in.SubagentType, ts)
				}
			case b.Name == "ListMcpResourcesTool" || b.Name == "ReadMcpResourceTool":
				var in struct {
					Server string `json:"server"`
				}
				if json.Unmarshal(b.Input, &in) == nil {
					st.record(scan.CatMCP, mcpServerKey(in.Server), ts)
				}
			case strings.HasPrefix(b.Name, "mcp__"):
				// Held until this call's tool_result arrives: a refused call is not
				// a use, and the payload is scored then. A result-less call (session
				// ended mid-call) counts as a use when the file ends, and is never
				// scored: absence of a result is not evidence of noise.
				if mcpPending != nil && b.ID != "" {
					mcpPending[b.ID] = pendingMCP{tool: b.Name, ts: ts}
				} else {
					recordMCPCall(st, b.Name, ts)
				}
			}
		case "tool_result":
			// Score the matching MCP call's payload, if any. Independent of the
			// skill-result correlation below: MCP and Skill never share an id.
			if mcpPending != nil {
				if call, ok := mcpPending[b.ToolUseID]; ok {
					delete(mcpPending, b.ToolUseID)
					if b.IsError && isRefusal(b.Content) {
						continue
					}
					recordMCPCall(st, call.tool, call.ts)
					total, noise := classifyPayload(b.Content)
					st.recordPayload(call.tool, total, noise)
				}
			}
			ps, ok := pending[b.ToolUseID]
			if !ok {
				continue
			}
			delete(pending, b.ToolUseID)
			// is_error is the authoritative signal that the tool itself failed.
			// A substring scan for "error" in the content misfires on successful
			// output that merely mentions the word (e.g. a linter reporting
			// "no errors found"), wrongly marking a working skill as broken.
			switch {
			case b.IsError && isRefusal(b.Content):
				// Someone or something said no before the skill ran. That says
				// nothing about the skill: not a use, and not a failure.
			case b.IsError:
				st.recordError(scan.CatSkill, ps.key, ps.ts)
			default:
				rec(ps.key, ps.ts)
			}
		}
	}
}

// pendingMCP is an MCP call waiting for its tool_result.
type pendingMCP struct {
	tool string
	ts   time.Time
}

// recordMCPCall counts one call of mcp__server__tool.
func recordMCPCall(st *Stats, name string, ts time.Time) {
	rest := strings.TrimPrefix(name, "mcp__")
	i := strings.Index(rest, "__")
	if i <= 0 {
		st.record(scan.CatMCP, rest, ts)
		return
	}
	st.record(scan.CatMCP, rest[:i], ts)
	// A server can itself be named a__b: only the configured
	// names can settle where the server ends, at join time.
	if strings.Contains(rest[i+2:], "__") {
		st.recordTool(scan.SafeText(rest), ts)
	}
}

// refusalPrefixes begin the tool_result text of a call that never ran, as Claude
// Code writes them in real transcripts: the user clicking no, a denied
// permission, an interrupt, and a hook or the harness blocking the call.
var refusalPrefixes = []string{
	"The user doesn't want to proceed",
	"Permission to use ",
	"Permission for this action was denied",
	"[Request interrupted by user",
	"PreToolUse:",
	"<tool_use_error>Blocked:",
}

// isRefusal reports a tool_result that holds one of them. The content is a string
// or a list of text blocks.
func isRefusal(content json.RawMessage) bool {
	var text string
	if json.Unmarshal(content, &text) != nil {
		var blocks []struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(content, &blocks) != nil || len(blocks) == 0 {
			return false
		}
		text = blocks[0].Text
	}
	text = strings.TrimSpace(text)
	for _, prefix := range refusalPrefixes {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

// readDelim returns the next record from br, without its delimiter. A record
// longer than max is dropped and reported with tooLong, leaving the reader at
// the start of the following one.
//
// bufio.Scanner cannot do that last part: one record over its buffer makes Scan
// return false for good, so a single oversized entry silently discarded every
// record after it in the same file and the loss was reported as one unreadable
// line. The delimiter is a parameter because the JSONL transcripts split on
// newlines and OpenCode's SQLite dump splits on \x1e.
func readDelim(br *bufio.Reader, delim byte, max int) (rec []byte, tooLong bool, err error) {
	var buf []byte
	trim := func(b []byte) []byte {
		b = bytes.TrimSuffix(b, []byte{delim})
		if delim == '\n' {
			b = bytes.TrimSuffix(b, []byte{'\r'})
		}
		return b
	}
	for {
		chunk, e := br.ReadSlice(delim)
		// Checked before appending, and on every chunk: the final chunk of an
		// over-long record arrives with the delimiter and no ErrBufferFull, so
		// a check confined to the buffer-full branch lets that record through.
		if len(buf)+len(chunk) > max {
			for e == bufio.ErrBufferFull {
				_, e = br.ReadSlice(delim)
			}
			if e != nil && e != io.EOF {
				return nil, true, e
			}
			return nil, true, nil
		}
		buf = append(buf, chunk...)
		if e == bufio.ErrBufferFull {
			continue
		}
		if e != nil {
			if e == io.EOF && len(buf) == 0 {
				return nil, false, io.EOF
			}
			if e != io.EOF {
				return nil, false, e
			}
		}
		return trim(buf), false, nil
	}
}

// ObserveCorpus widens this Stats' corpus bracket to cover another one's,
// so merging per-platform results keeps the earliest and latest transcript
// seen anywhere. Zero times are ignored.
func (s *Stats) ObserveCorpus(oldest, newest time.Time) {
	s.observeTranscript(oldest)
	s.observeTranscript(newest)
}
