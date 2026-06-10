package scan

import (
	"bufio"
	"bytes"
	"strings"
)

// parseFrontmatter extracts the name and description from a Markdown
// file with YAML frontmatter delimited by "---" lines. It returns the
// number of characters in the body (everything after the closing
// delimiter). Files without frontmatter yield empty name/description
// and the whole file as body.
func parseFrontmatter(b []byte) (name, description string, bodyChars int) {
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return "", "", len(b)
	}

	headerLen := len(sc.Text()) + 1
	inHeader := true
	for inHeader && sc.Scan() {
		line := sc.Text()
		headerLen += len(line) + 1
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			inHeader = false
			break
		}
		if v, ok := yamlValue(trimmed, "name"); ok {
			name = v
		}
		if v, ok := yamlValue(trimmed, "description"); ok {
			description = v
		}
	}
	if inHeader {
		// Unterminated frontmatter: treat whole file as body.
		return "", "", len(b)
	}
	bodyChars = len(b) - headerLen
	if bodyChars < 0 {
		bodyChars = 0
	}
	return name, description, bodyChars
}

// yamlValue returns the value of a single-line "key: value" YAML pair,
// with surrounding quotes removed.
func yamlValue(line, key string) (string, bool) {
	if !strings.HasPrefix(line, key+":") {
		return "", false
	}
	v := strings.TrimSpace(strings.TrimPrefix(line, key+":"))
	v = strings.Trim(v, `"'`)
	return v, true
}
