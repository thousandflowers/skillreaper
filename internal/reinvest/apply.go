package reinvest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	stepTimeout  = 120 * time.Second // `skills add` on a repo that is gone clones forever, even with git prompts off
	reapName     = "reap"            // stands for the running binary in a step (see reapCommand)
	outputTail   = 200
	changedShown = 3
)

// Install for Claude Code only: without -a, `skills add` installs for every agent it knows (~20), and exits 0
// even when one of them fails.
// Pinned: `npx -y skills` would run whatever was published last, as the user.
var (
	skillsCLI   = []string{"npx", "-y", "skills@1.7.6"}
	skillsFlags = []string{"-g", "-y", "-a", "claude-code"}
)

func joined(parts ...[]string) []string {
	var out []string
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// installCommand is the command that installs a candidate. ok is false when
// it needs manual setup (keys, env vars) or when what the registry gave
// cannot safely go on a command line.
func installCommand(c candidate) ([]string, bool) {
	if c.Kind == kindSkill {
		if !sourceRE.matches(c.Source) || !slugRE.matches(c.Slug) {
			return nil, false
		}
		return joined(skillsCLI, []string{"add", c.Source, "-s", c.Slug}, skillsFlags), true
	}
	name := mcpName(c)
	target := targetOf(c.Server)
	if !target.found() || !slugRE.matches(name) {
		return nil, false
	}
	if target.remote != nil {
		kind, _ := target.remote.str("type")
		address, _ := target.remote.str("url")
		return []string{"claude", "mcp", "add", "-s", "user", "--transport", transports[kind], name, address}, true
	}
	if truthy(target.pkg.val("environmentVariables")) {
		return nil, false
	}
	kind, _ := target.pkg.str("registryType")
	id, _ := target.pkg.str("identifier")
	version, _ := target.pkg.str("version")
	spec := packageSpecs[kind]
	return joined([]string{"claude", "mcp", "add", "-s", "user", name, "--"}, spec.runner, []string{spec.pin(id, version)}), true
}

func uninstallCommand(c candidate) []string {
	if c.Kind == kindSkill {
		return joined(skillsCLI, []string{"remove", c.Slug}, skillsFlags)
	}
	return []string{"claude", "mcp", "remove", "-s", "user", mcpName(c)}
}

func skillsDir() string { return filepath.Join(homeDir(), ".claude", "skills") }

// writtenDir is where written skills live; Claude Code loads them through a
// link in skillsDir(), and reap can tell them apart by where the link points.
func writtenDir() string { return filepath.Join(homeDir(), ".claude", "skills-reinvest") }

// present reports whether anything is at path, a dangling link included. An
// error other than "not there" is returned: not knowing is not "free to use".
func present(path string) (bool, error) {
	_, err := os.Lstat(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	}
	return false, err
}

// create writes a written skill and links it where Claude Code loads skills.
// It never replaces anything: a skill or link of that name already there is
// the user's.
func create(c candidate) error {
	link, home := filepath.Join(skillsDir(), c.Slug), filepath.Join(writtenDir(), c.Slug)
	if taken, err := present(link); err != nil {
		return err
	} else if taken {
		return fmt.Errorf("a skill named %s already exists in %s", c.Slug, skillsDir())
	}
	if taken, err := present(home); err != nil {
		return err
	} else if taken {
		return fmt.Errorf("%s already exists", home)
	}
	if err := os.MkdirAll(writtenDir(), sharedDir); err != nil {
		return err
	}
	if err := os.Mkdir(home, sharedDir); err != nil {
		return err // not made here, so nothing here to take back
	}
	err := os.WriteFile(filepath.Join(home, skillFile), []byte(c.Doc), sharedFile)
	if err == nil {
		err = os.MkdirAll(skillsDir(), sharedDir)
	}
	if err == nil {
		err = os.Symlink(home, link)
	}
	if err != nil {
		if undo := os.RemoveAll(home); undo != nil { // the folder this call made a moment ago
			return fmt.Errorf("%w (and %s could not be removed: %v)", err, home, undo)
		}
		return err
	}
	return nil
}

// pointsAt reports whether link is a symlink that resolves to target.
func pointsAt(link, target string) bool {
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		return false
	}
	want, err := filepath.EvalSymlinks(target)
	return err == nil && resolved == want
}

