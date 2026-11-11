package scan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// mcpServerConfig is the loose shape of one MCP server entry.
type mcpServerConfig struct {
	// Command is a string in most tools and a whole argv array in OpenCode.
	Command json.RawMessage `json:"command"`
	Args    []string        `json:"args"`
	Type    string          `json:"type"`
	URL     string          `json:"url"`
}

// display is the command line or URL with any credentials masked.
func (c mcpServerConfig) display() string {
	var argv []string
	var one string
	switch {
	case json.Unmarshal(c.Command, &one) == nil && one != "":
		argv = []string{one}
	case json.Unmarshal(c.Command, &argv) == nil && len(argv) > 0:
	default:
		return RedactSecrets(c.URL)
	}
	return RedactSecrets(strings.TrimSpace(strings.Join(append(argv, c.Args...), " ")))
}

// ScanMCP inventories MCP servers from the platform config file (user
// scope and per-project scope) and from installed plugins' .mcp.json
// files. configPath may point at a missing file (fresh install).
func ScanMCP(configPath, configDir, platformID, configFormat string) ([]Item, []Warning) {
	var items []Item
	var warns []Warning

	b, err := readCapped(configPath)
	plainJSON := true
	if err == nil {
		// The platform table names the encoding. Before it did, every config
		// was handed to encoding/json — so a TOML file failed on the first
		// character of its first key and a JSONC file on the "//" of a URL,
		// both reported as "unreadable JSON" as though the user's file were
		// broken. Neither platform's servers were ever inventoried.
		switch configFormat {
		case "jsonc":
			// Prune edits plain JSON only: it will not rewrite a file full of
			// someone's comments, so their servers are reported, not offered.
			plainJSON = json.Valid(b)
			b = StripJSONC(b)
		case "toml":
			return appendTOMLMCP(items, warns, b, configPath, platformID), warns
		case "yaml":
			return appendYAMLMCP(items, b, configPath, platformID), warns
		}
		var top map[string]json.RawMessage
		if jerr := json.Unmarshal(b, &top); jerr != nil {
			warns = append(warns, Warning{Path: configPath, Msg: "unreadable JSON: " + jerr.Error()})
		} else {
			firstServer := len(items)
			items = appendMCPServers(items, top["mcpServers"], "user-config", configPath, platformID, nil)
			for i := firstServer; i < len(items) && !plainJSON; i++ {
				items[i].Removable = false
			}
			// OpenCode's documented key. Prune only edits mcpServers, so these
			// are reported, not offered for removal.
			before := len(items)
			items = appendMCPServers(items, top["mcp"], "user-config", configPath, platformID, nil)
			for i := before; i < len(items); i++ {
				items[i].Removable = false
			}

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
		items = append(items, Item{
			Category:    CatMCP,
			Name:        name,
			Platform:    platformID,
			Source:      source,
			Path:        path,
			Description: cfg.display(),
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
			Description: RedactSecrets(desc),
			Removable:   false,
		})
	}
	return items
}

// appendYAMLMCP inventories the servers under a top-level mcp_servers: block,
// which is how Hermes writes them. Like the TOML reader it reads only that
// shape, by indentation, and it needs no dependency.
func appendYAMLMCP(items []Item, b []byte, configPath, platformID string) []Item {
	inBlock, childIndent := false, -1
	var cur *Item
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimRight(raw, " \t\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 {
			inBlock = strings.HasPrefix(trimmed, "mcp_servers:")
			childIndent, cur = -1, nil
			continue
		}
		if !inBlock {
			continue
		}
		if childIndent < 0 {
			childIndent = indent
		}
		key, val, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		key, val = strings.Trim(key, `"'`), strings.Trim(strings.TrimSpace(val), `"'`)
		if indent == childIndent {
			items = append(items, Item{Category: CatMCP, Name: key, Platform: platformID,
				Source: "user-config", Path: configPath})
			cur = &items[len(items)-1]
			continue
		}
		if cur != nil && (key == "command" || key == "url") && val != "" {
			cur.Description = RedactSecrets(val)
		}
	}
	return items
}
