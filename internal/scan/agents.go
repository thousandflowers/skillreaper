package scan

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ScanAgents inventories personal agents (~/.claude/agents/*.md) and
// plugin-provided agents. The Name field matches the subagent_type
// used in Task/Agent tool calls: bare for personal agents,
// "plugin:agent" for plugin agents.
func ScanAgents(claudeDir string) ([]Item, []Warning) {
	var items []Item
	var warns []Warning

	items, warns = appendAgentsFromDir(items, warns,
		filepath.Join(claudeDir, "agents"), "", "personal", time.Time{}, true)

	plugins, pw := installedPlugins(claudeDir)
	warns = append(warns, pw...)
	for _, p := range plugins {
		items, warns = appendAgentsFromDir(items, warns,
			filepath.Join(p.InstallPath, "agents"),
			p.Name+":", "plugin:"+p.FullName, p.InstalledAt, false)
	}
	return items, warns
}

func appendAgentsFromDir(items []Item, warns []Warning, dir, namePrefix, source string, installedAt time.Time, removable bool) ([]Item, []Warning) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			warns = append(warns, Warning{Path: dir, Msg: err.Error()})
		}
		return items, warns
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			warns = append(warns, Warning{Path: path, Msg: err.Error()})
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".md")
		name, desc, bodyChars := parseFrontmatter(b)
		if name == "" {
			name = stem
		}
		key := namePrefix + stem
		items = append(items, Item{
			Category:    CatAgent,
			Name:        key,
			Source:      source,
			Path:        path,
			Description: desc,
			DescChars:   len(key) + len(desc),
			BodyChars:   bodyChars,
			InstalledAt: installedAt,
			Removable:   removable,
		})
	}
	return items, warns
}
