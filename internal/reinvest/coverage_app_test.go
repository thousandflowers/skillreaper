//go:build darwin || linux

package reinvest

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

// What reinvest does when the disk, a program or the person says no: the
// branches a whole run seldom takes.

// ttyCovNotRoot skips a test that relies on permissions root ignores.
func ttyCovNotRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
}

// ttyCovReadOnly makes a folder read-only until the test ends.
func ttyCovReadOnly(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

// ttyCovUnderFile is a path inside a regular file, where nothing can be made.
func ttyCovUnderFile(t *testing.T, rest ...string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "file")
	writeFile(t, file, "")
	return filepath.Join(append([]string{file}, rest...)...)
}

func ttyCovDangling(t *testing.T, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "gone"), link); err != nil {
		t.Fatal(err)
	}
}

// ttyCovFileSizeLimit makes every write to a file past its first byte fail,
// as a full disk does, while fn runs.
func ttyCovFileSizeLimit(t *testing.T, fn func()) {
	t.Helper()
	var old syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &old); err != nil {
		t.Skip(err)
	}
	signal.Ignore(syscall.SIGXFSZ) // the write fails with EFBIG instead of killing the process
	defer signal.Reset(syscall.SIGXFSZ)
	limit := old
	limit.Cur = 1
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limit); err != nil {
		t.Skip(err)
	}
	defer func() {
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &old); err != nil {
			t.Fatal(err)
		}
	}()
	fn()
}

// ---------------------------------------------------------------------------
// app.go
// ---------------------------------------------------------------------------

func TestAppCov_with_no_HOME_the_home_is_the_users_own(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Skip(err)
	}
	t.Setenv("HOME", "")
	if got := homeDir(); got != u.HomeDir || got == "" {
		t.Errorf("home is %q, the user database says %q", got, u.HomeDir)
	}
}

func TestAppCov_the_cache_and_state_folders_follow_the_system(t *testing.T) {
	home := tempHome(t)
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	cache, state := filepath.Join(home, ".cache"), filepath.Join(home, ".local", "share")
	if runtime.GOOS == "darwin" {
		cache, state = filepath.Join(home, "Library", "Caches"), filepath.Join(home, "Library", "Application Support")
	}
	if got := defaultCacheDir(); got != filepath.Join(cache, "skillreaper", "reinvest") {
		t.Errorf("cache in %s", got)
	}
	if got := defaultStateDir(); got != filepath.Join(state, "skillreaper", "reinvest") {
		t.Errorf("state in %s", got)
	}
	if runtime.GOOS == "linux" {
		t.Setenv("XDG_CACHE_HOME", "/xdg/cache")
		t.Setenv("XDG_DATA_HOME", "/xdg/data")
		if defaultCacheDir() != "/xdg/cache/skillreaper/reinvest" || defaultStateDir() != "/xdg/data/skillreaper/reinvest" {
			t.Errorf("XDG folders ignored: %s %s", defaultCacheDir(), defaultStateDir())
		}
	}
}

func TestAppCov_a_private_file_that_cannot_be_written_is_an_error(t *testing.T) {
	if err := writePrivate(ttyCovUnderFile(t, "dir", "x.json"), "{}"); !errors.Is(err, syscall.ENOTDIR) {
		t.Errorf("a folder inside a file gave %v", err)
	}
	if err := writeShared(ttyCovUnderFile(t, "dir", "x.json"), "{}"); !errors.Is(err, syscall.ENOTDIR) {
		t.Errorf("a shared file in a folder inside a file gave %v", err)
	}
	dir := t.TempDir()
	if err := writePrivate(dir, "{}"); !errors.Is(err, syscall.EISDIR) {
		t.Errorf("a folder in the file's place gave %v", err)
	}
	path := filepath.Join(t.TempDir(), "report.md")
	ttyCovFileSizeLimit(t, func() {
		if err := writePrivate(path, "more than one byte"); !errors.Is(err, syscall.EFBIG) {
			t.Errorf("a write that fails gave %v", err)
		}
	})
}

