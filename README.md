```
S           K           I          L          L
######  #######   ###   ######  ####### ###### 
##   ## ##       ## ##  ##   ## ##      ##   ##
######  #####   ####### ######  #####   ###### 
##  ##  ##      ##   ## ##      ##      ##  ## 
##   ## ####### ##   ## ##      ####### ##   ##

skillreaper · last 30d · 34 sessions
7/378 items fired · 1% utilization
371 never used · ~22720 dead tokens/session

TOKENS  CATEGORY  NAME                 VERDICT  REASON
185     skill     import-timesheet     REAP     unused
185     skill     render-playlist      REAP     unused
177     skill     review-sitemap       REAP     unused
163     skill     deploy-dataset       REAP     unused
159     skill     validate-manifest    REAP     unused
157     skill     parse-contract       REAP     unused
149     skill     extract-receipt      REAP     unused
145     skill     sync-changelog       REAP     unused
111     skill     summarise-timesheet  REAP     unused
110     skill     export-timesheet     REAP     unused
(361 more never-used items not shown — use --json for all)

To prune: reap prune   (interactive, reversible via reap restore --all)

measured by skillreaper · github.com/thousandflowers/skillreaper
```
<!-- readme-numbers:end -->

<p align="center">
  <sub>Real <code>reap --agent</code> output, run against a generated sample stack
  (<a href="docs/gif-helpers/hero-fixture.sh">hero-fixture.sh</a>) — not anyone's
  install. The numbers further down are measured on mine.</sub>
</p>

<h1 align="center">
  Most of what your AI agent loads, it never uses.
</h1>
<p align="center">
  skillreaper finds the <b>unused skills, MCP servers, subagents, hooks and
  always-loaded prose</b> quietly filling your <b>context window</b> — proven
  from your own session transcripts — and prunes them reversibly.<br>
  Runs locally against transcripts you already have. Nothing is ever uploaded.
</p>



<p align="center">
  <a href="https://github.com/thousandflowers/skillreaper/actions/workflows/ci.yml"><img src="https://github.com/thousandflowers/skillreaper/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/thousandflowers/skillreaper/issues"><img src="https://badgen.net/github/open-issues/thousandflowers/skillreaper" alt="Issues"></a>
  <a href="https://github.com/thousandflowers/skillreaper/releases"><img src="https://img.shields.io/github/downloads/thousandflowers/skillreaper/total?cacheSeconds=6400&color=success" alt="Downloads"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue" alt="MIT"></a>
  <a href="https://github.com/avelino/awesome-go#artificial-intelligence"><img src="https://awesome.re/mentioned-badge.svg" alt="Mentioned in Awesome Go"></a>
  <a href="https://github.com/VoltAgent/awesome-agent-skills#community-skills"><img src="https://awesome.re/mentioned-badge.svg" alt="Mentioned in Awesome Agent Skills"></a>
</p>

<br>

```bash
npx skillreaper
```

<!-- readme-mine-headline:start -->
On my own installation, measured 2026-08-23: **382 items loaded, 13 ever fired — 3% utilization.**
That's ~19,755 dead tokens re-sent in every single session, ~1087k a month of
pure token waste, paid for on every request before you type anything.
<!-- readme-mine-headline:end -->

<sub>Measured 2026-08-23 on my own installation; raw output in
<a href="docs/measurements/">docs/measurements/</a>. Every "on my own installation"
figure on this page comes from that one run.</sub>

**One command. Zero config. Read-only.** It reads your real session transcripts,
finds every skill / MCP / agent your AI loads but never fires, and shows you
exactly what it costs you.

<br>

## Why this exists

### Why I built this

I was running out of context budget on every session. I had accumulated skills,
MCP servers, and agents over months most of them experiments I'd forgotten
about or be too busy to change it. Every new session loaded all of them, burning tokens before I'd typed a
single message.

I needed to know which ones were actually firing and which were just dead
weight. Nothing existing told me that from transcript evidence. So I built it.

It now supports six platforms and ships on Homebrew, npm, and as a static
binary for every major OS.

<br>

### Two costs of context bloat

**Wrong-tool picks.** Buried in a wall of irrelevant options, your agent
wastes turns reaching for the wrong tool. More turns = slower, costlier,
sloppier runs. This isn't about pennies — it's about work quality.

**Wasted tokens.** Dead instructions eat context every session and hurt
prompt-cache hit rate. A typical setup:

