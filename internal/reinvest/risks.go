package reinvest

import (
	"regexp"
	"sort"
	"strings"
)

// The rules that stand in front of every candidate before, and whatever, the
// verifier says. Each pattern is the prototype's, character for character,
// compiled for projected text (see pyre.go). Three of them used a lookaround,
// which RE2 does not have; each is rewritten below with the original beside it
// and the reason the two accept the same texts.

const (
	fetchWord = `(?:curl|wget|iwr|invoke-webrequest)\b`
	shellWord = `(?:ba|z|k|da|fi)?sh`
	backtick  = "`"
)

// secretPatterns are credentials as they look when pasted.
var secretPatterns = []string{
	`sk-ant-[\w-]{20,}`, `\bsk-[a-z0-9]{32,}`, `\bgh[pousr]_[a-z0-9]{30,}`, `\bgithub_pat_\w{20,}`,
	`\bakia[a-z0-9]{16}\b`, `\bxox[abprs]-[\w-]{10,}`, `-----begin [a-z ]*private key-----`,
}

type riskRule struct {
	expr string
	why  string
}

const (
	whyRunsDownload = "downloads and runs code"
	whyOverride     = "tries to override instructions"
	whyDataOut      = "may send data out"
	whySecretsOut   = "may send secrets out"
	whyRunsAnything = "has a tool that runs any command or script"
	whyControl      = "hides control sequences from the reader"
	whyHooks        = "installs hooks that run on their own"
)

var riskRules = []riskRule{
	// Python: FETCH [^\n]{0,200} \| [^\n]{0,80} (?<![\w.-]) SH \b
	// The lookbehind asks that the character before the shell's name is not a
	// word character, a dot or a hyphen. That character is the "|" itself when
	// nothing lies between (always allowed), else the last of the up-to-80:
	// so the run is either empty or up to 79 of anything plus one allowed one.
	{fetchWord + `[^\n]{0,200}\|(?:[^\n]{0,79}[^\n\w.-])?` + shellWord + `\b`, whyRunsDownload},
	{fetchWord + `[^\n]{0,200}\|\s*(?:sudo\s+)?(?:python3?|perl|ruby|node)\s*(?:-\s*)?$`, whyRunsDownload},
	{`\b` + shellWord + `\s+(?:-c\s+)?["']?(?:<\(|\$\()\s*` + fetchWord, whyRunsDownload},
	{`\beval\b[^\n]{0,20}\$\(\s*` + fetchWord, whyRunsDownload},
	{fetchWord + `[^\n]{0,200}(?:\s-o\s*|\s-O\s*|>\s*)\S+\.sh\b[\s\S]{0,300}(?:\b` + shellWord + `\s+\S*\.sh|chmod\s+\+x)`,
		whyRunsDownload},
	{`(?:ignore|disregard|forget|override)\s+(?:(?:all|any|of|the|your|my)\s+){0,3}` +
		`(?:previous|prior|above|earlier|system)\s+(?:instructions|rules|prompts?)`, whyOverride},
	{`ignora\s+(?:(?:tutte|le)\s+){0,2}istruzioni\s+(?:precedenti|sopra)`, whyOverride},
	{`["']risk["']\s*:[\s\S]{0,400}["']keep["']\s*:|["']keep["']\s*:[\s\S]{0,400}["']risk["']\s*:`,
		"carries a ready-made verdict for its own check"},
	{`base64\s+(?:-d|--decode)|b64decode|openssl\s+(?:enc\s+|base64\s+)[^\n]{0,40}-d\b`, "decodes hidden content"},
	{`(pastebin|ngrok|requestbin)`, whyDataOut},
	// the word alone is common and harmless, and Claude reads the rest: only sending to one counts here
	{`\b(post|send|upload|forward|curl|wget|push)\b[^\n]{0,80}webhook`, whyDataOut},
	{`(\.env|id_rsa|\.ssh/|keychain|api[_-]?key)[^\n]{0,80}(curl|post|upload|send|fetch)`, whySecretsOut},
	{`\b(?:curl|wget|scp|rsync|nc)\b[^\n]{0,120}(?:\.env\b|id_rsa|\.ssh/|\.aws/|\bkeychain\b)`, whySecretsOut},
	{`\brm\s+(?:-[a-z]+\s+)+["']?(?:/|~|\$home|\$\{home\})/?\*?["']?(?:\s|$|;)`, "destructive command"},
	{`xattr\s+-c`, "bypasses macOS quarantine"},
	{`(?:^|\s)!` + backtick, "runs a command as soon as it loads"},
	{strings.Join(secretPatterns, "|"), "holds what looks like a pasted secret"},
}

