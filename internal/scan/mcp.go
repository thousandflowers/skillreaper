package scan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// mcpServerConfig is the loose shape of one MCP server entry.
type mcpServerConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Type    string   `json:"type"`
	URL     string   `json:"url"`
}

// ScanMCP inventories MCP servers from the platform config file (user
// scope and per-project scope) and from installed plugins' .mcp.json
// files. configPath may point at a missing file (fresh install).
func ScanMCP(configPath, configDir, platformID, configFormat string) ([]Item, []Warning) {
	var items []Item
	var warns []Warning

	b, err := readCapped(configPath)
	if err == nil {
		// The platform table names the encoding. Before it did, every config
		// was handed to encoding/json — so a TOML file failed on the first
		// character of its first key and a JSONC file on the "//" of a URL,
		// both reported as "unreadable JSON" as though the user's file were
		// broken. Neither platform's servers were ever inventoried.
		switch configFormat {
		case "jsonc":
			b = StripJSONC(b)
		case "toml":
			return appendTOMLMCP(items, warns, b, configPath, platformID), warns
		}
		var top map[string]json.RawMessage
		if jerr := json.Unmarshal(b, &top); jerr != nil {
			warns = append(warns, Warning{Path: configPath, Msg: "unreadable JSON: " + jerr.Error()})
		} else {
			items = appendMCPServers(items, top["mcpServers"], "user-config", configPath, platformID, nil)

			var projects map[string]json.RawMessage
			if raw, ok := top["projects"]; ok {
				if jerr := json.Unmarshal(raw, &projects); jerr == nil {
					for projPath, projRaw := range projects {
						var proj struct {
							MCPServers json.RawMessage `json:"mcpServers"`
						}
						if json.Unmarshal(projRaw, &proj) == nil {
							items = appendMCPServers(items, proj.MCPServers, "project:"+projPath, configPath, platformID, nil)
						}
					}
				}
			}
		}
	} else if !os.IsNotExist(err) {
		warns = append(warns, Warning{Path: configPath, Msg: err.Error()})
	}

	plugins, pw := installedPlugins(configDir)
	warns = append(warns, pw...)
	for _, p := range plugins {
		before := len(items)
		// A plugin declares its servers either in a dedicated .mcp.json or inline
		// in its manifest. Reading only .mcp.json made manifest-declared servers
		// invisible: no row at all, so no verdict and no weight. .mcp.json is read
		// first so it wins a name collision.
		seen := map[string]bool{}
		for _, path := range []string{
			filepath.Join(p.InstallPath, ".mcp.json"),
			filepath.Join(p.InstallPath, ".claude-plugin", "plugin.json"),
		} {
			pb, err := readCapped(path)
			if err != nil {
				continue
			}
			var f struct {
				MCPServers json.RawMessage `json:"mcpServers"`
			}
			if jerr := json.Unmarshal(pb, &f); jerr != nil {
				warns = append(warns, Warning{Path: path, Msg: "unreadable JSON: " + jerr.Error()})
				continue
			}
			items = appendMCPServers(items, f.MCPServers, "plugin:"+p.FullName, path, platformID, seen)
		}
		// Plugin-shipped servers cannot be pruned per-server; mark not removable.
		for i := before; i < len(items); i++ {
			items[i].Platform = platformID
			items[i].Removable = false
		}
	}
	return items, warns
}

// appendMCPServers expands one mcpServers JSON object into Items.
// Servers from user/project config are removable. seen (nil-ok) suppresses
// names already emitted, so a plugin that declares the same server in both
// .mcp.json and its manifest yields one row, not two.
func appendMCPServers(items []Item, raw json.RawMessage, source, path, platformID string, seen map[string]bool) []Item {
	if len(raw) == 0 {
		return items
	}
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(raw, &servers); err != nil {
		return items
	}
	for name, cfgRaw := range servers {
		if seen != nil {
			if seen[name] {
				continue
			}
			seen[name] = true
		}
		var cfg mcpServerConfig
		_ = json.Unmarshal(cfgRaw, &cfg)
		display := cfg.URL
		if cfg.Command != "" {
			display = strings.TrimSpace(cfg.Command + " " + strings.Join(cfg.Args, " "))
		}
		items = append(items, Item{
			Category:    CatMCP,
			Name:        name,
			Platform:    platformID,
			Source:      source,
			Path:        path,
			Description: display,
			Removable:   true,
		})
	}
	return items
}

// appendTOMLMCP inventories MCP servers declared as [mcp_servers.<name>]
// tables, which is how Codex writes them. A config with no such table is the
// normal case and yields nothing, not a warning.
func appendTOMLMCP(items []Item, warns []Warning, b []byte, configPath, platformID string) []Item {
	for name, keys := range TOMLSubTables(b, "mcp_servers") {
		desc := keys["command"]
		if desc == "" {
			desc = keys["url"]
		}
		items = append(items, Item{
			Category:    CatMCP,
			Name:        name,
			Platform:    platformID,
			Source:      "user-config",
			Path:        configPath,
			Description: desc,
			Removable:   false,
		})
	}
	return items
}
