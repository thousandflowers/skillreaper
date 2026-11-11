package reinvest

import (
	"net/url"
	"regexp"
	"strings"
	"time"
)

// MCP servers come from the official registry; what is read about each is the
// README of the repository its package names.

const (
	mcpRegistry    = "https://registry.modelcontextprotocol.io/v0/servers"
	mcpPerTask     = 5
	mcpSearchLimit = "20"
	officialMeta   = "io.modelcontextprotocol.registry/official"
	readmeTimeout  = 10 * time.Second
	rawGitHub      = "https://raw.githubusercontent.com/"
	httpsScheme    = "https://"
	gitSuffix      = ".git"
)

// packageSpec is how one package type is run, pinned to the version the
// registry lists, and where its own metadata lives.
type packageSpec struct {
	runner []string
	pin    func(id, version string) string
	meta   func(id, version string) string
}

var packageSpecs = map[string]packageSpec{
	"npm": {
		runner: []string{"npx", "-y"},
		pin:    func(id, v string) string { return id + "@" + v },
		meta:   func(id, v string) string { return "https://registry.npmjs.org/" + id + "/" + v },
	},
	"pypi": {
		runner: []string{"uvx"},
		pin:    func(id, v string) string { return id + "==" + v },
		meta:   func(id, v string) string { return "https://pypi.org/pypi/" + id + "/" + v + "/json" },
	},
}

// transports maps the registry's name for a remote transport to Claude Code's.
var transports = map[string]string{"streamable-http": "http", "sse": "sse"}

var readmeNames = []string{"README.md", "readme.md"}

// The prototype's pattern ended in "$", which in Python also stands before a
// final line break: "\n?\z" says the same here.
var gitHubRepoRE = regexp.MustCompile(`\Ahttps?://github\.com/([^/]+/[^/#?]+?)(?:\.git)?(?:/tree/[^/]+/([^#?]+?))?/?(?:[#?].*)?\n?\z`)

var schemePrefixRE = regexp.MustCompile(`\Ahttps?://`)

// installTarget is what installing a server would actually run.
type installTarget struct {
	pkg    *obj // a pinned package, or
	remote *obj // an https remote
}

func (t installTarget) found() bool { return t.pkg != nil || t.remote != nil }

// asJSON is the target as the verifier is shown it.
func (t installTarget) asJSON() any {
	switch {
	case t.pkg != nil:
		return objOf("package", t.pkg)
	case t.remote != nil:
		return objOf("remote", t.remote)
	}
	return nil
}

