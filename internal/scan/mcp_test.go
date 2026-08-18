package scan

import (
	"path/filepath"
	"testing"
)

func TestScanMCP(t *testing.T) {
	home := buildFixtureHome(t)
	plugDir := filepath.Join(home, "plugins", "cache", "mkt", "coolplug", "1.0.0")
	mustWrite(t, filepath.Join(plugDir, ".mcp.json"),
		`{"mcpServers":{"plugtool":{"command":"npx","args":["plugtool-mcp"]}}}`)

	claudeJSON := filepath.Join(home, "dotclaude.json")
	mustWrite(t, claudeJSON, `{
		"someOtherKey": 42,
		"mcpServers": {"globalsrv": {"command": "uvx", "args": ["globalsrv"]}},
		"projects": {
			"/Users/test/proj": {"mcpServers": {"projsrv": {"type": "http", "url": "http://localhost:9999"}}}
		}
	}`)

	items, warns := ScanMCP(claudeJSON, home, "test")
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3: %+v", len(items), items)
	}

	g := findItem(items, "globalsrv")
	if g == nil || g.Source != "user-config" || !g.Removable {
		t.Errorf("globalsrv wrong: %+v", g)
	}
	if g != nil && g.Description != "uvx globalsrv" {
		t.Errorf("globalsrv description = %q", g.Description)
	}

	p := findItem(items, "projsrv")
	if p == nil || p.Source != "project:/Users/test/proj" || !p.Removable {
		t.Errorf("projsrv wrong: %+v", p)
	}
	if p != nil && p.Description != "http://localhost:9999" {
		t.Errorf("projsrv description = %q", p.Description)
	}

	pl := findItem(items, "plugtool")
	if pl == nil || pl.Source != "plugin:coolplug@mkt" || pl.Removable {
		t.Errorf("plugtool wrong: %+v", pl)
	}
}

// A plugin may declare its MCP servers inline in .claude-plugin/plugin.json
// instead of a dedicated .mcp.json. Reading only .mcp.json left those servers
// with no row at all — invisible rather than merely miscounted.
func TestScanMCPPluginManifestServers(t *testing.T) {
	home := buildFixtureHome(t)
	plugDir := filepath.Join(home, "plugins", "cache", "mkt", "coolplug", "1.0.0")
	mustWrite(t, filepath.Join(plugDir, ".mcp.json"),
		`{"mcpServers":{"dedicated":{"command":"npx","args":["dedicated-mcp"]},"both":{"command":"npx","args":["from-mcp-json"]}}}`)
	mustWrite(t, filepath.Join(plugDir, ".claude-plugin", "plugin.json"),
		`{"name":"coolplug","mcpServers":{"manifest":{"command":"node","args":["mcp/dist/index.js"]},"both":{"command":"node","args":["from-manifest"]}}}`)

	claudeJSON := filepath.Join(home, "dotclaude.json")
	mustWrite(t, claudeJSON, `{"mcpServers":{}}`)

	items, warns := ScanMCP(claudeJSON, home, "test")
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3 (dedicated, both, manifest): %+v", len(items), items)
	}

	for _, name := range []string{"dedicated", "manifest", "both"} {
		it := findItem(items, name)
		if it == nil {
			t.Errorf("%s: no row", name)
			continue
		}
		if it.Source != "plugin:coolplug@mkt" {
			t.Errorf("%s: Source = %q", name, it.Source)
		}
		if it.Removable {
			t.Errorf("%s: plugin-shipped server must not be removable", name)
		}
	}

	// Declared in both files: one row, and .mcp.json wins.
	if it := findItem(items, "both"); it != nil {
		if it.Description != "npx from-mcp-json" {
			t.Errorf("both: Description = %q, want the .mcp.json definition", it.Description)
		}
		if filepath.Base(it.Path) != ".mcp.json" {
			t.Errorf("both: Path = %q, want the .mcp.json path", it.Path)
		}
	}
}

func TestScanMCPMissingFile(t *testing.T) {
	home := t.TempDir()
	items, warns := ScanMCP(filepath.Join(home, "nope.json"), home, "test")
	if len(items) != 0 || len(warns) != 0 {
		t.Errorf("expected empty, got %d items %d warns", len(items), len(warns))
	}
}

func TestScanMCPCorrupt(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "bad.json")
	mustWrite(t, path, "{nope")
	_, warns := ScanMCP(path, home, "test")
	if len(warns) != 1 {
		t.Errorf("expected 1 warning, got %d", len(warns))
	}
}
