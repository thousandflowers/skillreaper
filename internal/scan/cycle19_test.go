package scan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cycle19Write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func cycle19JSON(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// What follows a script's path in a plugin's hook command is not part of its
// name: a ";" or "&" glued to it left the script unread and its skill REAP.
func TestPluginHookPathEndsAtShellPunctuation(t *testing.T) {
	for _, command := range []string{
		"sh ${CLAUDE_PLUGIN_ROOT}/start.sh; echo done",
		"echo \"$(cat ${CLAUDE_PLUGIN_ROOT}/start.sh)\"",
		"sh ${CLAUDE_PLUGIN_ROOT}/start.sh&",
		"sh ${CLAUDE_PLUGIN_ROOT}/start.sh|tee log",
	} {
		plugin := t.TempDir()
		cycle19Write(t, filepath.Join(plugin, "start.sh"), "cat skills/ps1/SKILL.md\n")
		hooks := cycle19JSON(t, command)
		cycle19Write(t, filepath.Join(plugin, "hooks", "hooks.json"), `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":`+hooks+`}]}]}}`)

		if !hookLoadedSkills(plugin)["ps1"] {
			t.Errorf("%s: the script was not read", command)
		}
	}
}

// A quote between the variable and the rest of a path changes nothing in the
// shell, and nothing here.
func TestUserHookPathWithAQuotedVariable(t *testing.T) {
	for _, command := range []string{
		`bash "$HOME"/.claude/hooks/start.sh`,
		`bash "${CLAUDE_CONFIG_DIR}"/hooks/start.sh`,
		`bash '$HOME'/.claude/hooks/start.sh`,
	} {
		home := t.TempDir()
		t.Setenv("HOME", home)
		dir := filepath.Join(home, ".claude")
		cycle19Write(t, filepath.Join(dir, "hooks", "start.sh"), "cat ~/.claude/skills/hooked/SKILL.md\n")
		quoted := cycle19JSON(t, command)
		cycle19Write(t, filepath.Join(dir, "settings.json"), `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":`+quoted+`}]}]}}`)

		if !userHookLoadedSkills(dir)["hooked"] {
			t.Errorf("%s: the script was not read", command)
		}
	}
}

// A script beside the hook's that is a link to a file (a dotfiles folder) is
// read like the others.
func TestUserHookReadsALinkedScriptBesideIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude")
	cycle19Write(t, filepath.Join(dir, "hooks", "wrap.sh"), "sh ./link.sh\n")
	cycle19Write(t, filepath.Join(home, "dotfiles", "real.sh"), "cat ~/.claude/skills/h-sym/SKILL.md\n")
	if err := os.Symlink(filepath.Join(home, "dotfiles", "real.sh"), filepath.Join(dir, "hooks", "link.sh")); err != nil {
		t.Skip(err)
	}
	if err := os.Symlink(filepath.Join(home, "dotfiles"), filepath.Join(dir, "hooks", "a-folder")); err != nil {
		t.Skip(err)
	}
	cycle19Write(t, filepath.Join(dir, "settings.json"), `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"bash ~/.claude/hooks/wrap.sh"}]}]}}`)

	if !userHookLoadedSkills(dir)["h-sym"] {
		t.Error("the linked script was not read")
	}
}

// A comment after a tab is a comment, as after a space.
func TestFrontmatterCommentAfterATab(t *testing.T) {
	if got := skillAgent([]byte("---\nagent: lonely\t# runs in lonely\n---\n")); got != "lonely" {
		t.Errorf("agent = %q", got)
	}
	if got := frontmatterList([]byte("---\nskills:\n  - one\t# first\n  - two\n---\n"), "skills"); len(got) != 2 || got[0] != "one" {
		t.Errorf("skills = %q", got)
	}
}

