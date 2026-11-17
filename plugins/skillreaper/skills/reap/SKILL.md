---
description: Report how much of the context this agent loads is never used - unused skills, MCP servers and subagents, measured from real session transcripts. Use when asked why the context window is full, what is wasting tokens, or what is safe to remove.
---

# skillreaper - context utilization report

Run `reap --agent`, then paste its output verbatim.

Do not reformat, re-order, summarise, truncate or re-align anything, and do not
add a heading or a table of your own: the binary already renders the report.
Paste all of it, to its last line: the attribution line, and any note that
follows it.

## Arguments

Append any `$ARGUMENTS` tokens that begin with `-`, each with the value that
follows it (`--days 7`, `--model <id>`); ignore anything else and say so. `--days N` sets the evidence window (default 30), `--model <id>` counts the tokens as that model does.
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
from here to see what they would do. They ask on a terminal, and here there is
none: they stop with `no terminal to ask on; pass --yes to go ahead`, and
`--yes` is the change itself, not a preview.

If the user explicitly asks in this turn, get an unambiguous yes first. When
they named an item, run `reap prune <name> --yes` and say that name back in
your question: the bare `reap prune --yes` takes every item reap would reap,
with no further question. Use the bare form only when they asked for all of
it, and say how many items that is before the yes. Either way, remind them of
`reap restore --all`. If the request is at all ambiguous, hand them the plain
`reap prune` command instead.

If the report was made with `--claude-dir` or `--claude-json`, repeat the same
flags on the prune and say the folder in your question: without them the prune
acts on the default folder, not the one the report was about. Write a name as
one quoted word, and after `--` if it begins with a dash.

For the per-category breakdown, point them at `/skillreaper:gap`.
