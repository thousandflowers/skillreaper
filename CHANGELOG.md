# Changelog

Notable changes to skillreaper. This file starts at 0.8.0; notes for earlier
releases are on the [GitHub releases
page](https://github.com/thousandflowers/skillreaper/releases).

## 0.9.0

**`skillreaper` now opens a menu: every item on one screen, a choice per row or
group, and a receipt before anything changes. `reap` alone still prints the
report, and every command still works from the command line.**

### The menu

```bash
skillreaper        # or: reap menu
```

The number at the top is what you re-send in every session. It falls as you
choose, in tokens or in money. Rows are grouped by what reap suggests (prune,
mute, not sure, lock, already muted, already pruned), by plugin, or flat.

- `space` takes reap's suggestion for a row, a group or a selection; `p`, `m`,
  `l` and `d` choose prune, mute, lock or delete yourself; `a` fills in every
  suggestion and, pressed again, puts back what was there.
- Any action can be chosen on any row or group. A verdict is a suggestion and
  never limits the choice; only what is physically impossible is refused, and
  the row says why.
- `o` turns a whole plugin off through `claude plugin disable`, after asking if
  you use anything in it.
- `enter` opens a row's card: weight, last use, errors, and the reason for the
  verdict in a sentence.
- Nothing is touched until `shift+enter` and a `y` on the receipt, which lists
  every change, what cannot be undone first. Where Shift cannot be read (ssh,
  some Linux terminals) the key is `x`.
- A prune, a mute or a lock already on disk is taken back from the same screen
  with `u`.
- Shift+arrows, `v` and the mouse select many rows; `g` regroups, `f` filters by
  platform, holding `s` shows reap's suggestion on every row.

The menu carries out its choices with the same code the commands use, under the
same lock. It runs on macOS and Linux; on Windows, in a pipe, or with any
argument, `skillreaper` prints the report as before.

Two things it does on macOS that no command did, both local: it reads the
currency format set on your Mac to show amounts in it, at a fixed ECB rate
shipped in the release; and, because Terminal.app sends Shift+arrows as plain
arrows, a small `osascript` helper tells it whether Shift (and `s`) is down
while the menu is open. Nothing is sent anywhere, and neither runs over ssh.

### reap delete

`reap delete <name>` removes one item for good. It takes exactly one item, asks
first, and nothing brings it back: a copy already in quarantine stays there.
Prune remains the reversible way and the default everywhere.

### MCP servers have a weight

An MCP server's tool schemas are built at runtime, so its rows used to read `?`
and the total under-counted. reap now measures them from what your transcripts
show Claude Code injected, and `reap why` says where a weight comes from or why
one is still unknown. Skills are weighed the same way, by what the listing
really injected, with multi-line descriptions read in full.

### Truer verdicts and numbers

- Uses are counted per platform and by the date of the event. A use in one tool
  says nothing about the copy installed in another.
- Only commands you typed count as uses, and a skill a plugin's SessionStart hook
  loads every session is no longer called unused.
- A skill named in your `CLAUDE.md`, an import or your rules is protected
  wherever reap runs from, not only in the directory that file lives in.
- A tool whose history cannot be read is no longer judged on other tools'
  sessions.
- Mute savings leave out the name, which stays loaded, and money figures follow
  each platform's own session count.

### Safer prune, mute and restore

- Quarantine paths are unique. Two items with the same name used to overwrite
  each other there, and `restore` could not bring both back.
- A name two items share is refused rather than guessed: select with
  `codex:skill:x` or a path.
- Named prune, mute and delete reach a plugin's item or a link; a bulk prune
  never does.
- `reap restore` with no arguments lists what is pruned, with ids.
- A kept item refuses a named prune or mute until `reap keep --remove`.

### Behaviour changes on the command line

- `echo y | reap prune` is refused, and a declined prompt exits 3.
- `--dry-run` on `prune` and `mute` exits 2: they show their plan and ask
  already.

### Not in this release

`reinvest`, which proposes a skill for the work no skill of yours covers, stays
in `experiments/` as a Python prototype.

## 0.8.0

**Every command that prints for a human was redesigned — `reap`, `reap gap`,
`reap route`, `reap by-project` and `reap prune`. If you read them in a
terminal they look different. If you parse them, nothing changed — see below.**

### Everything fits your terminal now

Each view measures the terminal and lays itself out to it. None of them emits a
line wider than that measure, at any width.

They previously had no width awareness at all. In the report, section rules
were a title plus sixty fixed dashes, the name column was pinned at 44
characters, and the headline box was sized from its own text, so on an
80-column terminal — what most SSH sessions are — lines ran off the right edge
and the table columns stopped lining up. The defect scales with the stack: the
wider the widest name, the further past the edge everything after it sits.

Measured on the fixture both binaries are run against, at 80 columns
(`docs/renders/summary.txt`, regenerate with `make renders`):

| at 80 columns     | before                | after                  |
| ----------------- | --------------------- | ---------------------- |
| `reap`            | 421 lines, widest 364 | 80 lines, widest 80    |
| `reap route`      | 20 lines, widest 109  | 24 lines, widest 80    |
| `reap by-project` | 16 lines, widest 60   | 11 lines, widest 80    |
| `reap gap`        | 11 lines, widest 78   | 12 lines, widest 80    |

`gap` and `by-project` already fit that fixture at 80 columns; they overflow on
a stack with longer names, which is the same defect and the reason they were
rebuilt on the same layout rather than left alone. `route` and `reap` overflow
on the fixture itself.

Width comes from `COLUMNS`, then the terminal's own size, then a fallback of 80
for pipes, redirects and CI logs. The layout holds at 60 columns at the narrow
end and stops widening past 100 at the wide end. `route` is the one view that
got longer: its prose used to run off the right edge rather than wrap.

### Nothing depends on colour any more

REAP was red and KEEP was green — unreadable for anyone who does not separate
those hues, and identical plain text in a monochrome terminal, under
`NO_COLOR`, or through a pipe. Every verdict now leads with an ASCII glyph —
`x` reap, `~` mute, `?` review, `=` keep, `.` info — and its word appears in the
group header above its rows.

`reap gap` was the worst case: it tinted each whole utilization row red, yellow
or green by band, so low utilization — the finding the command exists to
deliver — was carried by hue alone and collapsed to a single band without it.
That tint is gone; the bar and the percent already carry it. `gap`'s "noisy"
payload flag and `by-project`'s `repo-local` flag were also colour, and are
words in their own columns now.

Red and green no longer appear anywhere. Emphasis is bold, dim, and one accent
on structural rules.

### `reap gap` no longer draws 0% and 2% identically

Its utilization bar computed `filled = pct/10`, so anything under 10% rendered
as ten empty segments: "never fired once" and "fires, but rarely" were the same
picture, in the view built to tell them apart. A value below one segment now
draws a sliver. The token pair `~16394 →   331` under a header reading `TOKENS`
became two labelled columns, `TOK LOADED` and `TOK USED`.

The payload table clipped tool names at 40 characters
(`mcp__plugin_ecc_playwright__browser_nav…`) while spending ten columns on a
quality bar whose values all sit between 93% and 100%. The bar is gone and the
names are whole.

### `reap by-project` is one table instead of thirty small ones

It was a nested list — a skill heading, then its projects indented under it —
which spent three lines per skill when almost every skill fires in exactly one
project, and put the firing counts at a depth where they never lined up into a
column you could compare. One flat table, a row per skill-and-project pair: 40
lines down to 24. `1 project(s)` now reads `1 project`.

### Long lists are summarised, not dumped

Each verdict group shows its six heaviest rows and then states exactly what it
withheld: `… 283 more · ~14,720 tok/session · full list: reap --md`. The report
used to print all 383 rows, which left the command to run at line 463 of 469.
`reap prune` still lists every candidate before it touches anything, and it is
still reversible.

### Smaller changes to the same end

- The headline is three right-aligned figures on their own lines, rather than
  one red run-on inside a box that could overflow the terminal.
- A row no longer repeats its group's verdict, and a reason shared by every row
  in a group is stated once in the group header.
- The per-row weight bar (`▰▰▱▱▱`) was removed: rows are already sorted heaviest
  first and the number sits beside it, so the bar spent six columns restating
  two things already on the line.
- Columns that are empty for a whole category are dropped instead of printed as
  a field of dashes.
- `LAST` shows an age (`8d`, `never`) instead of an ISO date. Markdown output
  still shows the date.
- Token counts are grouped (`19,755`) in the text report only.
- Warnings are wrapped with a hanging indent, and the masthead says how many
  there are, so the caveats are visible before the verdicts they qualify.
- The report closes with a `DO` block naming each command beside what running it
  reclaims.
- Group and action lines say "1 item", not "1 items".
- The report closes by saying the analysis was local: only files on this machine
  were read, nothing was sent over the network. The claim was in the README and
  nowhere in the tool, which is not where it is asked. `--json`, `--md` and
  `--agent` are unchanged.

### `reap snapshot` and `reap diff`

A report answers "how does the stack look now". `reap snapshot` writes the run's
`--json` payload beside the evidence digest, and `reap diff` compares two of
them, newest against previous by default: what appeared, what left, what changed
verdict, and the movement in dead tokens and utilization.

The case it exists to catch leads the output: an item that is **back after being
pruned**. That line is read from the prune manifest, not inferred from the two
snapshots, so it distinguishes "this tool removed it and it returned" from
"something is different"; an item you restored yourself is not reported.

No new scanning and no new parser - the payload is exactly what `--json` already
prints, so a snapshot cannot disagree with the run that produced it. Snapshots
are never taken automatically.

### `SOURCE_DATE_EPOCH` pins the clock

`reap` measures its evidence window from the wall clock and renders ages against
it, so anything generated from a fixture carried a value that moved with the day
it was produced. Setting `SOURCE_DATE_EPOCH` to a Unix timestamp now pins the
instant the views are measured against, which is what lets the captures under
`docs/renders` be compared byte for byte.

Only the views take it. Timestamps that record something that actually happened,
the quarantine manifest, the lock file and the nudge state, keep the real clock,
so it can never write a false time into a file. A malformed value is ignored.

### The headline named a count it was not counting

**This one changes text you may be reading by eye.** The figure at the top of
the report read `items never used`, but the number under those words is
`DeadCount`, which counts REAP verdicts — not items that never fired. The two
differ whenever reap declines to condemn something it cannot see: an item on a
platform that publishes no transcript, where absence of a recorded use is not
evidence of no use, or one referred to from `CLAUDE.md`. On the maintainer's own
stack that was 369 against 367, printed directly above `13/382 items fired`, so
a reader who subtracted arrived at a third number and concluded the tool could
not count. It could. The label was wrong, and refusing to condemn what cannot be
observed is the argument this tool is built on, so the figure that says so is on
the front page now instead of nowhere.

The wording had been written out separately in four renderers — the text report,
`--md`, `--agent`, and the README generator — which is how a single wrong phrase
shipped in four places at once. All four derive it from one place now
(`report.NewHeadline`), so the next correction to it lands everywhere.

Old and new, both from the same fixture:

```
text report   0.7.0   ║  371 items never used · ~22720 dead tokens/session · ~$2.32/month  ║
              0.8.0       371  items never fired, of 378 loaded
                          371 of them marked REAP

--agent       0.7.0   371 never used · ~22720 dead tokens/session
              0.8.0   371 never fired · 371 marked REAP · ~22720 dead tokens/session

--md          0.7.0   **371 items never used · ~22720 dead tokens/session · ~$2.32/month**
              0.8.0   **371 never fired · 371 marked REAP · ~22720 dead tokens/session · ~$2.32/month**
```

On a stack where reap holds nothing back the two counts are equal, as they are
in that fixture; where it holds something back the second line also says how
many, as `367 of them marked REAP, 2 held back rather than condemned`.

Two smaller strings moved with it, both the same conflation: `--agent` prints
`Nothing to prune in this window.` where it said `Nothing unused in this
window.`, and its overflow line reads `(283 more marked REAP not shown — use
--json for all)` where it said `more never-used items`.

### What is machine-readable, and what moved

`--json` is byte-identical to 0.7.0 apart from its own `GeneratedAt` timestamp,
for every command, and so are `reap apm`, `reap manifest`, `reap why --json` and
`reap share` — verified by diffing both binaries against the same fixture. No
flag, subcommand or exit code changed, and nothing about what is counted, how
the window is computed, or any number changed.

`--md` and `--agent` changed in exactly one way: the headline wording above.
Anything reading those two for the words "never used" needs the new phrasing;
anything reading `--json` needs nothing.

Two rough edges were left alone for that reason. `reap apm` emits a proposed
`apm.yml` that tools consume, so its long comment lines stay long; and
`reap why` still prints `used 11 time(s)`, because that sentence is also the
`summary` field of `reap why --json`.

The reasoning behind each change is in
[`docs/renders/DECISIONS.md`](docs/renders/DECISIONS.md), with before and after
captures at 80 and 120 columns beside it.