// A path in quotes is one word, whatever it holds: a space, a bracket, an "&".
// Cut at those, the script was not found and the skill it loads was REAP.
func TestHookPathInQuotesIsOneWord(t *testing.T) {
	for _, folder := range []string{"my hooks", "my(dir)", "a&b"} {
		home := t.TempDir()
		t.Setenv("HOME", home)
		dir := filepath.Join(home, ".claude")
		cycle19Write(t, filepath.Join(dir, folder, "start.sh"), "cat ~/.claude/skills/plain/SKILL.md\n")
		cycle19Write(t, filepath.Join(dir, "settings.json"), `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":`+cycle19JSON(t, `bash "$HOME/.claude/`+folder+`/start.sh"`)+`}]}]}}`)
		if !userHookLoadedSkills(dir)["plain"] {
			t.Errorf("the user's hook in %q: the script was not read", folder)
		}

		plugin := t.TempDir()
		cycle19Write(t, filepath.Join(plugin, folder, "a.sh"), "cat skills/zed/SKILL.md\n")
		cycle19Write(t, filepath.Join(plugin, "hooks", "hooks.json"), `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":`+cycle19JSON(t, `bash "${CLAUDE_PLUGIN_ROOT}/`+folder+`/a.sh"`)+`}]}]}}`)
		if !hookLoadedSkills(plugin)["zed"] {
			t.Errorf("the plugin's hook in %q: the script was not read", folder)
		}
	}
	if got := shellWords(`a "b c"'d' e  "g`); len(got) != 4 || got[1] != "b cd" || got[3] != "g" {
		t.Errorf("words: %q", got)
	}
}

// A quoted description that runs over several lines is read whole, as mute
// strips it whole: read as its first line alone, a heavy skill weighed a few
// tokens, never came out MUTE, and the saving claimed for it was a hundredth.
func TestAQuotedValueOverSeveralLinesIsReadWhole(t *testing.T) {
	for _, c := range []struct{ header, want string }{
		{"name: x\ndescription: \"first line\n  second line\n  third \\\"quoted\\\" end\"\nlicense: MIT", `first line second line third \"quoted\" end`},
		{"description: 'first\nsecond, it''s here'\nlicense: MIT", "first second, it''s here"},
		{"description: \"closed here\"\nlicense: \"MIT\"", "closed here"},
		{"description: \"never closed\nlicense: MIT", "never closed"},
	} {
		got, ok := topLevelValue(strings.Split(c.header, "\n"), "description")
		if !ok || got != c.want {
			t.Errorf("%q: description = %q, want %q", c.header, got, c.want)
		}
	}
}

