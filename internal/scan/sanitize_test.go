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