// removeWritten undoes create(): the link, then the folder, only when the
// link points at the folder reinvest wrote.
func removeWritten(c candidate) error {
	link, home := filepath.Join(skillsDir(), c.Slug), filepath.Join(writtenDir(), c.Slug)
	if pointsAt(link, home) {
		if err := os.Remove(link); err != nil {
			return err
		}
	}
	if _, err := os.Lstat(home); err != nil {
		return err
	}
	return os.RemoveAll(home)
}

// isInstalled checks the skill is where Claude Code reads it: `skills add`
// exits 0 when some target failed.
func isInstalled(c candidate) bool {
	if c.Kind != kindSkill {
		return true // `claude mcp add` exits 1 on failure
	}
	info, err := os.Stat(filepath.Join(skillsDir(), c.Slug, skillFile))
	return err == nil && info.Mode().IsRegular()
}

// filesUnder lists the files below root, as paths relative to it with "/".
// Linked files count; linked folders are not entered.
func filesUnder(root, rel string) []string {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return nil
	}
	var out []string
	for _, entry := range entries {
		child := entry.Name()
		if rel != "" {
			child = rel + "/" + entry.Name()
		}
		if entry.IsDir() {
			out = append(out, filesUnder(root, child)...)
			continue
		}
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(child))); err == nil && info.Mode().IsRegular() {
			out = append(out, child)
		}
	}
	return out
}

// uncheckedFiles lists the files now in the skill's folder that are not, byte
// for byte, what the verifier read. `skills add` takes whatever the repo
// holds at that moment, so this is what ties the install to the check.
func uncheckedFiles(c candidate) []string {
	root := filepath.Join(skillsDir(), c.Slug)
	files := filesUnder(root, "")
	sort.Slice(files, func(i, j int) bool { return lessByParts(files[i], files[j]) })
	var out []string
	for _, rel := range files {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil || !utf8.Valid(raw) {
			out = append(out, rel)
			continue
		}
		text := string(raw)
		same := strings.Contains(c.Doc, fileMark(rel)+"\n"+text)
		if rel == skillFile {
			same = c.Doc == text || strings.HasPrefix(c.Doc, text+"\n")
		}
		if !same {
			out = append(out, rel)
		}
	}
	return out
}

const reapVariable = "REAP"

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode().Perm()&0o111 != 0
}

// reapCommand is the reap that reinvest calls: the running binary, unless the
// REAP environment variable names another. As in experiments/menu-prototype,
// a value that is a program's path is taken whole (it may hold spaces), and
// anything else is a command that may carry arguments.
func reapCommand() ([]string, error) {
	if value := os.Getenv(reapVariable); pyStrip(value) != "" {
		if isExecutableFile(value) {
			return []string{value}, nil
		}
		return pyFields(value), nil
	}
	binary, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return []string{binary}, nil
}

// runInEmptyDir runs one step from an empty folder, so no folder the user
// happens to be in can supply the program (npx prefers a local
// node_modules/.bin). nil on success, else why it failed.
func (a *app) runInEmptyDir(cmd []string) error {
	argv := append([]string{}, cmd...)
	if argv[0] == reapName {
		reap, err := a.reapCommand()
		if err != nil {
			return err
		}
		argv = append(reap, argv[1:]...)
	}
	dir, err := os.MkdirTemp("", "reinvest-step-")
	if err != nil {
		return err
	}
	res := a.runProcess(procSpec{argv: argv, env: []string{"GIT_TERMINAL_PROMPT=0"}, dir: dir, timeout: stepTimeout})
	if err := os.RemoveAll(dir); err != nil { // the folder made two lines up
		a.logf("step: %s not removed: %v", dir, err)
	}
	switch {
	case res.timedOut:
		return fmt.Errorf("no answer in %d s", int(stepTimeout.Seconds()))
	case res.err != nil:
		return res.err
	case res.code == 0:
		return nil
	}
	said := pyStrip(res.stderr)
	if said == "" {
		said = pyStrip(res.stdout)
	}
	if said = lastRunes(said, outputTail); said != "" {
		return errors.New(said)
	}
	return fmt.Errorf("exit %d", res.code)
}

// rollBack undoes a swap whose mute failed: unmutes what it muted and takes
// the new skill or server out again. It returns what could not be undone.
func (a *app) rollBack(c candidate, muted []string) []string {
	var left []string
	for _, name := range muted {
		if a.runStep([]string{reapName, "unmute", name}) != nil {
			left = append(left, name)
		}
	}
	var err error
	switch {
	case c.Installed: // installed by an earlier run: it stays, only this run's mutes are undone
	case c.Created:
		err = removeWritten(c)
	default:
		err = a.runStep(uninstallCommand(c))
	}
	if err != nil {
		left = append(left, c.Name+" (installed)")
	}
	return left
}

