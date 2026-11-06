package scan

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

// disabledPlugins names the plugins switched off in enabledPlugins, read in
// Claude Code's own order so the narrower scope wins. A disabled plugin loads
// nothing, so it is neither loaded weight nor waste. Unlisted means enabled.
func disabledPlugins(claudeDir string) map[string]bool {
	cwd, _ := os.Getwd()
	state := map[string]bool{}
	for _, path := range []string{
		filepath.Join(claudeDir, "settings.json"),
		filepath.Join(claudeDir, "settings.local.json"),
		filepath.Join(cwd, ".claude", "settings.json"),
		filepath.Join(cwd, ".claude", "settings.local.json"),
	} {
		b, err := readCapped(path)
		if err != nil {
			continue
		}
		var s struct {
			EnabledPlugins map[string]json.RawMessage `json:"enabledPlugins"`
		}
		if json.Unmarshal(b, &s) != nil {
			continue
		}
		for name, v := range s.EnabledPlugins {
			state[name] = string(v) != "false"
		}
	}
	off := map[string]bool{}
	for name, on := range state {
		if !on {
			off[name] = true
		}
	}
	return off
}

// installedPlugins reads the plugin registry. A missing file is normal
// (no plugins installed); a corrupt one yields a Warning.
func installedPlugins(claudeDir string) ([]pluginInfo, []Warning) {
	pluginRoot := filepath.Join(claudeDir, "plugins")
	info, err := os.Lstat(pluginRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []Warning{{Path: pluginRoot, Msg: err.Error()}}
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, []Warning{{Path: pluginRoot, Msg: "plugin root is not a regular directory; skipping plugins"}}
	}

	path := filepath.Join(pluginRoot, "installed_plugins.json")
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
	var warns []Warning
	off := disabledPlugins(claudeDir)
	for full, installs := range f.Plugins {
		if len(installs) == 0 || off[full] {
			continue
		}
		if !ValidPluginID(full) {
			warns = append(warns, Warning{Path: path, Msg: fmt.Sprintf("skipping a plugin with an invalid id: %q", SafeText(full))})
			continue
		}
		ins := installs[0]
		installPath, ok := resolveWithin(pluginRoot, ins.InstallPath)
		if !ok {
			warns = append(warns, Warning{
				Path: ins.InstallPath,
				Msg:  "plugin install path escapes plugin root; skipping",
			})
			continue
		}
		short, _, _ := strings.Cut(full, "@")
		ts, _ := time.Parse(time.RFC3339, ins.InstalledAt)
		out = append(out, pluginInfo{
			Name:        short,
			FullName:    full,
			InstallPath: installPath,
			InstalledAt: ts,
		})
	}
	return out, warns
}

// pluginIDPattern is name@marketplace. The id ends up in the command a user is
// told to run to disable the plugin, so nothing but this shape gets that far.
var pluginIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*@[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidPluginID reports whether an id from the registry has the plugin id shape.
func ValidPluginID(id string) bool { return pluginIDPattern.MatchString(id) }
