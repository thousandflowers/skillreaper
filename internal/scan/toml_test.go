package scan

import (
	"reflect"
	"testing"
)

func TestTOMLTables(t *testing.T) {
	doc := []byte(`
# a comment line
[tui.model_availability_nux]
seen = true

[projects."/Users/someone/repo"]
trust_level = "trusted"

[mcp_servers.fs]
command = "npx"
note = "a # inside a quoted value stays"
bare = value # but a trailing comment goes
`)
	got := TOMLTables(doc)

	if _, ok := got["tui.model_availability_nux"]; !ok {
		t.Error("a dotted table header was not recorded")
	}
	if _, ok := got[`projects."/Users/someone/repo"`]; !ok {
		t.Error("a quoted table header was not recorded")
	}
	fs := got["mcp_servers.fs"]
	if fs["command"] != "npx" {
		t.Errorf("command = %q, want npx", fs["command"])
	}
	if fs["note"] != "a # inside a quoted value stays" {
		t.Errorf("a # inside a quoted value was treated as a comment: %q", fs["note"])
	}
	if fs["bare"] != "value" {
		t.Errorf("a trailing comment on an unquoted value was not stripped: %q", fs["bare"])
	}
}

// A config with no tables at all is the normal case for an agent that declares
// no MCP servers, and must not read as a parse failure.
func TestTOMLTablesOnAnEmptyDocument(t *testing.T) {
	for _, doc := range []string{"", "# only a comment\n", "\n\n\n"} {
		if got := TOMLTables([]byte(doc)); len(got) != 0 {
			t.Errorf("got %v, want no tables for %q", got, doc)
		}
	}
}

func TestTOMLSubTables(t *testing.T) {
	doc := []byte(`
[mcp_servers.fs]
command = "npx"

[mcp_servers."with.dot"]
command = "uvx"

[mcp_servers.fs.env]
KEY = "v"

[other.thing]
x = 1
`)
	got := TOMLSubTables(doc, "mcp_servers")

	want := []string{"fs", "with.dot"}
	names := make([]string, 0, len(got))
	for k := range got {
		names = append(names, k)
	}
	if len(names) != len(want) {
		t.Fatalf("got %v, want exactly %v — a nested [mcp_servers.fs.env] is the child's, not ours", names, want)
	}
	for _, w := range want {
		if _, ok := got[w]; !ok {
			t.Errorf("missing sub-table %q", w)
		}
	}
	if got["fs"]["command"] != "npx" {
		t.Error("sub-table keys were lost")
	}
}

func TestTOMLSubTablesWithNoMatch(t *testing.T) {
	doc := []byte("[tui]\nx = 1\n")
	if got := TOMLSubTables(doc, "mcp_servers"); !reflect.DeepEqual(got, map[string]map[string]string{}) {
		t.Errorf("got %v, want empty", got)
	}
}