// install puts the candidate in place. The lines are what to tell the
// person; stop is true when the swap ends there and nothing is muted for it.
func (a *app) install(s swap) (lines []string, stop bool) {
	c := s.cand()
	fail := func(why string) ([]string, bool) {
		return []string{"  ✗  " + c.Name + ": " + why + " · nothing muted"}, true
	}
	switch {
	case c.Created:
		if err := create(c); err != nil {
			return fail(err.Error())
		}
		return []string{"  ✓  created " + c.Name + " in " + filepath.Join(writtenDir(), c.Slug)}, false
	case c.Installed:
		return nil, false
	}
	command, ok := installCommand(c)
	if !ok {
		return []string{"  ✗  " + c.Name + ": needs manual setup, nothing muted · " + c.ID}, true
	}
	if c.Kind == kindSkill { // the user's: never replaced, never rolled back
		if taken, err := present(filepath.Join(skillsDir(), c.Slug)); err != nil {
			return fail(err.Error())
		} else if taken {
			return fail("a skill named " + c.Slug + " already exists in " + skillsDir())
		}
	}
	if err := a.runStep(command); err != nil {
		return fail(err.Error())
	}
	if !a.isInstalled(c) {
		return fail("installed, but not for Claude Code")
	}
	if c.Kind == kindSkill {
		if changed := a.unchecked(c); len(changed) > 0 {
			state := "removed again"
			if a.runStep(uninstallCommand(c)) != nil {
				state = "still installed, remove it by hand"
			}
			shown := strings.Join(changed[:min(changedShown, len(changed))], ", ")
			return []string{"  ✗  " + c.Name + ": not what was checked (" + shown + ") · " + state + " · nothing muted"}, true
		}
	}
	lines = []string{"  ✓  installed " + c.Name}
	if c.Kind == kindMCP && len(s.Mute) == 0 {
		if err := a.remember(c); err != nil {
			lines = append(lines, "  ✗  not remembered for a later mute: "+err.Error())
		}
		return append(lines, "  ·  nothing muted yet: reinvest offers the mute once reap has weighed it"), true
	}
	return lines, false
}

// applyOne installs, then mutes. All or nothing: a failed mute unmutes what
// this swap muted and uninstalls (a server installed by an earlier run stays:
// only this run's mutes are undone).
func (a *app) applyOne(s swap) []string {
	c := s.cand()
	log, stop := a.install(s)
	if stop {
		return log
	}
	var muted []string
	for _, x := range s.Mute {
		// --yes: the swap was accepted in reinvest's own question, with this mute
		// listed in it. reap would otherwise ask again on a terminal nobody sees.
		err := a.runStep([]string{reapName, "--yes", "mute", x.Name})
		if err == nil {
			muted = append(muted, x.Name)
			continue
		}
		outcome := "  ↺  rolled back: nothing changed"
		if left := a.rollBack(c, muted); len(left) > 0 {
			outcome = "  ↺  rolled back, but still changed: " + strings.Join(left, ", ")
		}
		return append(log, "  ✗  reap mute "+x.Name+": "+err.Error(), outcome)
	}
	if c.Installed {
		if err := a.forgetServer(c); err != nil {
			log = append(log, "  ✗  "+c.Name+" not forgotten: "+err.Error())
		}
	}
	for _, name := range muted {
		log = append(log, "  ✓  muted "+name+" (reap unmute "+name+" undoes)")
	}
	return log
}

// apply carries out the chosen swaps, or with dryRun says what it would run.
func (a *app) apply(chosen []swap, dryRun bool) []string {
	var log []string
	for _, s := range chosen {
		if !dryRun {
			log = append(log, a.applyOne(s)...)
			continue
		}
		log = append(log, dryRunLines(s)...)
	}
	return log
}

func dryRunLines(s swap) []string {
	c := s.cand()
	var lines []string
	switch {
	case c.Created:
		lines = append(lines, "  ·  write "+filepath.Join(writtenDir(), c.Slug, skillFile)+", link it as "+filepath.Join(skillsDir(), c.Slug))
	case !c.Installed:
		command, ok := installCommand(c)
		if !ok {
			return []string{"  ✗  " + c.Name + ": needs manual setup, nothing muted · " + c.ID}
		}
		lines = append(lines, "  ·  "+strings.Join(command, " "))
	}
	for _, x := range s.Mute {
		lines = append(lines, "  ·  reap mute "+x.Name)
	}
	return lines
}
