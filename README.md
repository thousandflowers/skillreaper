
<p align="center">
  <img src="docs/reap-demo.gif" alt="reap in action" width="800">
</p>

<h1 align="center">
  Your AI agent reads 187 skill descriptions every session.<br>
  You use 4. Reap the rest.
</h1>

<p align="center">
  <a href="https://github.com/thousandflowers/skillreaper/actions/workflows/ci.yml"><img src="https://github.com/thousandflowers/skillreaper/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/thousandflowers/skillreaper/releases"><img src="https://img.shields.io/github/v/release/thousandflowers/skillreaper" alt="Release"></a>
  <a href="https://github.com/thousandflowers/skillreaper/issues"><img src="https://img.shields.io/github/issues/thousandflowers/skillreaper" alt="Issues"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue" alt="MIT"></a>
</p>

<br>

```bash
brew install thousandflowers/tap/skillreaper
reap
```

**One command. Zero config. Read-only.** It scans your transcripts, finds
every skill/agent/MCP your AI loads but never uses, and tells you exactly
what it costs in context window, latency, and money.

<br>

### The problem

Every Claude Code session loads 150–300 skill descriptions, agent configs,
and rule files into context. Most of it is dead weight:

- 187 items scanned
- 142 never used (76 %)
- 8 000 tok/session wasted
- ~2 160 000 tok/month burned on irrelevant instructions

Your agent scrolls through a wall of irrelevant tools looking for the right
one. Wrong picks cost turns. Turns cost tokens. Tokens cost money.

> `reap` points at the waste. You decide what goes.

<br>

### Before → After

| Before skillreaper | After skillreaper |
|---|---|
| 187 items loaded every session | 45 items, all actively used |
| Wrong tool 1 in 5 turns | Right tool on first try |
| 8 000 tok/session dead | Full context budget for real work |
| ~30 pages of irrelevant instructions read monthly | Zero |
| Lower cache hit rate = higher latency | Smaller prompt fits in cache |

<br>

### Install

```bash
# macOS — Homebrew (one tap)
brew install thousandflowers/tap/skillreaper

# npm / npx (any platform, Node ≥ 18)
npx skillreaper

# Go
go install github.com/thousandflowers/skillreaper/cmd/reap@latest

# Binary — macOS, Linux, Windows, amd64 + arm64
# https://github.com/thousandflowers/skillreaper/releases
```

Pre-built static binaries. Zero runtime dependencies.

<br>

### Usage

```bash
reap                     # scan + report (read-only)
reap prune               # quarantine unused items (reversible)
reap keep <name>         # protect an item from pruning
reap restore --all       # undo every prune
reap --json              # structured JSON output
reap --md                # markdown report
reap --days 7            # shorter evidence window
reap version             # print version
```

Everything is **reversible**. `reap prune` moves files to a `reaped/`
directory with a versioned manifest. Nothing is ever deleted. Run
`reap restore --all` and everything goes back exactly where it was.

<br>

### Verdicts

| Label | Meaning |
|---|---|
| **`REAP`** | Zero uses — safe to quarantine |
| **`KEEP`** | Used, tiny, or manually protected |
| **`REVIEW`** | Too new or not enough sessions |

Every verdict includes a reason suffix explaining *why*.

<br>

### Privacy

**100 % local.** Zero telemetry, zero network, zero uploads. Reads config
files and session transcripts on disk — your data never leaves your machine.

<br>

### Platform support

| Platform | Full support |
|---|---|
| **Claude Code** | ✅ |
| **OpenCode** | ✅ |
| **Codex CLI** | ✅ |
| **Hermes** | ✅ |
| **Cursor** | Inventory only (no local transcripts) |
| **OpenClaw** | Inventory only (no session history) |

<br>

### How it works

1. **Auto-detect** — probes every known config directory. Only installed
   platforms are scanned. No flags needed.
2. **Inventory** — scans skills, agents, MCP servers, hooks, and prose
   files across all detected platforms.
3. **Evidence** — parses session transcripts (JSONL or SQLite). Counts
   `tool_use` blocks and command invocations with timestamps.
4. **Cost** — character weight (`ceil(chars / 3.7)`) + init parser tool
   declarations. Model pricing auto-resolves by model name.
5. **Verdict** — REAP / KEEP / REVIEW with machine-readable reason.
6. **Act** — `reap prune` quarantines. `reap restore --all` undoes.

<br>

### Design

- **100 % local**, zero dependencies, single static binary (Go ≥ 1.22)
- **Multi-platform** — adding a new platform is one struct in
  `internal/platform/`
- **Reversible quarantine** — never deletes, never destructive
- **MIT licensed**

```
cmd/reap/       CLI entry point
internal/
  platform/     platform definitions + auto-detection
  scan/         inventory scanners
  usage/        transcript parser (JSONL + SQLite)
  report/       verdict logic + ANSI/JSON/MD renderers
  prune/        reversible quarantine
  cost/         model pricing
docs/           demo assets
```

<br>

---

<p align="center">
  <a href="https://github.com/thousandflowers/skillreaper/issues">Issues</a>
  ·
  <a href="https://github.com/thousandflowers/skillreaper/discussions">Discussions</a>
  ·
  <a href="https://github.com/thousandflowers/skillreaper/releases">Releases</a>
  ·
  <a href="LICENSE">MIT</a>
</p>