<!-- readme-mine-costs:start -->
- 382 items loaded
- 367 never used (96 %)
- 19 755 tok/session dead
- ~1 087 000 tok/month burned on irrelevant instructions
- ~$3.26/month, ~$39/year — the same waste priced instead of counted

<p align="center"><sub>The money line is one measurement of one stack, n=1, and the
weakest number here: <code>19 755 × 55 × $3.00 ÷ 1e6</code> — input tokens only, at
<code>claude-sonnet-4-6</code>'s $3.00/MTok default, with tokens estimated as
<code>ceil(chars / 3.7)</code> and the monthly session count extrapolated from a
30-day window. Change the model, the price, or how much you work and it moves;
the item and token counts do not. See <a href="#limitations-transparency">Limitations</a>.</sub></p>

<p align="center"><em>Measured on my own setup — 55 sessions over 30 days, 2026-08-23. Run <code>reap</code> to see yours.</em></p>
<!-- readme-mine-costs:end -->

skillreaper measures both, from evidence — no guessing.

> `reap` points at the waste. You decide what goes.

<br>

### Privacy

**100 % local.** Zero telemetry, zero network, zero uploads. Reads config
files and session transcripts on disk — your data never leaves your machine.

<br>

### Before → After: what removing token waste buys

<!-- readme-mine-table:start -->
| Before skillreaper | After skillreaper |
|---|---|
| 382 items loaded every session | 15 kept · 13 actually fire |
| 19 755 tok/session dead | Full context budget for real work |
| ≈ 73 000 dead chars ≈ 29 pages every session (at 500 words/pg) | Zero |
| Lower cache hit rate = higher latency | Smaller prompt fits in cache |

<sub>My own installation, measured 2026-08-23.</sub>
<!-- readme-mine-table:end -->

<br>

<p align="center">If this looks useful → <a href="https://github.com/thousandflowers/skillreaper">⭐ star the repo</a></p>

## Getting started

### Install

**Inside Claude Code** — adds `/skillreaper:reap` and `/skillreaper:gap`, so the
report renders in the conversation instead of a scrollback you have to re-read:

```
/plugin marketplace add thousandflowers/skillreaper
/plugin install skillreaper@skillreaper
```

The plugin is a thin wrapper: it drives the same binary, so install that too
with any line below. The skills fall back to `npx skillreaper` and tell you how
to install permanently if they can't find it.

**Install permanently** — Homebrew and npm install both names, `reap` and
`skillreaper`, so either one works. `go install` gives you `reap`:

```bash
# macOS — Homebrew
brew install thousandflowers/tap/skillreaper

# Any platform — npm (downloads the matching prebuilt, checksum-verified)
npm install -g skillreaper

# Any platform — Go (Go ≥ 1.24)
go install github.com/thousandflowers/skillreaper/cmd/reap@latest
```

> Already installed it with Homebrew? You don't need the npm package —
> both put `reap` and `skillreaper` in the same prefix, so `npm install -g`
> stops at an EEXIST link error rather than overwriting brew's copy.
> Pick one route: to switch to npm, run `brew uninstall skillreaper` first.
> Neither affects `npx skillreaper`, which runs from a cache and never
> links a global command.

