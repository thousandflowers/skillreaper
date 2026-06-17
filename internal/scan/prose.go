package scan

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ScanProse inventories always-loaded prose: the global CLAUDE.md,
// every file under ~/.claude/rules/, and the working directory's
// CLAUDE.md. These are injected verbatim into every session, so
// DescChars is the full file size. They are report-only (pruning
// prose means editing it, which stays a human decision).
func ScanProse(dir, cwd, platformID string) ([]Item, []Warning) {
	var items []Item
	var warns []Warning

	addFile := func(path, source string) {
		info, err := os.Stat(path)
		if err != nil {
			return
		}
		items = append(items, Item{
			Category:  CatProse,
			Name:      displayPath(path),
			Platform:  platformID,
			Source:    source,
			Path:      path,
			DescChars: int(info.Size()),
			Removable: false,
		})
	}

	addFile(filepath.Join(dir, "CLAUDE.md"), "global")

	rulesDir := filepath.Join(dir, "rules")
	realRules, _ := filepath.EvalSymlinks(rulesDir) // "" when rulesDir is absent
	err := filepath.WalkDir(rulesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		// Skip symlinks that resolve outside the rules tree, so a planted link
		// cannot make reap stat and surface a file elsewhere on disk.
		real, err := filepath.EvalSymlinks(path)
		if err != nil || (realRules != "" && !withinDir(realRules, real)) {
			return nil
		}
		addFile(path, "rules")
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		warns = append(warns, Warning{Path: rulesDir, Msg: err.Error()})
	}

	if cwd != "" {
		addFile(filepath.Join(cwd, "CLAUDE.md"), "project")
	}
	return items, warns
}

// displayPath shortens a home-relative path for display.
func displayPath(path string) string {
	home, err := os.UserHomeDir()
	if err == nil && strings.HasPrefix(path, home) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}
