package reinvest

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// --online on a local server: what is fetched, how a failure reads, and
// what is cached. No test here reaches the network.

func netCovServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// netCovLog makes the app's run log readable.
func netCovLog(a *app) *bytes.Buffer {
	var log bytes.Buffer
	a.log = &log
	return &log
}

func TestNetCov_a_page_is_fetched_under_reinvests_name(t *testing.T) {
	var agent, method string
	srv := netCovServer(t, func(w http.ResponseWriter, r *http.Request) {
		agent, method = r.Header.Get("User-Agent"), r.Method
		fmt.Fprint(w, `{"ok": true}`)
	})
	body, err := httpGet(srv.URL+"/api/search?q=release", time.Second)
	if err != nil || string(body) != `{"ok": true}` {
		t.Fatalf("got %q, %v", body, err)
	}
	if agent != userAgent || method != http.MethodGet {
		t.Errorf("asked with %s as %q", method, agent)
	}
}

func TestNetCov_an_answer_that_is_no_success_is_an_http_error(t *testing.T) {
	srv := netCovServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, "busy")
	})
	_, err := httpGet(srv.URL, time.Second)
	var status *httpError
	if !errors.As(err, &status) {
		t.Fatalf("got %v", err)
	}
	if status.code != 503 || status.retryAfter != "7" || err.Error() != "HTTP Error 503 Service Unavailable" {
		t.Errorf("got %+v: %q", status, err.Error())
	}
}

func TestNetCov_an_answer_is_read_up_to_its_limit(t *testing.T) {
	srv := netCovServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), maxResponse+1000))
	})
	body, err := httpGet(srv.URL, 10*time.Second)
	if err != nil || len(body) != maxResponse {
		t.Errorf("read %d bytes (%v), want %d", len(body), err, maxResponse)
	}
}

func TestNetCov_only_plain_urls_are_requested(t *testing.T) {
	for _, bad := range []string{"", "https://skills.sh/a b", "https://skills.sh/\x7f", "https://skills.sh/é", "\thttps://skills.sh"} {
		if plainURL(bad) {
			t.Errorf("%q passed as plain", bad)
		}
		if _, err := httpGet(bad, time.Second); err == nil || !strings.Contains(err.Error(), "outside ASCII") {
			t.Errorf("%q was requested: %v", bad, err)
		}
	}
	if !plainURL("https://skills.sh/api/search?q=a%20b&limit=12~") {
		t.Error("a printable URL was refused")
	}
}

func TestNetCov_a_url_or_server_that_fails_is_an_error(t *testing.T) {
	if _, err := httpGet("http://[::1/", time.Second); err == nil {
		t.Error("a URL that does not parse was requested")
	}
	srv := httptest.NewServer(http.NotFoundHandler())
	address := srv.URL
	srv.Close()
	if _, err := httpGet(address, time.Second); err == nil {
		t.Error("a server that is gone answered")
	}
	release := make(chan struct{})
	slow := netCovServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	start := time.Now()
	if _, err := httpGet(slow.URL, 50*time.Millisecond); err == nil || time.Since(start) > 5*time.Second {
		t.Errorf("a server that never answers gave %v after %s", err, time.Since(start))
	}
}

// netCovAnswers serves the given statuses and bodies in order, the last one
// again once they run out, and counts the requests.
func netCovAnswers(t *testing.T, answers ...[3]string) (string, *int) {
	t.Helper()
	var mu sync.Mutex
	count := 0
	srv := netCovServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		answer := answers[min(count, len(answers)-1)]
		count++
		mu.Unlock()
		if answer[1] != "" {
			w.Header().Set("Retry-After", answer[1])
		}
		code := http.StatusOK
		fmt.Sscan(answer[0], &code)
		w.WriteHeader(code)
		fmt.Fprint(w, answer[2])
	})
	return srv.URL, &count
}

func netCovWaits(a *app) *[]time.Duration {
	var waits []time.Duration
	a.sleep = func(d time.Duration) { waits = append(waits, d) }
	return &waits
}

func TestNetCov_a_busy_server_is_waited_out(t *testing.T) {
	a := fakeApp(t)
	a.httpGet = httpGet
	waits := netCovWaits(a)
	url, count := netCovAnswers(t, [3]string{"503", "1_0", ""}, [3]string{"429", "Wed, 21 Oct 2026 07:28:00 GMT", ""},
		[3]string{"200", "", `{"skills": []}`})
	got, err := a.getJSON(url, retryPolicy{tries: 3, retryOn: []int{429, 503}, maxWait: 8})
	if err != nil || dumpJSON(got) != `{"skills": []}` {
		t.Fatalf("got %v, %v", got, err)
	}
	// 1_0 is ten seconds, capped at eight; a date is no number: the backoff for the second try
	if *count != 3 || len(*waits) != 2 || (*waits)[0] != 8*time.Second || (*waits)[1] != 4*time.Second {
		t.Errorf("%d requests, waits %v", *count, *waits)
	}
}

