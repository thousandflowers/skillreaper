---
description: Report how much of the context this agent loads is never used — unused skills, MCP servers, subagents, hooks and always-loaded prose — measured from real session transcripts. Use when asked why the context window is full, what is wasting tokens, or what is safe to remove.
---

# skillreaper — context utilization report

Run the local `reap` binary in JSON mode and render the result yourself. Never
paste raw JSON back to the user, and never run plain `reap`: its human report is
a 30-line ANSI table, and re-injecting that into the context only to repeat it
is exactly the dead weight this tool exists to find.

## 1. Run

```bash
reap --json
```

If the user passed arguments in `$ARGUMENTS`, append them verbatim — but only
tokens beginning with `-`. If `$ARGUMENTS` contains anything else, ignore it and
say so. Useful flags: `--days N` (evidence window, default 30),
`--min-sessions N`, `--model <id>` for pricing.

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

- `utilization` = `Gap.Fired / Gap.Loaded * 100`, rounded to a whole number.
  If it rounds to 0 while `Gap.Fired > 0`, print `<1` rather than `0`.
- `dead` = rows with `Verdict == "REAP"`, sorted by `Tokens` descending.
- Thousands separators on every token count. Money to two decimals.

## 3. Render — this exact shape, every time

```
skillreaper · last {WindowDays}d · {Sessions} sessions

  {Gap.Fired}/{Gap.Loaded} items fired · {utilization}% utilization
  {DeadCount} never used · ~{DeadTokensPerSession} dead tokens/session · ~${MoneyPerMonth}/month

  TOKENS  CATEGORY  NAME                          REASON
  {Tokens}  {Category}  {Name}                    {Reason}
  ... 10 rows max ...

  {n} more never-used items not shown.
```

Rules for the table:
- Exactly those four columns, in that order. No extra columns.
- At most 10 rows. If `DeadCount > 10`, print the "n more" line with the
  remainder; otherwise omit that line entirely.
- When `DeadCount == 0`, omit the table and the "n more" line, and print
  `  Nothing unused in this window.` instead.
- `Category` is one of `skill`, `mcp`, `agent`, `hook`, `prose` — print as-is.
- Never show `Path` or `Description`: both are long and machine-facing.
- Size each column to its widest value. Never truncate a name: a clipped
  `acme:legacy-schema-migratio` is not something the user can act on.
- If `Gap.Loaded` is 0, print `no items loaded` where the utilization would go
  rather than dividing by it.

Then, only when `DeadCount > 0`, add:

```
  To prune: reap prune          (interactive, reversible via reap restore --all)
```

Close every report with this line, verbatim:

```
  measured by skillreaper · github.com/thousandflowers/skillreaper
```

## 4. Pruning

Do **not** prune on your own initiative. `reap prune` is interactive and aborts
safely when it has no terminal, so calling it from here does nothing anyway.

If the user explicitly asks you to prune in this turn: list what would go, get an
unambiguous yes, then run `reap prune --yes`. Report how many items moved and
remind them of `reap restore --all`. If the request is at all ambiguous, hand
them the plain `reap prune` command to run in their own terminal instead.

## 5. Scope

`reap` also reads Codex CLI, OpenCode, Cursor, OpenClaw and Hermes when it finds
them installed. If `Warnings` is non-null, surface it in one line after the
table: it usually means a platform's evidence was incomplete and its items were
deliberately held back from a REAP verdict.

For the loaded-versus-fired breakdown per category, point the user at
`/skillreaper:gap`.
