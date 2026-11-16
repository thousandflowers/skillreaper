package scan

import (
	"regexp"
	"strings"
	"unicode"
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

const (
	// inDouble is one character inside double quotes, an escaped one included.
	inDouble = `(?:[^"\\\n]|\\.)`
	// beforeName is what can stand right before a name: the start, a space, the
	// quote that opens it, or the punctuation of a JSON object.
	beforeName = `(^|[\s"'{,\[(])`
)

// secretPatterns mask the value and keep the name, so a command stays readable.
// Each runs over what the ones before it left, so a value already masked is
// masked again and stays as it is.
var secretPatterns = []struct {
	re   *regexp.Regexp
	with string
}{
	// A URL's user part: the password, up to the last "@" before the host, and a
	// lone token standing where the user would.
	{regexp.MustCompile(`(?i)(://[^/\s:@]+:)[^/\s]+@`), "${1}<redacted>@"},
	{regexp.MustCompile(`(://)[^/\s:@]+@`), "${1}<redacted>@"},
	{regexp.MustCompile(`(?i)([?&]` + secretWord + `=)[^&\s]+`), "${1}<redacted>"},
	{regexp.MustCompile(`(?i)(\bcookie\s*[:=]\s*)[^"'\n]+`), "${1}<redacted>"},
	// user:password after -u or --user, in quotes or not, glued to the flag or not.
	{regexp.MustCompile(`(^|\s)((?:-u|--user)(?:\s+|=)?)"[^":\n]+:` + inDouble + `*"`), "${1}${2}<redacted>"},
	{regexp.MustCompile(`(^|\s)((?:-u|--user)(?:\s+|=)?)'[^':\n]+:[^'\n]*'`), "${1}${2}<redacted>"},
	{regexp.MustCompile(`(^|\s)((?:-u|--user)(?:\s+|=)?[^\s:"']+:)[^\s"']+`), "${1}${2}<redacted>"},
	{regexp.MustCompile(`(?i)(\bbearer\s+)[^\s"']+`), "${1}<redacted>"},
	// A quoted argument that opens with the name, as a header does: all of it up
	// to the quote that closes it, spaces and escaped quotes included.
	{regexp.MustCompile(`(?i)("\s*-{0,2}` + secretWord + `\s*[=:]\s*)` + inDouble + `+`), "${1}<redacted>"},
	{regexp.MustCompile(`(?i)('\s*-{0,2}` + secretWord + `\s*[=:]\s*)[^'\n]+`), "${1}<redacted>"},
	// Authorization with no quote before it: scheme and credential are several
	// words, and nothing says where they end, so the rest of the line goes.
	{regexp.MustCompile(`(?i)((?:^|[^"'\w-])[\w-]*authorization\s*[:=]\s*)[^\n]+`), "${1}<redacted>"},
	// name, separator, value in quotes: the quotes and what they hold.
	{regexp.MustCompile(`(?i)` + beforeName + `(-{0,2}` + secretWord + `)(\\?["']?(?:\s*[=:]\s*|\s+))(?:"` + inDouble + `*"|'[^'\n]*'|\\"[^\n]*?\\")`), "${1}${2}${3}<redacted>"},
	// name=value and name: value. After "=" a value may begin with a dash.
	{regexp.MustCompile(`(?i)` + beforeName + `(-{0,2}` + secretWord + `)(\\?["']?\s*[=:]\s*)[^\s"'\\]+`), "${1}${2}${3}<redacted>"},
	// name value. A word that begins with a dash is the next flag, not a value.
	{regexp.MustCompile(`(?i)(^|[\s"'])(-{0,2}` + secretWord + `)(\s+)([^\s"'-][^\s"']*)`), "${1}${2}${3}<redacted>"},
}

// RedactSecrets masks credentials in a command line, URL or hook command. It
// runs where the text enters the inventory, so every surface inherits it.
func RedactSecrets(s string) string {
	for _, p := range secretPatterns {
		s = p.re.ReplaceAllString(s, p.with)
	}
	return s
}

// plainShellRunes are the marks a shell reads as part of a word, wherever
// they stand in it.
const plainShellRunes = "-_./:@+,%="

// ShellArg writes s as one word of a command line someone will paste: as it is
// when a shell would read it whole, in single quotes otherwise. A name with a
// space, a quote or a "$" must not turn into two arguments or into something
// the shell expands.
func ShellArg(s string) string {
	plain := s != ""
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune(plainShellRunes, r) {
			plain = false
		}
	}
	if plain {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
