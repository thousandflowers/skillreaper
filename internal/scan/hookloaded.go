package scan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// skillFileRe finds "skills/<name>/SKILL.md" in a script, whichever way it
// spells the path: a plain string or path.join('skills', 'name', 'SKILL.md').
var skillFileRe = regexp.MustCompile(`skills['"/\\,\s]+([\w.-]+)['"/\\,\s]+SKILL\.md`)

// pluginRootPathRe finds a path under the plugin root in a hook command.
var pluginRootPathRe = regexp.MustCompile(`\$\{?(?:CLAUDE_)?PLUGIN_ROOT\}?/([^"'\s]+)`)

// hookLoadedSkills names the skills a plugin's SessionStart hook injects. Such a
// skill is at work in every session while the transcripts show no invocation.
//
// ponytail: reads the folders holding the hook scripts rather than following
// what they run, because the path often sits in a sibling they import. A script
// there that names a SKILL.md for another purpose over-protects, which errs
// towards KEEP.
func hookLoadedSkills(installPath string) map[string]bool {
	commands := sessionStartCommands(installPath)
	if len(commands) == 0 {
		return nil
	}
	dirs := map[string]bool{"hooks": true}
	var files []string // scripts at the plugin's root: read alone, not with the README beside them
	for _, c := range commands {
		for _, m := range pluginRootPathRe.FindAllStringSubmatch(c, -1) {
			if dir := filepath.Dir(m[1]); dir != "." {
				dirs[dir] = true
			} else {
				files = append(files, m[1])
			}
		}
	}
	loaded := map[string]bool{}
	read := func(path string) {
		b, err := readCapped(path)
		if err != nil {
			return // a command may name a path that is not there; nothing to read
		}
		for _, m := range skillFileRe.FindAllSubmatch(b, -1) {
			loaded[string(m[1])] = true
		}
	}
	for dir := range dirs {
		abs, ok := resolveWithin(installPath, filepath.Join(installPath, dir))
		if !ok {
			continue
		}
		entries, _ := os.ReadDir(abs)
		for _, e := range entries {
			if e.Type().IsRegular() {
				read(filepath.Join(abs, e.Name()))
			}
		}
	}
	for _, f := range files {
		if abs, ok := resolveWithin(installPath, filepath.Join(installPath, f)); ok {
			read(abs)
		}
	}
	return loaded
}

// sessionStartCommands reads the SessionStart hook commands from the two places
// a plugin declares hooks. A file that is missing or malformed declares none.
func sessionStartCommands(installPath string) []string {
	var commands []string
	for _, rel := range []string{"hooks/hooks.json", ".claude-plugin/plugin.json"} {
		b, err := readCapped(filepath.Join(installPath, rel))
		if err != nil {
			continue
		}
		var s settingsHooks
		if json.Unmarshal(b, &s) != nil {
			continue
		}
		for _, g := range s.Hooks["SessionStart"] {
			for _, h := range g.Hooks {
				commands = append(commands, h.Command)
			}
		}
	}
	return commands
}

// hookWordRe finds what could be a path in a hook command.
var hookWordRe = regexp.MustCompile(`[~$\w./{}-]+`)

// userHookLoadedSkills names the skills the person's own SessionStart hooks
// read: named in the command itself, in a script the command names, or in a
// script beside that one in the config folder (a wrapper often calls it).
func userHookLoadedSkills(dir string) map[string]bool {
	loaded := map[string]bool{}
	home, _ := os.UserHomeDir()
	for _, base := range []string{"settings.json", "settings.local.json"} {
		b, err := readCapped(filepath.Join(dir, base))
		if err != nil {
			continue
		}
		var s settingsHooks
		if json.Unmarshal(b, &s) != nil {
			continue
		}
		for _, g := range s.Hooks["SessionStart"] {
			for _, h := range g.Hooks {
				text := h.Command
				for _, word := range hookWordRe.FindAllString(h.Command, -1) {
					text += hookScripts(underHome(word, home, dir), dir)
				}
				for _, m := range skillFileRe.FindAllStringSubmatch(text, -1) {
					loaded[m[1]] = true
				}
			}
		}
	}
	return loaded
}

// hookScripts is the text of the script at path and, when it lies in a folder
// of the config folder, of the scripts beside it. A path that is not absolute
// is the project's, wherever reap happens to run, and is not read.
func hookScripts(path, dir string) string {
	if !filepath.IsAbs(path) {
		return ""
	}
	script, err := readCapped(path)
	if err != nil {
		return ""
	}
	text := "\n" + string(script)
	folder := filepath.Dir(path)
	if rel, err := filepath.Rel(dir, folder); err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return text // outside the config folder, or the folder itself: the script alone
	}
	entries, _ := os.ReadDir(folder)
	for _, e := range entries {
		if e.Type().IsRegular() {
			if b, err := readCapped(filepath.Join(folder, e.Name())); err == nil {
				text += "\n" + string(b)
			}
		}
	}
	return text
}

// underHome writes out the folders a hook command names by a variable: the
// home as ~ or $HOME, the config folder as $CLAUDE_CONFIG_DIR.
func underHome(word, home, dir string) string {
	for short, full := range map[string]string{
		"~/": home, "$HOME/": home, "${HOME}/": home,
		"$CLAUDE_CONFIG_DIR/": dir, "${CLAUDE_CONFIG_DIR}/": dir,
	} {
		if rest, ok := strings.CutPrefix(word, short); ok {
			return filepath.Join(full, rest)
		}
	}
	return word
}
