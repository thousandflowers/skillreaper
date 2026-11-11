package reinvest

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The tests in this package are the prototype's (experiments/reinvest/test_reinvest.py),
// one for one: Python's Class.test_name is Go's TestClass_name, with the same
// inputs and the same expectations. Nothing here reaches the network or runs
// Claude: an app made by fakeApp fails the test if it tries.

// fakeApp is an app whose every door to the outside is shut.
func fakeApp(t *testing.T) *app {
	t.Helper()
	a := newApp(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	a.cacheDir, a.stateDir, a.projectsDir = t.TempDir(), t.TempDir(), t.TempDir()
	a.termTTY = false
	a.lookPath = func(name string) (string, error) { return name, nil }
	a.runProcess = func(spec procSpec) procResult {
		t.Errorf("a test ran a program: %v", spec.argv)
		return procResult{err: errors.New("no programs in tests")}
	}
	a.httpGet = func(url string, _ time.Duration) ([]byte, error) {
		t.Errorf("a test reached the network: %s", url)
		return nil, errors.New("no network in tests")
	}
	a.runStep = func(cmd []string) error {
		t.Errorf("a test ran a step: %v", cmd)
		return errors.New("no steps in tests")
	}
	a.runPager = func(argv []string, _ string) error {
		t.Errorf("a test opened a pager: %v", argv)
		return nil
	}
	a.pick = func([]swap) ([]swap, error) {
		t.Error("a test opened the picker")
		return nil, nil
	}
	a.sleep = func(time.Duration) {}
	return a
}

func termText(a *app) string { return a.term.(*bytes.Buffer).String() }

// js reads a JSON literal: what the Python tests wrote as dicts and lists.
func js(t *testing.T, text string) any {
	t.Helper()
	v, err := parseJSON(text)
	if err != nil {
		t.Fatalf("bad JSON in a test: %v\n%s", err, text)
	}
	return v
}

func jsObj(t *testing.T, text string) *obj {
	t.Helper()
	o, ok := js(t, text).(*obj)
	if !ok {
		t.Fatalf("not an object: %s", text)
	}
	return o
}

type asked struct {
	prompt string
	body   string
	opts   askOptions
}

// answering makes the app's Claude give these answers, in order, and returns
// where the questions are recorded. A nil answer is a call that never answered.
func answering(t *testing.T, a *app, answers ...any) *[]asked {
	t.Helper()
	var calls []asked
	a.ask = func(prompt, body string, opts askOptions) (any, error) {
		calls = append(calls, asked{prompt, body, opts})
		if len(calls) > len(answers) {
			t.Fatalf("Claude was asked %d times, the test expected %d", len(calls), len(answers))
		}
		return answers[len(calls)-1], nil
	}
	return &calls
}

// alwaysAnswering makes every call to Claude give the same answer.
func alwaysAnswering(a *app, answer any) *[]asked {
	var calls []asked
	a.ask = func(prompt, body string, opts askOptions) (any, error) {
		calls = append(calls, asked{prompt, body, opts})
		return answer, nil
	}
	return &calls
}

// claudePrints replaces only the `claude` process: the real parsing runs on
// what it "printed".
func claudePrints(a *app, stdout string, code int) {
	a.runProcess = func(procSpec) procResult { return procResult{stdout: stdout, code: code} }
}

// verifying makes the app's verifier return what fn says, and records the calls.
func verifying(a *app, fn func(req verifyRequest) verdict) *[]verifyRequest {
	var mu sync.Mutex
	var calls []verifyRequest
	a.verify = func(req verifyRequest) (verdict, error) {
		mu.Lock()
		calls = append(calls, req)
		mu.Unlock()
		return fn(req), nil
	}
	return &calls
}

func always(v verdict) func(verifyRequest) verdict {
	return func(verifyRequest) verdict { return v }
}

// stepsRecorded makes every step succeed unless fail says otherwise, and records them.
func stepsRecorded(a *app, fail func(cmd []string) error) *[][]string {
	var calls [][]string
	a.runStep = func(cmd []string) error {
		calls = append(calls, cmd)
		if fail != nil {
			return fail(cmd)
		}
		return nil
	}
	return &calls
}

func hasPrefixWords(cmd []string, words ...string) bool {
	if len(cmd) < len(words) {
		return false
	}
	for i, w := range words {
		if cmd[i] != w {
			return false
		}
	}
	return true
}

func sameWords(a, b []string) bool {
	return len(a) == len(b) && hasPrefixWords(a, b...)
}

func containsCmd(calls [][]string, cmd ...string) bool {
	for _, c := range calls {
		if sameWords(c, cmd) {
			return true
		}
	}
	return false
}

// tempHome points HOME at a scratch folder, where skills are installed and written.
func tempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func candNames(cands []candidate) []string {
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.Name
	}
	return out
}

func slugs(cands []candidate) []string {
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.Slug
	}
	return out
}

func equalStrings(t *testing.T, got, want []string) {
	t.Helper()
	if !sameWords(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func sessionIDsOf(g *obj) []string { return stringList(g.val(sessionIDsKey)) }

// tokensFor is the weight the Python tests compute by hand: bytes * 10 / 37, rounded up.
func tokensFor(text string) int { return (len(text)*10 + 36) / 37 }
