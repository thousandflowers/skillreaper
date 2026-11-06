package scan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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
	for _, c := range commands {
		for _, m := range pluginRootPathRe.FindAllStringSubmatch(c, -1) {
			dirs[filepath.Dir(m[1])] = true
		}
	}
	loaded := map[string]bool{}
	for dir := range dirs {
		abs, ok := resolveWithin(installPath, filepath.Join(installPath, dir))
		if !ok {
			continue
		}
		entries, err := os.ReadDir(abs)
		if err != nil {
			continue // a command may name a path that is not there; nothing to read
		}
		for _, e := range entries {
			if !e.Type().IsRegular() {
				continue
			}
			b, err := readCapped(filepath.Join(abs, e.Name()))
			if err != nil {
				continue
			}
			for _, m := range skillFileRe.FindAllSubmatch(b, -1) {
				loaded[string(m[1])] = true
			}
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
