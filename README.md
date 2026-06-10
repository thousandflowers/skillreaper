# skillreaper

> Declare 87 skills. Use 4. Reap the rest.

Audit your Claude Code / OpenCode / Cursor / Codex CLI stack — every
skill, MCP server, agent, hook, and prose file — cross-referenced with
real session transcripts. Tells you with evidence what you never use and
what it costs in context tokens every session.

```text
SKILLS (description injected every session)
NAME                              PLATFORM     SOURCE                    WEIGHT/SESSION  USES  LAST USED    VERDICT
ecc:plan                          claude-code  plugin:ecc@ecc            ~13 tok          2     2026-06-05   KEEP
graphify                          claude-code  personal                  ~270 tok         0     —            REAP
dead-skill                        opencode     personal                  ~100 tok         0     —            REAP
```

**No guesses. No config. Read-only analysis + safe reversible quarantine.**

## 🔒 Privacy

**skillreaper is 100 % local.** Zero telemetry, zero network, zero
uploads. It reads your config files and session transcripts on disk,
computes verdicts, and prints results. That's it. The binary has no
dependencies and makes no outbound connections.

Your transcript data — tool calls, prompts, file contents — never leaves
your machine.

## 📦 Install

```bash
# Go (any platform)
go install github.com/thousandflowers/skillreaper/cmd/reap@latest

# npm (requires Node ≥ 18)
npx skillreaper

# Homebrew (coming soon)
# brew install thousandflowers/tap/skillreaper

# Binary download
# https://github.com/thousandflowers/skillreaper/releases
```

Requires Go ≥ 1.22 for source builds. The npm wrapper downloads a
pre-built binary and falls back to `go install` if needed.

## 🚀 Usage

