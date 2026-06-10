# skillreaper

> You installed 87 skills. Your agent used 4. Reap the rest.

`reap` scans your Claude Code agent stack (skills, MCP servers, agents, hooks, prose) and cross-references it with **real session transcripts**. It tells you — with evidence — what you never use and what it costs you in context tokens every session.

No guesses. No config. Just read-only analysis + safe reversible prune.

## Install

```bash
go install github.com/thousandflowers/skillreaper/cmd/reap@latest
```

Or download a binary from [releases](https://github.com/thousandflowers/skillreaper/releases).

## Usage

```bash
reap                # scan + report (read-only, default)
reap --md           # Markdown report
reap --json         # JSON report
reap prune          # quarantine unused items (reversible)
reap restore --all  # undo every prune
reap version        # print version
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `--days N` | 30 | Evidence window in days |
| `--min-sessions N` | 10 | Sessions needed for REAP verdicts |
| `--price N` | 3.0 | Input token price ($/MTok) |
| `--json` | false | Output JSON |
| `--md` | false | Output Markdown |
| `--yes` | false | Prune without confirmation |

## How it works

**Inventory** — scans `~/.claude/` for skills, agents, MCP servers, hooks, and always-loaded prose (CLAUDE.md, rules). Also picks up plugin-provided items.

**Evidence** — parses session transcripts from `~/.claude/projects/`. Counts `tool_use` blocks and command invocations. Timestamps everything.

**Verdicts** — three outcomes:

- `REAP` — zero uses in window (≥ N sessions, installed before window)
- `KEEP` — used in window
- `REVIEW` — insufficient evidence (too few sessions, recently installed)

**Prune** — moves files to `~/.claude/reaped/` and records a manifest. No data loss. `reap restore` puts everything back.

### Cost estimation

Tokens ≈ `ceil(chars / 3.7)` (English prose average; documented estimate). MCP schema costs are unknown without running the server, so weight is marked `?`.

## Design

- 100% local, zero dependencies, single static binary
- Go ≥1.22, stdlib only
- All scanners accept explicit root paths — testable with fixtures
- Prune is **reversible quarantine**, never delete

## License

MIT
