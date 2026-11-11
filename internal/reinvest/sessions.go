package reinvest

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Reads the Claude Code sessions a person actually wrote, with per-session stats.

const (
	liveWindow      = 10 * time.Minute // a transcript written this recently is a session still open (e.g. the one running reinvest)
	transcriptExt   = ".jsonl"
	subagentsDir    = "subagents"
	interruptedMark = "[Request interrupted"
	minPromptChars  = 12
	unknownSkill    = "?"
)

// noisePrefixes start what Claude Code writes into a user turn by itself.
var noisePrefixes = []string{"<task-notification", "<bash-input", "<bash-stdout", "<bash-stderr", "<local-command",
	"Caveat:", "<command-message", "<user-prompt-submit-hook", interruptedMark}

var commandRE = regexp.MustCompile(`<command-name>[` + pySpaceClass + `]*/?([^<` + pySpaceClass + `]+)[` + pySpaceClass + `]*</command-name>`)

// usageKeys are the token counts of an assistant turn that are new work.
var usageKeys = []string{"input_tokens", "output_tokens", "cache_creation_input_tokens"}

// skillInputKeys are where a Skill tool call names the skill, by Claude Code version.
var skillInputKeys = []string{"skill", "command", "name"}

// blocks are the content blocks of an entry: a bare string is one text block.
func blocks(entry *obj) []*obj {
	switch content := entry.object("message").val("content").(type) {
	case string:
		return []*obj{objOf("type", "text", "text", content)}
	case []any:
		var out []*obj
		for _, item := range content {
			if b, ok := item.(*obj); ok {
				out = append(out, b)
			}
		}
		return out
	}
	return nil
}

// userText is the text the person typed. ok is false for tool results, meta
// and sidechain entries.
func userText(entry *obj) (string, bool) {
	if entry.val("type") != "user" || truthy(entry.val("isMeta")) || truthy(entry.val("isSidechain")) {
		return "", false
	}
	var parts []string
	for _, b := range blocks(entry) {
		switch b.val("type") {
		case "tool_result":
			return "", false
		case "text":
			text, _ := b.str("text")
			parts = append(parts, text)
		}
	}
	text := pyStrip(strings.Join(parts, "\n"))
	return text, text != ""
}

// isAutomated reports a session no person wrote: launched with `claude -p`
// or the SDK (entrypoint "sdk-*"), or holding no user turn at all.
func isAutomated(entries []*obj) bool {
	for _, e := range entries {
		if e.val("type") == "user" && !truthy(e.val("isMeta")) {
			entrypoint, has := e.get("entrypoint")
			return has && strings.HasPrefix(pyStr(entrypoint), "sdk")
		}
	}
	return true
}

// parseTimestamp reads an RFC 3339 time with its zone, "Z" included.
func parseTimestamp(v any) (time.Time, bool) {
	text, ok := v.(string)
	if !ok {
		return time.Time{}, false
	}
	text = strings.ReplaceAll(text, "Z", "+00:00")
	for _, layout := range []string{"2006-01-02T15:04:05.999999999Z07:00", "2006-01-02 15:04:05.999999999Z07:00"} {
		if t, err := time.Parse(layout, text); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// lastActivity is when the session was last used: its newest timestamped entry.
func lastActivity(entries []*obj) (time.Time, bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		if t, ok := parseTimestamp(entries[i].val("timestamp")); ok {
			return t, true
		}
	}
	return time.Time{}, false
}

type transcript struct {
	path    string
	entries []*obj
}

// lessByParts orders paths the way Python orders Path objects: folder by folder.
func lessByParts(a, b string) bool {
	pa, pb := strings.Split(filepath.ToSlash(a), "/"), strings.Split(filepath.ToSlash(b), "/")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return len(pa) < len(pb)
}

// transcriptPaths lists every *.jsonl under the projects folder, outside any
// "subagents" folder (subagent transcripts hold no user prompts).
func (a *app) transcriptPaths() []string {
	var paths []string
	root := a.projectsDir + string(filepath.Separator) // a linked projects folder is followed, as rglob does
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			a.logf("transcripts: %v", err)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), transcriptExt) && !hasPart(path, subagentsDir) {
			paths = append(paths, filepath.Clean(path))
		}
		return nil
	})
	if err != nil {
		a.logf("transcripts: %v", err)
	}
	sort.Slice(paths, func(i, j int) bool { return lessByParts(paths[i], paths[j]) })
	return paths
}

