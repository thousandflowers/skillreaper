# skillreaper

> Declare 87 skills. Use 4. Reap the rest.

<p align="center">
  <a href="https://github.com/thousandflowers/skillreaper/actions/workflows/ci.yml"><img src="https://github.com/thousandflowers/skillreaper/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/thousandflowers/skillreaper/releases"><img src="https://img.shields.io/github/v/release/thousandflowers/skillreaper" alt="Release"></a>
  <a href="https://github.com/thousandflowers/skillreaper/issues"><img src="https://img.shields.io/github/issues/thousandflowers/skillreaper" alt="Issues"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue" alt="MIT"></a>
</p>

<p align="center">
  <img src="docs/reap-demo.gif" alt="reap in action" width="720">
</p>

**Your AI agent loads every skill, MCP server, agent, and rule file
every single session.** Most of it never gets used. Skillreaper tells
you — with evidence from your own transcripts — what is bloat and what
it costs you in context window, latency, and response quality.

**One command. Zero config. Read-only. Reversible quarantine.**

```
Items scanned:  187
Items reaped:   142 (76 %)
```

---

## 🔒 Privacy

**skillreaper is 100 % local.** Zero telemetry, zero network, zero
uploads. It reads your config files and session transcripts on disk,
computes verdicts, and prints results. The binary has no dependencies
and makes no outbound connections. Your transcript data — tool calls,
prompts, file contents — never leaves your machine.

## 📦 Install

```bash
# Go (any platform)
go install github.com/thousandflowers/skillreaper/cmd/reap@latest

# npm (requires Node ≥ 18)
npx skillreaper

# Binary download (macOS, Linux, Windows — amd64 + arm64)
# https://github.com/thousandflowers/skillreaper/releases
```

Requires Go ≥ 1.22 for source builds. Releases provide pre-built
binaries for every combination.

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
| `--model M` | claude-sonnet-4-6 | Model key for automatic price lookup |
| `--price N` | auto | Input token price ($/MTok, overrides model lookup) |
| `--json` | false | Output JSON |
| `--md` | false | Output Markdown |
| `--claude-dir` | auto | Override config directory (backward compat) |
| `--claude-json` | auto | Override config file path |
| `--yes` | false | Skip confirmation before prune |

**Price resolution**: `--model` auto-resolves to the model's current
input price. `--price` overrides it. When neither is set, falls back to
Claude Sonnet 4.6 pricing ($3/MTok input).

### Output formats

- **Default**: colour-coded terminal table with REAP/KEEP/REVIEW verdicts
- **`--md`**: Markdown table (pipeline-friendly)
- **`--json`**: full structured JSON with timestamps, weights, verdicts

## 🔬 Case study: context is the bottleneck

On a real machine with Claude Code + ECC plugin installed for 6 months:

```
Items scanned:  187
Items kept:     23
Items reaped:   142 (76 %)
Items unclear:  22
```

**The $2.16/month saving** (at 8 000 wasted tokens/session, 3 sessions/day,
Sonnet 4.6 pricing) is almost incidental. The real impact:

- **142 fewer definitions** the agent reads every session before it can
  start working. That is 142 skill descriptions, agent configs, and
  inline instructions that do nothing but push real context out.
- **Shorter context means lower latency.** A smaller prompt has better
  cache hit rates and faster first-token generation.
- **Less noise means better responses.** Every irrelevant skill is a
  chance for the model to pick the wrong tool, hallucinate a command
  that does not exist, or waste a turn on something you uninstalled
  months ago.

> Every reaped skill shortens the model's effective context by the
> weight of its description and instructions — the agent does not have
> to read 142 irrelevant definitions before getting to work.

### Real numbers

```
8 000 wasted tok/session × 90 sessions/month
  = 720 000 tok/month
  = ~$2.16/month (Sonnet 4.6)
  = ~30 pages of instruction text your agent skims every month
```

## 🎯 What gets scanned

skillreaper inventories everything your agent loads at startup:

