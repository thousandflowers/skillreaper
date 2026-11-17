package reinvest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/thousandflowers/skillreaper/internal/scan"
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

func skillsDir() string { return filepath.Join(claudeDir(), "skills") }

// writtenDir is where written skills live; Claude Code loads them through a
// link in skillsDir(), and reap can tell them apart by where the link points.
func writtenDir() string { return filepath.Join(claudeDir(), "skills-reinvest") }

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
	left, err := present(home)
	if err != nil {
		return err
	}
	if left {
		// Left by an earlier run whose link was deleted since. With the same
		// text it is that skill: link it again. Another text is not replaced.
		if was, readErr := os.ReadFile(filepath.Join(home, skillFile)); readErr != nil || string(was) != c.Doc {
			return fmt.Errorf("%s is left from an earlier run, with another text: move it away and run reinvest again", home)
		}
	} else {
		if err := os.MkdirAll(writtenDir(), sharedDir); err != nil {
			return err
		}
		if err := os.Mkdir(home, sharedDir); err != nil {
			return err // not made here, so nothing here to take back
		}
		err = os.WriteFile(filepath.Join(home, skillFile), []byte(c.Doc), sharedFile)
	}
	if err == nil {
		err = os.MkdirAll(skillsDir(), sharedDir)
	}
	if err == nil {
		err = os.Symlink(home, link)
	}
	if err != nil && !left {
		if undo := removeAll(home); undo != nil { // the folder this call made a moment ago
			return fmt.Errorf("%w (and %s could not be removed: %v)", err, home, undo)
		}
	}
	return err
}

