package reinvest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// Where candidates come from: skills.sh search, plus each candidate's whole folder.

const (
	skillsShURL    = "https://skills.sh"
	searchTTL      = 7 * day // skills.sh rate-limits hard (429): reuse searches and documents for a week; install compares again
	perQuery       = 12
	skillFile      = "SKILL.md"
	skillsShTries  = 4  // a 429 waits Retry-After and retries, instead of giving up on skills.sh for the run
	skillsShWait   = 30 // seconds; longer Retry-After values are capped
	tooManyRequest = 429
)

// What may reach a command line: anything else is dropped where it comes in,
// so it can never become a flag or a URL. The first five ignored case in the
// prototype, so they fold; Z and D are what the projection writes for a word
// character and a digit outside ASCII (see pyre.go).
var (
	sourceRE    = fullPattern(`[a-z0-9][a-z0-9-]*/[a-z0-9_][\w.-]*`, true) // owner/repo
	slugRE      = fullPattern(`[a-z0-9][\w.-]*`, true)
	mcpNameRE   = fullPattern(`[a-z0-9][\w.-]*(/[a-z0-9][\w.-]*)*`, true)
	packageIDRE = fullPattern(`(@[a-z0-9][\w.-]*/)?[a-z0-9][\w.-]*`, true)
	// https only, and a shape every URL parser reads the same way: no userinfo, no backslash, no control characters.
	remoteRE    = fullPattern(`https://[a-z0-9.-]+(:[0-9D]+)?(/[\w.~%!$&'()*+,;=:@/?#-]*)?`, true)
	versionRE   = regexp.MustCompile(`\A(?:[0-9][` + pyWordClass + `.+-]*)\z`)
	subfolderRE = regexp.MustCompile(`\A(?:[` + pyWordClass + `-][` + pyWordClass + `.-]*(?:/[` + pyWordClass + `-][` + pyWordClass + `.-]*)*)\z`)
	nonNameRE   = regexp.MustCompile(`[^a-z0-9]+`)
)

// fileMark is how each bundled file appears after SKILL.md in what the verifier reads.
func fileMark(path string) string { return "===== FILE " + path + " =====" }

