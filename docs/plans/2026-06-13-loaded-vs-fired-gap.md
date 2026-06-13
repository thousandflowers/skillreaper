# Loaded vs Fired Gap Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface the loaded-vs-fired utilization gap as a compact line in `reap` and a dedicated `reap gap` breakdown view.

**Architecture:** Extend the existing `internal/report` package. A new `gap.go` file owns the `Gap`/`GapCat` model, computes it from the already-joined `report.Rows` inside `Build()`, and renders it (text/markdown/json). `cmd/reap/main.go` gains a `gap` subcommand reusing the existing `gather()` pipeline.

**Tech Stack:** Go 1.x, standard library only (`fmt`, `io`, `encoding/json`), `go test`.

**Spec:** `docs/specs/2026-06-13-loaded-vs-fired-gap-design.md`

---

## File Structure

- **Create** `internal/report/gap.go` — `Gap`, `GapCat` types; `computeGap()`; `renderGapLine()`, `RenderGap()`, `RenderGapMarkdown()`, `RenderGapJSON()`; gap helpers (`utilPct`, `utilBar`, `utilColor`, `gapLabel`).
- **Create** `internal/report/gap_test.go` — gap computation + render tests.
- **Modify** `internal/report/report.go` — add `Gap *Gap` field to `Report`; populate in `Build()`.
- **Modify** `internal/report/render.go` — add shared `painter()` helper; call `renderGapLine()` from `RenderText()`.
- **Modify** `cmd/reap/main.go` — `gap` case, `cmdGap()`, usage text.
- **Modify** `cmd/reap/main_test.go` — `TestRunGap`.

Definitions used throughout:
- **Loaded** = inventory item (one per `Row` in categories skill/agent/mcp).
- **Fired** = `Row.Uses > 0`.
- **Util %** = `Fired * 100 / Loaded` (integer), `n/a` when `Sessions == 0`.
- MCP token weight is unknown → not summed; rendered `?`.

---

### Task 1: Gap model + computation

**Files:**
- Create: `internal/report/gap.go`
- Modify: `internal/report/report.go` (add field + populate)
- Test: `internal/report/gap_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/report/gap_test.go`:

```go
package report

import (
	"bytes"
	"strings"
	"testing"
)

func TestComputeGap(t *testing.T) {
	r := fixtureReport()
	g := r.Gap
	if g == nil {
		t.Fatal("Gap is nil")
	}

	// fixtureReport inventory (skill/agent/mcp only):
	//   skills: used-skill (uses 4, ~28 tok), dead-skill (uses 0, ~100 tok),
	//           ecc:plan (uses 2 via bare "plan", ~14 tok)
	//   mcp:    deadsrv (uses 0, token weight unknown)
	// prose is excluded.
	if g.Loaded != 4 {
		t.Errorf("Loaded = %d, want 4", g.Loaded)
	}
	if g.Fired != 2 {
		t.Errorf("Fired = %d, want 2", g.Fired)
	}
	// MCP tokens excluded: 28 + 100 + 14 = 142 loaded, 28 + 14 = 42 fired.
	if g.LoadedTok != 142 {
		t.Errorf("LoadedTok = %d, want 142", g.LoadedTok)
	}
	if g.FiredTok != 42 {
		t.Errorf("FiredTok = %d, want 42", g.FiredTok)
	}

	byCat := map[string]GapCat{}
	for _, gc := range g.PerCat {
		byCat[string(gc.Category)] = gc
	}
	if byCat["skill"].Loaded != 3 || byCat["skill"].Fired != 2 {
		t.Errorf("skill = %+v, want Loaded 3 Fired 2", byCat["skill"])
	}
	if byCat["mcp"].Loaded != 1 || byCat["mcp"].Fired != 0 {
		t.Errorf("mcp = %+v, want Loaded 1 Fired 0", byCat["mcp"])
	}
	if byCat["mcp"].LoadedTok != 0 {
		t.Errorf("mcp LoadedTok = %d, want 0 (unknown)", byCat["mcp"].LoadedTok)
	}
	// prose/hook never appear in PerCat.
	if _, ok := byCat["prose"]; ok {
		t.Error("prose must not appear in Gap")
	}
}
```

