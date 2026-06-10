package scan

import (
	"os"
	"path/filepath"
	"time"
)

// ScanSkills inventories personal skills (<dir>/skills/<name>/SKILL.md)
// and skills shipped by installed plugins. The Name field is the
// invocation key as it appears in transcripts: bare for personal
// skills, "plugin:skill" for plugin skills.
func ScanSkills(dir, platformID string) ([]Item, []Warning) {
	var items []Item
	var warns []Warning

	items, warns = appendSkillsFromDir(items, warns,
		filepath.Join(dir, "skills"), "", "personal", time.Time{}, true, platformID)

	plugins, pw := installedPlugins(dir)
	warns = append(warns, pw...)
	for _, p := range plugins {
		items, warns = appendSkillsFromDir(items, warns,
			filepath.Join(p.InstallPath, "skills"),
			p.Name+":", "plugin:"+p.FullName, p.InstalledAt, false, platformID)
	}
	return items, warns
}

// appendSkillsFromDir scans one skills directory where each child
// directory holds a SKILL.md. namePrefix is "" for personal skills or
// "<plugin>:" for plugin-provided ones.
func appendSkillsFromDir(items []Item, warns []Warning, dir, namePrefix, source string, installedAt time.Time, removable bool, platformID string) ([]Item, []Warning) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			warns = append(warns, Warning{Path: dir, Msg: err.Error()})
		}
		return items, warns
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillPath := filepath.Join(dir, e.Name(), "SKILL.md")
		b, err := os.ReadFile(skillPath)
		if err != nil {
			continue // directory without SKILL.md is not a skill
		}
		name, desc, bodyChars := parseFrontmatter(b)
		if name == "" {
			name = e.Name()
		}
		key := namePrefix + e.Name()
		items = append(items, Item{
			Category:    CatSkill,
			Name:        key,
			Platform:    platformID,
			Source:      source,
			Path:        skillPath,
			Description: desc,
			DescChars:   len(key) + len(desc),
			BodyChars:   bodyChars,
			InstalledAt: installedAt,
			Removable:   removable,
		})
	}
	return items, warns
}
