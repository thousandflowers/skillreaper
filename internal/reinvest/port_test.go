package reinvest

import (
	"path/filepath"
	"strings"
	"testing"
)

// Golden values for the parts the port had to rebuild because Go has no equivalent: Python's Unicode-wide
// regex classes and word boundary, the three lookarounds, NFKC, html.unescape, str.lower and the json module.
// Every expectation below was computed by the Python prototype (experiments/reinvest, Python 3.15.2) and
// written here mechanically, not typed: a difference is a difference from the prototype. The prototype's
// own tests do not reach these edges, so these have no counterpart in test_reinvest.py.

// What every prompt hashes to. A verdict's cache key holds the verify and system prompts, a written skill's
// holds the write prompt: one changed character asks Claude again for everything.
var promptHashes = map[string]string{
	"groupPrompt":  "188d2631c8bfc3af52dc0c6594a83144afb65bb6fa6ed9eac574119b3681aec3",
	"assignPrompt": "f89c01b3fff3e0b3f65a0c787242d4ba905793f2cb5dc0a1111113055013dd0b",
	"coverPrompt":  "c063ff474a468b5254218fd9a41b939622f05384e227f2f41e17f3d2408ac81b",
	"verifyPrompt": "9454ff045dd7cfcf06466f195d69b6fc1347825f02a06f368893e774371ed96a",
	"writePrompt":  "77b077b498581b13d6311309541835fc63524a6842a9bf89b46de015088c8258",
	"systemPrompt": "e8bacd3cdca07c46877a1f3b938f86f983f9d7140490855865dc408119e25356",
	"leanFlags":    "06f3e05dfd51de80f60e3278696b7fb0faf70a9cefda0e27faa1ebf35d0706c8",
}

