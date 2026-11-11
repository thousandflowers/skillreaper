package scan

import (
	"strings"
)

// TOMLTables extracts table headers and their scalar string keys from a TOML
// document, enough to inventory the tables skillreaper cares about.
//
// This is deliberately not a TOML implementation. go.mod has no requires and
// that is a documented property of the project, so a dependency for one config
// file is a poor trade. What is parsed here is the shape agent tools actually
// write: [table] and [table."quoted.sub"] headers, key = "value" pairs, and
// comments. Arrays, inline tables, multi-line strings and typed scalars are
// read as opaque text — no caller needs them, and pretending otherwise is how
// a half-parser starts claiming more than it can do.
//
// The result maps a table header to its keys. A document with no tables
// returns an empty map, not an error: an agent config with no MCP section is
// the normal case, not a failure.
func TOMLTables(b []byte) map[string]map[string]string {
	tables := map[string]map[string]string{}
	current := ""

	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			current = tableHeader(line)
			if _, ok := tables[current]; !ok {
				tables[current] = map[string]string{}
			}
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || current == "" {
			continue
		}
		key := strings.Trim(strings.TrimSpace(k), `"'`)
		val := strings.TrimSpace(v)
		// Strip a trailing comment only when it is outside a quoted value, so
		// a "#" inside a string survives.
		if !strings.HasPrefix(val, `"`) && !strings.HasPrefix(val, `'`) {
			if i := strings.Index(val, "#"); i >= 0 {
				val = strings.TrimSpace(val[:i])
			}
		}
		tables[current][key] = strings.Trim(val, `"'`)
	}
	return tables
}

// TOMLSubTables returns the immediate children of a TOML table prefix, keyed by
// the child's own name. `TOMLSubTables(doc, "mcp_servers")` over a document
// holding [mcp_servers.fs] and [mcp_servers."with.dot"] yields "fs" and
// "with.dot".
func TOMLSubTables(b []byte, prefix string) map[string]map[string]string {
	out := map[string]map[string]string{}
	for header, keys := range TOMLTables(b) {
		parts := tomlPath(header)
		// Only immediate children: a deeper table belongs to the child, not here.
		if len(parts) == 2 && parts[0] == prefix {
			out[parts[1]] = keys
		}
	}
	dottedServers(b, prefix, out)
	return out
}

// tableHeader returns what is between a header's brackets, ignoring spaces, a
// trailing comment and the doubled brackets of an array of tables.
func tableHeader(line string) string {
	body := strings.TrimLeft(line, "[")
	var quote rune
	for i, r := range body {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == ']':
			return normalizeHeader(body[:i])
		}
	}
	return normalizeHeader(body)
}

func notBareKey(r rune) bool {
	return !(r == '_' || r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
}

// normalizeHeader drops spaces around segments and re-quotes any that need it,
// so TOMLTables keys stay unambiguous: mcp_servers."a.b" and not mcp_servers.a.b.
func normalizeHeader(h string) string {
	parts := tomlPath(h)
	for i, p := range parts {
		if strings.IndexFunc(p, notBareKey) >= 0 {
			parts[i] = `"` + p + `"`
		}
	}
	return strings.Join(parts, ".")
}

// tomlPath splits a dotted path, with quotes keeping a dot inside one segment.
func tomlPath(path string) []string {
	var parts []string
	var cur strings.Builder
	var quote rune
	for _, r := range path {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			cur.WriteRune(r)
		case r == '"' || r == '\'':
			quote = r
		case r == '.':
			parts = append(parts, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	parts = append(parts, strings.TrimSpace(cur.String()))
	return parts
}

// dottedServers adds servers written as dotted keys outside any table:
// prefix.name = { command = "x" }.
func dottedServers(b []byte, prefix string, out map[string]map[string]string) {
	inTable := false
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "[") {
			inTable = true
		}
		k, v, ok := strings.Cut(line, "=")
		if inTable || !ok || strings.HasPrefix(line, "#") {
			continue
		}
		parts := tomlPath(strings.TrimSpace(k))
		if len(parts) == 2 && parts[0] == prefix {
			out[parts[1]] = inlineTable(v)
		}
	}
}

// inlineTable reads the string keys of { a = "x", b = "y" }.
func inlineTable(v string) map[string]string {
	keys := map[string]string{}
	body := strings.Trim(strings.TrimSpace(v), "{}")
	for _, pair := range strings.Split(body, ",") {
		if k, val, ok := strings.Cut(pair, "="); ok {
			keys[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(val), `"'`)
		}
	}
	return keys
}