func TestAppCov_a_private_file_that_cannot_be_made_private_is_an_error(t *testing.T) {
	ttyCovNotRoot(t)
	// /dev/null opens for writing, but only its owner may change who reads it
	if err := writePrivate(os.DevNull, "x"); !errors.Is(err, os.ErrPermission) {
		t.Errorf("a file someone else owns gave %v", err)
	}
}

func TestAppCov_progress_on_a_terminal_rewrites_its_line(t *testing.T) {
	a := fakeApp(t)
	a.termTTY = true
	a.step("reading sessions", false)
	a.step("12 sessions read", true)
	a.failed("grouping got no answer")
	want := "  ⠋ reading sessions…" + clearLine + "  ✓ 12 sessions read\n" + clearLine + "  ✗ grouping got no answer\n"
	if got := termText(a); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAppCov_a_parallel_map_with_no_workers_still_runs_one(t *testing.T) {
	got := parallelMap([]int{1, 2, 3}, 0, func(n int) int { return n * n })
	if !sameInts(got, []int{1, 4, 9}) {
		t.Errorf("got %v", got)
	}
}

// ---------------------------------------------------------------------------
// terminal.go
// ---------------------------------------------------------------------------

type ttyCovFiler struct{ f *os.File }

func (s ttyCovFiler) File() *os.File { return s.f }

func TestAppCov_a_stream_in_front_of_a_file_is_that_file(t *testing.T) {
	f := ttyCovFile(t)
	if got, ok := fileOf(ttyCovFiler{f}); got != f || !ok {
		t.Errorf("got %v %v", got, ok)
	}
	if _, ok := fileOf(ttyCovFiler{}); ok {
		t.Error("a stream with no file has one")
	}
	if _, ok := fileOf((*os.File)(nil)); ok {
		t.Error("a nil file is a file")
	}
	if isTerminal(ttyCovFiler{f}) {
		t.Error("a plain file behind a stream is a terminal")
	}
}

func TestAppCov_a_last_line_with_no_newline_is_still_a_line(t *testing.T) {
	if got, err := readLine(strings.NewReader("y")); got != "y" || err != nil {
		t.Errorf("got %q, %v", got, err)
	}
	if got, err := readLine(strings.NewReader("")); got != "" || err != io.EOF {
		t.Errorf("nothing gave %q, %v", got, err)
	}
}

// ---------------------------------------------------------------------------
// picker.go
// ---------------------------------------------------------------------------

func TestAppCov_m_sets_the_mute_aside_and_back(t *testing.T) {
	swaps := ttyCovSwaps()
	swaps, _, _ = onKey(swaps, 0, 'm')
	if len(swaps[0].Mute) != 0 || len(swaps[0].Held) != 1 || swaps[0].Out != 0 {
		t.Errorf("after m: %+v", swaps[0])
	}
	if !strings.Contains(muteText(swaps[0]), "m to mute old-release again") {
		t.Errorf("shown as %q", muteText(swaps[0]))
	}
	swaps, _, _ = onKey(swaps, 0, 'm')
	if len(swaps[0].Mute) != 1 || swaps[0].Out != 60 {
		t.Errorf("after m again: %+v", swaps[0])
	}
	bare := []swap{{Cands: ttyCovSwaps()[0].Cands}}
	if got, _, _ := onKey(bare, 0, 'm'); len(got[0].Mute) != 0 || len(got[0].Held) != 0 {
		t.Error("m on a swap with nothing to mute changed it")
	}
}

func TestAppCov_a_candidate_accepted_above_is_marked_below(t *testing.T) {
	swaps := ttyCovSwaps()
	swaps[1].Cands = swaps[0].Cands
	swaps[0].OK = true
	var below string
	for _, d := range frame(swaps, 0, 40, 200) {
		if strings.Contains(d.text, "ship-release") && d.y > 2+rowsPerSwap {
			below = d.text
		}
	}
	if !strings.HasSuffix(below, "   (already picked above)") {
		t.Errorf("the second ship-release reads %q", below)
	}
	if alreadyPicked(swaps, 0) {
		t.Error("the first is marked although nothing above picked it")
	}
}

// The list scrolls: on a 24-row terminal six swaps do not fit, and the one
// under the cursor used to be drawn below the screen, its first line under
// the totals. It could be accepted without ever being seen.
func TestPickerScrollsToTheSwapUnderTheCursor(t *testing.T) {
	var swaps []swap
	for i := 0; i < 6; i++ {
		s := ttyCovSwaps()[0]
		s.Cands = []candidate{{Name: fmt.Sprintf("cand-%d", i), Gap: fmt.Sprintf("gap-%d", i), Why: fmt.Sprintf("why-%d", i)}}
		swaps = append(swaps, s)
	}
	const height, width = 24, 200
	for cur := range swaps {
		rows := map[int]string{}
		for _, d := range frame(swaps, cur, height, width) {
			if d.y != 0 && d.y != height-2 && (d.y < 2 || d.y > height-3) {
				t.Errorf("cursor %d: %q is drawn on row %d, outside the list", cur, d.text, d.y)
			}
			if d.y == height-2 && !strings.Contains(d.text, "accepted") {
				t.Errorf("cursor %d: %q is drawn over the totals", cur, d.text)
			}
			rows[d.y] += d.text
		}
		screen := fmt.Sprint(rows)
		for _, want := range []string{fmt.Sprintf("cand-%d", cur), fmt.Sprintf("gap-%d", cur), fmt.Sprintf("why-%d", cur)} {
			if !strings.Contains(screen, want) {
				t.Errorf("cursor %d: %s is not on the screen", cur, want)
			}
		}
	}
	// A list that fits is drawn from the top, as before.
	for _, d := range frame(swaps[:2], 1, height, width) {
		if strings.Contains(d.text, "cand-0") && d.y != 3 {
			t.Errorf("a list that fits moved: cand-0 on row %d", d.y)
		}
	}
}

func TestAppCov_a_command_line_ending_in_a_backslash_does_not_split(t *testing.T) {
	for _, line := range []string{`less -R\`, `less "-R\`} {
		if _, err := shlexSplit(line); !errors.Is(err, errNoEscaped) {
			t.Errorf("%q gave %v", line, err)
		}
	}
}

func TestAppCov_the_pager_reads_the_text_and_how_it_ends_is_its_own(t *testing.T) {
	out := filepath.Join(t.TempDir(), "paged")
	if err := runPager([]string{"/bin/sh", "-c", `cat > "$1"`, "sh", out}, "the whole SKILL.md"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, out); got != "the whole SKILL.md" {
		t.Errorf("the pager read %q", got)
	}
	if err := runPager([]string{"/bin/sh", "-c", "exit 3"}, ""); err != nil {
		t.Errorf("a pager that quit with 3 gave %v", err)
	}
	if err := runPager([]string{filepath.Join(t.TempDir(), "no-pager")}, ""); err == nil {
		t.Error("a pager that is not there gave no error")
	}
}

func TestAppCov_a_pager_that_cannot_run_falls_back_to_more(t *testing.T) {
	c := candidate{Name: "ship-release", Kind: kindSkill, Doc: "the doc"}
	for _, pager := range []string{"   ", `less "-R`} {
		a := fakeApp(t)
		log := netCovLog(a)
		var ran [][]string
		a.runPager = func(argv []string, text string) error {
			ran = append(ran, argv)
			if text != "the doc" {
				t.Errorf("the fallback got %q", text)
			}
			return errors.New("more: not found")
		}
		t.Setenv("PAGER", pager)
		a.preview(c)
		if len(ran) != 1 || !sameWords(ran[0], []string{fallbackPager}) {
			t.Errorf("PAGER=%q ran %v", pager, ran)
		}
		if lines := strings.Count(log.String(), "pager: "); lines != 2 || !strings.Contains(log.String(), "pager: more: not found") {
			t.Errorf("PAGER=%q logged %q", pager, log.String())
		}
	}
}

// ---------------------------------------------------------------------------
// apply.go
// ---------------------------------------------------------------------------

func TestAppCov_a_server_that_needs_keys_is_not_installed_by_a_command(t *testing.T) {
	c := candidate{Kind: kindMCP, Name: "fs", Server: jsObj(t, `{"name": "io.github.acme/fs", "packages": [{"registryType": "npm",
		"identifier": "fs", "version": "1.0.0", "environmentVariables": [{"name": "API_KEY"}]}]}`)}
	if command, ok := installCommand(c); ok || command != nil {
		t.Errorf("got %v", command)
	}
	if got := uninstallCommand(c); !sameWords(got, []string{"claude", "mcp", "remove", "-s", "user", "fs"}) {
		t.Errorf("uninstalled with %v", got)
	}
	if lines := dryRunLines(swap{Cands: []candidate{c}}); len(lines) != 1 || !strings.Contains(lines[0], "needs manual setup") {
		t.Errorf("a dry run says %q", lines)
	}
	ok := swap{Mute: []skill{{Name: "old"}}, Cands: []candidate{{Kind: kindSkill, Name: "r", Source: "acme/tools", Slug: "release"}}}
	if lines := dryRunLines(ok); len(lines) != 2 || lines[0] != "  ·  npx -y skills@1.7.6 add acme/tools -s release -g -y -a claude-code" ||
		lines[1] != "  ·  reap mute old" {
		t.Errorf("a dry run says %q", lines)
	}
}

func TestAppCov_not_knowing_whether_a_name_is_taken_is_an_error(t *testing.T) {
	file := ttyCovUnderFile(t)
	if taken, err := present(filepath.Join(file, "x")); taken || !errors.Is(err, syscall.ENOTDIR) {
		t.Errorf("got %v, %v", taken, err)
	}
}

func TestAppCov_a_written_skill_is_never_put_over_anything(t *testing.T) {
	c := writtenSwap().cand()
	skills := func(home string) string { return filepath.Join(home, ".claude", "skills") }
	written := func(home string) string { return filepath.Join(home, ".claude", "skills-reinvest") }

	home := tempHome(t)
	writeFile(t, skills(home), "a file where the skills folder goes")
	if err := create(c); !errors.Is(err, syscall.ENOTDIR) {
		t.Errorf("a skills folder that is a file gave %v", err)
	}

	home = tempHome(t)
	writeFile(t, written(home), "a file where the written folder goes")
	if err := create(c); !errors.Is(err, syscall.ENOTDIR) {
		t.Errorf("a written folder that is a file gave %v", err)
	}

	home = tempHome(t)
	writeFile(t, filepath.Join(written(home), c.Slug, "notes"), "someone's")
	if err := create(c); err == nil || !strings.Contains(err.Error(), "ship-release is left from an earlier run") {
		t.Errorf("a folder already there gave %v", err)
	}
	if got := readFile(t, filepath.Join(written(home), c.Slug, "notes")); got != "someone's" {
		t.Error("what was there was changed")
	}

	home = tempHome(t)
	ttyCovDangling(t, written(home))
	if err := create(c); err == nil || exists(filepath.Join(skills(home), c.Slug)) {
		t.Errorf("a written folder that is a broken link gave %v", err)
	}
}

func TestAppCov_a_written_skill_that_cannot_be_linked_is_taken_back(t *testing.T) {
	c := writtenSwap().cand()
	home := tempHome(t)
	ttyCovDangling(t, filepath.Join(home, ".claude", "skills"))
	if err := create(c); !errors.Is(err, syscall.EEXIST) {
		t.Errorf("a skills folder that is a broken link gave %v", err)
	}
	if exists(filepath.Join(home, ".claude", "skills-reinvest", c.Slug)) {
		t.Error("the written skill was left behind")
	}
}

func TestAppCov_a_written_skill_in_a_read_only_place_is_not_written(t *testing.T) {
	ttyCovNotRoot(t)
	c := writtenSwap().cand()
	home := tempHome(t)
	ttyCovReadOnly(t, filepath.Join(home, ".claude", "skills-reinvest"))
	if err := create(c); !errors.Is(err, os.ErrPermission) {
		t.Errorf("a read-only written folder gave %v", err)
	}

	home = tempHome(t)
	ttyCovReadOnly(t, filepath.Join(home, ".claude", "skills"))
	if err := create(c); !errors.Is(err, os.ErrPermission) {
		t.Errorf("a read-only skills folder gave %v", err)
	}
	if exists(filepath.Join(home, ".claude", "skills-reinvest", c.Slug)) {
		t.Error("the written skill was left behind when its link could not be made")
	}
}

func TestAppCov_only_a_link_to_the_target_points_at_it(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	writeFile(t, target, "")
	if pointsAt(target, target) {
		t.Error("a file points at itself")
	}
	link := filepath.Join(dir, "link")
	ttyCovDangling(t, link)
	if pointsAt(link, target) {
		t.Error("a broken link points at something")
	}
}

func TestAppCov_a_written_skill_that_cannot_be_removed_says_so(t *testing.T) {
	ttyCovNotRoot(t)
	c := writtenSwap().cand()
	home := tempHome(t)
	if err := create(c); err != nil {
		t.Fatal(err)
	}
	ttyCovReadOnly(t, filepath.Join(home, ".claude", "skills"))
	if err := removeWritten(c, false); !errors.Is(err, os.ErrPermission) {
		t.Errorf("a link in a read-only folder gave %v", err)
	}
	if !exists(filepath.Join(home, ".claude", "skills-reinvest", c.Slug)) {
		t.Error("the folder went although its link stayed")
	}
	tempHome(t)
	if err := removeWritten(c, false); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a written skill that is not there gave %v", err)
	}
}

