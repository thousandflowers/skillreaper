package scan

import (
	"regexp"
	"strings"
)

// SafeText returns s with everything a terminal or a pasted report would act on
// replaced: C0 and C1 controls, DEL, and bidi overrides become "?". Names come
// from directory names, config keys and transcripts, so none of it is trusted.
// A name with none of these comes back unchanged.
func SafeText(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r < 0x20, r >= 0x7f && r <= 0x9f,
			r >= 0x200e && r <= 0x200f, r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
			return '?'
		}
		return r
	}, s)
}

const secretWord = `[\w-]*(?:key|token|secret|passw(?:or)?d|auth|credential|bearer)[\w-]*`

// secretPatterns mask the value and keep the name, so a command stays readable.
var secretPatterns = []struct {
	re   *regexp.Regexp
	with string
}{
	{regexp.MustCompile(`(?i)(://[^/\s:@]+:)[^/\s@]+@`), "${1}<redacted>@"},
	{regexp.MustCompile(`(?i)([?&]` + secretWord + `=)[^&\s]+`), "${1}<redacted>"},
	{regexp.MustCompile(`(?i)(\bbearer\s+)\S+`), "${1}<redacted>"},
	{regexp.MustCompile(`(?i)(^|[\s"'])(-{0,2}` + secretWord + `)(=|:\s*|\s+)([^\s"'-]\S*)`), "${1}${2}${3}<redacted>"},
}

// RedactSecrets masks credentials in a command line, URL or hook command. It
// runs where the text enters the inventory, so every surface inherits it.
func RedactSecrets(s string) string {
	for _, p := range secretPatterns {
		s = p.re.ReplaceAllString(s, p.with)
	}
	return s
}
