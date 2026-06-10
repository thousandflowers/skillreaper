# skillreaper Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `skillreaper` — a zero-dependency Go CLI that finds and safely prunes unused skills/MCP servers/agents in a Claude Code setup, with evidence from real transcripts.

**Architecture:** Pipeline: `scan` (inventory from config trees) + `usage` (stream-parse transcript JSONL) → `report` (join + verdicts + renderers) → `prune` (reversible quarantine + manifest). All roots injectable for fixture-based tests.

**Tech Stack:** Go ≥1.22, stdlib only. GitHub Actions CI. goreleaser on tag.

---

### Task 1: Module + repo skeleton

**Files:** Create `go.mod`, `LICENSE` (MIT, holder "thousandflowers"), `.gitignore` (`/reap`, `dist/`, `coverage.out`), `.github/workflows/ci.yml` (go vet + `go test ./... -coverprofile`, ubuntu+macos, go 1.22.x)

- [ ] `go mod init github.com/thousandflowers/skillreaper`
- [ ] Commit `chore: module skeleton, MIT license, CI`

### Task 2: internal/cost — token + money estimation

**Files:** Create `internal/cost/cost.go`, `internal/cost/cost_test.go`

- [ ] Test first:

```go
func TestTokens(t *testing.T) {
	cases := []struct{ chars, want int }{{0, 0}, {37, 10}, {38, 11}, {1, 1}}
	for _, c := range cases {
		if got := Tokens(c.chars); got != c.want {
			t.Errorf("Tokens(%d)=%d want %d", c.chars, got, c.want)
		}
	}
}

func TestMoneyPerMonth(t *testing.T) {
	// 1000 dead tok/session × 60 sessions/mo × $3/MTok = $0.18
	got := MoneyPerMonth(1000, 60, 3.0)
	if math.Abs(got-0.18) > 1e-9 { t.Errorf("got %f", got) }
}
```

- [ ] Implement: `Tokens(chars int) int` = `ceil(chars/3.7)` via integer math `(chars*10+36)/37`; `MoneyPerMonth(tokPerSession, sessionsPerMonth int, pricePerMTok float64) float64`.
- [ ] Run, pass, commit `feat: token/money cost model`

### Task 3: internal/scan — types + frontmatter parser

**Files:** Create `internal/scan/types.go`, `internal/scan/frontmatter.go`, `internal/scan/frontmatter_test.go`

- [ ] Types:

```go
type Category string
const (
	CatSkill Category = "skill"; CatAgent Category = "agent"
	CatMCP Category = "mcp"; CatHook Category = "hook"; CatProse Category = "prose"
)
type Item struct {
	Category Category; Name, Source, Path, Description string
	DescChars, BodyChars int
	InstalledAt time.Time // zero = unknown
	Removable bool
}
type Warning struct{ Path, Msg string }
```

`Name` = invocation key (`"graphify"`, `"ecc:plan"`, mcp server name, hook `event#i`, prose path).

- [ ] Frontmatter: `parseFrontmatter(b []byte) (name, desc string, bodyChars int)` — between first two `---` lines; extract `name:`/`description:` line values (trim quotes); bodyChars = len after closing `---`. Tests: normal, missing frontmatter (whole file = body), quoted values, no description.
- [ ] Commit `feat: scan types + frontmatter parser`

### Task 4: scanner — personal + plugin skills

**Files:** Create `internal/scan/skills.go`, `internal/scan/skills_test.go`; fixtures built in `t.TempDir()` (skills/myskill/SKILL.md, plugins/installed_plugins.json with absolute installPath, plugins/cache/.../skills/subskill/SKILL.md)

- [ ] Tests: `ScanSkills(claudeDir)` returns personal `myskill` (Source `personal`, Removable true) + plugin `coolplug:subskill` (Source `plugin:coolplug@mkt`, Removable false, InstalledAt from installed_plugins.json `installedAt` RFC3339). Missing dirs → empty, no error. Corrupt installed_plugins.json → Warning, personal skills still returned.
- [ ] installed_plugins.json shape (verified real): `{"version":2,"plugins":{"name@mkt":[{"installPath":"/abs/path","installedAt":"2026-05-15T22:41:04.874Z"}]}}`. Plugin skill key = `<pluginName>:<skillDir>`.
- [ ] Commit `feat: skills scanner (personal + plugin)`