func TestAppCov_a_file_that_is_not_text_is_never_what_was_checked(t *testing.T) {
	home := tempHome(t)
	c := candidate{Kind: kindSkill, Slug: "release", Doc: "---\nname: release\n---\n"}
	writeFile(t, filepath.Join(home, ".claude", "skills", "release", skillFile), c.Doc)
	writeFile(t, filepath.Join(home, ".claude", "skills", "release", "logo.png"), "\x89PNG\xff")
	if got := uncheckedFiles(c); !sameWords(got, []string{"logo.png"}) {
		t.Errorf("unchecked %v", got)
	}
	if got := filesUnder(filepath.Join(home, "nowhere"), ""); got != nil {
		t.Errorf("a folder that is not there holds %v", got)
	}
}

// ttyCovStepApp runs steps through runInEmptyDir with the program faked.
func ttyCovStepApp(t *testing.T, res procResult) (*app, *[]procSpec) {
	t.Helper()
	a := fakeApp(t)
	var specs []procSpec
	a.runProcess = func(spec procSpec) procResult {
		specs = append(specs, spec)
		return res
	}
	return a, &specs
}

func TestAppCov_a_step_that_fails_says_why_in_its_own_words(t *testing.T) {
	for _, tc := range []struct {
		res  procResult
		want string
	}{
		{procResult{timedOut: true}, "no answer in 120 s"},
		{procResult{err: errors.New("exec: npx: not found")}, "exec: npx: not found"},
		{procResult{code: 1, stderr: "  npm ERR! 404  \n", stdout: "ignored"}, "npm ERR! 404"},
		{procResult{code: 1, stdout: "only on stdout\n"}, "only on stdout"},
		{procResult{code: 7, stderr: " \n"}, "exit 7"},
		{procResult{code: 1, stderr: strings.Repeat("x", outputTail) + "TAIL"}, strings.Repeat("x", outputTail-4) + "TAIL"},
	} {
		a, specs := ttyCovStepApp(t, tc.res)
		err := a.runInEmptyDir([]string{"npx", "skills", "add"})
		if err == nil || err.Error() != tc.want {
			t.Errorf("%+v gave %v, want %q", tc.res, err, tc.want)
		}
		if spec := (*specs)[0]; exists(spec.dir) || !sameWords(spec.env, []string{"GIT_TERMINAL_PROMPT=0"}) || spec.timeout != stepTimeout {
			t.Errorf("run with %+v", spec)
		}
	}
	a, specs := ttyCovStepApp(t, procResult{})
	a.reapCommand = func() ([]string, error) { return []string{"/opt/reap", "--x"}, nil }
	if err := a.runInEmptyDir([]string{reapName, "mute", "old"}); err != nil || !sameWords((*specs)[0].argv, []string{"/opt/reap", "--x", "mute", "old"}) {
		t.Errorf("reap ran as %v (%v)", (*specs)[0].argv, err)
	}
	a.reapCommand = func() ([]string, error) { return nil, errors.New("no binary") }
	if err := a.runInEmptyDir([]string{reapName, "mute", "old"}); err == nil || err.Error() != "no binary" || len(*specs) != 1 {
		t.Errorf("no reap gave %v after %d runs", err, len(*specs))
	}
}

