---
description: Report how much of the context this agent loads is never used - unused skills, MCP servers, subagents, hooks and always-loaded prose - measured from real session transcripts. Use when asked why the context window is full, what is wasting tokens, or what is safe to remove.
---

# skillreaper - context utilization report

Run `reap --agent`, then paste its output verbatim.

Do not reformat, re-order, summarise, truncate or re-align anything, and do not
add a heading or a table of your own: the binary already renders the report,
including its closing attribution line.

## Arguments

Append any `$ARGUMENTS` tokens that begin with `-`; ignore anything else and say
so. `--days N` sets the evidence window (default 30), `--model <id>` prices it.
For the full inventory rather than the top rows, use `reap --json`.

## If reap is not installed

Decide with `command -v reap || command -v skillreaper` and nothing else (use whichever name it finds) - never an absolute path, never a
filesystem search: a copy found off `PATH` may be an older build, and silently
using it hides a broken install.

If that fails, print exactly this and nothing else:

> `reap` is not installed. Run it once without installing anything:
>
> ```bash
> npx skillreaper
> ```
>
> Or install it permanently:
>
> ```bash
> brew install thousandflowers/tap/skillreaper
> ```
>
> ```bash
> go install github.com/thousandflowers/skillreaper/cmd/reap@latest
> ```

## If reap is too old

`--agent` needs reap ≥ 0.5.0. If it exits with
`flag provided but not defined: -agent`, say the installed build is too old and
give `brew upgrade skillreaper` (npm: `npm install -g skillreaper@latest`) - an
upgrade, never the install block above, which they have already done.

## Pruning

Never prune on your own initiative, and never run `reap prune` or `reap mute`
from here to see what they would do. Their question reads stdin, not a terminal:
a piped word answers it and the change goes through.

If the user explicitly asks in this turn: get an unambiguous yes, run
`reap prune --yes`, and remind them of `reap restore --all`. If the request is
at all ambiguous, hand them the plain `reap prune` command instead.

For the per-category breakdown, point them at `/skillreaper:gap`.
