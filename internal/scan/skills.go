package scan

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/thousandflowers/skillreaper/internal/safepath"
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
		start := len(items)
		items, warns = appendSkillsFromDir(items, warns,
			filepath.Join(p.InstallPath, "skills"),
			p.Name+":", "plugin:"+p.FullName, p.InstalledAt, false, platformID)
		loaded := hookLoadedSkills(p.InstallPath)
		for i := start; i < len(items); i++ {
			items[i].HookLoaded = loaded[strings.TrimPrefix(items[i].Name, p.Name+":")]
		}
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
		skillPath := filepath.Join(dir, e.Name(), "SKILL.md")
		readRoot, linkTarget := dir, ""
		if e.Type()&os.ModeSymlink != 0 {
			// Personal skills only: Claude Code loads a skill linked in from a
			// shared skills folder, so reap has to weigh it too.
			t, ok := safepath.LinkedSkillDir(filepath.Dir(dir), filepath.Join(dir, e.Name()))
			if namePrefix != "" || !ok {
				continue
			}
			readRoot, linkTarget = t, t
		} else if !e.IsDir() {
			continue
		}
		readPath := filepath.Join(readRoot, "SKILL.md")
		if readRoot == dir {
			readPath = skillPath
		}
		if _, ok := resolveWithin(readRoot, readPath); !ok {
			continue
		}
		b, err := readCapped(readPath)
		if err != nil {
			continue // directory without SKILL.md is not a skill
		}
		// The invocation key is the directory name (plus any plugin prefix) —
		// that is how the skill is addressed in transcripts. The frontmatter
		// name is metadata and intentionally not used as the key.
		_, desc, bodyChars := parseFrontmatter(b)
		key := namePrefix + e.Name()
		descChars := len(key) + len(desc)
		// Run only by hand: Claude Code leaves it out of the listing it injects.
		if v, _ := frontmatterValue(b, "disable-model-invocation"); strings.EqualFold(v, "true") {
			descChars = 0
		}
		items = append(items, Item{
			Category:    CatSkill,
			Name:        key,
			Platform:    platformID,
			Source:      source,
			Path:        skillPath,
			Description: desc,
			DescChars:   descChars,
			BodyChars:   bodyChars,
			InstalledAt: installedAt,
			Removable:   removable,
			ToolSurface: toolSurface(b, "allowed-tools"),
			LinkTarget:  linkTarget,
		})
	}
	return items, warns
}
