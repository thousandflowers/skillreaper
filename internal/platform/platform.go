// Package platform defines the AI coding tools that skillreaper
// supports and auto-detects which are installed on the current machine.
package platform

import (
	"os"
	"path/filepath"
	"sort"
)

// ID uniquely identifies an AI coding platform.
type ID string

const (
	ClaudeCode ID = "claude-code"
	OpenCode   ID = "opencode"
	Cursor     ID = "cursor"
	CodexCLI   ID = "codex"
	OpenClaw   ID = "openclaw"
	Hermes     ID = "hermes"
	GeminiCLI  ID = "gemini"
)

// Info describes one supported platform and its install paths.
type Info struct {
	ID         ID
	Name       string // display name
	ConfigDir  string // home config directory (~/.claude, ~/.config/opencode)
	ConfigFile string // main config file name
	// ConfigFormat is how ConfigFile is encoded. Empty means "json", which is
	// what every platform used before Codex (TOML) and OpenCode (JSONC) were
	// added — both of which were being handed to encoding/json and failing by
	// construction, with no parser for what they actually are.
	ConfigFormat   string // "" or "json" | "jsonc" | "toml" | "yaml"
	HasSkills      bool
	HasAgents      bool
	HasMCP         bool
	HasHooks       bool
	HasProse       bool
	HasTranscripts bool
	// TranscriptType picks the parser. "jsonl" is Claude Code's format, and
	// nothing else may claim it: the parser would find no fires in another tool's
	// lines and every item would read as unused. "codex-jsonl" and "sqlite" have
	// parsers. Anything else is declared unparsed, so its items stay REVIEW.
	TranscriptType string
	// NestedSkills means skills may sit in category folders, one level down.
	NestedSkills bool
	// ReadsProjectCLAUDE means the tool loads the working directory's CLAUDE.md.
	ReadsProjectCLAUDE bool
	// TranscriptDBTried is where a sqlite history was looked for, for the warning.
	TranscriptDBTried string

	// Derived from detection; empty when not installed.
	ConfigDirAbs  string // resolved absolute config directory
	ConfigFileAbs string // resolved absolute config file path

	// Transcript locations (resolved).
	TranscriptDirs []string // for jsonl-based tools
	TranscriptDB   string   // for sqlite-based tools

	// Item root paths (resolved).
	SkillDirs []string
	AgentDirs []string
	ProseDirs []string // always-loaded prose files
}

// All returns the list of all known platform definitions (paths relative).
func All() []Info {
	// Claude Code keeps both its directory and its .claude.json under
	// CLAUDE_CONFIG_DIR when that is set.
	claudeDir, claudeFile := "~/.claude", "~/.claude.json"
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		claudeDir, claudeFile = d, filepath.Join(d, ".claude.json")
	}
	return []Info{
		{
			ID:             ClaudeCode,
			Name:           "Claude Code",
			ConfigDir:      claudeDir,
			ConfigFile:     claudeFile,
			HasSkills:      true,
			HasAgents:      true,
			HasMCP:         true,
			HasHooks:       true,
			HasProse:       true,
			HasTranscripts: true,
			TranscriptType: "jsonl",

			ReadsProjectCLAUDE: true,
		},
		{
			ID:             OpenCode,
			Name:           "OpenCode",
			ConfigDir:      "~/.config/opencode",
			ConfigFile:     "~/.config/opencode/opencode.jsonc",
			ConfigFormat:   "jsonc",
			HasSkills:      true,
			HasAgents:      true,
			HasMCP:         true,
			HasHooks:       false,
			HasProse:       true,
			HasTranscripts: true,
			TranscriptType: "sqlite",
			// Its Claude Code compatibility: CLAUDE.md, the same as ProseDirs below.
			ReadsProjectCLAUDE: true,
		},
		{
			ID:             Cursor,
			Name:           "Cursor",
			ConfigDir:      "~/.cursor",
			ConfigFile:     "~/.cursor/mcp.json",
			HasSkills:      true,
			HasAgents:      true,
			HasMCP:         true,
			HasHooks:       false,
			HasProse:       false,
			HasTranscripts: false,
			TranscriptType: "none",
		},
		{
			ID:             CodexCLI,
			Name:           "Codex CLI",
			ConfigDir:      "~/.codex",
			ConfigFile:     "~/.codex/config.toml",
			ConfigFormat:   "toml",
			HasSkills:      true,
			HasAgents:      true,
			HasMCP:         true,
			HasHooks:       false,
			HasProse:       true,
			HasTranscripts: true,
			TranscriptType: "codex-jsonl",
		},
		{
			ID:             OpenClaw,
			Name:           "OpenClaw",
			ConfigDir:      "~/.openclaw",
			ConfigFile:     "~/.openclaw/openclaw.json",
			ConfigFormat:   "jsonc",
			HasSkills:      true,
			HasAgents:      false,
			HasMCP:         true,
			HasHooks:       false,
			HasProse:       false,
			HasTranscripts: false,
			TranscriptType: "none",
		},
		{
			ID:         GeminiCLI,
			Name:       "Gemini CLI",
			ConfigDir:  "~/.gemini",
			ConfigFile: "~/.gemini/settings.json",
			// Gemini's custom commands are TOML, not SKILL.md frontmatter, and
			// it has no subagent directory — claiming either would inventory
			// nothing while implying coverage. MCP servers live in settings.json
			// under the same mcpServers key every other platform here uses, and
			// GEMINI.md is loaded into every session the way CLAUDE.md is.
			HasSkills: false,
			HasAgents: false,
			HasMCP:    true,
			HasHooks:  false,
			HasProse:  true,
			// Gemini does keep session history, in a layout this parser does not
			// read yet. Declaring it is what marks the platform evidence-blind,
			// so its items surface as REVIEW instead of being REAP'd on evidence
			// that was never collected. Setting HasTranscripts:false would look
			// tidier and silently produce false REAP verdicts instead.
			HasTranscripts: true,
			TranscriptType: "gemini-json",
		},
		{
			ID:             Hermes,
			Name:           "Hermes",
			ConfigDir:      "~/.hermes",
			ConfigFile:     "~/.hermes/config.yaml",
			ConfigFormat:   "yaml",
			HasSkills:      true,
			HasAgents:      false,
			HasMCP:         true,
			HasHooks:       false,
			HasProse:       true,
			HasTranscripts: true,
			// The history is in ~/.hermes/state.db, which no parser reads yet;
			// ~/.hermes/sessions only holds request dumps.
			TranscriptType: "hermes-sqlite",
			NestedSkills:   true,
		},
	}
}