### Task 5: scanner — agents

**Files:** Create `internal/scan/agents.go`, `internal/scan/agents_test.go`

- [ ] `ScanAgents(claudeDir)` — `agents/*.md` (Name = frontmatter name or file stem, Removable true) + each plugin's `agents/*.md` (Name = `plugin:stem`, Removable false). Extract shared `installedPlugins(claudeDir)` helper reused by Task 4.
- [ ] Commit `feat: agents scanner`

### Task 6: scanner — MCP servers

**Files:** Create `internal/scan/mcp.go`, `internal/scan/mcp_test.go`

- [ ] `ScanMCP(claudeJSONPath, claudeDir string)`:
  - `~/.claude.json` top-level `mcpServers{name:cfg}` → Source `user-config`, Removable true.
  - `~/.claude.json` `projects{path:{mcpServers{...}}}` → Source `project:<path>`, Removable true.
  - plugin `.mcp.json` at installPath root (`mcpServers` map) → Source `plugin:<name>`, Removable false.
  - Parse with `map[string]json.RawMessage` (file huge; only decode known keys). Description = `command args...` joined for display.
- [ ] Tests: fixture .claude.json with both scopes + plugin .mcp.json; corrupt file → Warning.
- [ ] Commit `feat: mcp scanner`

### Task 7: scanner — hooks + always-loaded prose

**Files:** Create `internal/scan/hooks.go`, `internal/scan/prose.go`, tests

- [ ] `ScanHooks(claudeDir)`: `settings.json` + `settings.local.json` → `hooks{Event:[{hooks:[{type,command}]}]}` → Item per command, Name `Event#n`, Description = command, Removable false (report-only MVP).
- [ ] `ScanProse(claudeDir, cwd)`: `~/.claude/CLAUDE.md`, `~/.claude/rules/**/*.md`, `<cwd>/CLAUDE.md` → Item, DescChars = full file size (always injected).
- [ ] Commit `feat: hooks + prose scanners`

### Task 8: internal/usage — transcript parser

**Files:** Create `internal/usage/usage.go`, `internal/usage/usage_test.go`, fixture JSONL written by test

- [ ] API:

```go
type Stats struct {
	Sessions, FilesScanned, MalformedLines int
	WindowDays int
	Uses map[scan.Category]map[string]int      // invocation key → count
	Last map[scan.Category]map[string]time.Time
}
func Parse(projectsDir string, cutoff time.Time) (*Stats, error)
```

- [ ] Walk `projectsDir/**/*.jsonl`, skip mtime < cutoff. `bufio.Scanner`, 10MB max buffer. Fast pre-filter: line contains `"tool_use"` or `<command-name>`. Entry: `{type, timestamp, message{content json.RawMessage}}`; content array of `{type, name string, input json.RawMessage}`.
  - `name=="Skill"` → input `{"skill":string}` → Uses[CatSkill][skill]++
  - `name=="Task"||name=="Agent"` → input `{"subagent_type":string}` → CatAgent
  - `strings.HasPrefix(name,"mcp__")` → server = segment between `mcp__` and next `__` → CatMCP
  - user line with `<command-name>/x</command-name>` → CatSkill key `x` (strip `/`)
- [ ] Test fixture: 2 jsonl files — skill use `{"skill":"ecc:plan"}`, Task subagent_type, `mcp__blender__get_scene_info`, command-name line, 1 malformed line; 1 old file excluded via `os.Chtimes`. Assert counts, Sessions=2, MalformedLines=1.
- [ ] Commit `feat: transcript usage parser`

### Task 9: internal/report — join, verdicts, renderers

**Files:** Create `internal/report/report.go`, `internal/report/verdict.go`, `internal/report/ansi.go`, `internal/report/markdown.go`, tests

- [ ] Verdict:

```go
func Verdict(uses, sessions, minSessions int, installedAt time.Time, cutoff time.Time) string {
	if uses > 0 { return "KEEP" }
	if sessions < minSessions { return "REVIEW" }
	if !installedAt.IsZero() && installedAt.After(cutoff) { return "REVIEW" }
	return "REAP"
}
```

