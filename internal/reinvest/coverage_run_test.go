//go:build darwin || linux

package reinvest

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// write.go
// ---------------------------------------------------------------------------

func ttyCovGap() kind {
	return kind{Name: "Software Releases", Task: "Cut a release: bump, changelog, tag, CI.", IDs: []string{"rel-1"}}
}

func TestAppCov_an_answer_with_an_empty_field_writes_no_skill(t *testing.T) {
	a := fakeApp(t)
	alwaysAnswering(a, objOf("name", nil, "description", releaseDesc, "body", writtenBody))
	if _, ok, err := a.writeSkill(ttyCovGap(), [][]string{{"cut a release"}}, nil); ok || err != nil {
		t.Errorf("an answer with no name gave %v, %v", ok, err)
	}
}

func TestAppCov_a_run_that_cannot_ask_claude_stops_writing(t *testing.T) {
	a := fakeApp(t)
	a.ask = func(string, string, askOptions) (any, error) { return nil, errNoClaude }
	if _, _, err := a.writeSkill(ttyCovGap(), nil, nil); !errors.Is(err, errNoClaude) {
		t.Errorf("writeSkill gave %v", err)
	}
	if _, _, err := a.createdFor([]kind{ttyCovGap()}, nil, nil); !errors.Is(err, errNoClaude) {
		t.Errorf("createdFor gave %v", err)
	}
	if exists(a.cachePath(writtenFile)) {
		t.Error("a run that stopped saved its cache")
	}
}

func TestAppCov_a_damaged_written_cache_entry_is_ignored(t *testing.T) {
	a := fakeApp(t)
	writeFile(t, a.cachePath(writtenFile), `{"k1": ["only-a-slug"], "k2": ["a-slug", 3], "k3": ["ok-slug", "doc"]}`)
	got := a.loadWritten()
	if len(got) != 1 || got["k3"] != (writtenSkill{"ok-slug", "doc"}) {
		t.Errorf("got %v", got)
	}
}

func TestAppCov_written_skills_that_cannot_be_cached_are_an_error(t *testing.T) {
	a := fakeApp(t)
	a.cacheDir = ttyCovUnderFile(t, "cache")
	alwaysAnswering(a, objOf("name", "ship-release", "description", releaseDesc, "body", writtenBody))
	if _, _, err := a.createdFor([]kind{ttyCovGap()}, nil, nil); !errors.Is(err, syscall.ENOTDIR) {
		t.Errorf("got %v", err)
	}
}

// ---------------------------------------------------------------------------
// run.go
// ---------------------------------------------------------------------------

// ttyCovPrograms puts a fake claude on PATH and a fake reap in REAP, and a
// home of its own: Run then works on nothing of the person's.
func ttyCovPrograms(t *testing.T) string {
	t.Helper()
	home := tempHome(t)
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	bin := t.TempDir()
	writeFile(t, filepath.Join(bin, "report.json"), reapReport)
	for name, script := range map[string]string{
		"claude": "#!/bin/sh\n/bin/cat >/dev/null\necho '[]'\n",
		"reap":   "#!/bin/sh\nexec /bin/cat '" + filepath.Join(bin, "report.json") + "'\n",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv(reapVariable, filepath.Join(bin, "reap"))
	return home
}

func TestAppCov_Run_without_claude_fails_with_a_plain_message(t *testing.T) {
	ttyCovPrograms(t)
	t.Setenv("PATH", t.TempDir())
	var stdout, stderr bytes.Buffer
	if code := Run(nil, strings.NewReader(""), &stdout, &stderr); code != exitFailed {
		t.Errorf("exit %d", code)
	}
	if got := stderr.String(); got != errNoClaude.Error()+"\n" {
		t.Errorf("stderr %q", got)
	}
}

func TestAppCov_Run_with_nothing_to_propose_says_so_and_succeeds(t *testing.T) {
	ttyCovPrograms(t)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--days", "7"}, strings.NewReader(""), &stdout, &stderr); code != exitOK {
		t.Errorf("exit %d: %s", code, stderr.String())
	}
	out := stdout.String()
	for _, part := range []string{claudeNotice, "0 sessions read", "  no swaps: nothing verified for your gaps · details: " +
		filepath.Join(defaultCacheDir(), reportFile)} {
		if !strings.Contains(out, part) {
			t.Errorf("stdout does not show %q:\n%s", part, out)
		}
	}
	if strings.Contains(out, clearLine) {
		t.Error("progress meant for a terminal went to a pipe")
	}
}