// pointsAt reports whether link is a symlink that resolves to target.
func pointsAt(link, target string) bool {
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	if text, err := os.Readlink(link); err == nil && text == target {
		return true // as create wrote it, even when the folder it names is gone
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
func removeWritten(c candidate, linkOnly bool) error {
	link, home := filepath.Join(skillsDir(), c.Slug), filepath.Join(writtenDir(), c.Slug)
	if pointsAt(link, home) {
		if err := os.Remove(link); err != nil {
			return err
		}
	}
	if linkOnly { // the folder was there before this run: it stays as it was
		return nil
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
		if entry.IsDir() { // a real folder: a link to one is an entry like any other
			out = append(out, filesUnder(root, child)...)
			continue
		}
		out = append(out, child) // links and pipes too: uncheckedFiles refuses them
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
		path := filepath.Join(root, filepath.FromSlash(rel))
		if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
			out = append(out, rel) // a link can point anywhere once the check is over
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil || !utf8.Valid(raw) {
			out = append(out, rel)
			continue
		}
		if !checkedWhole(c.Doc, rel, string(raw)) {
			out = append(out, rel)
		}
	}
	// a file the verifier read and the install left out is a difference too:
	// it may be the one that guarded the others
	for _, m := range fileMarkRE.FindAllStringSubmatch(c.Doc, -1) {
		if !slices.Contains(files, m[1]) {
			out = append(out, m[1])
		}
	}
	sort.Slice(out, func(i, j int) bool { return lessByParts(out[i], out[j]) })
	return out
}

var fileMarkRE = regexp.MustCompile(`(?m)^===== FILE (.+) =====$`)

// checkedWhole says whether text is, whole, what the verifier read as the file
// rel of doc: the part ends where the next file begins or the document ends, so
// a file cut short at a line is not taken for it.
func checkedWhole(doc, rel, text string) bool {
	next := "\n\n" + fileMark("")[:len("===== FILE ")]
	if rel == skillFile {
		return doc == text || strings.HasPrefix(doc, text+next)
	}
	part := "\n" + fileMark(rel) + "\n" + text
	return strings.Contains(doc, part+next) || strings.HasSuffix(doc, part)
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
	binary, err := executable()
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
		return noAnswer(stepTimeout.Seconds())
	case res.err != nil:
		return cutOff{res.err}
	case res.code == 0:
		return nil
	}
	said := pyStrip(res.stderr)
	if said == "" {
		said = pyStrip(res.stdout)
	}
	if said = lastRunes(printable(said), outputTail); said != "" { // a remote's words, not its escape sequences
		return errors.New(said)
	}
	return fmt.Errorf("exit %d", res.code)
}

// takeOut uninstalls what a step just installed and says how that went.
func (a *app) takeOut(c candidate) string {
	if a.runStep(uninstallCommand(c)) != nil {
		return "still installed, remove it by hand"
	}
	return "removed again"
}

// rollBack undoes a swap whose mute failed: unmutes what it muted and takes
// the new skill or server out again. It returns what could not be undone.
func (a *app) rollBack(c candidate, muted []skill) []string {
	var left []string
	for _, x := range muted {
		if a.runStep([]string{reapName, "unmute", x.target()}) != nil {
			left = append(left, x.Name)
		}
	}
	var err error
	switch {
	case c.Installed: // installed by an earlier run: it stays, only this run's mutes are undone
	case c.Created:
		err = removeWritten(c, a.linkedAgain[c.Slug])
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
		home := filepath.Join(writtenDir(), c.Slug)
		left, _ := present(home) // there before this run: linked again, not made
		if err := create(c); err != nil {
			return fail(err.Error())
		}
		if left {
			if a.linkedAgain == nil {
				a.linkedAgain = map[string]bool{}
			}
			a.linkedAgain[c.Slug] = true
			return []string{"  ✓  linked " + c.Name + " again, from " + home}, false
		}
		return []string{"  ✓  created " + c.Name + " in " + home}, false
	case c.Installed:
		return nil, false
	}
	command, ok := installCommand(c)
	if !ok {
		return []string{"  ✗  " + c.Name + ": needs manual setup, nothing muted · " + printable(c.ID)}, true
	}
	if c.Kind == kindSkill { // the user's: never replaced, never rolled back
		if taken, err := present(filepath.Join(skillsDir(), c.Slug)); err != nil {
			return fail(err.Error())
		} else if taken {
			return fail("a skill named " + c.Slug + " already exists in " + skillsDir())
		}
	}
	if err := a.runStep(command); err != nil {
		if c.Kind == kindSkill {
			// the slug was free before the step: anything there now is the step's, half
			// written and never checked
			if left, _ := present(filepath.Join(skillsDir(), c.Slug)); left {
				return fail(err.Error() + " · " + a.takeOut(c))
			}
		}
		return fail(err.Error())
	}
	if !a.isInstalled(c) { // on disk, but not where Claude Code loads it, and never compared
		return fail("installed, but not for Claude Code · " + a.takeOut(c))
	}
	if c.Kind == kindSkill {
		if changed := a.unchecked(c); len(changed) > 0 {
			shown := strings.Join(changed[:min(changedShown, len(changed))], ", ")
			return []string{"  ✗  " + c.Name + ": not what was checked (" + shown + ") · " + a.takeOut(c) + " · nothing muted"}, true
		}
	}
	lines = []string{"  ✓  installed " + c.Name}
	if c.Kind == kindMCP && len(s.Mute) == 0 {
		if err := a.remember(c); err != nil {
			lines = append(lines, "  ✗  not remembered for a later mute: "+err.Error())
		}
		// Not a stop: with nothing to mute the swap is done, and no longer on offer.
		return append(lines, "  ·  nothing muted yet: reinvest offers the mute once reap has weighed it"), false
	}
	return lines, false
}

// applyOne installs, then mutes. All or nothing: a failed mute unmutes what
// this swap muted and uninstalls (a server installed by an earlier run stays:
// only this run's mutes are undone).
func (a *app) applyOne(s swap) []string {
	log, _ := a.carryOut(s)
	return log
}

// carryOut is applyOne that also says whether the swap is now in place.
func (a *app) carryOut(s swap) (log []string, applied bool) {
	c := s.cand()
	var muted []skill
	installing, inHand, undoing := true, "", false
	// Ctrl-C ends reinvest inside the step it interrupts. The swap is then half
	// done, and says so before going: what is in place, and what is not.
	defer setStopHook(func() {
		lines := stoppedHalfway(c, installing, log, muted, inHand, s.Mute)
		if undoing {
			lines = append(lines, "  ↺  it was undoing the swap after a mute failed: what is listed here may or may not be undone yet")
		}
		for _, line := range lines {
			fmt.Fprintln(a.term, line)
		}
	})()
	log, stop := a.install(s)
	if stop {
		return log, false
	}
	installing = false
	for _, x := range s.Mute {
		inHand = x.Name
		// --yes: the swap was accepted in reinvest's own question, with this mute
		// listed in it. reap would otherwise ask again on a terminal nobody sees.
		err := a.runStep([]string{reapName, "--yes", "mute", x.target()})
		if err == nil {
			muted = append(muted, x)
			continue
		}
		outcome := "  ↺  rolled back: nothing changed"
		inHand, undoing = "", true // this mute failed, it was not cut short; from here a stop lands in the undoing
		// A mute that gave no answer in time, or was killed, was cut off, perhaps
		// after it had done its work: it is undone too, and when that fails nobody
		// knows which it is.
		ended := errors.As(err, new(noAnswer)) || errors.As(err, new(cutOff))
		unsure := ended && a.runStep([]string{reapName, "unmute", x.target()}) != nil
		if left := a.rollBack(c, muted); len(left) > 0 {
			outcome = "  ↺  rolled back, but still changed: " + strings.Join(left, ", ")
		} else if unsure {
			outcome = "  ↺  rolled back, all but perhaps this mute"
		}
		log = append(log, "  ✗  reap mute "+x.Name+": "+err.Error(), outcome)
		if unsure {
			log = append(log, "  ?  "+x.Name+": its mute did not end cleanly and could not be undone (reap unmute lists what is muted)")
		}
		return log, false
	}
	if c.Installed {
		if err := a.forgetServer(c); err != nil {
			log = append(log, "  ✗  "+c.Name+" not forgotten: "+err.Error())
		}
	}
	for _, x := range muted {
		log = append(log, "  ✓  muted "+x.Name+" ("+undoMute(x)+" undoes)")
	}
	return log, true
}

// stoppedHalfway is what a swap says when a signal ends reinvest inside it.
func stoppedHalfway(c candidate, installing bool, log []string, muted []skill, inHand string, all []skill) []string {
	lines := []string{"", "  ⚠  stopped in the middle of the swap for " + c.Name + ":"}
	if installing {
		lines = append(lines, "  ?  "+c.Name+": its install was cut short (it may be half there)")
	}
	lines = append(lines, log...)
	done := map[string]bool{inHand: true}
	for _, x := range muted {
		done[x.Name] = true
		lines = append(lines, "  ✓  muted "+x.Name+" ("+undoMute(x)+" undoes)")
	}
	if inHand != "" && (len(muted) == 0 || muted[len(muted)-1].Name != inHand) {
		lines = append(lines, "  ?  "+inHand+": its mute was cut short (reap unmute lists what is muted)")
	}
	var rest []string
	for _, x := range all {
		if !done[x.Name] {
			rest = append(rest, x.Name)
		}
	}
	if len(rest) > 0 {
		lines = append(lines, "  ·  not muted: "+strings.Join(rest, ", "))
	}
	return lines
}

// apply carries out the chosen swaps, or with dryRun says what it would run.
// The swaps returned are the ones now in place.
func (a *app) apply(chosen []swap, dryRun bool) (log []string, applied []swap) {
	for _, s := range chosen {
		if dryRun {
			log = append(log, dryRunLines(s)...)
			continue
		}
		lines, ok := a.carryOut(s)
		log = append(log, lines...)
		if ok {
			applied = append(applied, s)
		}
	}
	return log, applied
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
			return []string{"  ✗  " + c.Name + ": needs manual setup, nothing muted · " + printable(c.ID)}
		}
		words := make([]string, len(command))
		for i, w := range command { // one word per argument: the line is there to be pasted
			words[i] = scan.ShellArg(printable(w))
		}
		lines = append(lines, "  ·  "+strings.Join(words, " "))
	}
	for _, x := range s.Mute {
		lines = append(lines, "  ·  "+reapLine("mute", x))
	}
	return lines
}

// cutOff is a step that ended without saying how it went: killed, or with its
// pipes never closed.
type cutOff struct{ error }

// noAnswer is a step that was cut off for taking too long, in seconds.
type noAnswer float64

func (n noAnswer) Error() string { return fmt.Sprintf("no answer in %d s", int(n)) }

// undoMute is the command that takes a mute back, as it is pasted: with the
// folder, when this run was pointed at one through the environment. A shell
// without the variable looks in the usual folder and finds nothing muted.
func undoMute(x skill) string {
	// Without its control characters: it is printed to be read, and a path
	// that holds them cannot be pasted either way.
	return reapLine("unmute", x)
}

// reapLine is a reap command on one skill, to be read and pasted: on its
// path, and in the config folder this run was pointed at.
func reapLine(verb string, x skill) string {
	cmd := "reap " + verb + " " + scan.ShellArg(printable(x.target()))
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		cmd = "CLAUDE_CONFIG_DIR=" + scan.ShellArg(dir) + " " + cmd
	}
	return cmd
}
