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
	return ScanSkillsIn(dir, platformID, false)
}

// ScanSkillsIn is ScanSkills for a platform that may file its skills in category
// folders (<dir>/skills/<category>/<name>/SKILL.md), which is how Hermes does it.
func ScanSkillsIn(dir, platformID string, nested bool) ([]Item, []Warning) {
	var items []Item
	var warns []Warning

	root := filepath.Join(dir, "skills")
	items, warns = appendSkillsFromDir(items, warns, root, "", "personal", time.Time{}, true, platformID)
	if nested {
		items, warns = appendCategoryFolders(items, warns, root, platformID)
	}
	if nested && len(items) == 0 && hasSubfolder(root) {
		warns = append(warns, Warning{Path: root, Advisory: true,
			Msg: "this skills folder has subfolders but no SKILL.md was found directly inside them, so nothing here was counted"})
	}

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

// ScanDeclaredSkills reads a further skills folder a platform declares, such as a
// folder shared between tools. Those skills are reported but not offered: their
// folder is not under the platform's root, and other tools may be loading them.
func ScanDeclaredSkills(dir, platformID string) ([]Item, []Warning) {
	items, warns := appendSkillsFromDir(nil, nil, dir, "", "personal", time.Time{}, false, platformID)
	return items, warns
}

// appendCategoryFolders scans each real subfolder that is not itself a skill.
func appendCategoryFolders(items []Item, warns []Warning, root, platformID string) ([]Item, []Warning) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return items, warns
	}
	for _, e := range entries {
		cat := filepath.Join(root, e.Name())
		if _, err := os.Stat(filepath.Join(cat, "SKILL.md")); !e.IsDir() || err == nil {
			continue
		}
		items, warns = appendSkillsFromDir(items, warns, cat, "", "personal", time.Time{}, true, platformID)
	}
	return items, warns
}

func hasSubfolder(root string) bool {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			return true
		}
	}
	return false
}

// otherName returns name when it says something key does not.
func otherName(name, key string) string {
	if name == key {
		return ""
	}
	return name
}

// appendSkillsFromDir scans one skills directory where each child
// directory holds a SKILL.md. namePrefix is "" for personal skills or
// "<plugin>:" for plugin-provided ones.
func appendSkillsFromDir(items []Item, warns []Warning, dir, namePrefix, source string, installedAt time.Time, removable bool, platformID string) ([]Item, []Warning) {
	// A skills folder that is itself a link (a dotfiles layout) still loads in the
	// tool, so its skills are weighed. reap only ever reads through it.
	rootTarget := ""
	if info, err := os.Lstat(dir); err == nil && info.Mode()&os.ModeSymlink != 0 {
		t, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return items, append(warns, brokenLink(dir))
		}
		rootTarget = t
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			warns = append(warns, Warning{Path: dir, Msg: err.Error()})
		}
		return items, warns
	}
	for _, e := range entries {
		skillPath := filepath.Join(dir, e.Name(), "SKILL.md")
		readPath, linkTarget, viaLink := skillPath, "", rootTarget
		if viaLink != "" {
			readPath = filepath.Join(rootTarget, e.Name(), "SKILL.md")
		}
		if e.Type()&os.ModeSymlink != 0 {
			link := filepath.Join(dir, e.Name())
			if t, ok := safepath.LinkedSkillDir(filepath.Dir(dir), link); ok && namePrefix == "" && viaLink == "" {
				// Claude Code loads a skill linked in from a shared skills folder,
				// and that link is one reap may move.
				readPath, linkTarget = filepath.Join(t, "SKILL.md"), t
			} else {
				t, err := filepath.EvalSymlinks(link)
				if err != nil {
					warns = append(warns, brokenLink(link))
					continue
				}
				readPath, viaLink = filepath.Join(t, "SKILL.md"), t
			}
		} else if !e.IsDir() {
			continue
		}
		// A SKILL.md that leads out of its folder is a link like the others.
		if linkTarget == "" && viaLink == "" {
			if _, err := os.Lstat(readPath); err != nil {
				continue // a plain folder with no SKILL.md is not a skill, and not a broken link
			}
			if _, ok := resolveWithin(dir, readPath); !ok {
				t, err := filepath.EvalSymlinks(readPath)
				if err != nil {
					warns = append(warns, brokenLink(readPath))
					continue
				}
				viaLink = t
			}
		}
		b, err := readCapped(readPath)
		if err != nil {
			// A directory without SKILL.md is not a skill. One that cannot be read
			// may be the heaviest one here, so it is not dropped in silence.
			if !os.IsNotExist(err) {
				warns = append(warns, Warning{Path: readPath, Msg: err.Error()})
			}
			continue
		}
		// The invocation key is the directory name (plus any plugin prefix) —
		// that is how the skill is addressed in transcripts. A differing
		// frontmatter name stays the key's alias: it can fire under either.
		fmName, desc, bodyChars := parseFrontmatter(b)
		key := namePrefix + e.Name()
		descChars := len(key) + len(desc)
		// Run only by hand: Claude Code leaves it out of the listing it injects.
		if v, _ := frontmatterValue(b, "disable-model-invocation"); strings.EqualFold(v, "true") {
			descChars = 0
		}
		items = append(items, Item{
			Category:        CatSkill,
			Name:            key,
			Platform:        platformID,
			Source:          source,
			Path:            skillPath,
			FrontmatterName: otherName(fmName, e.Name()),
			Description:     desc,
			DescChars:       descChars,
			BodyChars:       bodyChars,
			InstalledAt:     installedAt,
			Removable:       removable && viaLink == "",
			ToolSurface:     toolSurface(b, "allowed-tools"),
			LinkTarget:      linkTarget,
			ViaLink:         viaLink,
			FolderLink:      rootTarget != "",
		})
	}
	return items, warns
}

// brokenLink says a link was passed over. It cannot be loaded by anything either,
// so it is a note about the folder rather than a failure to read a skill.
func brokenLink(path string) Warning {
	return Warning{Path: path, Advisory: true, Msg: "this link leads nowhere (its target is missing, or it loops), so nothing here was counted"}
}
