# Design — Loaded vs Fired skills gap

**Issue:** #4 — Track and visualize the "Loaded vs Fired" skills gap
**Date:** 2026-06-13
**Status:** Approved (brainstorm)

## Problem

`reap` already computes, per item, whether it was used (`Uses`) and a prune
verdict. The brand headline — "your agent reads 187 skill descriptions, you
use 4" — is exactly a *loaded vs fired* ratio, but the tool never surfaces it
as a first-class, explicit metric. Issue #4 asks to **track** and **visualize**
this gap.

## Scope (phase 1)

Snapshot only. The gap is computed for the current evidence window and shown in
two places:

1. A compact **utilization line** in the default `reap` report.
2. A dedicated **`reap gap`** view with a per-category breakdown.

A future "trend over time" extension (snapshots appended to a history file,
`reap gap --trend`) is explicitly **out of scope** for this phase, but the data
model is designed to serialize cleanly so phase 2 needs no rework.

### Definitions

- **Loaded** — an inventory item whose description/schema is injected into
  context. The full skill/agent/MCP inventory.
- **Fired** — an item invoked at least once in the window (`Uses > 0`).
- **Gap / utilization** — `Fired / Loaded`. This is the *raw* utilization, the
  literal reading of the issue title and README headline. It is intentionally
  distinct from the existing red "shock box", which reports the *actionable*
  prunable weight (REAP verdict only). The two are complementary:
  - shock box → "what you can safely cut now"
  - gap line → "how much of what you carry you actually touch"

### Categories

Only categories with an invocation concept participate: **skill, agent, mcp**.
Hooks and prose are excluded (they have no "fired" event in this sense).

### Metric basis

Both **item count** and **token weight** are shown side by side (count drives
the headline %, tokens give the cost view). MCP token weight is unknown without
running the server, so MCP tokens render as `?` and are excluded from token
totals (consistent with existing `weightDisplay`).

## Approach

**Chosen: extend the `report` package.** The `Build()` function already joins
inventory items with usage evidence into `Rows`. The gap is derived from those
same rows — no new scanning, no duplicated join logic.

Rejected:
- A separate `internal/gap` package → duplicates the join logic in
  `report.Build`, violates DRY.
- Summary line only, no command → less than requested (`reap gap` wanted).

## Data model

Added to `internal/report` and computed inside `Build()`:

```go
// GapCat is the loaded-vs-fired breakdown for one category.
type GapCat struct {
    Category  scan.Category
    Loaded    int // # inventory items (description injected)
    Fired     int // # items with Uses>0 in the window
    LoadedTok int // sum of description tokens
    FiredTok  int // tokens of fired items
}

// Gap is the full loaded-vs-fired snapshot for the window.
type Gap struct {
    PerCat    []GapCat // order: skill, mcp, agent
    Loaded    int
    Fired     int
    LoadedTok int
    FiredTok  int
}
```

`Report` gains `Gap *Gap`. Populated in `Build()` by iterating the already-built
`Rows`, counting per category (skill/agent/mcp only) and summing tokens. MCP
token sums are left at 0 and rendered as `?` (weight unknown). Totals exclude
MCP tokens.

## Rendering

New functions in `internal/report/render.go`:

- `renderGapLine(w, g *Gap, color bool)` — compact one-liner appended to the
  default `RenderText` report, after the shock box:

  ```
    ⟡ utilization 4%  —  9/229 items fired · ~300/9 200 tok touched (30d)
  ```

- `RenderGap(w, r *Report, color bool)` — the full `reap gap` view. Bar
  represents utilization (`Fired/Loaded`); count and tokens side by side:

  ```
    ⟡ loaded vs fired — last 30 days · 142 sessions

    CATEGORY   LOADED  FIRED   UTIL   ────────────       TOKENS
    skills        187      4    2%    ▰▱▱▱▱▱▱▱▱▱     ~8 000 →   210
    mcp            12      3   25%    ▰▰▱▱▱▱▱▱▱▱          ? →     ?
    agents         30      2    7%    ▰▱▱▱▱▱▱▱▱▱     ~1 200 →    90
    ───────────────────────────────────────────────────────────────
    total         229      9    4%    ▰▱▱▱▱▱▱▱▱▱     ~9 200 →   300
  ```

Color: low utilization → red, medium → yellow, high → green, reusing the
existing ANSI palette. `--no-color` honored. The 10-segment bar reuses the
`▰`/`▱` block convention already in `weightDisplay`.

### Markdown

`RenderGap` markdown variant emits an equivalent table for `reap gap --markdown`.

## Command wiring

New `gap` case in `cmd/reap/main.go`. It runs the same pipeline as the default
report (`scan` → `usage.Parse` → `report.Build`), then calls `RenderGap`. It
inherits existing flags: `--days`, `--json`, `--markdown`, `--no-color`,
`--claude-dir`, `--claude-json`. `reap gap --json` serializes only `r.Gap`.

The compact utilization line is emitted in the default `reap` text report only.
JSON/Markdown report output already carries `Gap` in the payload.

`usageText` in `main.go` updated to list `reap gap`.

## Edge cases

- `Sessions == 0` → all fired = 0, utilization shown as `n/a` (no divide-by-
  zero), with a "no transcripts in window" note.
- `Loaded == 0` for a category → row skipped (no divide-by-zero).
- All items fired → 100%, full green bar.
- MCP token weight unknown → `?`, excluded from token totals.

## Testing (TDD, table-driven, matching existing `report_test.go` style)

- `Build` populates `Gap` correctly from item + stats fixtures.
- Utilization % rounding and divide-by-zero guards.
- `RenderGap` snapshot: header, per-category rows, total row, bar widths.
- `reap gap --json` emits valid `Gap`.
- Excluded categories (hook/prose) never appear in `Gap`.
- `Sessions == 0` renders `n/a` without panic.

## Future extension (out of scope, phase 2)

`Gap` is JSON-serializable. Phase 2 appends each run's snapshot to
`~/.skillreaper/history.jsonl` with a timestamp, and `reap gap --trend` reads
deltas (sparkline / arrow). No code now; the struct is the only hook needed.
