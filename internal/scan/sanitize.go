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

// secretWord is a name that says its value is a secret: one holding a telling
// word anywhere, or a short one (pass, pwd, sig) standing as a whole part of
// the name. Inside a longer word a short one means nothing: compass, design.
const secretWord = `(?:[\w-]*(?:key|token|secret|passw(?:or)?d|passphrase|auth|credential|bearer|cookie|session|signature)[\w-]*|(?:[\w-]*[-_])?(?:pass|pwd|pw|sig|jwt|dsn|creds?)(?:[-_][\w-]*)?)`

const (
	// inDouble is one character inside double quotes, an escaped one included.
	inDouble = `(?:[^"\\\n]|\\.)`
	// inSingle is one inside single quotes: '\'' is how a shell writes a quote there.
	inSingle = `(?:[^'\n]|'\\'')`
	// quotedValue is one argument in quotes, whichever way: "…", '…', $'…', or
	// \"…\" inside a string.
	// $'…' comes first: inside it a quote is written with a backslash, which
	// '…' cannot hold. Text glued after the closing quote is part of the word.
	quotedValue = `(?:\$'(?:[^'\\\n]|\\.)*'|\$?"` + inDouble + `*"|'` + inSingle + `*'|\\"[^\n]*?\\")[^\s"',;}\])]*`
	// bareValue runs to the next space, whatever it holds. One already masked
	// stays as it is, with the quote that closes it.
	bareValue = `(?:<redacted>|(?:\\.|\S)+)`
	// beforeName is what can stand right before a name: the start, a space, a
	// quote, the punctuation of a JSON object, and what joins commands and
	// assignments (--env=TOKEN=…, a;TOKEN=…, Server=db;Password=…).
	beforeName = `(^|[\s"'{,\[(=;|&>#\x60])`
	userFlag   = `(^|\s)((?:-u|--user)(?:\s+|=)?)`
)

// secretPatterns mask the value and keep the name, so a command stays readable.
// Each runs over what the ones before it left, so a value already masked is
// masked again and stays as it is. They read shapes, not secrets: a name that
// says nothing, or a secret passed as a bare argument, is shown as written.
// Masking too much is the mistake they are allowed.
var secretPatterns = []struct {
	re   *regexp.Regexp
	with string
}{
	// A URL's user part: the password, up to the last "@" before the host, and a
	// lone token standing where the user would. Then its query and fragment.
	{regexp.MustCompile(`(?i)(://[^/\s:@]*:)[^/\s]+@`), "${1}<redacted>@"},
	{regexp.MustCompile(`(://)[^/\s:@]+@`), "${1}<redacted>@"},
	{regexp.MustCompile(`(?i)([?&#]` + secretWord + `=)[^&\s]+`), "${1}<redacted>"},
	{regexp.MustCompile(`(?i)(\bcookie\s*[:=]\s*)[^"'\n]+`), "${1}<redacted>"},
	// curl's -b takes a cookie.
	{regexp.MustCompile(`(^|\s)(-b\s+)(?:` + quotedValue + `|[^\s=]+=\S+)`), "${1}${2}<redacted>"},
	// user:password after -u or --user, in quotes or not, glued to the flag or not.
	{regexp.MustCompile(userFlag + `"[^":\n]+:` + inDouble + `*"`), "${1}${2}<redacted>"},
	{regexp.MustCompile(userFlag + `'[^':\n]+:` + inSingle + `*'`), "${1}${2}<redacted>"},
	{regexp.MustCompile(`(^|\s)((?:-u|--user)(?:\s+|=)?[^\s:"']+:)\S+`), "${1}${2}<redacted>"},
	{regexp.MustCompile(`(?i)(\bbearer\s+)[^\s"']+`), "${1}<redacted>"},
	// A quoted argument that opens with the name, as a header does: all of it up
	// to the quote that closes it, spaces and escaped quotes included.
	{regexp.MustCompile(`(?i)("\s*-{0,2}` + secretWord + `\s*[=:]\s*)` + inDouble + `+`), "${1}<redacted>"},
	{regexp.MustCompile(`(?i)('\s*-{0,2}` + secretWord + `\s*[=:]\s*)` + inSingle + `+`), "${1}<redacted>"},
	// Authorization with no quote before it: scheme and credential are several
	// words, and nothing says where they end, so the rest of the line goes.
	{regexp.MustCompile(`(?i)((?:^|[^"'\w-])[\w-]*authorization\s*[:=]\s*)[^\n]+`), "${1}<redacted>"},
	// name, separator, value in quotes: the quotes and what they hold.
	{regexp.MustCompile(`(?i)` + beforeName + `(-{0,2}` + secretWord + `)(\\?["']?(?:\s*[=:]\s*|\s+))` + quotedValue), "${1}${2}${3}<redacted>"},
	// In a connection string a value runs to the next ";", spaces and all.
	{regexp.MustCompile(`(?i)(;\s*` + secretWord + `\s*=\s*)[^;"'\n]*;`), "${1}<redacted>;"},
	// name=value and name: value, then name value.
	{regexp.MustCompile(`(?i)` + beforeName + `(-{0,2}` + secretWord + `)(\\?["']?\s*[=:]\s*)` + bareValue), "${1}${2}${3}<redacted>"},
	{regexp.MustCompile(`(?i)` + beforeName + `(-{0,2}` + secretWord + `)(\s+)` + bareValue), "${1}${2}${3}<redacted>"},
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
