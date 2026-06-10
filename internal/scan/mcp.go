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
func ScanMCP(configPath, configDir, platformID string) ([]Item, []Warning) {
	var items []Item
	var warns []Warning

	b, err := os.ReadFile(configPath)
	if err == nil {
		var top map[string]json.RawMessage
		if jerr := json.Unmarshal(b, &top); jerr != nil {
			warns = append(warns, Warning{Path: configPath, Msg: "unreadable JSON: " + jerr.Error()})
		} else {
			items = appendMCPServers(items, top["mcpServers"], "user-config", configPath, platformID)

			var projects map[string]json.RawMessage
			if raw, ok := top["projects"]; ok {
				if jerr := json.Unmarshal(raw, &projects); jerr == nil {
					for projPath, projRaw := range projects {
						var proj struct {
							MCPServers json.RawMessage `json:"mcpServers"`
						}
						if json.Unmarshal(projRaw, &proj) == nil {
							items = appendMCPServers(items, proj.MCPServers, "project:"+projPath, configPath, platformID)
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
		path := filepath.Join(p.InstallPath, ".mcp.json")
		pb, err := os.ReadFile(path)
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
		// Plugin-shipped servers cannot be pruned per-server; mark not removable.
		before := len(items)
		items = appendMCPServers(items, f.MCPServers, "plugin:"+p.FullName, path, platformID)
		for i := before; i < len(items); i++ {
			items[i].Platform = platformID
			items[i].Removable = false
		}
	}
	return items, warns
}

// appendMCPServers expands one mcpServers JSON object into Items.
// Servers from user/project config are removable.
func appendMCPServers(items []Item, raw json.RawMessage, source, path, platformID string) []Item {
	if len(raw) == 0 {
		return items
	}
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(raw, &servers); err != nil {
		return items
	}
	for name, cfgRaw := range servers {
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