func TestNetCov_a_server_that_stays_busy_or_fails_otherwise_is_not_waited_for(t *testing.T) {
	a := fakeApp(t)
	a.httpGet = httpGet
	waits := netCovWaits(a)
	url, count := netCovAnswers(t, [3]string{"404", "", "gone"})
	if _, err := a.getJSON(url, defaultRetry); err == nil || *count != 1 || len(*waits) != 0 {
		t.Errorf("a 404 gave %v after %d requests, waits %v", err, *count, *waits)
	}
	url, count = netCovAnswers(t, [3]string{"503", "-1", ""})
	if _, err := a.getJSON(url, defaultRetry); err == nil || !strings.Contains(err.Error(), "negative Retry-After: -1") || *count != 1 {
		t.Errorf("a negative Retry-After gave %v after %d requests", err, *count)
	}
	url, count = netCovAnswers(t, [3]string{"502", "", ""})
	var status *httpError
	if _, err := a.getJSON(url, onceMore); !errors.As(err, &status) || status.code != 502 || *count != onceMore.tries {
		t.Errorf("a server busy every time gave %v after %d requests", err, *count)
	}
	if !containsInt([]int{1, 2}, 2) || containsInt([]int{1, 2}, 3) || containsInt(nil, 0) {
		t.Error("containsInt")
	}
}

func TestNetCov_an_answer_that_is_not_utf8_is_refused(t *testing.T) {
	a := fakeApp(t)
	a.httpGet = httpGet
	url, _ := netCovAnswers(t, [3]string{"200", "", "{\"a\": \"\xff\"}"})
	if got, err := a.getJSON(url, defaultRetry); got != nil || err == nil || !strings.Contains(err.Error(), "not UTF-8") {
		t.Errorf("got %v, %v", got, err)
	}
}

// ---------------------------------------------------------------------------
// The MCP registry
// ---------------------------------------------------------------------------

func TestNetCov_a_remote_address_with_no_host_has_none(t *testing.T) {
	for address, want := range map[string]string{"https://:8080/mcp": noHost, "https:///mcp": noHost,
		"https://API.Example.com:443/mcp?x#y": "api.example.com", "http://example.com": noHost} {
		if got := remoteHost(address); got != want {
			t.Errorf("%q has host %q, want %q", address, got, want)
		}
	}
}

func TestNetCov_a_repository_is_compared_without_scheme_or_dot_git(t *testing.T) {
	for address, want := range map[string]string{
		"https://github.com/Acme/Tool.git":   "github.com/acme/tool",
		"https://github.com/Acme/Tool.git\n": "github.com/acme/tool\n", // Python's "$" before a final line break
		"http://github.com/acme/tool/":       "github.com/acme/tool",
		"github.com/acme/tool.gitx":          "github.com/acme/tool.gitx",
	} {
		if got := repoKey(address); got != want {
			t.Errorf("%q is %q, want %q", address, got, want)
		}
	}
}

func TestNetCov_a_readme_is_read_only_from_github(t *testing.T) {
	a := fakeApp(t)
	var asked []string
	a.httpGet = func(url string, _ time.Duration) ([]byte, error) {
		asked = append(asked, url)
		return []byte("# readme"), nil
	}
	if got := a.readme("https://gitlab.com/acme/tool", ""); got != "" || len(asked) != 0 {
		t.Errorf("a repo outside GitHub gave %q after %v", got, asked)
	}
	if got := a.readme("https://github.com/acme/tool", "servers/fs"); got != "# readme" ||
		!sameWords(asked, []string{rawGitHub + "acme/tool/HEAD/servers/fs/README.md"}) {
		t.Errorf("got %q after %v", got, asked)
	}
}

func netCovServerObj(t *testing.T, text string) *obj { return jsObj(t, text) }