func TestAppCov_a_step_needs_its_empty_folder(t *testing.T) {
	a, specs := ttyCovStepApp(t, procResult{})
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "gone"))
	if err := a.runInEmptyDir([]string{"npx"}); err == nil || len(*specs) != 0 {
		t.Errorf("no temporary folder gave %v after %d runs", err, len(*specs))
	}
}

func TestAppCov_a_step_folder_that_will_not_go_is_logged(t *testing.T) {
	ttyCovNotRoot(t)
	a := fakeApp(t)
	log := netCovLog(a)
	t.Setenv("TMPDIR", t.TempDir())
	a.runProcess = func(spec procSpec) procResult {
		writeFile(t, filepath.Join(spec.dir, "node_modules", "x"), "") // what the step left
		ttyCovReadOnly(t, spec.dir)
		return procResult{}
	}
	if err := a.runInEmptyDir([]string{"npx"}); err != nil {
		t.Errorf("a step that worked gave %v", err)
	}
	if !strings.Contains(log.String(), "not removed") {
		t.Errorf("log %q", log.String())
	}
}

func TestAppCov_what_a_roll_back_could_not_undo_is_listed(t *testing.T) {
	a := fakeApp(t)
	stepsRecorded(a, func(cmd []string) error {
		if hasPrefixWords(cmd, reapName, "unmute", "a") || hasPrefixWords(cmd, "npx") {
			return errors.New("failed")
		}
		return nil
	})
	c := candidate{Kind: kindSkill, Name: "release", Source: "acme/tools", Slug: "release"}
	if left := a.rollBack(c, []skill{{Name: "a"}, {Name: "b"}}); !sameWords(left, []string{"a", "release (installed)"}) {
		t.Errorf("left %v", left)
	}
	tempHome(t)
	written := writtenSwap().cand()
	if left := a.rollBack(written, nil); !sameWords(left, []string{"ship-release (installed)"}) {
		t.Errorf("a written skill gone already left %v", left)
	}
}

