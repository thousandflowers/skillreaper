package scan

import (
	"bufio"
	"bytes"
	"strconv"
	"strings"
)

// trimBOM drops the UTF-8 byte order mark some Windows editors save, which would
// otherwise hide the opening "---".
func trimBOM(b []byte) []byte {
	return bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
}

// newLineScanner reads lines of any length readCapped lets through. A smaller
// limit stops the scan at the first long line and loses what follows it.
func newLineScanner(b []byte) *bufio.Scanner {
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64*1024), maxFileSize+1)
	return sc
}

// parseFrontmatter extracts the name and description from a Markdown
// file with YAML frontmatter delimited by "---" lines. It returns the
// number of characters in the body (everything after the closing
// delimiter). Files without frontmatter yield empty name/description
// and the whole file as body.
func parseFrontmatter(b []byte) (name, description string, bodyChars int) {
	b = trimBOM(b)
	sc := newLineScanner(b)

	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return "", "", len(b)
	}

	headerLines := 1
	inHeader := true
	var header []string
	for inHeader && sc.Scan() {
		line := sc.Text()
		headerLines++
		if closesHeader(line) {
			inHeader = false
			break
		}
		header = append(header, line)
	}
	if inHeader {
		// Unterminated frontmatter: treat whole file as body.
		return "", "", len(b)
	}
	name, _ = topLevelValue(header, "name")
	description, _ = topLevelValue(header, "description")
	return name, description, len(b) - lengthOfLines(b, headerLines)
}

// closesHeader says whether a line ends the frontmatter: "---" at the start of
// the line. An indented one is text inside a value (a rule in Markdown), as
// YAML reads it and as reap mute takes it.
func closesHeader(line string) bool {
	return strings.TrimRight(line, " \t\r") == "---"
}

// lengthOfLines is how many bytes the first n lines of b take, with whatever
// ends each of them.
func lengthOfLines(b []byte, n int) int {
	at := 0
	for ; n > 0 && at < len(b); n-- {
		end := bytes.IndexByte(b[at:], '\n')
		if end < 0 {
			return len(b)
		}
		at += end + 1
	}
	return at
}

// frontmatterValue returns the single-line value of key in a Markdown file's
// YAML frontmatter, and whether the key was present.
func frontmatterValue(b []byte, key string) (string, bool) {
	sc := newLineScanner(trimBOM(b))
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return "", false
	}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "---" {
			return "", false
		}
		if v, ok := yamlValue(line, key); ok {
			return v, true
		}
	}
	return "", false
}

// toolSurface counts the tools a skill/agent is restricted to, reading the
// first present of keys (e.g. "allowed-tools", "tools"). A missing key, an
// empty value, or "*" means unrestricted → ToolSurfaceAll. Otherwise it
// counts the comma-separated entries.
func toolSurface(b []byte, keys ...string) int {
	for _, k := range keys {
		v, ok := frontmatterValue(b, k)
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if v == "" || v == "*" {
			return ToolSurfaceAll
		}
		n := 0
		for _, part := range strings.Split(v, ",") {
			if strings.TrimSpace(part) != "" {
				n++
			}
		}
		if n == 0 {
			return ToolSurfaceAll
		}
		return n
	}
	return ToolSurfaceAll
}

// yamlValue returns the value of a single-line "key: value" YAML pair,
// with surrounding quotes removed and, for a double-quoted scalar, its
// backslash escapes resolved.
//
// Only the double-quoted form carries escapes in YAML; a single-quoted scalar
// is literal, so a "\n" there is two characters and stays two characters.
// strconv.Unquote covers the escapes that turn up in real frontmatter (\n, \t,
// \", \\) and refuses anything it does not know — a Windows path being the
// realistic case — in which case the raw text is a better answer than an error.
func yamlValue(line, key string) (string, bool) {
	if !strings.HasPrefix(line, key+":") {
		return "", false
	}
	v := strings.TrimSpace(strings.TrimPrefix(line, key+":"))
	if v != "" && (v[0] == '"' || v[0] == '\'') {
		// A quote that closes on the line ends the value there: what follows
		// is a comment, and was weighed and shown with the description.
		if end := quoteEnd(v[1:], v[0]); end >= 0 {
			v = v[:end+2]
		}
	} else if at := commentAt(v); at >= 0 {
		v = strings.TrimSpace(v[:at])
	}
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		if unquoted, err := strconv.Unquote(v); err == nil {
			return unquoted, true
		}
	}
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'"), true // two in a row write one
	}
	v = strings.Trim(v, `"'`)
	return v, true
}

