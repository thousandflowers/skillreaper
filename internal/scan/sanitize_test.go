package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeText(t *testing.T) {
	for in, want := range map[string]string{
		"plain-name_1:x":  "plain-name_1:x",
		"caffè":           "caffè",
		"a\x1b[2Kb":       "a?[2Kb",
		"a\nb\rc\td":      "a?b?c?d",
		"a\x7fb\u009bc":   "a?b?c",
		"a\u202eb\u2066c": "a?b?c",
	} {
		if got := SafeText(in); got != want {
			t.Errorf("SafeText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRedactSecrets(t *testing.T) {
	for _, c := range []struct{ in, keep, gone string }{
		{"npx some-mcp --api-key=SECRET1", "some-mcp --api-key=", "SECRET1"},
		{"npx some-mcp --token SECRET2 --verbose", "--verbose", "SECRET2"},
		{"https://x.example/mcp?a=1&token=SECRET3", "a=1", "SECRET3"},
		{"curl -H X-Auth: SECRET4 https://x", "https://x", "SECRET4"},
		{"curl -H 'Authorization: Bearer SECRET5'", "Authorization", "SECRET5"},
		{"https://user:SECRET6@host/x", "user", "SECRET6"},
		{"API_KEY=SECRET7 ./run.sh", "./run.sh", "SECRET7"},
		{"/usr/bin/plain --flag value", "/usr/bin/plain --flag value", "\x00"},
	} {
		got := RedactSecrets(c.in)
		if strings.Contains(got, c.gone) || !strings.Contains(got, c.keep) {
			t.Errorf("RedactSecrets(%q) = %q", c.in, got)
		}
	}
}

func TestParseFrontmatterBOMAndLongLine(t *testing.T) {
	_, d, _ := parseFrontmatter([]byte("\xef\xbb\xbf---\ndescription: hello\n---\nb\n"))
	if d != "hello" {
		t.Errorf("BOM: description = %q", d)
	}
	long := strings.Repeat("x", 2<<20)
	_, d, _ = parseFrontmatter([]byte("---\ndescription: " + long + "\n---\n"))
	if d != long {
		t.Errorf("long line: description has %d chars", len(d))
	}
}

func TestTOMLSubTablesForms(t *testing.T) {
	doc := `mcp_servers.inline = { command = "a" }
[mcp_servers.commented] # why
command = "a"
[mcp_servers."dotted.name"]
command = "b"
[mcp_servers."dotted.name".env]
X = "1"
  [ mcp_servers.spaced ]
command = "c"
`
	got := TOMLSubTables([]byte(doc), "mcp_servers")
	for _, name := range []string{"commented", "dotted.name", "inline", "spaced"} {
		if got[name] == nil {
			t.Errorf("%q missing from %v", name, got)
		}
	}
	if len(got) != 4 {
		t.Errorf("got %d servers, want 4: %v", len(got), got)
	}
}

func TestDisabledPluginsLeaveTheInventory(t *testing.T) {
	dir := t.TempDir()
	install := filepath.Join(dir, "plugins", "cache", "p")
	if err := os.MkdirAll(filepath.Join(install, "skills", "s"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(install, "skills", "s", "SKILL.md"), "---\ndescription: d\n---\n")
	write(filepath.Join(dir, "plugins", "installed_plugins.json"),
		`{"version":2,"plugins":{"p@m":[{"installPath":"`+install+`"}]}}`)
	if items, _ := ScanSkills(dir, "t"); len(items) != 1 {
		t.Fatalf("enabled: %d items", len(items))
	}
	write(filepath.Join(dir, "settings.json"), `{"enabledPlugins":{"p@m":false}}`)
	if items, _ := ScanSkills(dir, "t"); len(items) != 0 {
		t.Errorf("disabled plugin still scanned: %d items", len(items))
	}
	write(filepath.Join(dir, "settings.local.json"), `{"enabledPlugins":{"p@m":true}}`)
	if items, _ := ScanSkills(dir, "t"); len(items) != 1 {
		t.Errorf("the narrower scope did not win: %d items", len(items))
	}
}

func TestAppendYAMLMCP(t *testing.T) {
	doc := "model: x\nmcp_servers:\n  one:\n    command: npx --token SEKRET\n  two:\n    url: https://x\nother:\n  three:\n    command: y\n"
	items := appendYAMLMCP(nil, []byte(doc), "c", "hermes")
	if len(items) != 2 || items[0].Name != "one" || items[1].Name != "two" {
		t.Fatalf("items = %+v", items)
	}
	if strings.Contains(items[0].Description, "SEKRET") {
		t.Errorf("secret kept: %q", items[0].Description)
	}
}

// A word of a command someone will paste: left alone when plain, one quoted
// word otherwise.
func TestShellArg(t *testing.T) {
	for in, want := range map[string]string{
		"plain-name_1":          "plain-name_1",
		"skill:ecc:plan":        "skill:ecc:plan",
		"/Users/a/.claude/x.md": "/Users/a/.claude/x.md",
		"café":                  "café",
		"my skill":              "'my skill'",
		"/ho me é/SKILL.md":     "'/ho me é/SKILL.md'",
		"it's":                  `'it'\''s'`,
		"a;b":                   "'a;b'",
		"$HOME":                 "'$HOME'",
		"~/x":                   "'~/x'",
		"a*":                    "'a*'",
		"":                      "''",
	} {
		if got := ShellArg(in); got != want {
			t.Errorf("ShellArg(%q) = %s, want %s", in, got, want)
		}
	}
}

// The credential is what is masked, not the word before it: after
// "Authorization: Basic" the scheme was hidden and the credential printed, in
// --json, --md and the report's hook rows.
func TestRedactSecretsMasksTheCredentialItself(t *testing.T) {
	for _, c := range []struct{ in, keep, gone string }{
		{`curl -H "Authorization: Basic dXNlcjpwYXNzd29yZA==" http://x`, `" http://x`, "dXNlcjpwYXNzd29yZA=="},
		{`curl -H 'Authorization: Token abcdef123456' http://x`, `' http://x`, "abcdef123456"},
		{`curl -H "Authorization: Bearer abc123" http://x`, `<redacted>" http://x`, "abc123"},
		{`curl -H "X-Auth-Token: tok123" http://x`, `<redacted>" http://x`, "tok123"},
		{`curl -u admin:hunter2 http://x`, "http://x", "hunter2"},
		{`curl -H "Cookie: session=abc123; theme=dark" http://x`, `" http://x`, "abc123"},
	} {
		got := RedactSecrets(c.in)
		if strings.Contains(got, c.gone) || !strings.Contains(got, c.keep) {
			t.Errorf("RedactSecrets(%s) = %s", c.in, got)
		}
	}
}

// A secret has no shape that shows it: in quotes, glued to its flag, as the user
// part of a URL, or with a space in it.
func TestRedactSecretsMasksEveryShapeOfSecret(t *testing.T) {
	for _, c := range []struct{ in, keep, gone string }{
		{`run --token "abc123"`, "run --token", "abc123"},
		{`TOKEN="abc123" run.sh`, "run.sh", "abc123"},
		{`{"api_key":"abc123","model":"m1"}`, `"model":"m1"`, "abc123"},
		{`sh -c "echo {\"api_key\":\"abc123\"}"`, "sh -c", "abc123"},
		{`git clone https://ghp_SECRETPAT123@github.com/o/r`, "github.com/o/r", "ghp_SECRETPAT123"},
		{`curl --header "X-Token: abc def" https://x`, "https://x", "def"},
		{`curl -H "Authorization: Digest username=\"bob\", response=\"abc123\"" https://x`, "https://x", "bob"},
		{`curl -H "Authorization: Digest username=\"bob\", response=\"abc123\"" https://x`, "https://x", "abc123"},
		{`curl -H 'Authorization: Digest username="bob", response="abc123"' https://x`, "https://x", "abc123"},
		{`Proxy-Authorization: Basic dXNlcg==`, "Proxy-Authorization", "dXNlcg=="},
		{`Authorization: Digest username="bob", response="abc123"`, "Authorization", "abc123"},
		{`srv --token=-abc123`, "srv --token=", "abc123"},
		{`PASSWORD=-abc123 run.sh`, "run.sh", "abc123"},
		{`https://user:p@ss@host/x`, "host/x", "ss@"},
		{`curl --user bob:hunter2 https://x`, "https://x", "hunter2"},
		{`curl --user=bob:hunter2 https://x`, "https://x", "hunter2"},
		{`mysql -ubob:hunter2 db`, " db", "hunter2"},
		{`curl -u "bob:my pass" https://x`, "https://x", "pass"},
		{`curl -u 'bob:my pass' https://x`, "https://x", "pass"},
		{`npx -y @scope/pkg --port 8080 https://host:8080/path`, `npx -y @scope/pkg --port 8080 https://host:8080/path`, "\x00"},
	} {
		got := RedactSecrets(c.in)
		if strings.Contains(got, c.gone) || !strings.Contains(got, c.keep) {
			t.Errorf("RedactSecrets(%s) = %s", c.in, got)
		}
	}
}