Tests: all 4 branches.
- [ ] `Build(items []scan.Item, st *usage.Stats, opts Opts) *Report` — Row{Item, Uses, LastUsed, Verdict, Tokens}; usage match: exact key, else skills also match short form (suffix after `:`). Totals: DeadTokensPerSession (Σ DescChars of REAP skill/agent rows → cost.Tokens), DeadCount, SessionsPerMonth = Sessions×30/WindowDays, MoneyPerMonth via cost.
- [ ] JSON renderer = `json.MarshalIndent`. Markdown: tables per category. ANSI: hand-rolled colors (respect `NO_COLOR` + non-TTY), aligned columns, shock header. Renderer tests assert key substrings.
- [ ] Commit `feat: report builder + ansi/json/md renderers`

### Task 10: internal/prune — quarantine, manifest, restore

**Files:** Create `internal/prune/prune.go`, `internal/prune/prune_test.go`

- [ ] Manifest at `<claudeDir>/reaped/manifest.json`:

```go
type Entry struct {
	ID, Category, Name, From, To string
	ConfigPath string          `json:",omitempty"` // MCP: file edited
	JSONScope  string          `json:",omitempty"` // MCP: "" = top-level, else project path
	Payload    json.RawMessage `json:",omitempty"` // MCP: removed server cfg
	Timestamp  time.Time
	Restored   bool
}
```

- [ ] `QuarantineDir(claudeDir, item)` — `os.Rename` → `<claudeDir>/reaped/<category>/<name>`; append manifest. `RemoveMCP(claudeDir, claudeJSONPath, scope, name)` — backup file to `<claudeDir>/reaped/backups/<base>.<unixts>`, surgical edit via `map[string]json.RawMessage`, store removed payload in manifest.
- [ ] `Restore(claudeDir, id)` / `RestoreAll` — rename back; MCP re-insert payload; mark Restored.
- [ ] Tests in `t.TempDir()`: quarantine→restore round-trip (content identical); MCP remove keeps unrelated keys semantically intact (re-parse equality), restore re-inserts.
- [ ] Commit `feat: reversible prune + restore`

### Task 11: cmd/reap — CLI wiring

**Files:** Create `cmd/reap/main.go`, `cmd/reap/main_test.go`

- [ ] `run(args []string, stdin io.Reader, stdout, stderr io.Writer) int` (testable). Flags: `--days` 30, `--min-sessions` 10, `--price` 3.0, `--json`, `--md`, `--claude-dir`, `--claude-json`. Subcommands: default = scan+report; `prune [--yes]` (numbered picker on stdin: `1,3` / `all` / empty=abort; non-removable excluded with tip); `restore <id>|--all`; `version`.
- [ ] Integration test: fixture claudehome in TempDir → `run()` exit 0 + report substrings; missing dir → exit 1 friendly message.
- [ ] Commit `feat: reap CLI`

### Task 12: docs + launch kit + release plumbing

**Files:** Create `README.md`, `CONTRIBUTING.md`, `docs/launch/show-hn.md`, `docs/launch/reddit.md`, `docs/launch/x-thread.md`, `.goreleaser.yaml`, `.github/workflows/release.yml`

- [ ] README: hero output sample, pitch, install (`go install github.com/thousandflowers/skillreaper/cmd/reap@latest` + release binaries), how-it-works, honest estimation notes, safety (quarantine), roadmap (Codex/Cursor/Gemini), FAQ.
- [ ] goreleaser: darwin/linux, amd64+arm64, binary `reap`; release workflow on tag `v*`.
- [ ] Commit `docs: README + launch kit; ci: release pipeline`

### Task 13: real-machine validation + ship

- [ ] `go test ./... -cover` ≥80% per package (top up if short).
- [ ] `go build ./cmd/reap && ./reap --days 30` on owner's real `~/.claude` (180 files, 249MB): correct, <5s, no crash. Capture output for README sample.
- [ ] `gh repo create thousandflowers/skillreaper --public --source . --push`, add topics (`claude-code, ai-agents, mcp, cli, golang, developer-tools, context-engineering`), verify CI green, tag `v0.1.0`, verify release artifacts.