// A space written with a backslash before it is part of the word, as in a
// shell: `my\ hooks/start.sh` is one path.
func TestShellWordsTakeAnEscapedSpace(t *testing.T) {
	if got := shellWords(`bash ~/.claude/my\ hooks/start.sh "a\"b c" 'd\e'`); len(got) != 4 || got[1] != "~/.claude/my hooks/start.sh" || got[2] != `a"b c` || got[3] != `d\e` {
		t.Errorf("words: %q", got)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude")
	cycle19Write(t, filepath.Join(dir, "my hooks", "start.sh"), "cat ~/.claude/skills/alpha/SKILL.md\n")
	cycle19Write(t, filepath.Join(dir, "settings.json"), `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":`+cycle19JSON(t, `bash ~/.claude/my\ hooks/start.sh`)+`}]}]}}`)
	if !userHookLoadedSkills(dir)["alpha"] {
		t.Error("the script was not read")
	}
}

// A value is read as YAML reads it: without the comment after it, and with
// two single quotes in a row as one. The comment was weighed with the
// description, and shown with it.
func TestAValueIsReadWithoutItsCommentAndWithItsQuotesResolved(t *testing.T) {
	for line, want := range map[string]string{
		`description: "foo" # comment`:         "foo",
		`description: 'foo' # c`:               "foo",
		`description: Use for foo # a comment`: "Use for foo",
		`description: 'don''t'`:                "don't",
		`description: C#tools and a#b`:         "C#tools and a#b",
		`description: "a # b"`:                 "a # b",
		`description: "say \"hi\""`:            `say "hi"`,
	} {
		if got, ok := yamlValue(line, "description"); !ok || got != want {
			t.Errorf("%s: %q, want %q", line, got, want)
		}
	}
}

// Only a "---" at the start of a line closes the header: an indented one
// inside a description is text (a rule in Markdown), and mute strips the whole
// description. Ended there, the header gave a fraction of the weight.
func TestAnIndentedRuleInADescriptionDoesNotCloseTheHeader(t *testing.T) {
	doc := "---\nname: hr\ndescription: |\n  Use when the user wants the hr thing.\n  ---\n  Not for the other thing.\n---\nbody of hr\n"
	name, description, body := parseFrontmatter([]byte(doc))
	if name != "hr" || !strings.Contains(description, "Not for the other thing.") || body != len("body of hr\n") {
		t.Errorf("name %q, description %q, body %d", name, description, body)
	}
	if got := frontmatterList([]byte("---\nskills:\n  - a\n---\n- b\n"), "skills"); len(got) != 1 || got[0] != "a" {
		t.Errorf("a list ends where the header does: %q", got)
	}
}

// The body is what follows the header, byte for byte, whatever ends the lines.
func TestBodyOfAFileWithCRLFIsCountedExactly(t *testing.T) {
	doc := "---\r\nname: x\r\ndescription: d\r\nlicense: MIT\r\n---\r\nbody\r\n"
	if _, _, body := parseFrontmatter([]byte(doc)); body != len("body\r\n") {
		t.Errorf("body = %d, want %d", body, len("body\r\n"))
	}
}

// Every reader of a header ends it at the same line: one key after an indented
// "---" was seen by the weight and missed by the reader of single keys, so a
// manual-only skill was weighed as if Claude loaded it.
func TestEveryHeaderReaderEndsItAtTheSameLine(t *testing.T) {
	doc := []byte("---\nname: x\ndescription: |\n  first\n  ---\n  second\ndisable-model-invocation: true\nallowed-tools: Read\n---\nbody\n")
	if v, ok := frontmatterValue(doc, "disable-model-invocation"); !ok || v != "true" {
		t.Errorf("disable-model-invocation = %q, %v", v, ok)
	}
	if v, ok := frontmatterValue(doc, "allowed-tools"); !ok || v != "Read" {
		t.Errorf("allowed-tools = %q, %v", v, ok)
	}
}

// A value that is only a comment is no value.
func TestAValueThatIsOnlyACommentIsEmpty(t *testing.T) {
	for _, line := range []string{"allowed-tools: # none yet", "allowed-tools: #", "allowed-tools:   #x"} {
		if got, ok := yamlValue(line, "allowed-tools"); !ok || got != "" {
			t.Errorf("%s: %q", line, got)
		}
	}
	if got, _ := topLevelValue([]string{"description: # nothing"}, "description"); got != "" {
		t.Errorf("description = %q", got)
	}
}

// A backslash is an escape before a space, a quote or another backslash, and a
// character of the path anywhere else: a Windows path keeps its separators.
func TestShellWordsKeepAWindowsPath(t *testing.T) {
	got := shellWords(`node "C:\Users\me\.claude\hooks\x.js" --a my\ dir\\x`)
	if len(got) != 4 || got[1] != `C:\Users\me\.claude\hooks\x.js` || got[3] != `my dir\x` {
		t.Errorf("words: %q", got)
	}
}

// A value that begins on the line after its key is that text: read as empty,
// a 600-character description weighed a few tokens, and mute still took it all.
func TestAValueOnTheLinesAfterItsKeyIsRead(t *testing.T) {
	for header, want := range map[string]string{
		"name: n\ndescription:\n  first line\n  second line\nlicense: MIT": "first line second line",
		"description:\n  # a comment\n  real text":                         "real text",
		"description: # c\n  real text":                                    "real text",
		"description:\nlicense: MIT":                                       "",
	} {
		if got, _ := topLevelValue(strings.Split(header, "\n"), "description"); got != want {
			t.Errorf("%q: description = %q, want %q", header, got, want)
		}
	}
}

// A list of tools written one per line is that many tools, not "all".
func TestToolsWrittenAsAListAreCounted(t *testing.T) {
	for doc, want := range map[string]int{
		"---\nallowed-tools:\n  - Read\n  - Bash\n---\n": 2,
		"---\nallowed-tools: # c\n  - Read\n---\n":       1,
		"---\nallowed-tools: [Read, Bash, Grep]\n---\n":  3,
		"---\nallowed-tools: Read, Bash\n---\n":          2,
		"---\nname: x\n---\n":                            ToolSurfaceAll,
	} {
		if got := toolSurface([]byte(doc), "allowed-tools"); got != want {
			t.Errorf("%q: %d tools, want %d", doc, got, want)
		}
	}
}
