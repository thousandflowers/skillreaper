package scan

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// settingsHooks mirrors the hooks section of settings.json:
// {"hooks":{"Event":[{"matcher":...,"hooks":[{"type":"command","command":...}]}]}}.
type settingsHooks struct {
	Hooks map[string][]struct {
		Hooks []struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"hooks"`
	} `json:"hooks"`
}

// ScanHooks inventories hook commands from settings.json and
// settings.local.json. Hooks are report-only in this version
// (Removable false): their cost varies per event and removal is a
// settings edit the user should review.
func ScanHooks(dir, platformID string) ([]Item, []Warning) {
	var items []Item
	var warns []Warning
	for _, base := range []string{"settings.json", "settings.local.json"} {
		path := filepath.Join(dir, base)
		b, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				warns = append(warns, Warning{Path: path, Msg: err.Error()})
			}
			continue
		}
		var s settingsHooks
		if err := json.Unmarshal(b, &s); err != nil {
			warns = append(warns, Warning{Path: path, Msg: "unreadable JSON: " + err.Error()})
			continue
		}
		for event, groups := range s.Hooks {
			n := 0
			for _, g := range groups {
				for _, h := range g.Hooks {
					items = append(items, Item{
						Category:    CatHook,
						Name:        fmt.Sprintf("%s#%d", event, n),
						Platform:    platformID,
						Source:      base,
						Path:        path,
						Description: h.Command,
						Removable:   false,
					})
					n++
				}
			}
		}
	}
	return items, warns
}
