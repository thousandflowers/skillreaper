# Roadmap

What is planned, what is deliberately parked, and what has been decided against.
Grouped by the problem it solves, not by release. Nothing here is a date.

Decisions that are already settled and should not be re-litigated live in
[docs/decisions.md](docs/decisions.md); this file is the forward-looking half.

---

## 1. Make the shipped surface visible

`reap --help` lists 19 subcommands. The README documents the pruning ones well
and the rest barely, so the tool reads as a broom when most of it is
measurement.

- **Split read-only from writing commands, up front** — the safety guarantee is
  correct but spread across five places (README L26, L75, L133, L205-212,
  L243). A reader who stops at the fold has seen one of them. Add a "First 60
  seconds" path, and state in the report output itself that nothing left the
  machine. → [#50](https://github.com/thousandflowers/skillreaper/issues/50)
- **Surface `by-project`, `share` and `install-hook`** — one is a single line at
  L214, two are absent entirely, and `route` / `apm` start at L327 of 512.
  → [#51](https://github.com/thousandflowers/skillreaper/issues/51)

No behaviour changes. This is the cheapest work on the list and it gates
everything below that depends on people knowing the commands exist.

## 2. Make the team story actually work

The team need is real and reported: contributors have run a code review before
recommending it to an org, and the recurring observation is that teams do not
see context cost until they hit budgets.

The mechanism for it already shipped, and it is not CI — it is `reap apm`. As
proposed in [#8](https://github.com/thousandflowers/skillreaper/issues/8):
instead of `reap prune` quarantining files on one machine, the lean set is
encoded in a manifest every teammate's `apm install` reproduces.

What blocks it today is coordinate resolution:

- **Resolve coordinates from `Source` before falling back to a placeholder** —
  `resolveCoordinate` (`internal/report/apm.go:159`) consults `apm.lock.yaml`
  and nothing else. Measured on a maintainer machine: every emitted dependency
  is a placeholder, because no lockfile is present. `scan.Item.Source` already
  carries `plugin:<name@marketplace>` and is copied into the output at
  `apm.go:109` without ever being consulted. That is real provenance sitting two
  lines from the resolver. Consequence today: `reap apm` can reconcile a repo
  that already uses APM, but cannot bootstrap one — and bootstrapping is the
  onboarding case.
- **Capture provenance at install time** — the durable fix, and the one that
  makes the placeholder rate fall on its own: record upstream coordinates the
  moment a skill enters the repo, rather than reconstructing them afterwards.
  Same reason npm and pip write a lockfile at install: the only reliable moment
  to know where something came from is when it arrives. Legacy entries fill in
  as they are reinstalled.
- **Scan every detected platform** — `gather()` takes an override branch
  whenever `opts.claudeDir` is set, and `fillDefaults` sets it automatically
  whenever Claude Code is detected, so `platform.Detect()` runs and its result
  is discarded. `--help` advertises six platforms; `--json` returns rows with
  one distinct `Platform` value. For a multi-tool org this is the difference
  between a measurement and a partial one.
  → [#41](https://github.com/thousandflowers/skillreaper/issues/41)

## 3. Improve what the verdicts are worth

- **`--since` / `--until`, and a window that respects the corpus** — `--days`
  defaults to 30 and is the only temporal control, so a 200-day transcript
  corpus is scanned and 170 days of it discarded before any verdict. The report
  should state the window used against the history available.
  → [#53](https://github.com/thousandflowers/skillreaper/issues/53)
- **Separate rare from dead** — a skill fired four times a year is currently
  indistinguishable from one installed and forgotten. `keep`, `--grace-days` and
  `--min-tokens` all mitigate this manually or bluntly. This is the failure that
  costs trust permanently: prune once, lose something needed a month later.
  → [#54](https://github.com/thousandflowers/skillreaper/issues/54)
- **Measure wrong-tool picks** — the pitch has two halves. Dead context costs
  tokens: measured. Dead context degrades tool selection: asserted. The signal
  is already in the transcripts being parsed. Ships behind `reap why` first, and
  must not silently move a verdict.
  → [#55](https://github.com/thousandflowers/skillreaper/issues/55)

## 4. Track the stack over time

Every command answers "how does this look now". Nothing answers "what changed",
which is where the recurring questions are: did a pruned item come back after a
plugin update, is the bloat growing or flat, did a prune cost something.

`reap snapshot` writes the existing `--json` payload; `reap diff` compares two.
No new scanning — `--json` is already the complete serialization of a run.
→ [#52](https://github.com/thousandflowers/skillreaper/issues/52)

## 5. Parked, with the condition to unpark

- **A gallery of anonymised stacks, and README archetypes.** Both are wanted;
  both are blocked on volume, not on code. A gallery with three entries reads
  worse than no gallery, and archetype numbers that are estimated rather than
  observed reproduce the problem described in `decisions.md` about the dollar
  figure. `reap share` is already half the mechanism. Unpark when unsolicited
  outputs start arriving, and design the submission flow strictly opt-in — the
  "your data never leaves your machine" guarantee is load-bearing.
- **A web UI.** Changes how a team reads the data rather than what is measured,
  so it depends on §2 landing first. Parked as a question, not a plan: a local
  viewer over `--json` and a hosted multi-user dashboard are different products
  with different privacy stories.

## Not doing

- **A CI job that runs `reap`.** skillreaper measures local session
  transcripts; a CI runner has none. The job would either report an empty stack
  or inventory the runner's own files and declare them all dead. Making it work
  means every developer exporting locally and uploading — a data-collection
  problem, with a privacy cost, wrapped around a feature `reap apm` already
  delivers. The useful half of the idea is a recommended cadence in prose, which
  is part of [#51](https://github.com/thousandflowers/skillreaper/issues/51),
  and the periodic loop already exists as `reap install-hook`.
- **An estimated annual cost in dollars.** `docs/decisions.md` (2026-08-17)
  records why the report treats a dollar figure as a multiplication by an
  arbitrary constant rather than a measurement: the same stack has been reported
  at $24, $1.54, $1.84, $1.96 and $2.32 in two months without its context
  growing. Extrapolating to a year multiplies the spread by twelve alongside the
  number. Dead tokens per year is a count and is fine.
- **Git commit ranges (`--from <sha> --to <sha>`).** Transcripts carry
  timestamps, not SHAs, so the range has to be resolved by shelling into git per
  project directory to produce the time range `--since` / `--until` needs
  anyway. It breaks for non-git projects and for moved or deleted project
  directories, and answers in an awkward form a question people ask as "what was
  I using in July". Recorded in
  [#53](https://github.com/thousandflowers/skillreaper/issues/53).
