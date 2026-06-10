# skillreaper

> You installed 87 skills. Your agent used 4. Reap the rest.

`reap` scans your AI coding agent stack — skills, MCP servers, agents,
hooks, and always-loaded prose — and cross-references it with real
session transcripts. It tells you, with evidence, what you never use and
what it costs you in context tokens every session.

No guesses. No config. Read-only analysis + safe reversible prune.

## Supported platforms

| Platform | Config path | Transcripts | Detection |
|---|---|---|---|
| **Claude Code** | ~/.claude/ | ~/.claude/projects/*.jsonl | ✅ auto |
| **OpenCode** | ~/.config/opencode/ | ~/.opencode/opencode.db (SQLite) | ✅ auto |
| **Cursor** | ~/.cursor/ | — | ✅ auto |
| **Codex CLI** | ~/.codex/ | ~/.codex/sessions/*.jsonl | ✅ auto |
| **OpenClaw** | ~/.openclaw/ | — | ✅ auto |
| **Hermes** | ~/.hermes/ | ~/.hermes/sessions/*.jsonl | ✅ auto |

`reap` detects every installed platform automatically and scans all of
them in one pass — no flags needed.

## Install

```bash
go install github.com/thousandflowers/skillreaper/cmd/reap@latest
```

Or download a binary from [releases](https://github.com/thousandflowers/skillreaper/releases).

Requires Go ≥1.22.

## Usage

```bash
reap                # scan every installed platform + report (read-only)
reap --md           # Markdown report
reap --json         # JSON report
reap prune          # quarantine unused items (reversible)
reap restore --all  # undo every prune
reap version        # print version
```

### Report output

A typical report:

```
SKILLS (description injected every session)
NAME                              PLATFORM     SOURCE                    WEIGHT/SESSION  USES  LAST USED    VERDICT
ecc:plan                          claude-code  plugin:ecc@ecc            ~13 tok          2     2026-06-05   KEEP
graphify                          claude-code  personal                  ~270 tok         0     —            REAP
dead-skill                        opencode     personal                  ~100 tok         0     —            REAP

MCP SERVERS (tool schemas injected; weight unknown without running them)
NAME                              PLATFORM     SOURCE                    WEIGHT/SESSION  USES  LAST USED    VERDICT
time                             opencode     user-config               ?               0     —            REAP
```

Every row shows which **platform** the item belongs to, so you can
pinpoint the source of bloat.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--days N` | 30 | Evidence window in days |
| `--min-sessions N` | 10 | Sessions needed for REAP verdicts |
| `--price N` | 3.0 | Input token price ($/MTok) |
| `--json` | false | Output JSON |
| `--md` | false | Output Markdown |
| `--claude-dir` | auto | Override config directory (backward compat) |
| `--claude-json` | auto | Override config file path |
| `--yes` | false | Prune without confirmation |

## How it works

**Auto-detection** — `reap` probes the well-known config directories for
every supported platform. Only installed platforms are scanned.

**Inventory** — within each platform's config directory, `reap` scans:
- `skills/` — personal and plugin-supplied skills
- `agents/` — personal and plugin-supplied agents
- `mcpServers` from config JSON (user, per-project, and plugin scopes)
- `hooks` from `settings.json` / `settings.local.json`
- Always-loaded prose: `CLAUDE.md` (global + project), `rules/*.md`

**Evidence** — parses session transcripts (JSONL or SQLite depending on
the platform). Counts `tool_use` blocks and command invocations with
timestamps.

**Verdicts** — three outcomes per item:

- **REAP** — zero uses in the evidence window (≥ N sessions, installed
  before the window)
- **KEEP** — used at least once in the window
- **REVIEW** — insufficient evidence (too few sessions, or recently
  installed)

**Prune** — moves files to `<config-dir>/reaped/` with a recovery
manifest. No data loss. `reap restore` puts everything back.

### Cost estimation

Tokens ≈ `ceil(chars / 3.7)` (English prose average; documented
estimate). MCP schema sizes are unknown without running the server, so
their weight is marked `?`.

## Design

- **100% local**, zero dependencies, single static binary
- Go ≥1.22, stdlib only (no external JSON/DB libraries)
- **All scanners accept explicit root paths** — testable with bare
  directory fixtures
- **Prune is reversible quarantine** — never deletes, never destructive
- **Multi-platform architecture** — adding a new platform is one struct
  in `internal/platform/`
- Each platform has its own transcript source, config paths, and scan
  layout — all resolved at runtime

### Project structure

```
cmd/reap/            — CLI entry point
internal/
  platform/          — platform definitions + auto-detection
  scan/              — inventory scanners (skills, agents, MCP, hooks, prose)
  usage/             — transcript parser (JSONL + SQLite)
  report/            — evidence → verdicts + renderers (ANSI/JSON/MD)
  prune/             — reversible quarantine + restore
  cost/              — token/money estimation helpers
```

## License

MIT
