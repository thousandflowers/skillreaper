# Decisions

Technical decisions that are not obvious from the code, recorded so a
contributor does not have to re-litigate them. Newest first. Each entry says
what was decided and why; if the reasoning stops holding, change the code and
add a new entry rather than editing an old one.

## 2026-08-17 — The gofmt cleanup waits for PR #22, not the other way round

`internal/cost/cost_test.go` and `internal/platform/platform.go` drifted from
gofmt under go1.26. The fix is trivial and is held back anyway, in this order:
**#22 merges → `chore/repo-debt` rebases → `chore/repo-debt` merges → only then
the `gofmt -l` check enters CI.**

gofmt realigns the struct block in `TestTokens`, at `cost_test.go:29-38`. PR #22
inserts `TestTokensFor` at `@@ -29,6 +29,32 @@` — the same lines. Landing the
cleanup first would hand a first-time contributor a rebase conflict created
entirely by our own formatting debt, immediately after telling them in review
that the gofmt noise was not theirs to fix. Whose branch absorbs a conflict is a
social decision, not a mechanical one, and it belongs to the person who owns the
debt.

The CI check goes last for a duller reason: enabling it while the tree is still
unformatted turns the next push red.

## 2026-08-17 — `reap --agent` renders for agents; the skills stop rendering

**Supersedes "Plugin skills call `reap --json`, not `reap`" below.** That entry
stays as written: the reasoning was sound and the outcome was still wrong, and a
log that quietly edits itself is not a log.

`RenderAgent` and `RenderGapAgent` in `internal/report/agent.go` emit compact
plain text with no ANSI, no bars, no box drawing and no terminal-width padding.
The two `SKILL.md` files now say "run it, paste it verbatim" and carry no
rendering rules at all.

What changed our mind: a prose render spec grows a rule for every edge case
somebody hits — column widths, truncation, zero denominators — and every line of
it is context loaded in every session, which is the dead weight this tool exists
to measure. A skill that must gain weight to stay deterministic has lost its own
argument. The delegated rendering also disagreed with the binary on the numbers,
not just the layout: `utilPct` truncates, the spec said "round", so the same data
was reported as 1% by `reap gap` and 2% by the skill. In the binary that is one
`pctOf` and a table test; in prose it was a paragraph nobody could verify.

New functions rather than a flag threaded through the render path. `RenderText`
already takes a `color bool`, and `color=false` does **not** produce this format
— the box drawing, the `▰▱` bars, the 60-dash rules and the 44-char name clip
all survive it. A second bool would have to reach six functions, and the two
outputs would then share branches that can drift apart in silence.

## 2026-08-17 — `--agent` carries a signature; `--json` and `--md` still do not

`RenderAgent` writes its attribution line unconditionally: no colour flag, no
TTY check, no cooldown, and it never touches `NudgeState` or `RenderFooter`.

This is a deliberate exception to the rule one entry below, and the difference is
who reads the bytes. `--json` and `--md` are piped into other programs, where a
signature is a parse error waiting to happen. `--agent` is prose an agent hands
to a person, so the attribution belongs in the payload. Putting it in the
`SKILL.md` instead was the obvious alternative and was rejected: it would leave
exactly one formatting instruction in the prompt, and that is the crack the whole
render spec grew back through last time.

## 2026-08-17 — `TokenRatios` is a config table, not control flow

Per-model character-per-token ratios live in an exported `TokenRatios` map in
`internal/cost`, parallel to the existing `ModelPricing` map. Models absent from
the map fall back to the default 3.7 ratio.

Adding a model means adding a row, never touching `tokensWithRatio` or any
branch. That is the same extension point `ModelPricing` already established, so
the two maps stay learnable as one idea.

## 2026-08-17 — The footer signature is not a nudge

`RenderFooter` prints a permanent attribution line: no cooldown, no token
threshold, no opt-out, and it never touches `NudgeState`. The star-CTA remains
separate and stays throttled.

It lives in `cmdReport`'s default branch, which is what keeps it out of
`--json`, `--md` and `--quiet` without a hand-written gate — machine-readable
output must stay parseable. The colour flag decides only how the line is
painted, never whether it is printed, so redirecting to a file still carries
attribution.

## 2026-08-17 — Plugin commands are namespaced `/skillreaper:reap`

The Claude Code plugin exposes `/skillreaper:reap` and `/skillreaper:gap`. The
namespace is not abbreviated.

The verbosity is deliberate: the tool's name is repeated at every invocation,
and a short alias would collide with unrelated commands in a user's install.

## 2026-08-17 — The plugin does not bundle the binary

The plugin assumes `reap` is on `PATH` and prints a runnable install block when
it is not, leading with `npx skillreaper` because that needs no install at all.

`reap` already ships through Homebrew, npm/npx and `go install`. Bundling it
would mean committing six GOOS/GOARCH builds; downloading it from the plugin
would duplicate the checksum-verified fetch `npm/lib/release.js` already
performs. The skills resolve availability with `command -v reap` only — never an
absolute path, never a filesystem search, because a copy found off `PATH` may be
an older build and silently using it hides a broken install.

## 2026-08-17 — Plugin skills call `reap --json`, not `reap`

Both skills run the JSON mode and render the result themselves, following an
explicit render spec written into each `SKILL.md`.

The human report is a ~30-line ANSI table. Injecting it into an agent's context
only to have the model repeat it back is precisely the dead weight this tool
exists to find. **Known cost:** rendering delegated to a model is not
deterministic — the same spec has produced different column widths across runs.
The render specs pin column order, row limits, rounding and truncation to narrow
that, and the trade-off is revisited if the drift proves not to be containable.

## 2026-08-17 — The plugin ships no `hooks/hooks.json`

The plugin deliberately contains no hooks.

`reap install-hook` writes a `SessionStart` nudge into the user's own
`settings.json`, keyed by the marker comment `skillreaper-weekly-nudge`. A hook
shipped in the plugin would be a second, independent copy: anyone who already
ran `install-hook` would be nudged twice, and `reap uninstall-hook` can only
remove the `settings.json` copy, never the plugin's. If hooks are ever wanted
here, the plugin must *detect* that marker and supersede the manual hook rather
than coexist with it.
