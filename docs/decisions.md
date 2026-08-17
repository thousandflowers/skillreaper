# Decisions

Technical decisions that are not obvious from the code, recorded so a
contributor does not have to re-litigate them. Newest first. Each entry says
what was decided and why; if the reasoning stops holding, change the code and
add a new entry rather than editing an old one.

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