**Binary downloads** — macOS (Intel + Apple Silicon), Linux (amd64 + arm64),
Windows (amd64 + arm64) — all on the
[releases page](https://github.com/thousandflowers/skillreaper/releases).
Single static binary, no dependencies.

Upgrading, uninstalling, and platform-specific tips →
[INSTALL.md](INSTALL.md).

<br>

### Usage
💬 **Curious what `reap` finds in other setups?** [Share your output →](https://github.com/thousandflowers/skillreaper/discussions)

```bash
reap                          # scan + report (read-only)
reap gap                      # loaded-vs-fired utilization breakdown
reap prune                    # quarantine REAP items (reversible)
reap mute <name>              # strip description, keep skill available
reap unmute <name>            # restore description from backup
reap unmute --all             # restore all muted skills
reap keep <name>              # protect an item from pruning
reap restore --all            # undo every prune
reap why <name>               # explain in detail why an item got its verdict
reap by-project               # skills bucketed by the project that fired them
reap route                    # propose a usage-informed lazy-load routing plan (opt-in)
reap apm                      # emit a proposed APM apm.yml from this repo's firing
reap apm --diff apm.yml       # reconcile: what to add (fired, undeclared) / drop (declared, cold)
reap gap                      # now also scores MCP payload quality (fires-but-noise)
reap manifest <name>          # emit a release manifest for one skill
reap install-hook             # install weekly nudge (SessionStart hook)
reap install-hook --dry-run   # preview without writing
reap uninstall-hook           # remove hook, other hooks untouched
reap --json                   # structured JSON output
reap --md                     # markdown report
reap --days 7                 # shorter evidence window
reap --mute-threshold 0.20    # firing rate below which MUTE triggers (default 20%)
reap version                  # print version
```

<br>

### Demo

<p align="center">
  <img src="docs/reap-demo.gif" alt="reap in action" width="800"><br>
  <sub>Recorded against a small sample fixture
  (<a href="docs/gif-helpers/demo-fixture.sh">demo-fixture.sh</a>), so the
  numbers are the fixture's, not a real stack's.</sub>
</p>

<br>

Everything is **reversible**. `reap prune` moves files to a `reaped/`
directory with a versioned manifest. Nothing is ever deleted. Run
`reap restore --all` and everything goes back exactly where it was.

Every write is **atomic** (temp file + rename) and **confined** to your
Claude directory, so an interrupted prune, mute, or hook edit leaves the
original file intact — never a half-written mix.

<br>

## Reading the report

### Verdicts

| Label | Meaning |
|---|---|
| **`REAP(broken)`** | Invoked but errored — broken, not just cold |
| **`REAP`** | Zero uses — safe to quarantine |
| **`MUTE`** | Used rarely + heavy — description stripped, skill stays available |
| **`KEEP`** | Used, tiny, or manually protected |
| **`REVIEW`** | Too new or not enough sessions |

Every verdict includes a reason suffix explaining *why*.

<br>

### Loaded vs fired: measuring context-window utilization

Beyond the prune verdicts, `reap gap` shows your **utilization rate** —
how much of what you load you actually use.

<!-- readme-gap:start -->
```
  ⟡ loaded vs fired — last 30 days · 34 sessions

  CATEGORY   LOADED  FIRED   UTIL                TOKENS
  skills        298      6     2%   ▱▱▱▱▱▱▱▱▱▱   ~21693 →   278
  mcp            12      1     8%   ▱▱▱▱▱▱▱▱▱▱         ? →     ?
  agents         68      0     0%   ▱▱▱▱▱▱▱▱▱▱   ~ 1305 →     0
  ─────────────────────────────────────────────────────────
  total         378      7     1%   ▱▱▱▱▱▱▱▱▱▱   ~22998 →   278

  ⟡ mute 2 heavy low-use skills · ~102 tok/session recoverable via `reap mute`
```
<!-- readme-gap:end -->

<p align="center"><sub>Same generated sample stack as the report at the top of this
page, so the two agree. The numbers further up the page are measured on mine.</sub></p>

Each row breaks down by category (skill, MCP, agent) with item count, token
weight, and a 10-segment utilization bar. The token column reads
*loaded → actually used*: the left number loads every session, the right is all
that is ever touched — the gap between them is dead weight that reloads each
time. Low utilization (<10 %) is red, medium (<50 %) yellow,
high (≥50 %) green.

The default `reap` report also includes a compact utilization summary line:

<!-- readme-utilization:start -->
```
  ⟡ utilization 1%  —  7/378 items fired · ~278/22998 tok touched (30d)
```
<!-- readme-utilization:end -->

<p align="center"><sub>Same generated sample stack, not my install — as with the two
blocks above it.</sub></p>

This is the **real** gap between what your agent carries and what it fires —
complementary to the shock box (which only counts items that are safe to prune
right now).

<pre>reap gap          # text breakdown
reap gap --json   # JSON output
reap gap --md     # markdown table</pre>

The gap view also scores **payload quality** for MCP tools: when a tool fires,
is the result signal or noise? A fetch/screenshot tool can fire 80× and return
mostly base64 or boilerplate every call — green under load utilization, but
context burned on each call. Tools that fire often *and* return mostly noise are
flagged `⚑ noisy`. This is the second utilization axis (load is the first), and
mute does not catch it.

<br>

## Beyond pruning

### route — context engineering for large tool libraries

After pruning, a library of hundreds of legit skills still grows resident
context linearly. `reap route` proposes a category-router organization driven by
**real firing evidence**, not text similarity: frequently-fired skills stay
exposed; the rare long tail is pushed behind leaf routers (grouped by namespace,
else dominant firing project) loaded on demand. It is strictly opt-in and
secondary to pruning — and below ~150 skills, native loading is usually enough,
so the plan says so. The output is a **plan**: proposed, never auto-applied.

<pre>reap route                      # text plan
reap route --json               # JSON
reap route --md                 # markdown
reap route --route-threshold 0.05   # route skills firing in <5% of sessions
reap route --route-min-skills 200   # only show a plan past 200 surviving skills</pre>

<br>

### apm — emit a proposed APM manifest

`reap apm` turns this repo's firing evidence into a proposed
[APM](https://github.com/microsoft/apm) `apm.yml` (skills only, first cut).
Read-only: it prints YAML, never edits the repo or runs `apm install`. KEEP →
include, REAP → omit, REVIEW → never auto-omit. Upstream coordinates are
recovered from `apm.lock.yaml` when present; otherwise the skill becomes a clearly
marked `TODO` comment rather than an invented coordinate.

<pre>reap apm                        # propose apm.yml (yaml)
reap apm --json                 # JSON
reap apm --md                   # markdown
reap apm --diff apm.yml         # reconcile: add fired-but-undeclared, drop declared-but-cold</pre>

<br>

### Weekly nudge

```bash
reap install-hook
```

Installs a `SessionStart` hook that runs a passive audit at the start of each
Claude Code session. If 7 days have passed and the REAP or MUTE count has grown
since the last check, it prints a single line to stderr:

```
skillreaper: 3 skills flagged for pruning since last check. Run reap to review.
```

Nothing else. No blocking. State stored at `~/.claude/reaped/nudge-state.json`.

`reap uninstall-hook` removes only the skillreaper entry — other hooks untouched.



## Transparency and internals

### Platform support

| Platform | Full support |
|---|---|
| **Claude Code** | ✅ |
| **Codex CLI** | ✅ |
| **Hermes** | ✅ |
| **OpenCode** | ✅ (usage evidence needs the `sqlite3` CLI; inventory-only without it) |
| **Cursor** | Inventory only (no local transcripts) |
| **OpenClaw** | Inventory only (no session history) |
| **Gemini CLI** | Inventory only (session history is stored in a layout skillreaper does not parse yet, so its items surface as REVIEW, never REAP) |

<br>

### How the evidence is gathered

1. **Auto-detect** — probes every known config directory. Only installed
   platforms are scanned. No flags needed.
2. **Inventory** — scans skills, agents, MCP servers, hooks, and prose
   files across all detected platforms.
3. **Evidence** — parses JSONL session transcripts (Claude Code, Codex CLI,
   Hermes). Counts `tool_use` blocks and command invocations with timestamps.
   OpenCode's SQLite history is read via the `sqlite3` CLI (read-only) when it
   is on `PATH`; without it, OpenCode stays inventory-only.
4. **Cost** — character weight (`ceil(chars / 3.7)`) + init parser tool
   declarations. Model pricing auto-resolves by model name.
5. **Verdict** — REAP / KEEP / REVIEW with machine-readable reason.
6. **Act** — `reap prune` quarantines. `reap restore --all` undoes.

<br>

### Limitations (transparency)

**Token counts are approximate.** The tool estimates tokens as
`ceil(chars / 3.7)`, based on the average English BPE tokenizer rate.
Real token counts vary by tokenizer (Claude vs GPT vs Gemini) and content
(more code ≈ more tokens per char). This is a documented approximation —
the relative ranking matters more than the absolute number.

**Platform format stability.** Each supported platform has its own config
layout and transcript format. These change over time as platforms evolve.
Parser updates are an ongoing maintenance reality. The project is architected
for easy fixes (one struct per platform in `internal/platform/`), but format
changes can lag by days to weeks after a platform update.

**OpenCode evidence needs the `sqlite3` CLI.** OpenCode stores session history
in a SQLite database. skillreaper reads it through the system `sqlite3` binary
in read-only mode — the real engine, so WAL-mode databases and overflow pages
are handled correctly (a hand-rolled parser would not). No Go dependency is
added. When `sqlite3` is **not** on `PATH`, OpenCode items have no usage
evidence: they stay **REVIEW (never REAP)** with a warning at scan time. The
same safety net applies to any platform with no readable session transcripts.

**Incomplete evidence never flags an item.** The scanner caps how much it
reads per transcript record. If a record is oversized or unreadable, that
platform's evidence is marked incomplete and its items stay **REVIEW (never
REAP/MUTE)**, with a warning naming the platform — partial evidence can never
mistakenly mark a tool as dead.

**Not a tool declaration fix.** Claude Code's deferred tools reduce the
*init-time tool declaration* overhead. Skillreaper addresses a different
problem: **always-loaded skill/agent/prose files.** If a skill description
is 248 characters, it is read into context every session — regardless of
lazy tool loading. These two optimizations are complementary, not competing.

<br>

### Design and internals

- **100 % local**, zero dependencies, single static binary (Go ≥ 1.24)
- **Multi-platform** — adding a new platform is one struct in
  `internal/platform/`
- **Reversible quarantine** — never deletes, never destructive
- **MIT licensed**

```
cmd/reap/       CLI entry point
internal/
  platform/     platform definitions + auto-detection
  scan/         inventory scanners (claudemd.go: CLAUDE.md protection)
  usage/        transcript parser — tool_use + error tracking
  report/       verdict logic (REAP/MUTE/KEEP/REVIEW) + ANSI/JSON/MD renderers
  prune/        reversible quarantine
  mute/         description strip + backup/restore
  safepath/     shared path-confinement boundary (prune/mute/scan)
  atomicfile/   crash-safe writes (temp file + rename)
  hook/         SessionStart install/uninstall + nudge state
  cost/         model pricing
  readme/       maintainer-only README figure generator (not shipped)
docs/           demo assets
```

<br>

---

### Roadmap

Tracked as issues, grouped by the problem they solve.

- **Docs** — split read-only from writing commands and add a "First 60 seconds"
  ([#50](https://github.com/thousandflowers/skillreaper/issues/50)); surface
  `by-project`, `share` and `install-hook`
  ([#51](https://github.com/thousandflowers/skillreaper/issues/51)).
- **Teams** — `reap apm` is what reproduces a lean set across a team, but
  coordinates resolve only from `apm.lock.yaml`, so a repo without one gets
  placeholders. Plus scanning every detected platform, not only Claude Code
  ([#41](https://github.com/thousandflowers/skillreaper/issues/41)).
- **Better verdicts** — `--since` / `--until` and a window that respects the
  corpus ([#53](https://github.com/thousandflowers/skillreaper/issues/53));
  telling rare apart from dead
  ([#54](https://github.com/thousandflowers/skillreaper/issues/54)); measuring
  wrong-tool picks
  ([#55](https://github.com/thousandflowers/skillreaper/issues/55)).
- **Over time** — `reap snapshot` / `reap diff` over the existing `--json`
  payload ([#52](https://github.com/thousandflowers/skillreaper/issues/52)).

Not planned: a CI job running `reap` (a runner has no session transcripts, so it
would measure nothing — the periodic loop is `reap install-hook`), an
extrapolated annual dollar figure (see [docs/decisions.md](docs/decisions.md)),
and git commit ranges (transcripts carry timestamps, which `--since` /
`--until` already covers).

<br>

---

### Where it is used

Included in [awesome-go](https://github.com/avelino/awesome-go#artificial-intelligence)
(182k ★, Artificial Intelligence section) and
[awesome-agent-skills](https://github.com/VoltAgent/awesome-agent-skills#community-skills)
(30k ★). Three external contributors, 17 merged commits. The verdict logic has
been ported into another project with attribution in the code
([lean-agency](https://github.com/badithya-ms/lean-agency)).

<br>

---

### Acknowledgements

v0.2.0 ideas were inspired by work from the r/claudeskills community:

- **[groundskeeper](https://github.com/zvoque/groundskeeper)** — SessionStart weekly nudge pattern and live usage tracking approach
- **[optimize](https://github.com/codeprakhar25/optimize)** — name-only middle state (implemented as MUTE) and CLAUDE.md reference protection
- Broken-vs-cold distinction direction inspired by discussion on r/claudeskills

<br>

<p align="center">
  <a href="https://github.com/thousandflowers/skillreaper/issues">Issues</a>
  ·
  <a href="https://github.com/thousandflowers/skillreaper/discussions">Discussions</a>
  ·
  <a href="https://github.com/thousandflowers/skillreaper/releases">Releases</a>
  ·
  <a href="LICENSE">MIT</a>
</p>