func TestAppCov_an_install_that_fails_mutes_nothing(t *testing.T) {
	home := tempHome(t)
	skillSwap := swap{Mute: []skill{{Name: "old"}}, Cands: []candidate{{Kind: kindSkill, Name: "release", Source: "acme/tools",
		Slug: "release", ID: "acme/tools/release"}}}
	a := fakeApp(t)
	calls := stepsRecorded(a, func([]string) error { return errors.New("npm ERR! 404") })
	if log := a.applyOne(skillSwap); len(log) != 1 || log[0] != "  ✗  release: npm ERR! 404 · nothing muted" {
		t.Errorf("a failed install: %q", log)
	}
	bad := skillSwap
	bad.Cands = []candidate{{Kind: kindSkill, Name: "bad", Source: "acme/tools; rm", Slug: "x", ID: "bad-id"}}
	if log := a.applyOne(bad); len(log) != 1 || log[0] != "  ✗  bad: needs manual setup, nothing muted · bad-id" {
		t.Errorf("an unsafe source: %q", log)
	}
	writeFile(t, filepath.Join(home, ".claude", "skills"), "a file")
	if log := a.applyOne(skillSwap); len(log) != 1 || !strings.Contains(log[0], "not a directory · nothing muted") {
		t.Errorf("a skills folder that is a file: %q", log)
	}
	if len(*calls) != 1 {
		t.Errorf("steps %v", *calls)
	}
}