// topLevelValue reads an unindented key's value from frontmatter lines the way
// YAML does for the forms skill headers use: a one-line scalar, a plain scalar
// continued on indented lines, and a block scalar ("|" literal, ">" folded,
// with an optional chomping indicator). An indented key belongs to a nested
// map ("metadata:\n  description: ...") and is not the skill's own.
func topLevelValue(lines []string, key string) (string, bool) {
	for i, line := range lines {
		if strings.TrimLeft(line, " \t") != line {
			continue
		}
		line = strings.TrimRight(line, " \t\r")
		v, ok := yamlValue(line, key)
		if !ok {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, key+":"))
		cont := continuation(lines[i+1:])
		if style, chomp, ok := blockHeader(raw); ok {
			return blockScalar(cont, style, chomp), true
		}
		if raw != "" && (raw[0] == '"' || raw[0] == '\'') {
			// A quote left open runs on to the line that closes it, however
			// those lines are indented: all of it is the value, as mute takes it.
			if whole, ok := quotedOver(raw[1:], lines[i+1:], raw[0]); ok {
				return whole, true
			}
			return v, true
		}
		if raw == "" {
			return v, true
		}
		parts := []string{v}
		for _, c := range cont {
			if t := strings.TrimSpace(c); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, " "), true
	}
	return "", false
}

// continuation returns the lines after a key that still belong to its value:
// blank or indented ones, up to the next unindented line.
func continuation(lines []string) []string {
	for i, l := range lines {
		if strings.TrimSpace(l) != "" && strings.TrimLeft(l, " \t") == l {
			return lines[:i]
		}
	}
	return lines
}

// blockHeader recognises a block scalar indicator: "|" or ">", then an
// optional chomping sign and indentation digit, in either order.
func blockHeader(raw string) (style, chomp byte, ok bool) {
	if i := commentAt(raw); i >= 0 {
		raw = strings.TrimSpace(raw[:i])
	}
	if raw == "" || (raw[0] != '|' && raw[0] != '>') {
		return 0, 0, false
	}
	for _, c := range raw[1:] {
		switch {
		case c == '-' || c == '+':
			chomp = byte(c)
		case c < '0' || c > '9':
			return 0, 0, false
		}
	}
	return raw[0], chomp, true
}

// blockScalar renders a block scalar's lines: dedented, joined with newlines
// (literal) or folded into spaces with blank lines kept as breaks (folded),
// then chomped: clip keeps one final newline, "-" none, "+" all.
func blockScalar(lines []string, style, chomp byte) string {
	indent := -1
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			indent = len(l) - len(strings.TrimLeft(l, " \t"))
			break
		}
	}
	if indent < 0 {
		return ""
	}
	body := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			body = append(body, "")
		} else if len(l)-len(strings.TrimLeft(l, " \t")) >= indent { // its own spaces, never its text
			body = append(body, strings.TrimRight(l[indent:], "\r"))
		} else {
			body = append(body, strings.TrimSpace(l))
		}
	}
	trailing := 0
	for len(body) > 0 && body[len(body)-1] == "" {
		body = body[:len(body)-1]
		trailing++
	}

	var out strings.Builder
	if style == '|' {
		out.WriteString(strings.Join(body, "\n"))
	} else {
		breaks := 0
		for i, l := range body {
			if l == "" {
				breaks++
				continue
			}
			if i > 0 {
				if breaks > 0 {
					out.WriteString(strings.Repeat("\n", breaks))
				} else {
					out.WriteByte(' ')
				}
			}
			out.WriteString(l)
			breaks = 0
		}
	}
	switch chomp {
	case '-':
	case '+':
		out.WriteString(strings.Repeat("\n", trailing+1))
	default:
		out.WriteByte('\n')
	}
	return out.String()
}

