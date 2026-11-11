package reinvest

import (
	"regexp"
	"strings"
	"unicode"
)

var reminderRE = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)

// clean drops injected reminders and puts the text on one line.
func clean(text string) string {
	return collapseSpace(reminderRE.ReplaceAllString(text, ""))
}

const ellipsis = "…"

// cut is clean text, shortened to n characters with an ellipsis when longer.
func cut(text string, n int) string {
	text = clean(text)
	if runeLen(text) <= n {
		return text
	}
	return firstRunes(text, n-1) + ellipsis
}

// canon turns "plugin:skill" and "/skill" into "skill".
func canon(name string) string {
	name = strings.TrimLeft(pyStrip(name), "/")
	if i := strings.LastIndex(name, ":"); i >= 0 {
		name = name[i+1:]
	}
	return pyLower(name)
}

func isFormatChar(r rune) bool { return unicode.Is(unicode.Cf, r) }

const quoteChars = "\"'"

// blockIndicators are the YAML values that announce the text on the lines below.
var blockIndicators = map[string]bool{">": true, "|": true, ">-": true, "|-": true, ">+": true, "|+": true, "": true}

func isKeyChar(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_' || c == '-'
}

// frontmatterKey splits "key: value" for a key at the start of the line.
func frontmatterKey(line string) (key, value string, ok bool) {
	end := 0
	for end < len(line) && isKeyChar(line[end]) {
		end++
	}
	if end == 0 || end >= len(line) || line[end] != ':' {
		return "", "", false
	}
	return line[:end], pyStrip(line[end+1:]), true
}

func startsWithSpace(line string) bool {
	for _, r := range line {
		return isPySpace(r)
	}
	return false
}

// frontmatter reads the YAML frontmatter of a SKILL.md, flat keys only
// (enough for name and description).
func frontmatter(text string) map[string]string {
	out := map[string]string{}
	lines := pySplitLines(text)
	if len(lines) == 0 || pyStrip(lines[0]) != "---" {
		return out
	}
	key, hasKey := "", false
	var buf []string
	flush := func() {
		if hasKey {
			out[key] = pyStrip(strings.Join(buf, " "))
		}
	}
	for _, line := range lines[1:] {
		if pyStrip(line) == "---" {
			break
		}
		if k, value, ok := frontmatterKey(line); ok {
			flush()
			key, hasKey, buf = k, true, nil
			if !blockIndicators[value] {
				buf = []string{strings.Trim(value, quoteChars)}
			}
		} else if hasKey && startsWithSpace(line) {
			buf = append(buf, strings.Trim(pyStrip(line), quoteChars))
		}
	}
	flush()
	return out
}
