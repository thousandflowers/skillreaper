// Package scan inventories the context payload of an AI coding agent
// installation: skills, agents, MCP servers, hooks, and always-loaded
// prose. All scanners take explicit root paths so tests can run against
// fixture trees.
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
	Name        string // invocation key: "graphify", "ecc:plan", mcp server name
	Platform    string // platform ID: "claude-code", "opencode", "cursor", etc.
	Source      string // "personal", "plugin:<name@mkt>", "user-config", "project:<path>"
	Path        string
	Description string    // text injected into context (skills/agents) or display string
	DescChars   int       // chars injected every session
	BodyChars   int       // chars loaded on invocation (SKILL.md body)
	InstalledAt time.Time // zero when unknown
	Removable   bool      // safe to prune automatically
	// ToolSurface is the permission breadth of a skill/agent: the count of
	// tools it is restricted to via the "allowed-tools" (skill) or "tools"
	// (agent) frontmatter field. ToolSurfaceAll means no restriction — the
	// item may use every tool, the widest and most prune-worthy surface.
	// Zero for categories where it does not apply (MCP, hook, prose).
	ToolSurface int
}

// ToolSurfaceAll marks an item with no allowed-tools restriction: it can use
// every tool. It is the widest permission surface, so an unused item with
// ToolSurfaceAll is the first thing worth pruning.
const ToolSurfaceAll = -1

// Warning records a non-fatal problem found while scanning.
type Warning struct {
	Path string
	Msg  string
}