```bash
reap                # scan every installed platform + report (read-only)
reap --json         # JSON report
reap --md           # Markdown report
reap prune          # quarantine unused items (reversible)
reap restore --all  # undo every prune
reap version        # print version
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `--days N` | 30 | Evidence window in days |
| `--min-sessions N` | 10 | Minimum sessions for REAP verdict |
| `--price N` | auto | Input token price ($/MTok, resolved from `--model`) |
| `--model M` | claude-3-5-sonnet | Model key for price lookup |
| `--json` | false | Output JSON |
| `--md` | false | Output Markdown |
| `--claude-dir` | auto | Override config directory (backward compat) |
| `--claude-json` | auto | Override config file path |
| `--yes` | false | Skip confirmation before prune |

**Price resolution**: `--model` sets a known model → price auto-resolves.
`--price` overrides the model price. When neither is set, falls back to
Claude 3.5 Sonnet pricing.

### Report columns

Each item shows its **platform** and **source** so you can pinpoint
where bloat comes from — personal skills, plugin registries, or
project-level configs.

### Output formats

- **Default**: colour-coded terminal table with verdicts
- **`--md`**: Markdown table (pipeline-friendly)
- **`--json`**: full structured JSON with timestamps, weights, verdicts

## 🔬 Case study: what a typical audit finds

On a real machine with Claude Code + ECC plugin installed for 6 months:

```
Items scanned:  187
Items kept:     23
Items reaped:   142 (76 %)
Items unclear:  22
```

The 142 reaped items — skills, agents, unused MCP servers — were costing
an estimated **~8 000 wasted tokens per session**. With 3 Claude Code
sessions per day and Claude 3.5 Sonnet pricing ($3/MTok input), that is:

```
8 000 tok × 3 × 30 = 720 000 tok/month → $2.16/month in wasted input
```

More importantly, every reaped skill shortens the agent's context window
by the weight of its description and instructions — meaning the agent
does not have to read 142 irrelevant definitions before getting to work.

## ⚙️ How it works

**Auto-detection** — probes every well-known config directory:

| Platform | Config path | Transcripts | Detection |
|---|---|---|---|
| **Claude Code** | `~/.claude/` | `~/.claude/projects/*.jsonl` | ✅ auto |
| **OpenCode** | `~/.config/opencode/` | `~/.opencode/opencode.db` (SQLite) | ✅ auto |
| **Cursor** | `~/.cursor/` | — | ✅ auto |
| **Codex CLI** | `~/.codex/` | `~/.codex/sessions/*.jsonl` | ✅ auto |
| **OpenClaw** | `~/.openclaw/` | — | ✅ auto |
| **Hermes** | `~/.hermes/` | `~/.hermes/sessions/*.jsonl` | ✅ auto |

Only installed platforms are scanned.

**Inventory** — within each config directory, `reap` scans:
- `skills/` — personal and plugin-supplied skills
- `agents/` — personal and plugin-supplied agents
- `mcpServers` from config JSON (user, per-project, and plugin scopes)
- `hooks` from `settings.json` / `settings.local.json`
- Always-loaded prose: `CLAUDE.md` (global + project), `rules/*.md`

**Evidence** — parses session transcripts (JSONL or SQLite). Counts
`tool_use` blocks, command invocations, and tool declarations from init
messages — all with timestamps.

**Cost estimation** — two sources:
1. **Character weight**: `ceil(chars / 3.7)` — every skill description,
   agent definition, and prose file has a known character count.
2. **Dead tool declarations**: init messages list every declared tool.
   Tools declared in init but never used in any session carry their
   schema weight as pure overhead. These appear as `dead tool chars` in
   the report summary.

MCP server schema sizes are unknown without running the server, so their
weight is marked `?`.

**Verdicts** — three outcomes:

- **REAP** — zero uses in the evidence window (≥ N sessions, installed
  before the window)
- **KEEP** — used at least once in the window
- **REVIEW** — insufficient evidence (too few sessions, or recently
  installed)

### Model pricing map

`--model` accepts known model names and resolves their input token price
automatically. Current map:

| Model | Input price ($/MTok) |
|---|---|
| claude-3-5-sonnet | 3.00 |
| claude-4-opus | 15.00 |
| claude-5-sonnet | 3.00 |
| gpt-4o | 2.50 |
| gpt-4o-mini | 0.15 |
| o3-mini | 1.10 |

The map lives in [`internal/cost/`](internal/cost/cost.go) — one-file
patch to add a new model.

### Prune is reversible quarantine

`reap prune` moves files to `<config-dir>/reaped/` with a recovery
manifest. Nothing is ever deleted. `reap restore --all` puts everything
back.

The manifest (`reaped/manifest.json`) includes a version field and
quarantine timestamp for future format migrations.

```json
{
  "version": 1,
  "quarantined_at": "2026-06-10T15:30:00Z",
  "entries": [
    {
      "id": "...",
      "category": "skill",
      "name": "dead-skill",
      "from": "~/.claude/skills/dead-skill",
      "to": "~/.claude/reaped/skills/dead-skill",
      "timestamp": "..."
    }
  ]
}
```

## 🏗 Design

- **100 % local**, zero dependencies, single static binary
- Go ≥ 1.22, stdlib only (no external JSON/DB libraries)
- All scanners accept explicit root paths — testable with bare
  directory fixtures
- Prune is reversible quarantine — never deletes, never destructive
- Multi-platform architecture — adding a new platform is one struct
  in `internal/platform/`
- Each platform has its own transcript source, config paths, and scan
  layout — all resolved at runtime

### Project structure

```
cmd/reap/            — CLI entry point, flag handling
internal/
  platform/          — platform definitions + auto-detection
  scan/              — inventory scanners (skills, agents, MCP, hooks, prose)
  usage/             — transcript parser (JSONL + SQLite) + init analysis
  report/            — evidence → verdicts + renderers (ANSI/JSON/MD)
  prune/             — reversible quarantine + restore + manifest
  cost/              — model pricing map + token/money helpers
npm/                 — npm wrapper (npx skillreaper)
```

## ⚖ License

MIT