// targetOf picks what installing this server would run: a pinned package,
// else an https remote, else nothing. The one place that chooses, so the
// label the user sees and the command that runs cannot disagree.
func targetOf(server *obj) installTarget {
	for _, raw := range server.list("packages") {
		pkg, _ := raw.(*obj)
		kind, _ := pkg.str("registryType")
		id, okID := pkg.str("identifier")
		version, okVersion := pkg.str("version")
		if _, known := packageSpecs[kind]; known && okID && okVersion && packageIDRE.matches(id) && versionRE.MatchString(version) {
			return installTarget{pkg: pkg}
		}
	}
	for _, raw := range server.list("remotes") {
		remote, _ := raw.(*obj)
		kind, _ := remote.str("type")
		address, okAddress := remote.str("url")
		if _, known := transports[kind]; known && okAddress && remoteRE.matches(address) {
			return installTarget{remote: remote}
		}
	}
	return installTarget{}
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

const noHost = "None" // what Python prints for a URL it finds no host in

// remoteHost is urlparse(address).hostname for an address remoteRE accepted.
func remoteHost(address string) string {
	if len(address) < len(httpsScheme) || asciiLower(address[:len(httpsScheme)]) != httpsScheme {
		return noHost // a scheme spelled with a letter outside ASCII is no scheme
	}
	netloc := address[len(httpsScheme):]
	if end := strings.IndexAny(netloc, "/?#"); end >= 0 {
		netloc = netloc[:end]
	}
	host, _, _ := strings.Cut(netloc, ":")
	if host == "" {
		return noHost
	}
	return pyLower(host)
}

// mcpWhere is "runs locally" for a package, "remote, <host>" for a hosted
// server: a remote one sees what you send it.
func mcpWhere(server *obj) string {
	target := targetOf(server)
	switch {
	case !target.found():
		return "unknown origin"
	case target.pkg != nil:
		return "runs locally"
	}
	address, _ := target.remote.str("url")
	return "remote, " + remoteHost(address)
}

// readme fetches the README of the server itself: its own folder when it has
// one, with no falling back to the repo's.
func (a *app) readme(repoURL, subfolder string) string {
	m := gitHubRepoRE.FindStringSubmatch(repoURL)
	if m == nil {
		return ""
	}
	folder := subfolder
	if folder == "" {
		folder = m[2]
	}
	folder = strings.Trim(folder, "/")
	if folder != "" && !subfolderRE.MatchString(folder) {
		return ""
	}
	for _, name := range readmeNames {
		path := name
		if folder != "" {
			path = folder + "/" + name
		}
		raw, err := a.httpGet(rawGitHub+m[1]+"/HEAD/"+path, readmeTimeout)
		if err != nil {
			a.logf("readme %s: %v", path, err)
			continue
		}
		return decodeUTF8Replace(raw)
	}
	return ""
}

func isCurrent(entry *obj) bool {
	meta := entry.object("_meta").object(officialMeta)
	return meta.val("status") == "active" && meta.val("isLatest") == true
}

// repoKey is a repository address without its scheme and ".git", lower case.
func repoKey(address string) string {
	address = schemePrefixRE.ReplaceAllString(address, "")
	switch { // Python's "$" also stands before a final line break
	case strings.HasSuffix(address, gitSuffix):
		address = strings.TrimSuffix(address, gitSuffix)
	case strings.HasSuffix(address, gitSuffix+"\n"):
		address = strings.TrimSuffix(address, gitSuffix+"\n") + "\n"
	}
	return pyLower(strings.Trim(address, "/"))
}

// packageMatchesRepo reports whether a package names the repo whose README
// was read: otherwise a clean README vouches for any package. Only a remote
// has nothing to compare: the verifier and the user see its host instead.
func (a *app) packageMatchesRepo(server *obj) bool {
	target := targetOf(server)
	if target.pkg == nil {
		return target.found()
	}
	kind, _ := target.pkg.str("registryType")
	id, _ := target.pkg.str("identifier")
	version, _ := target.pkg.str("version")
	repo := ""
	if address, has := server.object("repository").get("url"); has {
		repo = repoKey(pyStr(address))
	}
	meta, err := a.httpJSON(packageSpecs[kind].meta(id, version), onceMore)
	if err != nil { // not confirmed is not a match
		a.logf("package metadata %s: %v", id, err)
		return false
	}
	return repo != "" && strings.Contains(pyLower(dumpJSON(meta)), repo)
}

func (a *app) serverDoc(server *obj) string {
	if !a.packageMatchesRepo(server) {
		return ""
	}
	repo := server.object("repository")
	address, _ := repo.str("url")
	subfolder := ""
	if truthy(repo.val("subfolder")) {
		subfolder = pyStr(repo.val("subfolder"))
	}
	return a.readme(address, subfolder)
}

func lastSegment(name string) string {
	return name[strings.LastIndex(name, "/")+1:]
}

// registryServers lists the servers the registry returns for these words,
// one per name, in the order met. Only the active latest version of each: an
// old or deleted entry is never a candidate.
func (a *app) registryServers(words []string) []*obj {
	seen := map[string]bool{}
	var servers []*obj
	for _, w := range words {
		query := "search=" + url.QueryEscape(w) + "&limit=" + mcpSearchLimit + "&version=latest"
		data, err := a.httpJSON(mcpRegistry+"?"+query, onceMore)
		if err != nil {
			a.logf("MCP registry %s: %v", pyQuote(w), err)
			continue
		}
		page, _ := data.(*obj)
		for _, raw := range page.list("servers") {
			entry, _ := raw.(*obj)
			server := entry.object("server")
			name, ok := server.str("name")
			if !ok || !isCurrent(entry) || !mcpNameRE.matches(name) || seen[name] {
				continue
			}
			if repo, has := server.get("repository"); has && truthy(repo) {
				if _, isObject := repo.(*obj); !isObject {
					continue
				}
			}
			seen[name] = true
			servers = append(servers, server)
		}
	}
	return servers
}

// searchRegistry finds MCP servers for these words (the registry search is
// literal: single words work, phrases don't), each with its README.
func (a *app) searchRegistry(words []string) []candidate {
	servers := a.registryServers(words)
	if len(servers) > mcpPerTask*2 {
		servers = servers[:mcpPerTask*2]
	}
	docs := parallelMap(servers, 8, a.serverDoc)
	var out []candidate
	for i, server := range servers {
		if docs[i] == "" || len(out) == mcpPerTask {
			continue
		}
		name, _ := server.str("name")
		title := lastSegment(name)
		if truthy(server.val("title")) {
			title = pyStr(server.val("title"))
		}
		desc := ""
		if v, has := server.get("description"); has {
			desc = pyStr(v)
		}
		out = append(out, candidate{ID: name, Kind: kindMCP, Name: oneLine(title), Desc: oneLine(desc),
			Server: server, Doc: docs[i]})
	}
	return out
}
