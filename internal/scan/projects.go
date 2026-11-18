package scan

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ProjectLoaded names the skills a project's own agents preload (`skills:` in
// .claude/agents/*.md) and the ones its own SessionStart hooks read. They are
// at work in that project's sessions with no use of their own on record, like
// the ones the user's folder loads. unread says why one of those files could
// not be read, when one could not: it may name any skill.
func ProjectLoaded(project, config string) (byAgents, byHooks map[string]bool, unread string) {
	byAgents = map[string]bool{}
	dir := filepath.Join(project, ".claude")
	entries, _ := os.ReadDir(filepath.Join(dir, "agents"))
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		path := filepath.Join(dir, "agents", e.Name())
		b, err := readCapped(path)
		if err != nil {
			// Too large to read, say: what can be opened and is not read.
			if info, statErr := os.Stat(path); statErr == nil && info.Mode().IsRegular() && unread == "" {
				unread = fmt.Sprintf("%s could not be read (%v), and may say that it is at work", SafeText(path), err)
			}
			continue
		}
		for _, name := range frontmatterList(b, "skills") {
			byAgents[name] = true
		}
	}
	return byAgents, hookLoadedFrom(dir, config, project), cmp.Or(UnreadProtection(dir), unread)
}

// UnreadProtection says why a file of a config folder that tells what is at
// work could not be read: its settings (the session's agent, the hooks that
// load skills) or an agent's file (the skills it preloads). Empty when all
// could be, or are not there. What is not a regular file (a pipe, a folder)
// is passed by without being opened: opening a pipe never returns.
func UnreadProtection(dir string) string {
	paths := []string{filepath.Join(dir, "settings.json"), filepath.Join(dir, "settings.local.json")}
	agents, _ := filepath.Glob(filepath.Join(dir, "agents", "*.md"))
	unread := func(path string, err error) string {
		return fmt.Sprintf("%s could not be read (%v), and may say that it is at work", SafeText(path), err)
	}
	// A folder reap may not enter or list hides files that may be there.
	if _, err := os.ReadDir(filepath.Join(dir, "agents")); errors.Is(err, fs.ErrPermission) {
		return unread(filepath.Join(dir, "agents"), err)
	}
	for _, path := range append(paths, agents...) {
		info, err := os.Stat(path)
		if errors.Is(err, fs.ErrPermission) {
			return unread(path, err)
		}
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			return unread(path, err)
		}
		_ = f.Close()
	}
	return ""
}