func TestAppCov_a_run_whose_cache_cannot_be_made_stops(t *testing.T) {
	a := fakeApp(t)
	a.cacheDir = ttyCovUnderFile(t, "cache")
	if err := a.run(options{}); !errors.Is(err, syscall.ENOTDIR) {
		t.Errorf("got %v", err)
	}
	a = fakeApp(t)
	if err := os.Mkdir(a.cachePath(logFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := a.run(options{}); !errors.Is(err, syscall.EISDIR) {
		t.Errorf("a log that cannot be made gave %v", err)
	}
}

func TestAppCov_proposals_that_cannot_be_saved_stop_the_run(t *testing.T) {
	a, _ := pipelineApp(t, verdict{Keep: true, Risk: noRisk, Fit: 4, Reason: "fits", Use: "Cuts your releases."})
	if err := os.Mkdir(a.cachePath(swapsFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := a.run(options{days: defaultDays, max: defaultSwaps, dryRun: true}); !errors.Is(err, syscall.EISDIR) {
		t.Errorf("got %v", err)
	}
}

func TestAppCov_a_pipeline_step_that_fails_stops_the_run(t *testing.T) {
	stop := errors.New("stop here")
	for _, tc := range []struct {
		name   string
		isStop bool // the error is the one the broken step gave
		breaks func(t *testing.T, a *app)
	}{
		{"remembering sessions", false, func(t *testing.T, a *app) { a.stateDir = ttyCovUnderFile(t, "state") }},
		{"grouping", true, ttyCovFailOn(groupPrompt, stop)},
		{"finding gaps", true, ttyCovFailOn(coverPrompt, stop)},
		{"writing skills", true, ttyCovFailOn(writePrompt, stop)},
		{"verifying", true, func(t *testing.T, a *app) {
			a.verify = func(verifyRequest) (verdict, error) { return verdict{}, stop }
		}},
		{"the report", false, func(t *testing.T, a *app) {
			if err := os.MkdirAll(a.reportPath(), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"settling servers", false, func(t *testing.T, a *app) {
			if err := os.MkdirAll(a.cachePath(weighLaterFile), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		a, _ := pipelineApp(t, verdict{Keep: true, Risk: noRisk, Fit: 4, Reason: "fits", Use: "Cuts your releases."})
		tc.breaks(t, a)
		_, err := a.pipeline(options{days: defaultDays, max: defaultSwaps})
		if err == nil || (tc.isStop && !errors.Is(err, stop)) {
			t.Errorf("%s failed, the pipeline gave %v", tc.name, err)
		}
	}
}

// ttyCovFailOn makes one question to Claude fail as a run that cannot go on.
func ttyCovFailOn(prompt string, err error) func(t *testing.T, a *app) {
	return func(t *testing.T, a *app) {
		ask := a.ask
		a.ask = func(p, body string, opts askOptions) (any, error) {
			if p == prompt {
				return nil, err
			}
			return ask(p, body, opts)
		}
	}
}

// ttyCovWaitingServer leaves an MCP server an earlier run installed, now
// weighed by reap's report (300 tok/session).
func ttyCovWaitingServer(t *testing.T, cacheDir string) {
	t.Helper()
	writeFile(t, filepath.Join(cacheDir, weighLaterFile),
		`{"weighed": {"name": "Weighed server", "gap": "docs", "why": "Reads your docs.", "fit": 4}}`)
}

func TestAppCov_a_server_weighed_since_is_proposed_first_and_its_mutes_are_not_reused(t *testing.T) {
	a, _ := pipelineApp(t, verdict{Keep: true, Risk: noRisk, Fit: 4, Reason: "fits", Use: "Cuts your releases."})
	ttyCovWaitingServer(t, a.cacheDir)
	result, err := a.pipeline(options{days: defaultDays, max: defaultSwaps})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.swaps) == 0 || result.swaps[0].cand().Name != "Weighed server" || !result.swaps[0].cand().Installed {
		t.Fatalf("swaps %v", ttyCovNames(result.swaps))
	}
	muted := map[string]bool{}
	for _, s := range result.swaps {
		for _, m := range s.Mute {
			if muted[m.Name] {
				t.Errorf("%s is muted by two swaps", m.Name)
			}
			muted[m.Name] = true
		}
	}
}

func TestAppCov_an_online_search_with_no_cache_folder_stops(t *testing.T) {
	a := fakeApp(t)
	a.cacheDir = ttyCovUnderFile(t, "cache")
	if _, _, err := a.candidates(options{online: true}, []kind{ttyCovGap()}, nil, reapState{}); !errors.Is(err, syscall.ENOTDIR) {
		t.Errorf("got %v", err)
	}
}

func TestAppCov_with_nothing_found_and_nothing_refused_the_report_is_named(t *testing.T) {
	a := fakeApp(t)
	if err := a.show(outcome{}, false); err != nil {
		t.Fatal(err)
	}
	if got := termText(a); got != "\n  no swaps: nothing verified for your gaps · details: "+a.reportPath()+"\n" {
		t.Errorf("got %q", got)
	}
}

func TestAppCov_with_no_picker_the_proposals_are_still_listed(t *testing.T) {
	pt := ttyCovOpen(t, 24, 100)
	a := fakeApp(t)
	var stderr bytes.Buffer
	a.stdin, a.termTTY, a.stderr = pt.slave, true, &stderr
	a.pick = func([]swap) ([]swap, error) { return nil, errNoPicker }
	if err := a.show(outcome{swaps: ttyCovSwaps()}, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(termText(a), "ship-release") || !strings.Contains(stderr.String(), "the proposals are listed above, nothing was changed") {
		t.Errorf("terminal %q, stderr %q", termText(a), stderr.String())
	}
	a.pick = func([]swap) ([]swap, error) { return nil, errInterrupted }
	if err := a.show(outcome{swaps: ttyCovSwaps()}, false); !errors.Is(err, errInterrupted) {
		t.Errorf("an interrupted picker gave %v", err)
	}
}

type ttyCovFailingReader struct{}

func (ttyCovFailingReader) Read([]byte) (int, error) { return 0, errors.New("keyboard unplugged") }

func TestAppCov_an_answer_that_cannot_be_read_changes_nothing(t *testing.T) {
	a := fakeApp(t)
	calls := stepsRecorded(a, nil)
	a.stdin = ttyCovFailingReader{}
	a.pick = func(swaps []swap) ([]swap, error) { return swaps, nil }
	if err := a.chooseAndApply([]swap{writtenSwap("old")}, false); err == nil || err.Error() != "keyboard unplugged" || len(*calls) != 0 {
		t.Errorf("got %v after %v", err, *calls)
	}
}

func TestAppCov_Run_interrupted_in_the_picker_exits_130_with_the_terminal_back(t *testing.T) {
	ttyCovPrograms(t)
	ttyCovWaitingServer(t, defaultCacheDir())
	pt := ttyCovOpen(t, 30, 160)
	before := ttyCovSettings(t, pt.slave)
	var stderr bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- Run(nil, pt.slave, pt.slave, &stderr) }()
	screen := ttyCovFirstFrame(t, pt)
	if !strings.Contains(screen, "Weighed server") || !strings.Contains(screen, clearLine+"  ✓ 0 sessions read") {
		t.Errorf("the screen: %q", screen)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != exitSignal || stderr.Len() != 0 {
			t.Errorf("exit %d, stderr %q", code, stderr.String())
		}
	case <-time.After(ttyCovWait):
		t.Fatal("Run never returned")
	}
	if ttyCovSettings(t, pt.slave) != before {
		t.Error("the terminal was left in raw mode")
	}
}