func hasPart(path, part string) bool {
	return containsString(strings.Split(filepath.ToSlash(path), "/"), part)
}

// splitTextLines splits as a file read in text mode does: on \n, \r\n and \r.
func splitTextLines(text string) []string {
	return strings.Split(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\n")
}

func readEntries(path string) ([]*obj, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entries []*obj
	for _, line := range splitTextLines(decodeUTF8Replace(raw)) {
		v, err := parseJSON(line)
		if err != nil {
			continue
		}
		if entry, ok := v.(*obj); ok {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

// readTranscripts yields the transcripts of sessions a person wrote, last
// used inside the window and no longer open.
func (a *app) readTranscripts(days int, each func(transcript)) {
	now := a.now()
	oldest, newest := now.Add(-time.Duration(days)*day), now.Add(-liveWindow)
	for _, path := range a.transcriptPaths() {
		// mtime only rules out old files: Claude Code later appends untimestamped metadata lines
		// ("atis-latch", "pr-link") to old transcripts in batches, so mtime says nothing about activity.
		info, err := os.Stat(path)
		if err != nil {
			a.logf("transcript %s: %v", path, err)
			continue
		}
		if info.ModTime().Before(oldest) {
			continue
		}
		entries, err := readEntries(path)
		if err != nil {
			a.logf("transcript %s: %v", path, err)
			continue
		}
		last, ok := lastActivity(entries)
		if ok && !last.Before(oldest) && !last.After(newest) && !isAutomated(entries) {
			each(transcript{path, entries})
		}
	}
}

func hasNoisePrefix(text string) bool {
	text = strings.TrimLeftFunc(text, isPySpace)
	for _, prefix := range noisePrefixes {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

// readSessions gives one session per human transcript: prompts, tokens,
// friction signals and the skills it invoked.
func (a *app) readSessions(days int, skillNames map[string]bool) []session {
	var out []session
	a.readTranscripts(days, func(t transcript) {
		s := session{ID: strings.TrimSuffix(filepath.Base(t.path), transcriptExt), Skills: map[string]bool{}}
		for _, e := range t.entries {
			s = addEntry(s, e, skillNames)
		}
		if len(s.Prompts) > 0 {
			out = append(out, s)
		}
	})
	return out
}

func addEntry(s session, e *obj, skillNames map[string]bool) session {
	if s.TS == "" {
		s.TS, _ = e.str("timestamp")
	}
	raw, typed := userText(e)
	switch {
	case typed:
		return addPrompt(s, raw, skillNames)
	case e.val("type") == "user":
		for _, b := range blocks(e) {
			if b.val("type") == "tool_result" && truthy(b.val("is_error")) {
				s.Errors++
			}
		}
	case e.val("type") == "assistant":
		usage := e.object("message").object("usage")
		for _, key := range usageKeys {
			n, _ := pyInt(usage.val(key))
			s.Tokens += n
		}
		for _, b := range blocks(e) {
			if b.val("type") != "tool_use" {
				continue
			}
			s.Tools++
			if b.val("name") == "Skill" {
				s.Skills[canon(invokedSkill(b.object("input")))] = true
			}
		}
	}
	return s
}

func addPrompt(s session, raw string, skillNames map[string]bool) session {
	if strings.HasPrefix(raw, interruptedMark) {
		s.Interrupts++
		return s
	}
	if m := commandRE.FindStringSubmatch(raw); m != nil {
		if name := canon(m[1]); skillNames[name] {
			s.Skills[name] = true
		}
		return s
	}
	if text := clean(raw); !hasNoisePrefix(raw) && runeLen(text) >= minPromptChars {
		s.Prompts = append(s.Prompts, text)
	}
	return s
}

func invokedSkill(input *obj) string {
	for _, key := range skillInputKeys {
		if name, ok := input.str(key); ok && name != "" {
			return name
		}
	}
	return unknownSkill
}

// week names the ISO week a timestamp falls in, or "?".
func week(ts string) string {
	text := strings.ReplaceAll(ts, "Z", "+00:00")
	for _, layout := range []string{"2006-01-02T15:04:05.999999999Z07:00", "2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05.999999999", "2006-01-02"} {
		if t, err := time.Parse(layout, text); err == nil {
			year, w := t.ISOWeek()
			return fmt.Sprintf("%d-%02d", year, w)
		}
	}
	return "?"
}
