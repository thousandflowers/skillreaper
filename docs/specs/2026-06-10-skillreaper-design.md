# skillreaper — Design Spec

**Date:** 2026-06-10
**Status:** Approved (carte blanche from owner)
**Goal:** Original open-source CLI with GitHub-trending potential. Ships same day.

## Pitch

> You installed 87 skills. Your agent used 4. Reap the rest.

`skillreaper` (binary: `reap`) scans your AI-agent stack (Claude Code first), cross-references it with your **real session transcripts**, and tells you — with evidence — which skills, MCP servers, agents, and hooks you never use and what they cost you in context tokens every session. Then it prunes them safely (reversible quarantine, never delete).

## Why this can trend

- Plugin fatigue is peaking (2026): typical power-user setups carry hundreds of skills/hooks/MCP tools.
- No existing tool does **evidence-based pruning from transcripts**: Tencent AI-Infra-Guard = security scanning; context-lens = live context visualization; ccusage = cost tracking. None answer "what can I safely remove?"
- Personal shock number ("you burn ~40k tokens/session on dead weight") is a shareable screenshot → viral mechanic.
- 100% local, zero deps, single static binary → zero-friction adoption.

Rejected alternatives: educational agent-from-scratch (saturated, 4.2k★ competitors), POSIX-sh agent stunt (novelty dented by `bashagt`), macOS menubar pet (platform-limited).

## Scope (MVP, one day)

**In:** Claude Code on macOS/Linux. Inventory + usage evidence + cost estimate + report (ANSI/JSON/Markdown) + safe prune/restore.
**Out (roadmap only):** Codex CLI, Cursor, Gemini CLI scanners; hook usage attribution; MCP schema token measurement; Windows testing.

## Data sources (verified on a real machine)

| Surface | Path | Inventory | Usage evidence |
|---|---|---|---|
| Personal skills | `~/.claude/skills/<name>/SKILL.md` | name + frontmatter description | `Skill` tool_use, `input.skill == name` |
| Plugin skills | `~/.claude/plugins/cache/<mkt>/<plugin>/<ver>/skills/<s>/SKILL.md` via `installed_plugins.json` (v2: `plugins["name@mkt"][].installPath`) | same | `input.skill == "plugin:skill"` |
| Agents | `~/.claude/agents/*.md` + plugin `agents/` | frontmatter description | `Task`/`Agent` tool_use `input.subagent_type` |
| MCP servers | `~/.claude.json` (top-level + per-project `mcpServers`), project `.mcp.json`, plugin `.mcp.json` | server name + command | tool_use name `mcp__<server>__<tool>` |
| Hooks | `settings.json` / `settings.local.json` `hooks` | event + command | none (inventory + flag only) |
| Always-loaded prose | `~/.claude/CLAUDE.md`, `~/.claude/rules/**`, project `CLAUDE.md` | file + token weight | n/a (report weight only) |

Transcripts: `~/.claude/projects/<encoded>/<uuid>.jsonl` — stream-parse lines, extract `tool_use` blocks (`name`, `input`), plus `<command-name>` tags in user messages for slash-command invocations. Window: `--days N` (default 30) filtered by file mtime + entry timestamps. Malformed lines: skip + count.

## Cost model (documented, honest)

- Tokens ≈ `ceil(chars / 3.7)` (English prose average; documented as estimate).
- **Per-session constant weight** = skill *descriptions* (the available-skills list), agent descriptions, CLAUDE.md/rules full text. SKILL.md body counts as *on-invoke* cost, reported separately.
- MCP servers: schema cost unknown without running the server → report usage evidence only, weight marked `?`.
- Money estimate: dead tokens/session × measured sessions/month × input price (default $3/MTok, `--price` flag).

## Verdicts

- `REAP` — zero invocations in window AND window covers ≥ N sessions (default ≥ 10 sessions, else `REVIEW`).
- `KEEP` — used in window.
- `REVIEW` — insufficient evidence (few sessions, new install — installedAt newer than window).

## Prune mechanics (safety first)

- Default is **dry-run**: prints plan. `--yes` applies. Interactive numbered picker otherwise.
- Personal skills/agents → move to `~/.claude/reaped/<category>/<name>` + append `~/.claude/reaped/manifest.json` (id, original path, timestamp).
- MCP servers (user-scope `~/.claude.json`, project `.mcp.json`) → timestamped file backup, then remove entry.
- Plugin skills: **report-only** (suggest `/plugin` disable) — never touch plugin cache.
- `reap restore [id|--all]` reverses via manifest/backups. Never `rm`.

## CLI

```
reap                 # scan + report (read-only, default)
reap --days 60 --json | --md
reap prune [--yes]
reap restore [id|--all]
reap version
```

## Architecture (Go ≥1.22, stdlib only)

```
cmd/reap/main.go          # arg parsing, subcommand dispatch
internal/scan/            # inventory scanners (ClaudeDir injectable for tests)
internal/usage/           # JSONL transcript stream parser (10MB line buffer)
internal/cost/            # token + money estimation
internal/report/          # ansi.go, json.go, markdown.go
internal/prune/           # quarantine + manifest + restore
```

All scanners take explicit root paths (no hidden `os.UserHomeDir` deep in logic) → testable with fixture trees.

## Error handling

- Missing `~/.claude` → friendly "no Claude Code installation found" exit 1.
- Unreadable/malformed JSON files → warn (stderr), continue, count in report footer.
- No transcripts in window → inventory-only report with explicit warning banner.

## Testing (TDD, ≥80% coverage)

- Fixture tree: `testdata/claudehome/` (skills, plugins cache, settings, .claude.json) + `testdata/transcripts/*.jsonl` (golden samples incl. malformed lines, namespaced skills, mcp tools, Task agents).
- Unit: each scanner, usage parser, cost model, verdict logic, prune/restore round-trip (on `t.TempDir()` copies).
- Golden test for ANSI/JSON/MD reports.
- CI: GitHub Actions — vet + test + coverage gate + build (macOS/Linux). Release: goreleaser on tag.

## Launch kit (in repo, `docs/launch/`)

README: hero demo (GIF/output sample), shock-stat framing, install (`go install`, release binaries, brew later), honest "how it estimates" section. Drafts: Show HN, r/ClaudeAI, X thread. Repo: MIT license, topics, CONTRIBUTING.

## Success criteria

- `reap` on the owner's machine produces correct, non-crashing report over 180 transcripts / 249MB in < 5s.
- Prune→restore round-trip lossless.
- Tests green, coverage ≥80%, CI green on first push.
- Repo public at `github.com/thousandflowers/skillreaper` with full launch kit.