func TestNetCov_a_package_must_name_the_repo_whose_readme_is_read(t *testing.T) {
	a := fakeApp(t)
	log := netCovLog(a)
	var asked []string
	a.httpJSON = func(url string, policy retryPolicy) (any, error) {
		asked = append(asked, url)
		if strings.Contains(url, "broken") {
			return nil, errors.New("HTTP Error 500")
		}
		return js(t, `{"info": {"project_urls": {"Source": "https://github.com/Acme/Tool"}}}`), nil
	}
	pypi := netCovServerObj(t, `{"name": "io.github.acme/tool", "repository": {"url": "https://github.com/acme/tool.git"},
		"packages": [{"registryType": "pypi", "identifier": "acme-tool", "version": "1.0.0"}]}`)
	if !a.packageMatchesRepo(pypi) || !sameWords(asked, []string{"https://pypi.org/pypi/acme-tool/1.0.0/json"}) {
		t.Errorf("a PyPI package naming its repo was not matched; asked %v", asked)
	}
	broken := netCovServerObj(t, `{"name": "io.github.acme/broken", "repository": {"url": "https://github.com/acme/broken"},
		"packages": [{"registryType": "npm", "identifier": "broken", "version": "1.0.0"}]}`)
	if a.packageMatchesRepo(broken) || !strings.Contains(log.String(), "package metadata broken: HTTP Error 500") {
		t.Errorf("metadata that cannot be read matched; log %q", log.String())
	}
	remote := netCovServerObj(t, `{"name": "a/remote", "remotes": [{"type": "sse", "url": "https://mcp.example.com/sse"}]}`)
	if !a.packageMatchesRepo(remote) {
		t.Error("a remote server has nothing to compare and is shown by host: it should pass")
	}
	if a.packageMatchesRepo(netCovServerObj(t, `{"name": "a/nothing"}`)) {
		t.Error("a server with nothing to install passed")
	}
}

func TestNetCov_a_servers_readme_is_read_from_its_own_folder(t *testing.T) {
	a := fakeApp(t)
	a.httpJSON = func(string, retryPolicy) (any, error) { return js(t, `{"repository": "github.com/acme/tools"}`), nil }
	var asked []string
	a.httpGet = func(url string, _ time.Duration) ([]byte, error) {
		asked = append(asked, url)
		return []byte("# fs server"), nil
	}
	server := netCovServerObj(t, `{"name": "io.github.acme/fs", "repository": {"url": "https://github.com/acme/tools", "subfolder": "src/fs"},
		"packages": [{"registryType": "npm", "identifier": "fs", "version": "1.0.0"}]}`)
	if got := a.serverDoc(server); got != "# fs server" || !sameWords(asked, []string{rawGitHub + "acme/tools/HEAD/src/fs/README.md"}) {
		t.Errorf("got %q from %v", got, asked)
	}
}

func netCovEntry(name, extra string) string {
	return `{"server": {"name": "` + name + `", "repository": {"url": "https://github.com/acme/` + lastSegment(name) + `"}` + extra +
		`, "packages": [{"registryType": "npm", "identifier": "` + lastSegment(name) + `", "version": "1.0.0"}]},
		"_meta": {"` + officialMeta + `": {"status": "active", "isLatest": true}}}`
}

func TestNetCov_a_registry_search_that_fails_skips_only_its_word(t *testing.T) {
	a := fakeApp(t)
	log := netCovLog(a)
	a.httpJSON = func(url string, _ retryPolicy) (any, error) {
		if strings.Contains(url, "search=broken") {
			return nil, errors.New("HTTP Error 503")
		}
		return js(t, `{"servers": [`+netCovEntry("io.github.acme/git", "")+`]}`), nil
	}
	servers := a.registryServers([]string{"broken", "git"})
	if len(servers) != 1 || !strings.Contains(log.String(), "MCP registry 'broken': HTTP Error 503") {
		t.Errorf("got %d servers, log %q", len(servers), log.String())
	}
}

func TestNetCov_registry_results_are_capped_and_named_by_their_title(t *testing.T) {
	a := fakeApp(t)
	entries := make([]string, 0, mcpPerTask*3)
	for i := range mcpPerTask * 3 {
		extra := ""
		if i == 0 {
			extra = `, "title": "Acme Server 0", "description": "Reads\nfiles"`
		}
		entries = append(entries, netCovEntry(fmt.Sprintf("io.github.acme/s%d", i), extra))
	}
	a.httpJSON = func(url string, _ retryPolicy) (any, error) {
		if strings.HasPrefix(url, mcpRegistry) {
			return js(t, `{"servers": [`+strings.Join(entries, ",")+`]}`), nil
		}
		id := strings.Split(strings.TrimPrefix(url, "https://registry.npmjs.org/"), "/")[0]
		return objOf("repository", "github.com/acme/"+id), nil // every package names its repo
	}
	var mu sync.Mutex
	read := map[string]bool{}
	a.httpGet = func(url string, _ time.Duration) ([]byte, error) {
		mu.Lock()
		read[url] = true
		mu.Unlock()
		return []byte("# " + url), nil
	}
	got := a.searchRegistry([]string{"files"})
	if len(got) != mcpPerTask || len(read) != mcpPerTask*2 {
		t.Errorf("%d candidates from %d READMEs read; want %d from %d", len(got), len(read), mcpPerTask, mcpPerTask*2)
	}
	if read[rawGitHub+"acme/s10/HEAD/README.md"] {
		t.Error("a server past the cap was read")
	}
	if got[0].Name != "Acme Server 0" || got[0].Desc != "Reads files" || got[1].Name != "s1" || got[1].Desc != "" {
		t.Errorf("named %q (%q), %q (%q)", got[0].Name, got[0].Desc, got[1].Name, got[1].Desc)
	}
}