Note: `bytes` and `strings` are imported now because later tasks in this same file use them. If your linter flags them as unused at this step, that is expected until Task 2 — proceed; they are used by the end of Task 2.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/report/ -run TestComputeGap -v`
Expected: compile error — `r.Gap undefined` / `GapCat undefined`.

- [ ] **Step 3: Create the gap model + computation**

Create `internal/report/gap.go`:

```go
package report

import "github.com/thousandflowers/skillreaper/internal/scan"

// GapCat is the loaded-vs-fired breakdown for one category.
type GapCat struct {
	Category  scan.Category
	Loaded    int // inventory items whose description is injected
	Fired     int // items invoked at least once in the window
	LoadedTok int // sum of description tokens (0 for MCP — weight unknown)
	FiredTok  int // tokens of fired items (0 for MCP)
}

// Gap is the loaded-vs-fired snapshot for the window.
type Gap struct {
	PerCat    []GapCat // order: skill, mcp, agent
	Loaded    int
	Fired     int
	LoadedTok int
	FiredTok  int
}

// gapOrder fixes the category order shown in the gap view.
var gapOrder = []scan.Category{scan.CatSkill, scan.CatMCP, scan.CatAgent}

// computeGap derives the loaded-vs-fired snapshot from joined rows.
// Only skill/agent/mcp participate. MCP token weight is unknown, so its
// token sums are left at zero and excluded from totals.
func computeGap(rows []Row) *Gap {
	idx := map[scan.Category]int{}
	g := &Gap{}
	for i, c := range gapOrder {
		g.PerCat = append(g.PerCat, GapCat{Category: c})
		idx[c] = i
	}
	for _, row := range rows {
		i, ok := idx[row.Category]
		if !ok {
			continue
		}
		gc := &g.PerCat[i]
		gc.Loaded++
		g.Loaded++
		fired := row.Uses > 0
		if fired {
			gc.Fired++
			g.Fired++
		}
		if row.Category == scan.CatMCP {
			continue // token weight unknown without running the server
		}
		gc.LoadedTok += row.Tokens
		g.LoadedTok += row.Tokens
		if fired {
			gc.FiredTok += row.Tokens
			g.FiredTok += row.Tokens
		}
	}
	return g
}
```

- [ ] **Step 4: Wire Gap into the Report**

In `internal/report/report.go`, add a field to the `Report` struct (after the `Warnings` field):

```go
	Warnings             []scan.Warning
	Gap                  *Gap
