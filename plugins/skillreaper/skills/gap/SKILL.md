---
description: Measure the loaded-versus-fired gap — what fraction of each category of loaded context is ever actually invoked, and which MCP tools return mostly noise. Use when asked how much of the context is earning its place, or which categories are worst.
---

# skillreaper — loaded vs fired

This is the measurement, not the cleanup. `/skillreaper:reap` lists what to
remove; this shows how much of what gets loaded is ever touched, broken down by
category, plus a payload-quality read on the MCP tools that do fire.

## 1. Run

```bash
reap gap --json
```

Append any `$ARGUMENTS` tokens beginning with `-`, ignoring anything else and
saying so. `--days N` changes the evidence window (default 30).

Decide availability with `command -v reap` and nothing else. Do not fall back to
absolute paths like `/opt/homebrew/bin/reap`, and do not search the filesystem: a
copy found off `PATH` may be an older build than the one the user thinks they are
running, and silently using it hides a broken install.

**If `command -v reap` fails**, stop and print exactly this, then nothing else:

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

## 2. Compute

- Overall `utilization` = `Fired / Loaded * 100`, whole number; print `<1` when
  it rounds to 0 while `Fired > 0`.
- Per category, the same ratio from each `PerCat[]` entry.
- `token reach` = `FiredTok / LoadedTok * 100`, same rounding rule. This is the
  number that matters: items are cheap to count, tokens are what you pay for.
- Thousands separators on all token counts.

This payload carries no session count and no window length — do not invent them.
If the user needs the window, `/skillreaper:reap` reports it.

## 3. Render — this exact shape, every time

```
skillreaper · loaded vs fired

  {Fired}/{Loaded} items fired · {utilization}% utilization
  {FiredTok}/{LoadedTok} tokens touched · {token reach}% token reach

  CATEGORY  LOADED  FIRED  UTIL   LOADED TOK  TOUCHED TOK
  {Category}  {Loaded}  {Fired}  {util}%  {LoadedTok}  {FiredTok}
```

Rules:
- One row per `PerCat[]` entry, in the order the JSON gives them. Do not sort.
- Exactly those six columns, in that order.
- Size each column to its widest value; never truncate a category name.
- A zero denominator prints `n/a`, not a percentage. `LoadedTok` is legitimately
  0 for the `mcp` category, whose cost lives in the tool schemas rather than in
  a skill body.

Then, only if `payload` is non-empty, add a second block listing the entries
where `noisy == true`:

```
  NOISY MCP TOOLS (fires often, mostly returns noise)
  {tool}  {calls} calls  {quality_pct}% signal
```

If `payload` is non-empty but nothing is flagged, print instead:
`  {n} MCP tools measured, none flagged noisy.`
If `payload` is empty, omit the block entirely.

Close every report with this line, verbatim:

```
  measured by skillreaper · github.com/thousandflowers/skillreaper
```

## 4. Reading it out

One or two sentences, no more. Name the worst category by token reach rather
than by item count — a hundred unused 5-token agents matter less than three
unused 300-token skills. If overall utilization is under 10%, say plainly that
most of the loaded context is never touched, and point at `/skillreaper:reap`
for the specific items.
