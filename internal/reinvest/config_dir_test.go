package reinvest

import (
	"path/filepath"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/report"
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

// A second Claude config folder is a second stack: its sessions, its groups
// and its proposals are its own. One memory for all of them sent the prompts
// of one profile to Claude on behalf of another, and proposed skills from them.
func TestConfigDir_each_folder_keeps_its_own_memory_and_cache(t *testing.T) {
	home := tempHome(t)
	plain := newApp(nil, nil, nil)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	named := newApp(nil, nil, nil)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "profile-a"))
	first := newApp(nil, nil, nil)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "profile-b"))
	second := newApp(nil, nil, nil)

	// The usual folder keeps the places it always had, named or not.
	if plain.stateDir != defaultStateDir() || plain.cacheDir != defaultCacheDir() ||
		named.stateDir != plain.stateDir || named.cacheDir != plain.cacheDir {
		t.Errorf("the usual folder moved: %s %s / %s %s", plain.stateDir, plain.cacheDir, named.stateDir, named.cacheDir)
	}
	for _, other := range []*app{plain, second} {
		if first.stateDir == other.stateDir || first.cacheDir == other.cacheDir {
			t.Errorf("two folders share %s or %s", first.stateDir, first.cacheDir)
		}
	}
	if _, err := first.rememberSessions(sess("mine")); err != nil {
		t.Fatal(err)
	}
	got, err := second.rememberSessions(nil)
	if err != nil || len(got) != 0 {
		t.Errorf("the second folder remembers %d sessions of the first (%v)", len(got), err)
	}
	// reap's hint reads the proposals of the folder it reports on.
	if want := second.cachePath(swapsFile); report.ReinvestPath(filepath.Join(home, "profile-b")) != want {
		t.Errorf("reap looks in %s, reinvest writes %s", report.ReinvestPath(filepath.Join(home, "profile-b")), want)
	}
	if want := plain.cachePath(swapsFile); report.ReinvestPath(filepath.Join(home, ".claude")) != want || report.ReinvestPath("") != want {
		t.Errorf("for the usual folder reap looks in %s, reinvest writes %s", report.ReinvestPath(""), want)
	}
}