| Layer | What is found | Transcript evidence |
|---|---|---|
| Skills (`skills/`) | Personal + plugin skills (.md files) | ✅ tool_use + command-name calls |
| Agents (`agents/`) | Agent definitions (JSON) | ✅ Task/Agent tool_use blocks |
| MCP servers (`mcpServers`) | User, project, plugin scopes | ✅ mcp__ tool invocations |
| Hooks (`hooks/`) | Post-tool-use + stop hooks | ❌ not tracked in transcripts |
| Prose (`CLAUDE.md`, `rules/`) | Global + project always-loaded docs | ❌ always loaded, no tracking |

### Platform support

| Platform | Inventory | Transcript evidence | Status |
|---|---|---|---|
| **Claude Code** | ✅ full | ✅ JSONL | Full |
| **OpenCode** | ✅ full | ✅ SQLite | Full |
| **Codex CLI** | ✅ full | ✅ JSONL | Full |
| **Hermes** | ✅ full | ✅ JSONL | Full |
| **Cursor** | ✅ inventory | ❌ no transcripts | Inventory only |
| **OpenClaw** | ✅ inventory | ❌ no transcripts | Inventory only |

Platforms without transcript access still benefit from inventory scans:
you see everything installed, but evidence-based verdicts (`REAP`/`KEEP`)
work fully only on platforms with transcript parsing. Items on
read-only platforms always show `REVIEW`.

## ⚙️ How it works

**Auto-detection** — probes every well-known config directory at launch.
Only installed platforms are scanned. No flags needed.

**Inventory** — within each config directory, `reap` scans skills, agents,
MCP servers, hooks, and always-loaded prose files.

**Evidence** — parses session transcripts (JSONL or SQLite depending on
platform). Counts `tool_use` blocks, `<command-name>` invocations, and
tool declarations from init messages — all with timestamps.

**Cost estimation** — two sources:
1. **Character weight**: `ceil(chars / 3.7)` — every skill description,
   agent definition, and prose file has a known character count.
2. **Dead tool declarations**: init messages list every declared tool.
   Tools declared in init but never used carry their schema weight as
   pure overhead. Reported as `init parser: ~N chars of tool descriptions
   unused per session`.

MCP server schema sizes are unknown without running the server, so their
weight is marked `?`.

**Verdicts** — three outcomes:

- **REAP** — zero uses in the evidence window (≥ N sessions, installed
  before the window)
- **KEEP** — used at least once in the window
- **REVIEW** — insufficient evidence (too few sessions, recently
  installed, or platform without transcript parsing)

### Model pricing (June 2026)

`--model` resolves the input token price automatically. Supported models
and their current input pricing from the respective providers:

| Model | Input price ($/MTok) | Source |
|---|---|---|
| claude-opus-4-7 | 5.00 | [platform.claude.com](https://platform.claude.com) |
| claude-opus-4-6 | 5.00 | [platform.claude.com](https://platform.claude.com) |
| claude-opus-4-5 | 5.00 | [platform.claude.com](https://platform.claude.com) |
| claude-sonnet-4-6 | 3.00 | [platform.claude.com](https://platform.claude.com) |
| claude-sonnet-4-5 | 3.00 | [platform.claude.com](https://platform.claude.com) |
| claude-haiku-4-5 | 1.00 | [platform.claude.com](https://platform.claude.com) |
| claude-3-5-sonnet | 3.00 | [platform.claude.com](https://platform.claude.com) |
| gpt-4o | 2.50 | [openai.com](https://openai.com/pricing) |
| gpt-4o-mini | 0.15 | [openai.com](https://openai.com/pricing) |
| o3-mini | 1.10 | [openai.com](https://openai.com/pricing) |

Only models with **verifiable pricing from official API docs** are
included. Default: `claude-sonnet-4-6` (the most widely used Claude
model as of June 2026). Add new models by editing
[`internal/cost/cost.go`](internal/cost/cost.go) — one file.

### Prune is reversible quarantine

`reap prune` moves files to `<config-dir>/reaped/` with a versioned
manifest. Nothing is ever deleted. `reap restore --all` puts everything
back exactly where it was.

```json
{
  "version": 1,
  "quarantined_at": "2026-06-10T15:30:00Z",
  "entries": [
    {
      "id": "a1b2c3d4",
      "category": "skill",
      "name": "graphify",
      "from": "~/.claude/skills/graphify",
      "to": "~/.claude/reaped/skills/graphify",
      "timestamp": "2026-06-10T15:30:00Z"
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
docs/                — screenshots, demo assets
```

## ⚖ License

MIT
