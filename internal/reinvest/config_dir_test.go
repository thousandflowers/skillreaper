package reinvest

import (
	"path/filepath"
	"testing"
)

// Claude Code keeps everything under CLAUDE_CONFIG_DIR when it is set, and reap
// reads it there: reinvest reads sessions and writes skills in the same place.
func TestConfigDir_follows_CLAUDE_CONFIG_DIR_as_reap_does(t *testing.T) {
	home := tempHome(t)
	for env, want := range map[string]string{
		"":                         filepath.Join(home, ".claude"),
		filepath.Join(home, "alt"): filepath.Join(home, "alt"),
		"~/elsewhere":              filepath.Join(home, "elsewhere"),
	} {
		t.Setenv("CLAUDE_CONFIG_DIR", env)
		a := newApp(nil, nil, nil)
		if got := []string{a.projectsDir, skillsDir(), writtenDir()}; got[0] != filepath.Join(want, "projects") ||
			got[1] != filepath.Join(want, "skills") || got[2] != filepath.Join(want, "skills-reinvest") {
			t.Errorf("CLAUDE_CONFIG_DIR=%q: %v, want under %s", env, got, want)
		}
	}
}
