package reinvest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A kind of work the model gave no name to, and a candidate skills.sh gave no
// name to, are still called something: "for  (4 sessions)" and a blank where
// the name of what is installed belongs.
func TestCycle26_nothing_goes_by_an_empty_name(t *testing.T) {
	sessions := []session{{ID: "s1", Prompts: []string{"p"}}}
	for _, name := range []string{"", "   ", "\n"} {
		groups := []*obj{objOf("name", name, "task", "Cut a release: bump, changelog, tag.", sessionIDsKey, []any{"s1"})}
		kinds := kindsOfWork(groups, sessions)
		if len(kinds) != 1 || strings.TrimSpace(kinds[0].Name) == "" {
			t.Errorf("a group named %q: %+v", name, kinds)
		}
	}
	for _, name := range []string{"  ", "​", "\n"} {
		c, ok := skillItem(objOf("id", "o/r/s", "source", "o/r", "skillId", "s", "name", name, "installs", int64(50)))
		if !ok || c.Name != "s" {
			t.Errorf("a result named %q: %+v, %v", name, c, ok)
		}
	}
}

// A README or a package's metadata that could not be fetched is a request
// that could not be made, like a search: with --mcp the step read "0
// candidates" when GitHub did not answer. A page that is not there is not.
func TestCycle26_every_request_that_fails_is_counted(t *testing.T) {
	server := objOf("repository", objOf("url", "https://github.com/o/r"),
		"packages", []any{objOf("registryType", "pypi", "identifier", "pkg", "version", "1.0")})
	a := fakeApp(t)
	a.httpGet = func(string, time.Duration) ([]byte, error) { return nil, errors.New("dial tcp: no route to host") }
	a.httpJSON = func(string, retryPolicy) (any, error) { return nil, errors.New("dial tcp: no route to host") }
	a.readme("https://github.com/o/r", "")
	a.packageMatchesRepo(server)
	a.sayLookups()
	if said := termText(a); !strings.Contains(said, "could not be made") || !strings.Contains(said, "no route to host") {
		t.Errorf("unreachable:\n%s", said)
	}

	b := fakeApp(t)
	b.httpGet = func(string, time.Duration) ([]byte, error) {
		return nil, &httpError{code: 404, status: "404 Not Found"}
	}
	b.httpJSON = func(string, retryPolicy) (any, error) { return nil, &httpError{code: 404, status: "404 Not Found"} }
	b.readme("https://github.com/o/r", "")
	b.packageMatchesRepo(server)
	b.sayLookups()
	if said := termText(b); strings.Contains(said, "could not be made") {
		t.Errorf("a page that is not there is called a failed request:\n%s", said)
	}
}

// The reason printed is short, and says what it is when the answer was a web
// page: "json: expecting value at offset 0" named no cause.
func TestCycle26_the_reason_for_a_failed_request_is_short_and_plain(t *testing.T) {
	a := fakeApp(t)
	a.lookupFailed("skills.sh search 'x'", errors.New(strings.Repeat("a very long address ", 60)))
	a.sayLookups()
	if said := termText(a); len(said) > 600 {
		t.Errorf("%d characters:\n%s", len(said), said)
	}
	b := fakeApp(t)
	b.httpGet = func(string, time.Duration) ([]byte, error) {
		return []byte("<html>sign in to the hotel wifi</html>"), nil
	}
	if _, err := b.getJSON("https://skills.sh/api/search?q=x", onceMore); err == nil || !strings.Contains(err.Error(), "not JSON") {
		t.Errorf("a web page for an answer: %v", err)
	}
}

// One folder of sessions that cannot be read makes the count low, and is
// said: it was in the log only.
func TestCycle26_an_unreadable_folder_among_the_sessions_is_said(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	a, _ := pipelineApp(t, verdict{Keep: true, Risk: noRisk, Fit: 5, Reason: "fits", Use: "x"})
	bad := filepath.Join(a.projectsDir, "-tmp-closed")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bad, 0); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bad, 0o755) })
	_ = a.run(options{days: defaultDays, max: defaultSwaps})
	if said := termText(a); !strings.Contains(said, "✗") || !strings.Contains(said, "could not be read") || !strings.Contains(said, "4 sessions read") {
		t.Errorf("one closed folder:\n%s", said)
	}
}
