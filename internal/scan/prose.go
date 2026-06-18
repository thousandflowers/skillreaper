package scan

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/thousandflowers/skillreaper/internal/safepath"
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
	realRules := ""
	realRoot, rootErr := filepath.EvalSymlinks(dir)
	if info, err := os.Lstat(rulesDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			warns = append(warns, Warning{Path: rulesDir, Msg: "rules directory is not a regular directory; skipping rules"})
		} else if rootErr != nil || realRoot == "" {
			warns = append(warns, Warning{Path: rulesDir, Msg: "scan root could not be resolved; skipping rules"})
		} else if realRules, err = filepath.EvalSymlinks(rulesDir); err != nil || !safepath.WithinDir(realRoot, realRules) {
			warns = append(warns, Warning{Path: rulesDir, Msg: "rules directory resolves outside scan root; skipping rules"})
			realRules = ""
		}
	} else if !os.IsNotExist(err) {
		warns = append(warns, Warning{Path: rulesDir, Msg: err.Error()})
	}
	err := filepath.WalkDir(rulesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") || realRules == "" {
			return nil
		}
		// Skip symlinks that resolve outside the rules tree, so a planted link
		// cannot make reap stat and surface a file elsewhere on disk.
		real, err := filepath.EvalSymlinks(path)
		if err != nil || !safepath.WithinDir(realRules, real) {
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
