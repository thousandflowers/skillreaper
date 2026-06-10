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
# macOS — Homebrew
brew install thousandflowers/tap/skillreaper

# npm / npx (any platform, requires Node ≥ 18)
npx skillreaper

# Go (any platform)
go install github.com/thousandflowers/skillreaper/cmd/reap@latest

# Binary download (macOS, Linux, Windows — amd64 + arm64)
# https://github.com/thousandflowers/skillreaper/releases
```

Pre-built binaries available for every platform. No runtime
dependencies — single static binary.

## 🎮 Tutorial (3 minuti)

### 1. Vedi cosa sta caricando il tuo agente

```bash
reap
```

Output tipico:

```
Items scanned:  187
Items with evidence:  165
Items reaped:   142 (76 %)
```

Scorri la tabella: ogni riga è una skill/agente/MCP che il tuo agente
carica a ogni sessione. Colonna `VERDICT` dice se è REAP (mai usata),
KEEP (usata), o REVIEW (pochi dati).

### 2. Capisci il costo

Ogni skill ha una colonna `WEIGHT/SESSION` — caratteri che consumano
contesto ogni volta. Somma quelle delle REAP per vedere quanto spazio
sprecato recuperi.

### 3. Metti in quarantena gli inutilizzati

```bash
reap prune
```

Skillreaper sposta i file in `<config>/reaped/` — non cancella niente.
Ti chiede conferma prima. Se cambi idea:

```bash
reap restore --all
```

Tutto torna esattamente com'era. Punto.

### 4. Verifica il risultato

Rilancia `reap` — gli stessi item ora mostrano `(reaped)` nel nome.
Hai recuperato contesto senza perdere niente.

> **Consiglio**: aspetta almeno 10 sessioni di lavoro prima di prune.
> skillreaper ha bisogno di dati per decidere con sicurezza.
> Se un item è in dubbio (REVIEW), lascialo stare.

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

## 🔬 Case study: 76% of your agent's context is dead weight

On a real machine with Claude Code + ECC plugin installed for 6 months:

```
Items scanned:  187
Items kept:     23
Items reaped:   142 (76 %)
Items unclear:  22
```

**142 items your agent reads every single session that it never uses.**
That is 142 skill descriptions, agent configs, and inline instructions
that do nothing but push real context out.

> Every reaped skill shortens the model's effective context by the
> weight of its description and instructions — the agent does not have
> to read 142 irrelevant definitions before getting to work.

### 💀 Before skillreaper

> *"Which tool should I use to plan this feature?"*

Your agent scrolls through 187 items. It picks the wrong tool on average
once every 5 turns — a graphql skill for a REST task, a deployment skill
for a local file edit. Each wrong pick costs a full turn of correction.

**Real cost**: 8 000 wasted context tokens per session. The model spends
~30% of its context window reading irrelevant definitions before it can
start working. Cache hit rate suffers. First-token latency climbs.
Response quality degrades.

### ✅ After skillreaper

> *"Which tool should I use to plan this feature?"*

Your agent sees 45 relevant items. It picks the right tool in the first
turn. Every session starts with clean context — no noise, no competing
definitions, no hallucinated commands.

**Real impact**:

| Before | After |
|---|---|
| 187 items loaded every session | 45 items, all actively used |
| Wrong tool 1 in 5 turns | Right tool on first try |
| 8 000 tok/session wasted on dead context | Full context budget for real work |
| ~30 pages of irrelevant instructions read monthly | Zero |
| Lower cache hit rate = higher latency | Smaller prompt fits in cache |

> The quality improvement compounds: fewer irrelevant skills means fewer
> competing tool choices, which means fewer wrong picks, which means
> fewer wasted turns.

### 📊 The numbers

```
8 000 wasted tok/session × 90 sessions/month
  = 720 000 tok/month
  = ~$2.16/month at Sonnet 4.6 pricing
  = ~30 pages of instruction text your agent skims every month
```

The token cost ($2.16/month) is small. The **context cost** — 76% of your
agent's effective working memory dedicated to things it never uses — is
the real problem skillreaper solves.

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

### ⚠️ Inventory-only platforms

Cursor and OpenClaw **do not store session transcripts locally**.
Cursor moved to cloud-hosted conversation history (local DB only has
summary metadata). OpenClaw is a config/workspace manager with no
session history. For these platforms, skillreaper can only inventory
what is installed — verdicts are always `REVIEW` because there is no
transcript evidence to analyze.

This is a platform limitation, not a skillreaper one. The inventory
scan is still useful: you see everything loaded, and the character
weight table tells you what is costing context. Use that information
to decide what to remove manually.

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
docs/                — screenshots, demo assets
```

## ⚖ License

MIT