// frontmatterList reads a top-level list from the frontmatter, in the ways
// YAML writes one: `key: a, b`, `key: [a, b]` (over several lines too), or one
// item to a line under the key, with blank lines and comments among them.
func frontmatterList(b []byte, key string) []string {
	sc := newLineScanner(trimBOM(b))
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return nil
	}
	var out []string
	add := func(s string) {
		for _, part := range strings.Split(strings.Trim(strings.TrimSpace(s), "[]"), ",") {
			if part = strings.Trim(strings.TrimSpace(part), `"'`); part != "" && part != "null" && part != "~" {
				out = append(out, part)
			}
		}
	}
	under, open := false, false // below the key reading its items; inside brackets not closed yet
	for sc.Scan() {
		line := sc.Text()
		if closesHeader(line) {
			return out
		}
		topLevel := line == strings.TrimLeft(line, " \t")
		if at := commentAt(line); at >= 0 {
			line = line[:at] // a comment, to the end of the line
		}
		text := strings.TrimSpace(line)
		item, isItem := strings.CutPrefix(text, "-")
		switch {
		case text == "" || strings.HasPrefix(text, "#"):
			// a blank line or a comment of its own: the list goes on
		case open:
			add(text)
			open = !strings.Contains(text, "]")
		case under && isItem:
			add(item)
		case topLevel:
			under = false
			if value, ok := strings.CutPrefix(text, key+":"); ok {
				value = strings.TrimSpace(value)
				under = value == ""
				open = strings.HasPrefix(value, "[") && !strings.Contains(value, "]")
				add(value)
			}
		default:
			under = false // an indented line that is no item: a mapping, not a list
		}
	}
	return nil // a header that never closes is no frontmatter
}

// skillAgent is the agent a skill's frontmatter names under `agent:`.
func skillAgent(b []byte) string {
	// The key at the top level: "agent:" inside a description is words.
	if named := frontmatterList(b, "agent"); len(named) > 0 {
		return named[0]
	}
	return ""
}

// SkillAgents names, by platform, the agents some skill runs in.
func SkillAgents(items []Item) map[string]map[string]bool {
	agents := map[string]map[string]bool{}
	for _, it := range items {
		if it.RunsIn == "" {
			continue
		}
		if agents[it.Platform] == nil {
			agents[it.Platform] = map[string]bool{}
		}
		agents[it.Platform][strings.ToLower(it.RunsIn)] = true
	}
	return agents
}

// AgentLoaded names, by platform, the skills some agent preloads.
func AgentLoaded(items []Item) map[string]map[string]bool {
	loaded := map[string]map[string]bool{}
	for _, it := range items {
		for _, name := range it.Preloads {
			if loaded[it.Platform] == nil {
				loaded[it.Platform] = map[string]bool{}
			}
			loaded[it.Platform][strings.ToLower(name)] = true
		}
	}
	return loaded
}

// commentAt is where a comment begins in a line: at the space or tab before a
// "#". -1 when there is none.
func commentAt(line string) int {
	for i := 1; i < len(line); i++ {
		if line[i] == '#' && (line[i-1] == ' ' || line[i-1] == '\t') {
			return i - 1
		}
	}
	return -1
}

// quotedOver reads a quoted value that does not close on its own line: first
// is what follows the opening quote there, rest the header's later lines. The
// lines are joined with a space, as YAML folds them. False when the quote
// closes on its line, or never closes: then the value is that one line.
func quotedOver(first string, rest []string, quote byte) (string, bool) {
	if quoteEnd(first, quote) >= 0 {
		return "", false
	}
	parts := []string{strings.TrimSpace(first)}
	for _, line := range rest {
		text := strings.TrimSpace(line)
		if end := quoteEnd(text, quote); end >= 0 {
			if last := strings.TrimSpace(text[:end]); last != "" {
				parts = append(parts, last)
			}
			return strings.Join(parts, " "), true
		}
		if text != "" {
			parts = append(parts, text)
		}
	}
	return "", false
}

// quoteEnd is where the quote that opened a value closes in text, or -1.
// Inside double quotes a backslash makes the next character itself; inside
// single quotes two in a row write one.
func quoteEnd(text string, quote byte) int {
	for i := 0; i < len(text); i++ {
		switch {
		case quote == '"' && text[i] == '\\':
			i++
		case text[i] == quote && quote == '\'' && i+1 < len(text) && text[i+1] == '\'':
			i++
		case text[i] == quote:
			return i
		}
	}
	return -1
}
