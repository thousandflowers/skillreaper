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
			// Array-of-tables headers ([[x]]) are tables for this purpose.
			name := strings.Trim(line, "[]")
			current = name
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
		rest, ok := strings.CutPrefix(header, prefix+".")
		if !ok || rest == "" {
			continue
		}
		// Only immediate children: a deeper table belongs to the child, not here.
		name := strings.Trim(rest, `"'`)
		if strings.Contains(rest, ".") && !strings.HasPrefix(rest, `"`) {
			continue
		}
		out[name] = keys
	}
	return out
}
