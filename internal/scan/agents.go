package scan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ScanAgents inventories personal agents (<dir>/agents/*.md) and
// plugin-provided agents. The Name field matches the subagent_type
// used in Task/Agent tool calls: bare for personal agents,
// "plugin:agent" for plugin agents.
func ScanAgents(dir, platformID string) ([]Item, []Warning) {
	var items []Item
	var warns []Warning

	items, warns = appendAgentsFromDir(items, warns,
		filepath.Join(dir, "agents"), "", "personal", time.Time{}, true, platformID)

	plugins, pw := installedPlugins(dir)
	warns = append(warns, pw...)
	for _, p := range plugins {
		items, warns = appendAgentsFromDir(items, warns,
			filepath.Join(p.InstallPath, "agents"),
			p.Name+":", "plugin:"+p.FullName, p.InstalledAt, false, platformID)
	}
	if main := mainAgents(dir); len(main) > 0 {
		for i := range items {
			items[i].MainAgent = main[strings.ToLower(items[i].Name)] || items[i].FrontmatterName != "" && main[strings.ToLower(items[i].FrontmatterName)]
		}
	}
	return items, warns
}

// mainAgents names the agents the settings run the session itself as
// (`"agent": "name"`, what `claude --agent name` does for one run). Such an
// agent is the session, and no transcript records it as a use.
func mainAgents(dir string) map[string]bool {
	main := map[string]bool{}
	for _, base := range []string{"settings.json", "settings.local.json"} {
		b, err := readCapped(filepath.Join(dir, base))
		if err != nil {
			continue
		}
		var s struct {
			Agent string `json:"agent"`
		}
		if json.Unmarshal(b, &s) == nil && s.Agent != "" {
			main[strings.ToLower(s.Agent)] = true
		}
	}
	return main
}

func appendAgentsFromDir(items []Item, warns []Warning, dir, namePrefix, source string, installedAt time.Time, removable bool, platformID string) ([]Item, []Warning) {
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
		if _, ok := resolveWithin(dir, path); !ok {
			continue
		}
		b, err := readCapped(path)
		if err != nil {
			warns = append(warns, Warning{Path: path, Msg: err.Error()})
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".md")
		// The subagent_type recorded in transcripts is the file name (plus any
		// plugin prefix); by convention it equals the stem. A differing frontmatter
		// name stays the key's alias: it can fire under either.
		fmName, desc, bodyChars := parseFrontmatter(b)
		key := namePrefix + stem
		items = append(items, Item{
			Category:        CatAgent,
			Name:            key,
			Platform:        platformID,
			Source:          source,
			Path:            path,
			FrontmatterName: otherName(fmName, stem),
			Description:     desc,
			DescChars:       len(key) + len(desc),
			BodyChars:       bodyChars,
			InstalledAt:     datedOr(installedAt, path),
			Removable:       removable,
			ToolSurface:     toolSurface(b, "tools", "allowed-tools"),
			Preloads:        frontmatterList(b, "skills"),
		})
	}
	return items, warns
}

// datedOr is the install time a plugin records, or for an item of your own,
// which has none, its file's last change. An edit counts as new: that errs
// toward REVIEW(grace), never toward REAP.
func datedOr(installedAt time.Time, path string) time.Time {
	if !installedAt.IsZero() {
		return installedAt
	}
	if info, err := os.Stat(path); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}