func expandHome(path string) string {
	if len(path) == 0 || path[0] != '~' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

// Detect probes which platforms are installed and returns their
// resolved Info structs. Platforms are sorted: Claude Code first,
// then alphabetically.
func Detect() []Info {
	all := All()
	var installed []Info
	for _, p := range all {
		resolved := resolve(p)
		if resolved.ConfigDirAbs != "" {
			installed = append(installed, resolved)
		}
	}
	if len(installed) == 0 {
		return nil
	}
	sort.SliceStable(installed, func(i, j int) bool {
		if installed[i].ID == ClaudeCode {
			return true
		}
		if installed[j].ID == ClaudeCode {
			return false
		}
		return installed[i].ID < installed[j].ID
	})
	return installed
}

func resolve(p Info) Info {
	cfgDir := expandHome(p.ConfigDir)
	cfgFile := expandHome(p.ConfigFile)

	info, err := os.Stat(cfgDir)
	if err != nil || !info.IsDir() {
		return p
	}

	p.ConfigDirAbs = cfgDir
	if _, err := os.Stat(cfgFile); err == nil {
		p.ConfigFileAbs = cfgFile
	}

	home, _ := os.UserHomeDir()

	switch p.ID {
	case ClaudeCode:
		p.SkillDirs = []string{filepath.Join(cfgDir, "skills")}
		p.AgentDirs = []string{filepath.Join(cfgDir, "agents")}
		p.ProseDirs = []string{
			filepath.Join(cfgDir, "CLAUDE.md"),
			filepath.Join(cfgDir, "rules"),
		}
		p.TranscriptDirs = []string{filepath.Join(cfgDir, "projects")}

	case OpenCode:
		p.SkillDirs = []string{filepath.Join(cfgDir, "skills")}
		p.AgentDirs = []string{filepath.Join(cfgDir, "agents")}
		if claudeDir := filepath.Join(home, ".claude"); dirExists(claudeDir) {
			p.ProseDirs = []string{
				filepath.Join(claudeDir, "CLAUDE.md"),
				filepath.Join(claudeDir, "rules"),
			}
		}
		if p.ConfigFileAbs == "" {
			if alt := filepath.Join(cfgDir, "opencode.json"); fileExists(alt) {
				p.ConfigFileAbs = alt
			}
		}
		dataDir := os.Getenv("XDG_DATA_HOME")
		if dataDir == "" {
			dataDir = filepath.Join(home, ".local", "share")
		}
		// Where OpenCode keeps it today, then where older versions did.
		current := filepath.Join(dataDir, "opencode", "opencode.db")
		p.TranscriptDBTried = current
		for _, db := range []string{current, filepath.Join(home, ".opencode", "opencode.db")} {
			if fileExists(db) {
				p.TranscriptDB = db
				break
			}
		}

	case Cursor:
		p.SkillDirs = []string{filepath.Join(cfgDir, "skills-cursor")}
		p.AgentDirs = []string{filepath.Join(cfgDir, "agents")}
		cursorSkills := filepath.Join(cfgDir, "skills")
		if dirExists(cursorSkills) {
			p.SkillDirs = append(p.SkillDirs, cursorSkills)
		}
		agentSkills := filepath.Join(home, ".agents", "skills")
		if dirExists(agentSkills) {
			p.SkillDirs = append(p.SkillDirs, agentSkills)
		}

	case CodexCLI:
		p.SkillDirs = []string{filepath.Join(cfgDir, "skills")}
		agentSkills := filepath.Join(home, ".agents", "skills")
		if dirExists(agentSkills) {
			p.SkillDirs = append(p.SkillDirs, agentSkills)
		}
		p.ProseDirs = []string{
			filepath.Join(cfgDir, "AGENTS.md"),
			filepath.Join(cfgDir, "rules"),
		}
		codexAgents := filepath.Join(cfgDir, "agents")
		if dirExists(codexAgents) {
			p.AgentDirs = []string{codexAgents}
		}
		sessionsDir := filepath.Join(cfgDir, "sessions")
		if dirExists(sessionsDir) {
			p.TranscriptDirs = []string{sessionsDir}
		}

	case OpenClaw:
		p.SkillDirs = []string{filepath.Join(cfgDir, "skills")}
		agentSkills := filepath.Join(home, ".agents", "skills")
		if dirExists(agentSkills) {
			p.SkillDirs = append(p.SkillDirs, agentSkills)
		}

	case GeminiCLI:
		p.ProseDirs = []string{filepath.Join(cfgDir, "GEMINI.md")}
		// Recorded so the report can name where evidence would come from, even
		// though no parser reads this layout yet.
		if tmpDir := filepath.Join(cfgDir, "tmp"); dirExists(tmpDir) {
			p.TranscriptDirs = []string{tmpDir}
		}

	case Hermes:
		p.SkillDirs = []string{filepath.Join(cfgDir, "skills")}
	}

	return p
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
