package reinvest

import "strings"

const (
	frontmatterMark = "---"
	descriptionKey  = "description:"
	manualOnlyKey   = "disable-model-invocation"
	indentChars     = " \t"
)

// bytesToTokens is reap's weight: bytes / 3.7, rounded up.
func bytesToTokens(n int) int { return (n*10 + 36) / 37 }

// reapTokens is what reap will report for this skill once installed, computed the way reap does
// (internal/scan/skills.go, internal/scan/frontmatter.go, internal/cost/cost.go): bytes of the directory name
// plus the description, / 3.7 rounded up. The description is read whole, as Claude Code injects it: block
// scalars and continued lines too.
func reapTokens(slug, doc string) int {
	lines := pySplitLines(doc)
	if len(lines) == 0 || pyStrip(lines[0]) != frontmatterMark {
		return bytesToTokens(len(slug))
	}
	var header []string
	for _, line := range lines[1:] {
		if pyStrip(line) == frontmatterMark {
			break
		}
		header = append(header, line)
	}
	for _, line := range header {
		key, value, found := strings.Cut(line, ":")
		if found && key == manualOnlyKey && pyLower(pyStrip(value)) == "true" {
			return 0 // run only by hand: Claude Code never lists it, as reap counts it
		}
	}
	return bytesToTokens(len(slug + description(header)))
}

func isIndented(line string) bool { return line != strings.TrimLeft(line, indentChars) }

// description is the unindented `description:` value, as reap's topLevelValue reads it.
func description(header []string) string {
	for i, line := range header {
		if isIndented(line) || !strings.HasPrefix(line, descriptionKey) {
			continue
		}
		raw := pyStrip(strings.TrimRight(line, " \t\r")[len(descriptionKey):])
		var cont []string
		for _, next := range header[i+1:] {
			if pyStrip(next) != "" && !isIndented(next) {
				break
			}
			cont = append(cont, next)
		}
		indicator, _, _ := strings.Cut(raw, " #")
		indicator = pyStrip(indicator)
		if isBlockIndicator(indicator) {
			return block(cont, indicator[:1], chomping(indicator))
		}
		return inlineDescription(raw, cont)
	}
	return ""
}

func isBlockIndicator(indicator string) bool {
	if !strings.HasPrefix(indicator, "|") && !strings.HasPrefix(indicator, ">") {
		return false
	}
	return strings.Trim(indicator[1:], "+-0123456789") == ""
}

func chomping(indicator string) string {
	switch {
	case strings.Contains(indicator, "-"):
		return "-"
	case strings.Contains(indicator, "+"):
		return "+"
	}
	return ""
}

func inlineDescription(raw string, cont []string) string {
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		if v, err := parseJSON(raw); err == nil {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return strings.Trim(raw, quoteChars)
	}
	if raw == "" || raw[0] == '"' || raw[0] == '\'' {
		return strings.Trim(raw, quoteChars)
	}
	parts := []string{strings.Trim(raw, quoteChars)}
	for _, line := range cont {
		if stripped := pyStrip(line); stripped != "" {
			parts = append(parts, stripped)
		}
	}
	return strings.Join(parts, " ")
}

// block reads a YAML block scalar: literal ("|") keeps the lines, folded
// (">") joins them, and the chomping sign decides the line breaks at the end.
func block(lines []string, style, chomp string) string {
	first, found := "", false
	for _, line := range lines {
		if pyStrip(line) != "" {
			first, found = line, true
			break
		}
	}
	if !found {
		return ""
	}
	indent := runeLen(first) - runeLen(strings.TrimLeft(first, indentChars))
	body := make([]string, 0, len(lines))
	for _, line := range lines {
		switch {
		case pyStrip(line) == "":
			body = append(body, "")
		case runeLen(line) >= indent:
			body = append(body, strings.TrimRight(string([]rune(line)[indent:]), "\r"))
		default:
			body = append(body, pyStrip(line))
		}
	}
	trailing := 0
	for len(body) > 0 && body[len(body)-1] == "" {
		body = body[:len(body)-1]
		trailing++
	}
	out := strings.Join(body, "\n")
	if style != "|" {
		out = foldLines(body)
	}
	switch chomp {
	case "-":
		return out
	case "+":
		return out + strings.Repeat("\n", trailing+1)
	}
	return out + "\n"
}

// foldLines joins the lines of a folded scalar: a single break is a space,
// an empty line is a line break.
func foldLines(body []string) string {
	var out strings.Builder
	breaks := 0
	for i, line := range body {
		if line == "" {
			breaks++
			continue
		}
		if i > 0 {
			if breaks > 0 {
				out.WriteString(strings.Repeat("\n", breaks))
			} else {
				out.WriteString(" ")
			}
		}
		out.WriteString(line)
		breaks = 0
	}
	return out.String()
}