```

In `Build()`, just before `return r` (after `sortRows(r.Rows)`):

```go
	sortRows(r.Rows)
	r.Gap = computeGap(r.Rows)
	return r
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/report/ -run TestComputeGap -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/report/gap.go internal/report/report.go internal/report/gap_test.go
git commit -m "feat: compute loaded-vs-fired gap in report (#4)"
```

---

### Task 2: Compact utilization line in the default report

**Files:**
- Modify: `internal/report/render.go` (add `painter` helper; call `renderGapLine`)
- Modify: `internal/report/gap.go` (add `renderGapLine` + util helpers)
- Test: `internal/report/gap_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/report/gap_test.go`:

```go
func TestRenderTextHasGapLine(t *testing.T) {
	var buf bytes.Buffer
	RenderText(&buf, fixtureReport(), false)
	out := buf.String()
	// fixtureReport: 2 of 4 fired → 50% utilization.
	if !strings.Contains(out, "utilization") {
		t.Error("report text missing utilization line")
	}
	if !strings.Contains(out, "50%") {
		t.Errorf("expected 50%% utilization, got:\n%s", out)
	}
	if !strings.Contains(out, "2/4 items fired") {
		t.Error("report text missing fired/loaded counts")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/report/ -run TestRenderTextHasGapLine -v`
Expected: FAIL — "missing utilization line".

- [ ] **Step 3: Add the shared painter helper**

In `internal/report/render.go`, add this helper immediately after the ANSI const block:

```go
// painter returns a function that wraps text in an ANSI code when color
// is enabled, and returns it unchanged otherwise.
func painter(color bool) func(code, s string) string {
	return func(code, s string) string {
		if !color {
			return s
		}
		return code + s + cReset
	}
}
```

Then, in `RenderText`, replace the existing inline closure:

```go
	paint := func(code, s string) string {
		if !color {
			return s
		}
		return code + s + cReset
	}
```

with:

```go
	paint := painter(color)
```

- [ ] **Step 4: Add util helpers + renderGapLine**

Replace the import line at the top of `internal/report/gap.go`:

```go
import "github.com/thousandflowers/skillreaper/internal/scan"
```

with:

```go
import (
	"fmt"
	"io"

	"github.com/thousandflowers/skillreaper/internal/scan"
)
```

Append to `internal/report/gap.go`:

```go
// utilPct returns the integer utilization percent and its display string.
// When there are no sessions there is no evidence, so it reports "n/a".
func utilPct(fired, loaded, sessions int) (int, string) {
	if sessions == 0 || loaded == 0 {
		return 0, "n/a"
	}
	pct := fired * 100 / loaded
	return pct, fmt.Sprintf("%d%%", pct)
}

// utilColor maps a utilization percent to an ANSI color: low is bad (red),
// mid is yellow, high is good (green).
func utilColor(pct int) string {
	switch {
	case pct < 10:
		return cRed
	case pct < 50:
		return cYell
	default:
		return cGreen
	}
}

// utilBar renders a 10-segment bar filled proportionally to pct.
func utilBar(pct int) string {
	filled := pct / 10
	if filled < 0 {
		filled = 0
	}
	if filled > 10 {
		filled = 10
	}
	out := make([]rune, 0, 10)
	for i := 0; i < filled; i++ {
		out = append(out, '▰')
	}
	for i := filled; i < 10; i++ {
		out = append(out, '▱')
	}
	return string(out)
}

// gapLabel is the human label for a gap category.
func gapLabel(c scan.Category) string {
	switch c {
	case scan.CatSkill:
		return "skills"
	case scan.CatMCP:
		return "mcp"
	case scan.CatAgent:
		return "agents"
	default:
		return string(c)
	}
}

// renderGapLine appends a one-line utilization summary to the default report.
func renderGapLine(w io.Writer, r *Report, color bool) {
	g := r.Gap
	if g == nil || g.Loaded == 0 {
		return
	}
	paint := painter(color)
	_, utilStr := utilPct(g.Fired, g.Loaded, r.Sessions)
	fmt.Fprintf(w, "\n  %s %s  —  %d/%d items fired · ~%d/%d tok touched (%dd)\n",
		paint(cBCyan, "⟡"),
		paint(cBold, "utilization "+utilStr),
		g.Fired, g.Loaded, g.FiredTok, g.LoadedTok, r.WindowDays)
}
```

- [ ] **Step 5: Call renderGapLine from RenderText**

In `internal/report/render.go`, inside `RenderText`, add the call right after the `DeadToolChars` block (just before the `for _, sec := range sectionTitles` loop):

```go
	if r.DeadToolChars > 0 {
		fmt.Fprintf(w, "  %s\n", paint(cDim, fmt.Sprintf("(init: ~%d chars of tool descriptions unused per session)", r.DeadToolChars)))
	}

	renderGapLine(w, r, color)

	for _, sec := range sectionTitles {
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/report/ -run 'TestRenderText|TestComputeGap' -v`
Expected: PASS (existing `TestRenderText` still passes; new gap-line test passes).

- [ ] **Step 7: Commit**

```bash
git add internal/report/gap.go internal/report/render.go internal/report/gap_test.go
git commit -m "feat: utilization line in default report (#4)"
```

---

### Task 3: `reap gap` text view

**Files:**
- Modify: `internal/report/gap.go` (add `RenderGap` + `writeGapRow`)
- Test: `internal/report/gap_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/report/gap_test.go`:

```go
func TestRenderGap(t *testing.T) {
	var buf bytes.Buffer
	RenderGap(&buf, fixtureReport(), false)
	out := buf.String()
	for _, want := range []string{"loaded vs fired", "skills", "mcp", "total", "60 sessions"} {
		if !strings.Contains(out, want) {
			t.Errorf("gap view missing %q", want)
		}
	}
	// MCP token weight is unknown and must render as "?".
	if !strings.Contains(out, "?") {
		t.Error("gap view must mark MCP tokens as ?")
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("color disabled but ANSI codes present")
	}
}

func TestRenderGapNoSessions(t *testing.T) {
	r := fixtureReport()
	r.Sessions = 0
	var buf bytes.Buffer
	RenderGap(&buf, r, false) // must not panic on divide-by-zero
	if !strings.Contains(buf.String(), "n/a") {
		t.Error("expected n/a utilization when no sessions")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/report/ -run TestRenderGap -v`
Expected: FAIL — `RenderGap` undefined.

- [ ] **Step 3: Implement RenderGap**

Append to `internal/report/gap.go`:

```go
// RenderGap writes the dedicated loaded-vs-fired breakdown. The bar shows
// per-category utilization; counts and token weight sit side by side. The
// whole row is tinted by its utilization so alignment is unaffected by ANSI.
func RenderGap(w io.Writer, r *Report, color bool) {
	paint := painter(color)
	g := r.Gap

	fmt.Fprintf(w, "\n  %s\n\n",
		paint(cBold, fmt.Sprintf("⟡ loaded vs fired — last %d days · %d sessions", r.WindowDays, r.Sessions)))

	if g == nil || g.Loaded == 0 {
		fmt.Fprintf(w, "  %s\n\n", paint(cDim, "no inventory found."))
		return
	}

	fmt.Fprintf(w, "  %-9s %7s %6s %6s   %-10s   %s\n",
		"CATEGORY", "LOADED", "FIRED", "UTIL", "", "TOKENS")

	for _, gc := range g.PerCat {
		if gc.Loaded == 0 {
			continue
		}
		writeGapRow(w, gapLabel(gc.Category), gc, gc.Category == scan.CatMCP, r.Sessions, paint)
	}

	fmt.Fprintf(w, "  %s\n", paint(cDim, "─────────────────────────────────────────────────────────"))
	total := GapCat{Loaded: g.Loaded, Fired: g.Fired, LoadedTok: g.LoadedTok, FiredTok: g.FiredTok}
	writeGapRow(w, "total", total, false, r.Sessions, paint)
	fmt.Fprintln(w)
}

// writeGapRow renders one aligned row, tinted by utilization. mcp marks the
// token columns as unknown ("?").
func writeGapRow(w io.Writer, label string, gc GapCat, mcp bool, sessions int, paint func(code, s string) string) {
	pct, utilStr := utilPct(gc.Fired, gc.Loaded, sessions)
	bar := utilBar(pct)
	tok := "      ? →     ?"
	if !mcp {
		tok = fmt.Sprintf("~%5d → %5d", gc.LoadedTok, gc.FiredTok)
	}
	line := fmt.Sprintf("  %-9s %7d %6d %6s   %-10s   %s",
		label, gc.Loaded, gc.Fired, utilStr, bar, tok)
	fmt.Fprintln(w, paint(utilColor(pct), line))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/report/ -run TestRenderGap -v`
Expected: PASS (both `TestRenderGap` and `TestRenderGapNoSessions`).

- [ ] **Step 5: Commit**

```bash
git add internal/report/gap.go internal/report/gap_test.go
git commit -m "feat: reap gap text breakdown view (#4)"
```

---

### Task 4: Markdown + JSON gap output

**Files:**
- Modify: `internal/report/gap.go` (add `encoding/json` import; `RenderGapMarkdown`, `RenderGapJSON`)
- Test: `internal/report/gap_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/report/gap_test.go`:

```go
func TestRenderGapMarkdown(t *testing.T) {
	var buf bytes.Buffer
	RenderGapMarkdown(&buf, fixtureReport())
	out := buf.String()
	for _, want := range []string{"# loaded vs fired", "| skills |", "| total |", "| Category |"} {
		if !strings.Contains(out, want) {
			t.Errorf("gap markdown missing %q", want)
		}
	}
}

func TestRenderGapJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderGapJSON(&buf, fixtureReport()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `"Loaded": 4`) {
		t.Errorf("gap json missing Loaded: %s", out)
	}
	if !strings.Contains(out, `"Fired": 2`) {
		t.Errorf("gap json missing Fired: %s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/report/ -run 'TestRenderGapMarkdown|TestRenderGapJSON' -v`
Expected: FAIL — `RenderGapMarkdown` / `RenderGapJSON` undefined.

- [ ] **Step 3: Add the json import**

In `internal/report/gap.go`, update the import block to add `encoding/json` (final block):

```go
import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/thousandflowers/skillreaper/internal/scan"
)
```

- [ ] **Step 4: Implement the markdown + json renderers**

Append to `internal/report/gap.go`:

```go
// RenderGapMarkdown writes the gap breakdown as a Markdown table.
func RenderGapMarkdown(w io.Writer, r *Report) {
	g := r.Gap
	fmt.Fprintf(w, "# loaded vs fired\n\n")
	fmt.Fprintf(w, "Window: last %d days · %d sessions\n\n", r.WindowDays, r.Sessions)
	if g == nil || g.Loaded == 0 {
		fmt.Fprintln(w, "_No inventory found._")
		return
	}
	fmt.Fprintln(w, "| Category | Loaded | Fired | Util | Loaded tok | Fired tok |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|")

	row := func(label string, gc GapCat, mcp bool) {
		_, utilStr := utilPct(gc.Fired, gc.Loaded, r.Sessions)
		lt, ft := fmt.Sprintf("~%d", gc.LoadedTok), fmt.Sprintf("~%d", gc.FiredTok)
		if mcp {
			lt, ft = "?", "?"
		}
		fmt.Fprintf(w, "| %s | %d | %d | %s | %s | %s |\n",
			label, gc.Loaded, gc.Fired, utilStr, lt, ft)
	}
	for _, gc := range g.PerCat {
		if gc.Loaded == 0 {
			continue
		}
		row(gapLabel(gc.Category), gc, gc.Category == scan.CatMCP)
	}
	row("total", GapCat{Loaded: g.Loaded, Fired: g.Fired, LoadedTok: g.LoadedTok, FiredTok: g.FiredTok}, false)
}

// RenderGapJSON writes only the Gap snapshot as indented JSON.
func RenderGapJSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r.Gap)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/report/ -v`
Expected: PASS (all report tests, including the gap test functions).

- [ ] **Step 6: Commit**

```bash
git add internal/report/gap.go internal/report/gap_test.go
git commit -m "feat: markdown + json gap output (#4)"
```

---

### Task 5: `reap gap` command wiring

**Files:**
- Modify: `cmd/reap/main.go` (usage text, `gap` case, `cmdGap`)
- Test: `cmd/reap/main_test.go` (`TestRunGap`, `TestRunGapJSON`)

- [ ] **Step 1: Write the failing test**

Append to `cmd/reap/main_test.go`:

```go
func TestRunGap(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	var out, errOut bytes.Buffer

	code := run([]string{
		"gap",
		"--claude-dir", claudeDir, "--claude-json", claudeJSON,
		"--days", "30", "--min-sessions", "1",
	}, strings.NewReader(""), &out, &errOut)

	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errOut.String())
	}
	for _, want := range []string{"loaded vs fired", "skills", "total"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("gap output missing %q", want)
		}
	}
}

func TestRunGapJSON(t *testing.T) {
	claudeDir, claudeJSON := buildFixture(t)
	var out, errOut bytes.Buffer

	code := run([]string{
		"gap",
		"--claude-dir", claudeDir, "--claude-json", claudeJSON,
		"--min-sessions", "1", "--json",
	}, strings.NewReader(""), &out, &errOut)

	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := decoded["Loaded"]; !ok {
		t.Errorf("gap json missing Loaded key: %s", out.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/reap/ -run TestRunGap -v`
Expected: FAIL — `gap` is an unknown command (exit code 2), output lacks "loaded vs fired".

- [ ] **Step 3: Add the gap line to usageText**

In `cmd/reap/main.go`, in the `usageText` const, add this line directly after the `reap [flags]` line:

```
  reap gap [flags]          loaded-vs-fired utilization breakdown
```

The surrounding lines for reference (the new line is the second one):

```
  reap [flags]              scan and report (read-only)
  reap gap [flags]          loaded-vs-fired utilization breakdown
  reap prune [flags]        quarantine unused items (reversible)
```

- [ ] **Step 4: Add the gap case + cmdGap**

In `run()`, add a `gap` case to the `switch cmd` block (immediately after `case "":`):

```go
	case "":
		return cmdReport(opts, stdout, stderr)
	case "gap":
		return cmdGap(opts, stdout, stderr)
```

Add the `cmdGap` function immediately after `cmdReport`:

```go
func cmdGap(opts options, stdout, stderr io.Writer) int {
	r, err := gather(opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	switch {
	case opts.asJSON:
		if err := report.RenderGapJSON(stdout, r); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
	case opts.asMarkdown:
		report.RenderGapMarkdown(stdout, r)
	default:
		report.RenderGap(stdout, r, colorEnabled(opts, stdout))
	}
	return 0
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./cmd/reap/ -run TestRunGap -v`
Expected: PASS (both `TestRunGap` and `TestRunGapJSON`).

- [ ] **Step 6: Run the full suite + vet**

Run: `go vet ./... && go test ./...`
Expected: PASS, no vet warnings.

- [ ] **Step 7: Manual smoke test**

Run: `go run ./cmd/reap gap`
Then: `go run ./cmd/reap | head -25`
Expected: `reap gap` prints the breakdown table with a `total` row; default `reap` shows the new `⟡ utilization …` line under the red shock box.

- [ ] **Step 8: Commit**

```bash
git add cmd/reap/main.go cmd/reap/main_test.go
git commit -m "feat: reap gap subcommand (#4)"
```

---

## Spec Coverage Check

- Snapshot in default report → Task 2 (`renderGapLine`).
- Dedicated `reap gap` view → Task 3 (`RenderGap`) + Task 5 (command).
- Count + tokens side by side → Tasks 2/3 (line + breakdown columns).
- Skill/agent/mcp only, prose/hook excluded → Task 1 (`gapOrder`), tested.
- MCP tokens unknown → `?` → Tasks 1/3/4.
- JSON (gap only) + Markdown → Task 4 + Task 5.
- `Sessions == 0` → `n/a`, no panic → Task 3 (`TestRenderGapNoSessions`).
- Future trend hook → `Gap` is JSON-serializable (Task 1); no code now.

## Symbol Reference (consistency)

Functions/types defined once and reused with these exact names:
- `Gap`, `GapCat` (Task 1)
- `computeGap(rows []Row) *Gap` (Task 1)
- `painter(color bool) func(code, s string) string` (Task 2, in render.go)
- `utilPct(fired, loaded, sessions int) (int, string)`, `utilColor(pct int) string`, `utilBar(pct int) string`, `gapLabel(c scan.Category) string` (Task 2)
- `renderGapLine(w io.Writer, r *Report, color bool)` (Task 2)
- `RenderGap(w io.Writer, r *Report, color bool)`, `writeGapRow(...)` (Task 3)
- `RenderGapMarkdown(w io.Writer, r *Report)`, `RenderGapJSON(w io.Writer, r *Report) error` (Task 4)
- `cmdGap(opts options, stdout, stderr io.Writer) int` (Task 5)

## Notes

- `Gap` uses no JSON struct tags — consistent with the rest of the package (e.g. existing tests assert `"DeadCount"`), so fields serialize by their Go names (`Loaded`, `Fired`, …).
- README headline numbers ("187 read, 4 used") are illustrative; the gap view reports the real per-install figures.
