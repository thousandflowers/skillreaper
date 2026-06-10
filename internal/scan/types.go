// Package scan inventories the context payload of a Claude Code
// installation: skills, agents, MCP servers, hooks, and always-loaded
// prose (CLAUDE.md, rules). All scanners take explicit root paths so
// tests can run against fixture trees.
package scan

import "time"

// Category classifies an inventoried item.
type Category string

const (
	CatSkill Category = "skill"
	CatAgent Category = "agent"
	CatMCP   Category = "mcp"
	CatHook  Category = "hook"
	CatProse Category = "prose"
)

// Item is one entry in the agent-stack inventory.
type Item struct {
	Category    Category
	Name        string // invocation key: "graphify", "ecc:plan", mcp server name, "SessionStart#0", file path
	Source      string // "personal", "plugin:<name@mkt>", "user-config", "project:<path>"
	Path        string
	Description string    // text injected into context (skills/agents) or display string
	DescChars   int       // chars injected every session
	BodyChars   int       // chars loaded on invocation (SKILL.md body)
	InstalledAt time.Time // zero when unknown
	Removable   bool      // safe to prune automatically
}

// Warning records a non-fatal problem found while scanning.
type Warning struct {
	Path string
	Msg  string
}
