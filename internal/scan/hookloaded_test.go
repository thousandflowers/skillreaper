package scan

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func installPlugin(t *testing.T, home, name string, files map[string]string) {
	t.Helper()
	root := filepath.Join(home, "plugins", "cache", "mkt", name, "1.0.0")
	for rel, body := range files {
		mustWrite(t, filepath.Join(root, rel), body)
	}
	b, err := json.Marshal(map[string]any{"version": 2, "plugins": map[string]any{
		name + "@mkt": []map[string]string{{"installPath": root}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(home, "plugins", "installed_plugins.json"), string(b))
}

const skillFile = "---\nname: x\ndescription: d\n---\nbody"

func hookLoadedByName(t *testing.T, home string) map[string]bool {
	t.Helper()
	items, _ := ScanSkills(home, "claude-code")
	out := map[string]bool{}
	for _, it := range items {
		out[it.Name] = it.HookLoaded
	}
	return out
}

// The shapes below are the real ones: a sibling script holding the path, a
// script in a folder named only by plugin.json, and an extensionless shell file.
func TestHookLoadedSkills(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  map[string]bool
	}{
		{
			name: "js path.join in a sibling of the hook script",
			files: map[string]string{
				"hooks/hooks.json":       `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"node ${CLAUDE_PLUGIN_ROOT}/hooks/activate.js"}]}]}}`,
				"hooks/activate.js":      `require('./instructions')`,
				"hooks/instructions.js":  `path.join(__dirname, '..', 'skills', 'always', 'SKILL.md')`,
				"skills/always/SKILL.md": skillFile,
				"skills/manual/SKILL.md": skillFile,
			},
			want: map[string]bool{"p:always": true, "p:manual": false},
		},
		{
			name: "hook declared in plugin.json, script outside hooks/",
			files: map[string]string{
				".claude-plugin/plugin.json": `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"node \"${CLAUDE_PLUGIN_ROOT}/src/hooks/a.js\""}]}]}}`,
				"src/hooks/a.js":             `path.join(__dirname, '..', 'skills', 'always', 'SKILL.md')`,
				"skills/always/SKILL.md":     skillFile,
			},
			want: map[string]bool{"p:always": true},
		},
		{
			name: "shell script with a plain path",
			files: map[string]string{
				"hooks/hooks.json":       `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/hooks/run session-start"}]}]}}`,
				"hooks/session-start":    `cat "${PLUGIN_ROOT}/skills/always/SKILL.md"`,
				"skills/always/SKILL.md": skillFile,
			},
			want: map[string]bool{"p:always": true},
		},
		{
			name: "same script, but hooked on another event",
			files: map[string]string{
				"hooks/hooks.json":       `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/hooks/a.js"}]}]}}`,
				"hooks/a.js":             `skills/always/SKILL.md`,
				"skills/always/SKILL.md": skillFile,
			},
			want: map[string]bool{"p:always": false},
		},
		{
			name: "session hook that loads no skill protects none",
			files: map[string]string{
				"hooks/hooks.json":      `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/hooks/a.js"}]}]}}`,
				"hooks/a.js":            `console.log('hello')`,
				"skills/other/SKILL.md": skillFile,
			},
			want: map[string]bool{"p:other": false},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			installPlugin(t, home, "p", c.files)
			got := hookLoadedByName(t, home)
			for name, want := range c.want {
				if got[name] != want {
					t.Errorf("%s hook-loaded = %v, want %v", name, got[name], want)
				}
			}
		})
	}
}
