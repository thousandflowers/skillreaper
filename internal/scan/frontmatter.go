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

	headerLen := len(sc.Text()) + 1
	inHeader := true
	var header []string
	for inHeader && sc.Scan() {
		line := sc.Text()
		headerLen += len(line) + 1
		if strings.TrimSpace(line) == "---" {
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
	bodyChars = len(b) - headerLen
	if bodyChars < 0 {
		bodyChars = 0
	}
	return name, description, bodyChars
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
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		if unquoted, err := strconv.Unquote(v); err == nil {
			return unquoted, true
		}
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
		if raw == "" || raw[0] == '"' || raw[0] == '\'' {
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
	if i := strings.Index(raw, " #"); i >= 0 {
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
		} else if len(l) >= indent {
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