// ---------------------------------------------------------------------------
// skills.sh
// ---------------------------------------------------------------------------

func netCovRefuseSkillsSh(t *testing.T, a *app) {
	a.skillsSh = func(url string) (any, error) {
		t.Errorf("skills.sh was asked %s", url)
		return nil, errors.New("not in this test")
	}
}

func TestNetCov_a_fresh_search_is_read_from_the_cache(t *testing.T) {
	a := fakeApp(t)
	a.now = time.Now
	netCovRefuseSkillsSh(t, a)
	writeFile(t, a.cacheFile("search", "release", ".json"), `{"skills": [{"id": "a/b/c"}]}`)
	if got := dumpJSON(a.searchPage("Release")); got != `{"skills": [{"id": "a/b/c"}]}` {
		t.Errorf("got %s", got)
	}
}

func TestNetCov_a_search_that_fails_contributes_nothing(t *testing.T) {
	a := fakeApp(t)
	log := netCovLog(a)
	a.skillsSh = func(string) (any, error) { return nil, errors.New("HTTP Error 429") }
	if got := a.searchPage("release"); got != nil || !strings.Contains(log.String(), "skills.sh search 'release': HTTP Error 429") {
		t.Errorf("got %v, log %q", got, log.String())
	}
	if exists(a.cacheFile("search", "release", ".json")) {
		t.Error("a failed search was cached")
	}
}

// netCovUnwritableCache points the cache inside a file, where nothing can be written.
func netCovUnwritableCache(t *testing.T, a *app) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "file")
	writeFile(t, file, "")
	a.cacheDir = filepath.Join(file, "cache")
}

func TestNetCov_a_search_that_cannot_be_cached_is_still_used(t *testing.T) {
	a := fakeApp(t)
	log := netCovLog(a)
	netCovUnwritableCache(t, a)
	a.skillsSh = func(string) (any, error) { return js(t, `{"skills": []}`), nil }
	if got := dumpJSON(a.searchPage("release")); got != `{"skills": []}` || !strings.Contains(log.String(), "not cached") {
		t.Errorf("got %s, log %q", got, log.String())
	}
}

func TestNetCov_a_skill_document_comes_from_the_cache_while_fresh(t *testing.T) {
	a := fakeApp(t)
	a.now = time.Now
	log := netCovLog(a)
	netCovRefuseSkillsSh(t, a)
	item := candidate{ID: "acme/tools/release", Source: "acme/tools", Slug: "release"}
	path := a.cacheFile("doc", item.ID, ".md")
	writeFile(t, path, "---\nname: release\n---\n")
	if got := a.fetchedSkillDoc(item); got != "---\nname: release\n---\n" {
		t.Errorf("got %q", got)
	}
	writeFile(t, path, "not text \xff")
	if got := a.fetchedSkillDoc(item); got != "" || !strings.Contains(log.String(), "cached copy unreadable") {
		t.Errorf("a damaged cached document gave %q, log %q", got, log.String())
	}
}

func TestNetCov_a_skill_document_that_cannot_be_cached_is_skipped(t *testing.T) {
	a := fakeApp(t)
	log := netCovLog(a)
	netCovUnwritableCache(t, a)
	a.skillsSh = func(string) (any, error) {
		return js(t, `{"files": [{"path": "SKILL.md", "contents": "---\nname: release\n---\nbody"}]}`), nil
	}
	item := candidate{ID: "acme/tools/release", Source: "acme/tools", Slug: "release"}
	if got := a.fetchedSkillDoc(item); got != "" || !strings.Contains(log.String(), "skill doc 'acme/tools/release'") {
		t.Errorf("got %q, log %q", got, log.String())
	}
}

func TestNetCov_a_skill_from_an_unsafe_source_is_never_downloaded(t *testing.T) {
	a := fakeApp(t)
	netCovRefuseSkillsSh(t, a)
	for _, item := range []candidate{{Source: "acme/tools; rm", Slug: "release"}, {Source: "acme/tools", Slug: "-rf"}} {
		if got := a.downloadSkill(item); got != "" {
			t.Errorf("%+v gave %q", item, got)
		}
	}
}
