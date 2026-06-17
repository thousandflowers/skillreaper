package scan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// pluginInfo describes one installed Claude Code plugin.
type pluginInfo struct {
	Name        string // short name, before the "@": "ecc"
	FullName    string // "ecc@ecc-marketplace"
	InstallPath string
	InstalledAt time.Time
}

// installedPluginsFile mirrors ~/.claude/plugins/installed_plugins.json
// (version 2): {"version":2,"plugins":{"name@mkt":[{"installPath":...,"installedAt":...}]}}.
type installedPluginsFile struct {
	Version int `json:"version"`
	Plugins map[string][]struct {
		InstallPath string `json:"installPath"`
		InstalledAt string `json:"installedAt"`
	} `json:"plugins"`
}

// installedPlugins reads the plugin registry. A missing file is normal
// (no plugins installed); a corrupt one yields a Warning.
func installedPlugins(claudeDir string) ([]pluginInfo, []Warning) {
	path := filepath.Join(claudeDir, "plugins", "installed_plugins.json")
	b, err := readCapped(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []Warning{{Path: path, Msg: err.Error()}}
	}
	var f installedPluginsFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, []Warning{{Path: path, Msg: "unreadable JSON: " + err.Error()}}
	}
	var out []pluginInfo
	for full, installs := range f.Plugins {
		if len(installs) == 0 {
			continue
		}
		ins := installs[0]
		short := full
		if i := strings.IndexByte(full, '@'); i >= 0 {
			short = full[:i]
		}
		ts, _ := time.Parse(time.RFC3339, ins.InstalledAt)
		out = append(out, pluginInfo{
			Name:        short,
			FullName:    full,
			InstallPath: ins.InstallPath,
			InstalledAt: ts,
		})
	}
	return out, nil
}