func sha256Hex(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// cacheFile names a cache file by hash: ids differing only in punctuation or
// case get their own file, and no id is too long for a name.
func (a *app) cacheFile(prefix, key, suffix string) string {
	return a.cachePath(prefix + "_" + sha256Hex(key) + suffix)
}

// searchQueries is what to search for a kind of work: its queries, then its
// name and each pair of words in it, which are broader.
func searchQueries(group *obj) []string {
	var terms []string
	for _, q := range group.list("queries") {
		if s, ok := q.(string); ok {
			terms = append(terms, s)
		}
	}
	var words []string
	if name, ok := group.str("name"); ok {
		words = pyFields(pyLower(name))
	}
	if len(words) > 0 {
		terms = append(terms, strings.Join(words, " "))
	}
	for i := 0; i+1 < len(words); i++ {
		terms = append(terms, words[i]+" "+words[i+1])
	}
	// One per spelling, whatever its case: written as it first came, placed
	// where it last came (the prototype's dict of reversed terms does this).
	first, last := map[string]string{}, map[string]int{}
	var keys []string
	for i, term := range terms {
		key := pyLower(term)
		if _, seen := first[key]; !seen {
			first[key] = term
			keys = append(keys, key)
		}
		last[key] = i
	}
	sort.SliceStable(keys, func(i, j int) bool { return last[keys[i]] < last[keys[j]] })
	out := make([]string, len(keys))
	for i, key := range keys {
		out[i] = first[key]
	}
	return out
}

// skillItem is one search result, or false if it is not a shape we can name,
// fetch and install.
func skillItem(v any) (candidate, bool) {
	sk, ok := v.(*obj)
	if !ok {
		return candidate{}, false
	}
	id, ok := sk.str("id")
	if !ok {
		return candidate{}, false
	}
	source, okSource := sk.str("source")
	slug, okSlug := sk.str("skillId")
	if !okSource || !okSlug || !sourceRE.matches(source) || !slugRE.matches(slug) {
		return candidate{}, false
	}
	installs, ok := pyInt(sk.val("installs"))
	if !ok {
		return candidate{}, false
	}
	name := slug
	if truthy(sk.val("name")) {
		name = pyStr(sk.val("name"))
	}
	return candidate{ID: printable(id), Kind: kindSkill, Source: source, Slug: slug, Name: oneLine(name),
		Installs: installs, Hits: map[string]int{}}, true
}

func (a *app) isFresh(path string) bool {
	info, err := os.Stat(path)
	return err == nil && a.now().Sub(info.ModTime()) < searchTTL
}

// searchPage is one query's results, from the cache when it is fresh.
func (a *app) searchPage(q string) any {
	path := a.cacheFile("search", pyLower(q), ".json")
	if a.isFresh(path) {
		if raw, err := os.ReadFile(path); err == nil && utf8.Valid(raw) {
			if data, err := parseJSON(string(raw)); err == nil {
				return data
			}
		}
	}
	data, err := a.skillsSh(skillsShURL + "/api/search?q=" + url.QueryEscape(q) + "&limit=" + fmt.Sprint(perQuery))
	if err != nil { // a failed query just contributes nothing
		a.logf("skills.sh search %s: %v", pyQuote(q), err)
		return nil
	}
	if err := writeShared(path, dumpJSON(data)); err != nil {
		a.logf("skills.sh search %s: not cached: %v", pyQuote(q), err)
	}
	return data
}

// searchSkills gives search results only (no SKILL.md yet): name, repo,
// installs and each query's rank.
func (a *app) searchSkills(queries []string, skipNames map[string]bool) []candidate {
	found := map[string]candidate{}
	var order []string
	for _, q := range queries {
		page, _ := a.searchPage(q).(*obj)
		skills, _ := page.val("skills").([]any)
		for rank, raw := range skills {
			item, ok := skillItem(raw)
			if !ok {
				a.logf("skills.sh search %s: skipped a malformed result", pyQuote(q))
				continue
			}
			if _, seen := found[item.ID]; !seen {
				found[item.ID] = item
				order = append(order, item.ID)
			}
			found[item.ID].Hits[q] = rank
		}
	}
	// by name and by folder: a result named differently can still land on a skill the user already has
	var out []candidate
	for _, id := range order {
		if it := found[id]; !skipNames[canon(it.Name)] && !skipNames[canon(it.Slug)] {
			out = append(out, it)
		}
	}
	return out
}

// fetchedSkillDoc is everything `skills add` would install, as text, cached
// for a week. "" when it cannot be read: that one candidate is skipped, and a
// miss may be a transient failure, so it is not cached.
func (a *app) fetchedSkillDoc(item candidate) string {
	path := a.cacheFile("doc", item.ID, ".md")
	if a.isFresh(path) {
		raw, err := os.ReadFile(path)
		if err == nil && utf8.Valid(raw) {
			return string(raw)
		}
		a.logf("skill doc %s: cached copy unreadable (%v)", pyQuote(item.ID), err)
		return "" // one bad document costs only that candidate
	}
	doc := a.downloadSkill(item)
	if doc == "" {
		return ""
	}
	if err := writeShared(path, doc); err != nil {
		a.logf("skill doc %s: %v", pyQuote(item.ID), err)
		return ""
	}
	return doc
}

// skillsShJSON asks skills.sh one request at a time, waiting out 429s.
func (a *app) skillsShJSON(url string) (any, error) {
	a.skillsShMu.Lock()
	defer a.skillsShMu.Unlock()
	return a.httpJSON(url, retryPolicy{tries: skillsShTries, retryOn: []int{tooManyRequest}, maxWait: skillsShWait})
}

// renderTree is SKILL.md, then every other file under its own mark: the whole
// of what gets installed.
func renderTree(tree map[string]string) string {
	rest := make([]string, 0, len(tree))
	for path := range tree {
		if path != skillFile {
			rest = append(rest, path)
		}
	}
	sort.Strings(rest)
	parts := []string{tree[skillFile]}
	for _, path := range rest {
		parts = append(parts, "\n"+fileMark(path)+"\n"+tree[path])
	}
	return strings.Join(parts, "\n")
}

func nameKey(name string) string {
	return strings.Trim(nonNameRE.ReplaceAllString(pyLower(name), "-"), "-")
}

func sameName(name, slug string) bool { return nameKey(name) == nameKey(slug) }

// downloadSkill fetches the skill's whole folder from skills.sh, the only
// source that lists every file. A folder with a file we cannot read as text,
// or whose own name is not the slug, is refused: what is checked must be what
// gets installed.
func (a *app) downloadSkill(item candidate) string {
	if !sourceRE.matches(item.Source) || !slugRE.matches(item.Slug) {
		return ""
	}
	data, err := a.skillsSh(skillsShURL + "/api/download/" + item.Source + "/" + item.Slug)
	if err != nil { // logged: a silent miss is what hid the 429s
		a.logf("skills.sh download %s: %v", item.ID, err)
		return ""
	}
	page, _ := data.(*obj)
	files, ok := page.val("files").([]any)
	if !ok {
		return ""
	}
	tree := map[string]string{}
	for _, raw := range files {
		entry, _ := raw.(*obj)
		path, okPath := entry.str("path")
		contents, okContents := entry.str("contents")
		if okPath && okContents {
			tree[path] = contents
		}
	}
	if _, has := tree[skillFile]; !has || len(tree) != len(files) {
		return ""
	}
	if !sameName(frontmatter(tree[skillFile])["name"], item.Slug) {
		return ""
	}
	return renderTree(tree)
}
