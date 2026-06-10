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
)

// Info describes one supported platform and its install paths.
type Info struct {
	ID              ID
	Name            string // display name
	ConfigDir       string // home config directory (~/.claude, ~/.config/opencode)
	ConfigFile      string // main config file name
	HasSkills       bool
	HasAgents       bool
	HasMCP          bool
	HasHooks        bool
	HasProse        bool
	HasTranscripts  bool
	TranscriptType  string // "jsonl" | "sqlite" | "none"

	// Derived from detection; empty when not installed.
	ConfigDirAbs   string // resolved absolute config directory
	ConfigFileAbs  string // resolved absolute config file path

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
	home, _ := os.UserHomeDir()
	_ = home
	return []Info{
		{
			ID:           ClaudeCode,
			Name:         "Claude Code",
			ConfigDir:    "~/.claude",
			ConfigFile:   "~/.claude.json",
			HasSkills:    true,
			HasAgents:    true,
			HasMCP:       true,
			HasHooks:     true,
			HasProse:     true,
			HasTranscripts: true,
			TranscriptType: "jsonl",
		},
		{
			ID:           OpenCode,
			Name:         "OpenCode",
			ConfigDir:    "~/.config/opencode",
			ConfigFile:   "~/.config/opencode/opencode.jsonc",
			HasSkills:    true,
			HasAgents:    true,
			HasMCP:       true,
			HasHooks:     false,
			HasProse:     true,
			HasTranscripts: true,
			TranscriptType: "sqlite",
		},
		{
			ID:           Cursor,
			Name:         "Cursor",
			ConfigDir:    "~/.cursor",
			ConfigFile:   "~/.cursor/mcp.json",
			HasSkills:    true,
			HasAgents:    true,
			HasMCP:       true,
			HasHooks:     false,
			HasProse:     false,
			HasTranscripts: false,
			TranscriptType: "none",
		},
		{
			ID:           CodexCLI,
			Name:         "Codex CLI",
			ConfigDir:    "~/.codex",
			ConfigFile:   "~/.codex/config.toml",
			HasSkills:    true,
			HasAgents:    true,
			HasMCP:       true,
			HasHooks:     false,
			HasProse:     true,
			HasTranscripts: true,
			TranscriptType: "jsonl",
		},
		{
			ID:           OpenClaw,
			Name:         "OpenClaw",
			ConfigDir:    "~/.openclaw",
			ConfigFile:   "~/.openclaw/openclaw.json",
			HasSkills:    true,
			HasAgents:    false,
			HasMCP:       true,
			HasHooks:     false,
			HasProse:     false,
			HasTranscripts: false,
			TranscriptType: "none",
		},
		{
			ID:           Hermes,
			Name:         "Hermes",
			ConfigDir:    "~/.hermes",
			ConfigFile:   "~/.hermes/config.yaml",
			HasSkills:    true,
			HasAgents:    false,
			HasMCP:       true,
			HasHooks:     false,
			HasProse:     true,
			HasTranscripts: true,
			TranscriptType: "jsonl",
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
		opencodeDB := filepath.Join(home, ".opencode", "opencode.db")
		if _, err := os.Stat(opencodeDB); err == nil {
			p.TranscriptDB = opencodeDB
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

	case Hermes:
		p.SkillDirs = []string{filepath.Join(cfgDir, "skills")}
		sessionsDir := filepath.Join(cfgDir, "sessions")
		if dirExists(sessionsDir) {
			p.TranscriptDirs = []string{sessionsDir}
		}
	}

	return p
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
