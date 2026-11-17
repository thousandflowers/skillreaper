package reinvest

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// What a found candidate is installed with is on screen before the question.
// For an MCP server it is all there is to see of the code that will then start
// with every session: the question used to come with the server's name alone.
func TestCycle17_what_will_be_installed_is_shown_before_the_question(t *testing.T) {
	a := fakeApp(t)
	server := netCovServerObj(t, `{"name": "io.github.bad/files", "repository": {"url": "https://github.com/o/r"},
		"packages": [{"registryType": "npm", "identifier": "@bad/files-evil", "version": "1.0.0"}]}`)
	found := swap{Cands: []candidate{{Kind: kindMCP, Name: "files", Server: server}}, Mute: []skill{{Name: "old"}}}

	a.showSwap(found)
	a.showSwap(writtenSwap("old")) // written here: nothing is fetched or run for it

	out := termText(a)
	if want := "will run: claude mcp add -s user files -- npx -y @bad/files-evil@1.0.0"; !strings.Contains(out, want) {
		t.Errorf("the terminal lacks %q:\n%s", want, out)
	}
	if strings.Count(out, "will run:") != 1 {
		t.Errorf("a skill written here says it will run something:\n%s", out)
	}
}

// What leaves the machine as a search is a few short words. The words come
// from a model's answer, and nothing held it to that: a paragraph of a session
// could go out in a URL.
func TestCycle17_only_short_plain_words_go_out_as_a_search(t *testing.T) {
	long := strings.Repeat("word ", 400)
	got := searchWords([]string{"release manager", "Dear Acme CFO, the wire to IBAN IT60X", long, "git-flow helper", "one two three four", "C++ build", "", "x\ny"})

	equalStrings(t, got, []string{"release manager", "git-flow helper", "C++ build"})
	if many := searchWords(strings.Fields(strings.Repeat("q ", 50))); len(many) != 1 {
		t.Errorf("a word repeated is searched %d times", len(many))
	}
	var lots []string
	for i := range 40 {
		lots = append(lots, "term"+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}
	if kept := searchWords(lots); len(kept) != maxSearches {
		t.Errorf("%d searches kept, want %d", len(kept), maxSearches)
	}
}

// An npm version that is a range ("1", "1.x") is resolved again at every
// launch: only a whole version is a pin.
func TestCycle17_an_npm_package_is_pinned_to_a_whole_version(t *testing.T) {
	entry := func(kind, version string) *obj {
		return netCovServerObj(t, `{"name": "a/b", "packages": [{"registryType": "`+kind+`", "identifier": "pkg", "version": "`+version+`"}]}`)
	}
	for version, want := range map[string]bool{"1": false, "1.x": false, "1.2": false, "1.2.3": true, "1.2.3-beta.1": true, "latest": false} {
		if got := targetOf(entry("npm", version)).found(); got != want {
			t.Errorf("npm %s: installable %v, want %v", version, got, want)
		}
	}
	if !targetOf(entry("pypi", "1.0")).found() {
		t.Error("a PyPI version of two parts is exact, and was refused")
	}
}

// A page asked for over https is not taken from http, wherever it sends us.
func TestCycle17_a_redirect_never_leaves_https(t *testing.T) {
	req := func(raw string) *http.Request {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Request{URL: u}
	}
	secure, plain := req("https://skills.sh/a"), req("http://elsewhere.example/a")
	if err := noDowngrade(plain, []*http.Request{secure}); err == nil {
		t.Error("https was followed to http")
	}
	if err := noDowngrade(req("https://other.example/a"), []*http.Request{secure}); err != nil {
		t.Errorf("https to https: %v", err)
	}
	if err := noDowngrade(plain, []*http.Request{plain}); err != nil {
		t.Errorf("a local test server over http: %v", err)
	}
	if err := noDowngrade(secure, make([]*http.Request, 10)); !errors.Is(err, errTooManyRedirects) {
		t.Errorf("ten redirects: %v", err)
	}
}
