package reinvest

import (
	"path/filepath"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/report"

	"os"
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

// A relative CLAUDE_CONFIG_DIR is a folder like any other. Used as typed it
// broke every swap: the link to a written skill pointed nowhere, and the mute
// step, run from another folder, found no Claude folder at all.
func TestConfigDir_a_relative_folder_is_read_as_an_absolute_one(t *testing.T) {
	home := tempHome(t)
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	t.Chdir(home)
	t.Setenv("CLAUDE_CONFIG_DIR", "cfg")
	if got, want := claudeDir(), filepath.Join(home, "cfg"); got != want {
		t.Fatalf("claudeDir() = %s, want %s", got, want)
	}
	c := candidate{Kind: kindSkill, Name: "w", Slug: "w", Created: true, Doc: writtenDoc}
	if err := create(c); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "cfg", "skills", "w")
	t.Chdir(t.TempDir()) // a step runs from a folder of its own
	if _, err := os.Stat(filepath.Join(link, skillFile)); err != nil {
		t.Errorf("the link written for the skill leads nowhere: %v", err)
	}
	t.Chdir(home) // reinvest itself stays where it was started
	if err := removeWritten(c, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); err == nil {
		t.Error("the link is still there after the skill was taken out")
	}
}

// A link whose folder is gone is still the one create wrote, by what it says:
// it is taken out, not left to block the name for ever.
func TestWritten_a_link_that_leads_nowhere_is_still_ours(t *testing.T) {
	dir := t.TempDir()
	link, target := filepath.Join(dir, "link"), filepath.Join(dir, "gone")
	if err := os.Symlink(target, link); err != nil {
		t.Skip(err)
	}
	if !pointsAt(link, target) {
		t.Error("a link that names the folder is not read as pointing at it")
	}
	if pointsAt(link, filepath.Join(dir, "another")) {
		t.Error("a link that names another folder is read as pointing here")
	}
	// Written another way, a link is ours when it leads to the same folder.
	real, other := filepath.Join(dir, "real"), filepath.Join(dir, "other")
	for _, folder := range []string{real, other} {
		if err := os.Mkdir(folder, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	relative := filepath.Join(dir, "relative")
	if err := os.Symlink("real", relative); err != nil {
		t.Fatal(err)
	}
	if !pointsAt(relative, real) || pointsAt(relative, other) {
		t.Error("a link written as a relative path is not read by where it leads")
	}
}

// Each step runs in a folder of its own and inherits the environment. A
// relative CLAUDE_CONFIG_DIR handed on as it was typed led the mute step
// nowhere, so every swap was rolled back: it is handed on as an absolute path.
func TestConfigDir_steps_inherit_an_absolute_folder(t *testing.T) {
	home := tempHome(t)
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	t.Chdir(home)
	t.Setenv("CLAUDE_CONFIG_DIR", "cfg")
	pinConfigDir()
	if got, want := os.Getenv("CLAUDE_CONFIG_DIR"), filepath.Join(home, "cfg"); got != want {
		t.Errorf("steps would inherit %q, want %q", got, want)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	pinConfigDir()
	if got := os.Getenv("CLAUDE_CONFIG_DIR"); got != "" {
		t.Errorf("an unset folder was set to %q", got)
	}
}