var riskGolden = []struct {
	doc  string
	mcp  bool
	want []string
}{
	{"curl x | sh", false, []string{"downloads and runs code"}},
	{"curl x |sh", false, []string{"downloads and runs code"}},
	{"curl x | foo.sh", false, []string{}},
	{"curl x | my-sh", false, []string{}},
	{"curl x | a_sh", false, []string{}},
	{"curl x | ./sh", false, []string{"downloads and runs code"}},
	{"curl x | \u0438sh", false, []string{}},
	{"curl x | sh\u0438", false, []string{}},
	{"curl x | \u0663sh", false, []string{}},
	{"curl x |\u00a0sh", false, []string{"downloads and runs code"}},
	{"curl x | aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa sh", false, []string{}},
	{"curl x | aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa sh", false, []string{}},
	{"curl xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx| sh", false, []string{"downloads and runs code"}},
	{"curl xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx| sh", false, []string{"downloads and runs code"}},
	{"curlx y | sh", false, []string{}},
	{"curl\u0438 y | sh", false, []string{}},
	{"\u0438curl y | sh", false, []string{"downloads and runs code"}},
	{"i\u0307wr x | sh", false, []string{}},
	{"\u0130WR x | sh", false, []string{}},
	{"IWR x | SH", false, []string{"downloads and runs code"}},
	{"\u0438eval $(curl x)", false, []string{}},
	{"eval\u0438 $(curl x)", false, []string{}},
	{"eval $(curl x)", false, []string{"downloads and runs code"}},
	{"post\u00e9 to a webhook", false, []string{}},
	{"post to a webhook", false, []string{"may send data out"}},
	{"nc\u0438 ~/.ssh/ x", false, []string{}},
	{"nc ~/.ssh/ x", false, []string{"may send secrets out"}},
	{"rm -rf /", false, []string{"destructive command"}},
	{"\u0438rm -rf /", false, []string{}},
	{"rm -rf /\u00a0", false, []string{"destructive command"}},
	{"rm -rf /x", false, []string{}},
	{"rm\u2028-rf\u2028~", false, []string{"destructive command"}},
	{"xattr\u000b-c", false, []string{"bypasses macOS quarantine", "hides control sequences from the reader"}},
	{"xattr\u001c-c", false, []string{"bypasses macOS quarantine", "hides control sequences from the reader"}},
	{"xattr\u00a0-c", false, []string{"bypasses macOS quarantine"}},
	{"xattr\u2003-c", false, []string{"bypasses macOS quarantine"}},
	{"xattr\u200b-c", false, []string{}},
	{"base64\u3000--decode", false, []string{"decodes hidden content"}},
	{"\u017fk-ant-a1a1a1a1a1a1a1a1a1a1", false, []string{"holds what looks like a pasted secret"}},
	{"SK-ANT-A1A1A1A1A1A1A1A1A1A1", false, []string{"holds what looks like a pasted secret"}},
	{"sk-ant-\u0438\u0438\u0438\u0438\u0438\u0438\u0438\u0438\u0438\u0438\u0438\u0438\u0438\u0438\u0438\u0438\u0438\u0438\u0438\u0438", false, []string{"holds what looks like a pasted secret"}},
	{"\u212a sk-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false, []string{"holds what looks like a pasted secret"}},
	{"x sk-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\u0438", false, []string{}},
	{"asK-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false, []string{}},
	{"akiaaaaaaaaaaaaaaaaa", false, []string{"holds what looks like a pasted secret"}},
	{"akiaaaaaaaaaaaaaaaaa\u0438", false, []string{}},
	{"akiaaaaaaaaaaaaaaaaa.", false, []string{"holds what looks like a pasted secret"}},
	{"-----BEGIN OPEN\u017f\u017fH PRIVATE KEY-----", false, []string{"holds what looks like a pasted secret"}},
	{"bash", true, []string{"has a tool that runs any command or script"}},
	{"`bash`", true, []string{"has a tool that runs any command or script"}},
	{"``bash``", true, []string{}},
	{"x`bash`", true, []string{"has a tool that runs any command or script"}},
	{"`bash``", true, []string{}},
	{" bash", true, []string{"has a tool that runs any command or script"}},
	{"- bash", true, []string{"has a tool that runs any command or script"}},
	{"* exec:", true, []string{"has a tool that runs any command or script"}},
	{"| shell |", true, []string{"has a tool that runs any command or script"}},
	{"a bash", true, []string{}},
	{"bash:", true, []string{"has a tool that runs any command or script"}},
	{"bash :", true, []string{"has a tool that runs any command or script"}},
	{"bash\u00a0|", true, []string{"has a tool that runs any command or script"}},
	{"bashes", true, []string{}},
	{"\u0438`eval`", true, []string{"has a tool that runs any command or script"}},
	{"`eval`\u0438", true, []string{"has a tool that runs any command or script"}},
	{"run_command", true, []string{"has a tool that runs any command or script"}},
	{"run-command", true, []string{"has a tool that runs any command or script"}},
	{"runCommand", true, []string{"has a tool that runs any command or script"}},
	{"run\u0438_command", true, []string{}},
	{"xrun_command", true, []string{}},
	{"run_commands", true, []string{"has a tool that runs any command or script"}},
	{"run_commandx", true, []string{}},
	{"Executes arbitrary commands", true, []string{"runs arbitrary commands"}},
	{"executes\u00a0arbitrary commands", true, []string{"runs arbitrary commands"}},
	{"runs applescript", true, []string{"runs arbitrary AppleScript"}},
	{"an AppleScript Runner", true, []string{"runs arbitrary AppleScript"}},
	{"browserEvaluate", true, []string{"has a tool that runs any command or script"}},
	{"sendKeys", true, []string{"has a tool that runs any command or script"}},
	{"os-ascript", true, []string{}},
}

var redactGolden = [][2]string{
	{"ghp_a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1", "[secret removed]"},
	{"GHP_A1A1A1A1A1A1A1A1A1A1A1A1A1A1A1A1A1A1", "[secret removed]"},
	{"ghp_a\u0131a\u0131a\u0131a\u0131a\u0131a\u0131a\u0131a\u0131a\u0131a\u0131a\u0131a\u0131a\u0131a\u0131a\u0131", "[secret removed]"},
	{"ghp_a\u0438a\u0438a\u0438a\u0438a\u0438a\u0438a\u0438a\u0438a\u0438a\u0438a\u0438a\u0438a\u0438a\u0438a\u0438", "ghp_a\u0438a\u0438a\u0438a\u0438a\u0438a\u0438a\u0438a\u0438a\u0438a\u0438a\u0438a\u0438a\u0438a\u0438a\u0438"},
	{"xoxb-\u0663\u0663\u0663\u0663\u0663\u0663\u0663\u0663\u0663\u0663", "[secret removed]"},
	{"keep sk-ant-b2b2b2b2b2b2b2b2b2b2 and ghp_c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3 apart", "keep [secret removed] and [secret removed] apart"},
	{"-----begin private key-----sk-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "[secret removed][secret removed]"},
}

// sourceRE, slugRE, mcpNameRE, packageIDRE on the same inputs.
var nameGolden = []struct {
	text string
	want [4]bool
}{
	{"o/r", [4]bool{true, false, true, false}},
	{"O/R", [4]bool{true, false, true, false}},
	{"\u0131o/r", [4]bool{true, false, true, false}},
	{"o/r\u00e9", [4]bool{true, false, true, false}},
	{"-o/r", [4]bool{false, false, false, false}},
	{"o/-r", [4]bool{false, false, false, false}},
	{"o/_r", [4]bool{true, false, false, false}},
	{"a/b/c", [4]bool{false, false, true, false}},
	{"o/r ", [4]bool{false, false, false, false}},
	{"o/r\n", [4]bool{false, false, false, false}},
}

// What mcpWhere says of a server with only this remote.
var remoteGolden = [][2]string{
	{"https://x.example/mcp", "remote, x.example"},
	{"HTTPS://X.EXAMPLE/MCP", "remote, x.example"},
	{"http\u017f://x.example/", "remote, None"},
	{"https://x.example:8080/", "remote, x.example"},
	{"https://x.example:\u0663/", "remote, x.example"},
	{"https://\u212a.example/", "remote, k.example"},
	{"https://u@x.example/", "unknown origin"},
	{"https://x.example/\\", "unknown origin"},
	{"https://x.example/ ", "unknown origin"},
	{"https://x.example", "remote, x.example"},
	{"https://[::1]/", "unknown origin"},
	{"http://x.example/", "unknown origin"},
}

// nfkc, htmlUnescape, pyLower, collapseSpace and pySplitLines on the same inputs.
var textGolden = []struct {
	text                        string
	nfkc, unescaped, lower, one string
	lines                       []string
}{
	{"\uff43\uff55\uff52\uff4c x", "curl x", "\uff43\uff55\uff52\uff4c x", "\uff43\uff55\uff52\uff4c x", "\uff43\uff55\uff52\uff4c x", []string{"\uff43\uff55\uff52\uff4c x"}},
	{"\ufb01le", "file", "\ufb01le", "\ufb01le", "\ufb01le", []string{"\ufb01le"}},
	{"e\u0301", "\u00e9", "e\u0301", "e\u0301", "e\u0301", []string{"e\u0301"}},
	{"\u00c5\u212bA\u030a", "\u00c5\u00c5\u00c5", "\u00c5\u212bA\u030a", "\u00e5\u00e5a\u030a", "\u00c5\u212bA\u030a", []string{"\u00c5\u212bA\u030a"}},
	{"\uac01", "\uac01", "\uac01", "\uac01", "\uac01", []string{"\uac01"}},
	{"\u1100\u1161\u11a8", "\uac01", "\u1100\u1161\u11a8", "\u1100\u1161\u11a8", "\u1100\u1161\u11a8", []string{"\u1100\u1161\u11a8"}},
	{"\u0f73", "\u0f71\u0f72", "\u0f73", "\u0f73", "\u0f73", []string{"\u0f73"}},
	{"a\u0315\u0300\u05aeb", "\u00e0\u05ae\u0315b", "a\u0315\u0300\u05aeb", "a\u0315\u0300\u05aeb", "a\u0315\u0300\u05aeb", []string{"a\u0315\u0300\u05aeb"}},
	{"\u2460\u00b2\u00bd", "121\u20442", "\u2460\u00b2\u00bd", "\u2460\u00b2\u00bd", "\u2460\u00b2\u00bd", []string{"\u2460\u00b2\u00bd"}},
	{"\u1e9b\u0323", "\u1e69", "\u1e9b\u0323", "\u1e9b\u0323", "\u1e9b\u0323", []string{"\u1e9b\u0323"}},
	{"\u03a3\u0391\u03a3 \u03a3", "\u03a3\u0391\u03a3 \u03a3", "\u03a3\u0391\u03a3 \u03a3", "\u03c3\u03b1\u03c2 \u03c3", "\u03a3\u0391\u03a3 \u03a3", []string{"\u03a3\u0391\u03a3 \u03a3"}},
	{"\u0130stanbul", "\u0130stanbul", "\u0130stanbul", "i\u0307stanbul", "\u0130stanbul", []string{"\u0130stanbul"}},
	{"x\u200by\u00adz\ufeff", "x\u200by\u00adz\ufeff", "x\u200by\u00adz\ufeff", "x\u200by\u00adz\ufeff", "x\u200by\u00adz\ufeff", []string{"x\u200by\u00adz\ufeff"}},
	{"&#124; &pipe; &amp;lt; &#27;x &#0; &#x80; &notit; &nbsp &#xD800; &#99999999; &", "&#124; &pipe; &amp;lt; &#27;x &#0; &#x80; &notit; &nbsp &#xD800; &#99999999; &", "| &pipe; &lt; x \ufffd \u20ac \u00acit; \u00a0 \ufffd \ufffd &", "&#124; &pipe; &amp;lt; &#27;x &#0; &#x80; &notit; &nbsp &#xd800; &#99999999; &", "&#124; &pipe; &amp;lt; &#27;x &#0; &#x80; &notit; &nbsp &#xD800; &#99999999; &", []string{"&#124; &pipe; &amp;lt; &#27;x &#0; &#x80; &notit; &nbsp &#xD800; &#99999999; &"}},
	{"a\u000d\nb\u000bc\u001cd\u2028e\u0085f", "a\u000d\nb\u000bc\u001cd\u2028e\u0085f", "a\u000d\nb\u000bc\u001cd\u2028e\u0085f", "a\u000d\nb\u000bc\u001cd\u2028e\u0085f", "a b c d e f", []string{"a", "b", "c", "d", "e", "f"}},
	{"  a \u00a0 b\u001f c  ", "  a   b\u001f c  ", "  a \u00a0 b\u001f c  ", "  a \u00a0 b\u001f c  ", "a b c", []string{"  a \u00a0 b\u001f c  "}},
}

// dumpJSON and dumpJSONText of a string.
var stringGolden = [][3]string{
	{"caf\u00e9 \U0001f600 \u007f \u001b \u2028 \"q\" \\ \n\u0009", "\"caf\\u00e9 \\ud83d\\ude00 \\u007f \\u001b \\u2028 \\\"q\\\" \\\\ \\n\\t\"", "\"caf\u00e9 \U0001f600 \u007f \\u001b \u2028 \\\"q\\\" \\\\ \\n\\t\""},
	{"", "\"\"", "\"\""},
}

// parseJSON then dumpJSON; "" where json.loads refuses the text.
var valueGolden = [][2]string{
	{"{\"b\": [1, 2.5, 1e+16, 1e-05, 100.0, -0.0, null, true], \"a\": {\"\\u00e9\": \"x\"}}", "{\"b\": [1, 2.5, 1e+16, 1e-05, 100.0, -0.0, null, true], \"a\": {\"\\u00e9\": \"x\"}}"},
	{"{\"a\": 1, \"a\": 2}", "{\"a\": 2}"},
	{"[1, 2,]", ""},
	{"\"\\ud800\"", "\"?\""},
	{"{\"k\": 123456789012345678901234567890}", "{\"k\": 123456789012345678901234567890}"},
}

// Cache keys the prototype computed: for a skill from skills.sh, an MCP server and a written skill, all on the
// same task and document; then a written skill's own key, a search page's file and a document's.
var keyGolden = struct{ skill, mcp, written, write, search, doc, identity string }{
	"0b3fa80867e94df95017d8080b1165acde12475d68aff91bb077aafcbe984c00", "6a87a29d84ab130d20e838042aded75eba51226cfb1f68302882f0de2a82ec51", "8bdca299f1276d81f3f4ab425d1c5fe4441a94fad1ad8887faf9a3a870d8a423", "65c350c3f3241be288619d62b14c18f4a3d880af8376d1c586149c5c28836916",
	"search_caea954dbcac186f4cecf5a151e844ce6cd6bbb8895f3c0deab27590b7af9006.json", "doc_a1bda1587e3da57472da1dd585c01baa2ea7fc6a032f5ca1d72ce861efe52fe0.md",
	"{\"registry name\": \"io.github.a/foo\", \"installs\": {\"package\": {\"registryType\": \"npm\", \"identifier\": \"foo\", \"version\": \"1.2.3\", \"transport\": {\"type\": \"stdio\"}}}}",
}

func TestPort_prompts_are_the_prototypes_word_for_word(t *testing.T) {
	prompts := map[string]string{"groupPrompt": groupPrompt, "assignPrompt": assignPrompt, "coverPrompt": coverPrompt,
		"verifyPrompt": verifyPrompt, "writePrompt": writePrompt, "systemPrompt": systemPrompt,
		"leanFlags": strings.Join(leanFlags, "\x00")}
	for name, want := range promptHashes {
		if got := sha256Hex(prompts[name]); got != want {
			t.Errorf("%s differs from the prototype's", name)
		}
	}
	if model != "sonnet" || thinkTokens != "16000" || verifyEffort != "high" || docChars != 200000 {
		t.Error("the model is not run as the prototype ran it")
	}
}

func TestPort_risk_rules_on_the_rebuilt_edges(t *testing.T) {
	for _, c := range riskGolden {
		if got := risks(c.doc, c.mcp); !sameWords(got, c.want) {
			t.Errorf("%q (mcp %v)\n   got  %q\n   want %q", c.doc, c.mcp, got, c.want)
		}
	}
}

func TestPort_redact_ignores_case_as_python_does(t *testing.T) {
	for _, c := range redactGolden {
		if got := redact(c[0]); got != c[1] {
			t.Errorf("%q\n   got  %q\n   want %q", c[0], got, c[1])
		}
	}
}

func TestPort_names_that_may_reach_a_command_line(t *testing.T) {
	for _, c := range nameGolden {
		got := [4]bool{sourceRE.matches(c.text), slugRE.matches(c.text), mcpNameRE.matches(c.text), packageIDRE.matches(c.text)}
		if got != c.want {
			t.Errorf("%q: got %v, want %v", c.text, got, c.want)
		}
	}
}

func TestPort_remote_addresses(t *testing.T) {
	for _, c := range remoteGolden {
		server := objOf("remotes", []any{objOf("type", "sse", "url", c[0])})
		if got := mcpWhere(server); got != c[1] {
			t.Errorf("%q: got %q, want %q", c[0], got, c[1])
		}
	}
}

func TestPort_text_is_read_as_python_reads_it(t *testing.T) {
	for _, c := range textGolden {
		if got := nfkc(c.text); got != c.nfkc {
			t.Errorf("nfkc(%q) = %q, want %q", c.text, got, c.nfkc)
		}
		if got := htmlUnescape(c.text); got != c.unescaped {
			t.Errorf("htmlUnescape(%q) = %q, want %q", c.text, got, c.unescaped)
		}
		if got := pyLower(c.text); got != c.lower {
			t.Errorf("pyLower(%q) = %q, want %q", c.text, got, c.lower)
		}
		if got := clean(c.text); got != c.one {
			t.Errorf("clean(%q) = %q, want %q", c.text, got, c.one)
		}
		if got := pySplitLines(c.text); !sameWords(got, c.lines) {
			t.Errorf("pySplitLines(%q) = %q, want %q", c.text, got, c.lines)
		}
	}
}

func TestPort_json_is_written_and_read_as_python_does(t *testing.T) {
	for _, c := range stringGolden {
		if got := dumpJSON(c[0]); got != c[1] {
			t.Errorf("dumpJSON(%q) = %s, want %s", c[0], got, c[1])
		}
		if got := dumpJSONText(c[0]); got != c[2] {
			t.Errorf("dumpJSONText(%q) = %s, want %s", c[0], got, c[2])
		}
	}
	for _, c := range valueGolden {
		v, err := parseJSON(c[0])
		if (err != nil) != (c[1] == "") {
			t.Errorf("parseJSON(%s): error %v", c[0], err)
			continue
		}
		if err == nil && dumpJSON(v) != c[1] {
			t.Errorf("%s came back as %s, want %s", c[0], dumpJSON(v), c[1])
		}
	}
	if _, err := parseJSONStrict(`{"a": 1, "a": 2}`, true); err == nil {
		t.Error("a key given twice was read where a verdict is expected")
	}
}

func TestPort_cache_keys_are_the_prototypes(t *testing.T) {
	a := fakeApp(t)
	used := []skill{{Name: "ship-release", Desc: "Cuts a release é"}}
	installed := []skill{{Name: "ecc:code-review", Desc: strings.Repeat("Reviews code ", 30)},
		{Name: "release-helper", Desc: "prepare versioned release changelog tag"}}
	server := jsObj(t, `{"name": "io.github.a/foo", "packages": [{"registryType": "npm", "identifier": "foo", "version": "1.2.3", "transport": {"type": "stdio"}}]}`)
	base := candidate{Task: "Prepare a versioned release: bump, changelog, tag, CI.", Doc: "---\nname: pdf\ndescription: café \U0001f600\n---\n# release\n"}
	fromSkillsSh, fromRegistry, written := base, base, base
	fromSkillsSh.Kind, fromSkillsSh.Name, fromSkillsSh.Source, fromSkillsSh.Slug, fromSkillsSh.ID = kindSkill, "pdf", "o/r", "pdf", "o/r/pdf"
	fromRegistry.Kind, fromRegistry.Name, fromRegistry.ID, fromRegistry.Server = kindMCP, "foo", "io.github.a/foo", server
	written.Kind, written.Created = kindSkill, true
	got := map[string]string{
		"skill":    a.verifyKey(requestFor(fromSkillsSh, used, installed)),
		"mcp":      a.verifyKey(requestFor(fromRegistry, used, installed)),
		"written":  a.verifyKey(requestFor(written, used, installed)),
		"write":    writeKey(kind{Name: "releasing", Task: "cut a release", IDs: []string{"b", "a"}}),
		"search":   filepath.Base(a.cacheFile("search", pyLower("File Organizer"), ".json")),
		"doc":      filepath.Base(a.cacheFile("doc", "o/r/pdf", ".md")),
		"identity": identity(fromRegistry),
	}
	want := map[string]string{"skill": keyGolden.skill, "mcp": keyGolden.mcp, "written": keyGolden.written, "write": keyGolden.write,
		"search": keyGolden.search, "doc": keyGolden.doc, "identity": keyGolden.identity}
	for name := range want {
		if got[name] != want[name] {
			t.Errorf("%s: got %s, want %s", name, got[name], want[name])
		}
	}
}