func TestAppCov_a_changed_skill_that_cannot_be_removed_says_so(t *testing.T) {
	tempHome(t)
	a := fakeApp(t)
	a.isInstalled = func(candidate) bool { return true }
	a.unchecked = func(candidate) []string { return []string{"a.sh", "b.sh", "c.sh", "d.sh"} }
	stepsRecorded(a, func(cmd []string) error {
		if hasPrefixWords(cmd, "npx", "-y", "skills@1.7.6", "remove") {
			return errors.New("busy")
		}
		return nil
	})
	s := swap{Mute: []skill{{Name: "old"}}, Cands: []candidate{{Kind: kindSkill, Name: "release", Source: "acme/tools", Slug: "release"}}}
	want := "  ✗  release: not what was checked (a.sh, b.sh, c.sh) · still installed, remove it by hand · nothing muted"
	if log := a.applyOne(s); len(log) != 1 || log[0] != want {
		t.Errorf("got %q", log)
	}
}

func TestAppCov_a_server_that_cannot_be_remembered_says_so(t *testing.T) {
	a := fakeApp(t)
	stepsRecorded(a, nil)
	a.remember = func(candidate) error { return errors.New("disk full") }
	s := swap{Cands: []candidate{{Kind: kindMCP, Name: "fs", Server: jsObj(t, `{"name": "io.github.acme/fs",
		"packages": [{"registryType": "npm", "identifier": "fs", "version": "1.0.0"}]}`)}}}
	log := a.applyOne(s)
	if len(log) != 3 || log[1] != "  ✗  not remembered for a later mute: disk full" || !strings.Contains(log[2], "nothing muted yet") {
		t.Errorf("got %q", log)
	}
}