// An MCP server's tools run on every call without the agent reading them first: one that runs any command or
// script hands the model a shell under a friendlier name. Matched against tool names and capability phrases,
// with camelCase and hyphens first turned into underscores. A bare word counts only where it stands as a tool name.
var mcpRiskRules = []riskRule{
	{`\b(run|execute|exec|start|spawn|eval|evaluate)_(command|shell|script|applescript|jxa|code|terminal|process|` +
		`python|bash)s?\b`, whyRunsAnything},
	{`\b(shell_exec|exec_command|run_shell|eval_code|browser_evaluate|python_repl|send_keys|osascript)\b`, whyRunsAnything},
	// Python: (?m)(?:^[\s*|-]*|(?<!`)`)(?:bash|shell|exec|terminal|spawn|eval)(?:`(?!`)|\s*(?::|\||$))
	// (?<!`)` is a backtick with no backtick before it: at the front of the
	// pattern that is the start of the text, or one other character, then the
	// backtick. `(?!`) is a backtick with none after it: at the end of the
	// pattern that is the backtick, then one other character or the end.
	{`(?m)(?:^[\s*|-]*|(?:\A|[^` + backtick + `])` + backtick + `)(?:bash|shell|exec|terminal|spawn|eval)` +
		`(?:` + backtick + `(?:[^` + backtick + `]|\z)|\s*(?::|\||$))`, whyRunsAnything},
	{`\b(execute|run)s? (arbitrary|any|shell|terminal|system) (commands?|code|scripts?)`, "runs arbitrary commands"},
	{`\b(execute|run)s? (commands or )?applescript|applescript (execution|runner)`, "runs arbitrary AppleScript"},
}

type compiledRule struct {
	match pattern
	why   string
}

func compileRules(rules []riskRule, flags string) []compiledRule {
	out := make([]compiledRule, len(rules))
	for i, rule := range rules {
		out[i] = compiledRule{mustPattern(flags+rule.expr, false), rule.why}
	}
	return out
}

var (
	riskPatterns    = compileRules(riskRules, `(?m)`) // the prototype searched these with re.M
	mcpRiskPatterns = compileRules(mcpRiskRules, ``)
	secretPattern   = mustPattern(strings.Join(secretPatterns, "|"), true)
	// Control characters a pager or terminal acts on (escape sequences, the clipboard one, backspace): never in a real doc.
	controlChars = regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f\x7f-\x{9f}]`)
	continuedRE  = regexp.MustCompile(`\\\r?\n[ \t]*`)
	camelCaseRE  = regexp.MustCompile(`([a-z0-9])([A-Z])`)
)

const secretRemoved = "[secret removed]"

// printable is third-party text made safe to store and print: no control characters.
func printable(text string) string { return controlChars.ReplaceAllString(text, "") }

func oneLine(text string) string { return clean(printable(text)) }

// normalized is the text as a reader or shell takes it: entities decoded,
// lookalikes folded, invisibles out, continued lines joined.
func normalized(doc string) string {
	text := nfkc(htmlUnescape(doc))
	text = strings.Map(func(r rune) rune {
		if isFormatChar(r) {
			return -1
		}
		return r
	}, text)
	return continuedRE.ReplaceAllString(text, " ")
}

// toolForm writes camelCase and hyphenated names with underscores, lower case.
func toolForm(text string) string {
	runes := []rune(camelCaseRE.ReplaceAllString(text, "${1}_${2}"))
	for i := 1; i+1 < len(runes); i++ {
		if runes[i] == '-' && isPyWord(runes[i-1]) && isPyWord(runes[i+1]) {
			runes[i] = '_'
		}
	}
	return pyLower(string(runes))
}

// risks names what the rules find in a document, sorted. With mcp the rules
// for a server's tools apply too.
func risks(doc string, mcp bool) []string {
	text := normalized(doc)
	low := project(pyLower(text), false)
	found := map[string]bool{}
	for _, rule := range riskPatterns {
		if rule.match.matchesProjected(low) {
			found[rule.why] = true
		}
	}
	if mcp {
		tools := project(toolForm(text), false)
		for _, rule := range mcpRiskPatterns {
			if rule.match.matchesProjected(tools) || rule.match.matchesProjected(low) {
				found[rule.why] = true
			}
		}
	}
	if controlChars.MatchString(doc) {
		found[whyControl] = true
	}
	if _, has := frontmatter(doc)["hooks"]; has {
		found[whyHooks] = true
	}
	out := make([]string, 0, len(found))
	for why := range found {
		out = append(out, why)
	}
	sort.Strings(out)
	return out
}

// redact takes pasted credentials out of session text, before anything is written from it.
func redact(text string) string { return secretPattern.replaceAll(text, secretRemoved) }
