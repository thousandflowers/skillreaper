---
description: Measure the loaded-versus-fired gap — what fraction of each category of loaded context is ever actually invoked, and which MCP tools return mostly noise. Use when asked how much of the context is earning its place, or which categories are worst.
---

# skillreaper — loaded vs fired

Run `reap gap --agent`, then paste its output verbatim.

Do not reformat, re-order, summarise, truncate or re-align anything, and do not
add a heading or a table of your own: the binary already renders the report,
including its closing attribution line.

## Arguments

Append any `$ARGUMENTS` tokens that begin with `-`; ignore anything else and say
so. `--days N` sets the evidence window (default 30).

## If reap is not installed

Decide with `command -v reap` and nothing else — never an absolute path, never a
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

`reap gap --agent` needs reap ≥ 0.5.0. If it exits with
`flag provided but not defined: -agent`, say the installed build is too old and
give `brew upgrade skillreaper` (npm: `npm install -g skillreaper@latest`) — an
upgrade, never the install block above, which they have already done.

## Reading it out

After the pasted report you may add one or two sentences, no more. Name the
worst category by token reach rather than by item count — a hundred unused
5-token agents matter less than three unused 300-token skills. `/skillreaper:reap`
lists the specific items.