func TestAppCov_a_failed_mute_whose_roll_back_fails_says_what_is_left(t *testing.T) {
	tempHome(t)
	a := fakeApp(t)
	stepsRecorded(a, func(cmd []string) error {
		switch {
		case sameWords(cmd, []string{reapName, "--yes", "mute", "second"}):
			return errors.New("refused")
		case sameWords(cmd, []string{reapName, "unmute", "first"}):
			return errors.New("refused too")
		}
		return nil
	})
	log := a.applyOne(writtenSwap("first", "second"))
	if n := len(log); n != 3 || log[1] != "  ✗  reap mute second: refused" || log[2] != "  ↺  rolled back, but still changed: first" {
		t.Errorf("got %q", log)
	}
}

func TestAppCov_a_server_weighed_now_that_cannot_be_forgotten_says_so(t *testing.T) {
	a := fakeApp(t)
	stepsRecorded(a, nil)
	a.cacheDir = ttyCovUnderFile(t, "cache")
	s := swap{Mute: []skill{{Name: "old"}}, Cands: []candidate{{Kind: kindMCP, Name: "fs", Installed: true, Server: objOf("name", "fs")}}}
	log := a.applyOne(s)
	if len(log) != 2 || !strings.HasPrefix(log[0], "  ✗  fs not forgotten: ") || log[1] != "  ✓  muted old (reap unmute old undoes)" {
		t.Errorf("got %q", log)
	}
}

// In a window too small to show the swap under the cursor the picker draws no
// swap at all, only what to do: a swap nobody can see is not there to accept.
func TestPickerInAWindowTooSmallDrawsNoSwap(t *testing.T) {
	swaps := ttyCovSwaps()
	for _, size := range [][2]int{{0, 0}, {1, 80}, {6, 80}, {8, 80}, {24, 23}, {8, 30}} {
		drawn := frame(swaps, 0, size[0], size[1])
		for _, d := range drawn {
			if d.y != 0 || !strings.HasPrefix(tooSmallNotice, d.text) {
				t.Errorf("at %dx%d the picker draws %q on row %d", size[0], size[1], d.text, d.y)
			}
		}
		if roomFor(size[0], size[1]) {
			t.Errorf("%dx%d counts as room for a swap", size[0], size[1])
		}
	}
	if !roomFor(minPickerRows, minPickerCols) || len(frame(swaps, 0, minPickerRows, minPickerCols)) < 5 {
		t.Error("the smallest window that fits a swap does not show it")
	}
}

// An accepted candidate with no weight yet makes the net unknown, not zero.
func TestPickerTotalsWithAnUnweighedCandidate(t *testing.T) {
	s := ttyCovSwaps()[0]
	s.OK = true
	s.Cands = []candidate{{Name: "srv", Kind: kindMCP}}
	if got := totals([]swap{s}); !strings.HasPrefix(got, "1 accepted") || !strings.HasSuffix(got, "net unknown (an MCP weight is unknown)") {
		t.Errorf("totals = %q", got)
	}
}
