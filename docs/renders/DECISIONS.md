# Why the report reads the way it does

These are the presentation decisions behind the v0.8.0 text report, one entry
per change, each naming what was hard to read before and what the change does
about it. Nothing here changed what is measured, how the window is computed, or
any number. `--json` is byte for byte what it was, apart from its own
`GeneratedAt` timestamp, and so is every subcommand that emits it. `--md` and
`--agent` changed in one respect only, and not a layout one: the headline that
read "items never used" over a count of REAP verdicts now names both counts.
That correction is described in the changelog, with the old and new lines beside
each other.

The captures beside this file are the evidence, and they are made by
`make renders`, which builds two binaries — "before" from the previous release
tag, "after" from the working tree — and runs both against the same generated
fixture. The fixture is the control, not a convenience. These files are evidence
about the *form* of the report, so the input has to be identical on both sides
for the comparison to isolate the only variable under test; a before and an
after taken against two different stacks would measure the stacks. The size of
the waste is a different claim with its own evidence, which lives in
`docs/measurements/` and in the README's "my install" block, and conflating the
two datasets is what put a figure on the page that no binary could reproduce.
`make renders-check` rebuilds the pair and fails if the committed captures have
drifted from what the binaries print, because generated evidence that nothing
re-checks goes stale in silence — these captures went on showing a headline the
binary had stopped printing, and nothing in the repo could notice.
The width-labelled ones are folded at their stated width, because a terminal
hard-wraps a long line at the right edge and the bytes on the wire contain no
newline there; a capture that stored raw bytes would record "before at 80" and
"before at 120" as the same file, which is exactly the difference being shown.
Folding is a no-op for output that already fits, which is itself the result.

The default report is captured as `report-*`; the other commands that print for
a human are captured beside it. Every one of them had the same defect, because
every one of them sized itself from its data instead of from the terminal.

Measured on the fixture, at 80 columns. Every figure below is a line of
`summary.txt`, which `make renders` writes beside the captures, so none of them
has to be taken on trust:

| view (at 80 columns) | before                | after               |
| -------------------- | --------------------- | ------------------- |
| `reap`               | 421 lines, widest 364 | 80 lines, widest 80 |
| `reap route`         | 20 lines, widest 109  | 24 lines, widest 80 |
| `reap by-project`    | 16 lines, widest 60   | 11 lines, widest 80 |
| `reap gap`           | 11 lines, widest 78   | 12 lines, widest 80 |

`gap` and `by-project` fit this fixture at 80 columns before the change; they
overflow on a stack with longer names. `reap` and `route` overflow on the
fixture itself, and `reap prune`'s by-source summary has the same defect for the
same reason — all four sized themselves from their data instead of from the
terminal, which is why all four were rebuilt on one layout.

Everything fits 80 now. `route` is the one view that got longer, and honestly
so: its prose used to run to 109 columns and simply spill off the edge, and
wrapping two paragraphs costs lines that overflowing did not.

## The width is measured, and nothing may exceed it

The old report had no idea how wide the terminal was. Section rules were a
title plus exactly sixty dashes. The name column was pinned at 44 characters
whatever else shared the line. The headline box was sized from its own text. In
the fixture that puts **the widest line at 364 columns, in a 421-line report**,
and on an 80-column terminal every one of those long rows wraps — because when a
terminal wraps a table row the remainder restarts at column zero and the columns
stop lining up. The same report is 80 lines wide by 80 lines long after the
change, and the margin grows with the stack: the longer the names, the further
past the edge everything after them used to sit. That is the failure the brief calls unreadable
fragments, and it was the report's normal condition over SSH.

Width now comes from `COLUMNS`, then the kernel's window size, then a fallback
of 80 when neither answers — a pipe, a redirect, a CI log. Every line is
produced by the table layout, wrapped, or truncated, so fitting the measure is
a property of the code rather than of whichever names happen to be installed on
the machine. `TestTextReportNeverExceedsMeasure` renders a deliberately hostile
report at seven widths and fails on a single overlong line.

Two limits are deliberate. Below 60 columns the report stops shrinking: a name
column narrower than that identifies nothing, and a table of unidentifiable
rows is worth less than the columns kept at its expense. Above 100 it stops
growing: a 200-column line is not read, it is tracked back and forth, and the
eye loses its row between the name and the verdict. That is why the
120-column capture is 100 columns wide, and why 120 and 200 render identically.

## The headline is three aligned figures, not a red box

Three numbers used to share one line inside a double-ruled box, separated by
interpuncts and painted bright red: `367 items never used · ~19755 dead
tokens/session · ~$5.22/month`. Three reading problems in one object. The box
was sized from its own content, so a longer number would push it past the
terminal and split the rules from the text. The interpuncts ran a count, a rate
and a price together as one phrase, so the eye had to parse the sentence before
it could take in any single figure. And the emphasis was hue, which a
monochrome terminal, `NO_COLOR`, a pipe and a meaningful share of readers all
fail to receive.

The figures are stacked now, values right-aligned in a column, one unit named
per line. The digits line up so magnitudes compare vertically, each figure says
what it measures, and the emphasis is weight and isolation rather than colour.
Nothing in the block is sized from the data, so nothing in it can overflow.

## Each verdict carries a word and an ASCII glyph, never a colour

REAP was red and KEEP was green. That is the one distinction in this report
that must never be ambiguous, and it was riding on the channel that fails most
often — for readers who do not separate red from green, and for every
monochrome terminal, `NO_COLOR` run and pipe, where the two verdicts rendered
as identical plain text.

Every verdict now leads with a glyph — `x` reap, `~` mute, `?` review, `=`
keep, `.` info — and its full word stands in the group header directly above
its rows. Shape survives what hue does not. The glyphs are ASCII rather than
box-drawing characters so they also survive a font that lacks them and a
screenshot scaled down. `TestVerdictsSurviveMonochrome` asserts the glyphs are
one column, ASCII and mutually distinct, and that the words appear with every
escape code off.

Colour was reduced to match. There is no red and no green anywhere in the
report now; hierarchy is bold, dim, and one cyan accent on structural rules.
Turning colour off changes how the report looks and nothing about what it says.

## A group of 289 rows stopped saying the same thing 289 times

Every row printed its verdict and reason joined together — `REAP · unused` —
289 times in a row, under a header that already read `reap 289 items`. Thirteen
columns of the scarcest resource on the line, spent restating what the reader
had been told immediately above.

A row now carries only its reason, and when every row in a group shares one
reason the reason is hoisted into the group header — `x  REAP · unused` — and
the column disappears entirely. Where reasons genuinely differ, as in a REVIEW
group holding both `no-transcript` and `grace`, the NOTE column stays and now
carries only what actually varies between rows.

## The weight bar is gone

Each row carried `▰▰▱▱▱`, a five-block bar proportional to the heaviest item in
its section. It cost six columns. Rows are already sorted heaviest first, so
the bar's rank information was the sort order restated; and its magnitude
information was the number sitting immediately to its left, stated less
precisely. It also meant different things in different sections, since each bar
was scaled to its own section's maximum — a full bar in PROSE and a full bar in
SKILLS were not comparable.

This is precisely the trade the brief warns about: it made the screenshot
livelier and the reading slower, spending six of eighty columns to repeat two
things already on the line. Deleting it is what bought the name column its
width at 80.

## Six rows per group, and an honest line about the rest

A real stack inventories 383 items and the old report printed every one. The
answer — the command to run — sat at line 463 of 469, behind six screens of
rows that mostly said the same thing, with the caveats that qualify the whole
report buried in there alongside it.

Each group now shows its six heaviest rows and then states exactly what was
withheld: `… 283 more · ~14,720 tok/session · full list: reap --md`. Nothing is
concealed. `reap prune` still lists every candidate before it touches anything
and is still reversible; `--json` and `--md` still emit the whole inventory;
`reap why <name>` still explains any single item in full. The report's job is a
verdict and an action. The complete inventory is a second-pass need, and it is
one flag away.

## Columns that say nothing are dropped, and expendable ones give way first

The prose and hook sections printed USES, LAST and PERM as a field of dashes,
because those categories carry no usage evidence at all. Any column whose cells
are all empty is now dropped from its table, which is what removes them.

When the measure still cannot hold what remains, columns are shed in an order
that encodes what a reader is here for: PERM first, then LAST, then SRC. The
name, the weight, the use count and the reason behind the verdict are the
decision itself and are never traded away. An earlier version of this clamped
the name column up to a minimum instead, which produced a table three columns
wider than its own terminal. The guarantee has to be fitting the measure, and a
guarantee that yields to a preference is not one.

## Dates became ages, and long numbers grew separators

The LAST column printed `2026-08-21`. The only question that column is ever
asked is how stale an item is, and a date makes the reader do arithmetic
against today to answer it — in ten columns. It reads `8d`, `never`, `today`
now. Markdown keeps the ISO date, which is the right form for a document that
outlives the run that produced it.

`19755` became `19,755` for the same kind of reason: identical value, but only
one of the two is taken in at a glance, and five- and six-digit token counts
are what this report exists to show. Text output only — every machine-readable
format keeps the bare integer.

## Warnings are wrapped, and their existence is announced at the top

Four warnings, the longest 379 characters, were printed one per line. At any
real width each became five ragged rows starting at column zero, so two
adjacent warnings were visually indistinguishable from one. They are wrapped
now with a hanging indent, which keeps each one a single block.

More consequentially, the masthead ends with `· 4 warnings below`. Each of
those warnings says the verdicts may be wrong about an entire platform. A
reader who discovers that only after scrolling past the verdicts has already
read the verdicts wrong.

## The last screenful is the answer

In a terminal the end of the output is what remains on screen when the prompt
returns; the top has scrolled away. The old report spent that space on the
estimate disclaimer, with the action above it and the headline six screens
back.

The report now closes with a `DO` block naming each command beside what running
it buys: `reap prune   367 items · ~19,755 tok/session reclaimed`. The count
and the weight are repeated there deliberately, so the last screenful stands on
its own — what to run, and what running it gets you.

## `reap gap` stopped saying its own finding in colour alone

The utilization table tinted each whole row by its band: red under 10%, yellow
under 50%, green above. This was the worst place in the tool for that. Low
utilization *is* the finding `gap` exists to deliver, and it was being
delivered in the one channel a monochrome terminal, `NO_COLOR`, a pipe and a
share of readers all fail to receive — where three bands collapse into one.
The bar and the percent beside it already carry magnitude in a form everyone
gets, so the tint is gone rather than replaced by another decoration.

The bar had a matching flaw underneath it. `filled = pct/10` drew 0% and 9% as
ten identical empty segments, so "never fired once" and "fires, but rarely"
were the same picture — again, precisely the distinction the view is for. A
value below one segment now draws a sliver, which is neither nothing nor a
segment rounded up to overstate it.

The token pair was one field, `~16394 →   331`, under a single header reading
`TOKENS`. An arrow the reader has to decode before either number means
anything, and a header naming neither of them. They are two labelled columns,
`TOK LOADED` and `TOK USED`.

## The payload table was spending its width on the wrong thing

Tool names were clipped at 40 characters —
`mcp__plugin_ecc_playwright__browser_nav…` — which cuts exactly the part that
says which tool it is. Beside that sat a ten-segment quality bar. In real data
every quality lands between 93% and 100%, so those bars were ten near-identical
columns restating a percentage already on the line. The bar is gone, the name
column absorbed the width, and the rows now name their tools in full.

"noisy" was a yellow tint on the row; it is a word in its own column, and the
column disappears when nothing is flagged.

## `by-project` became one table instead of thirty little ones

It was a nested list: a bold skill heading, then its projects indented
underneath. Most skills fire in exactly one project, so that spent three lines
saying what one line says — and it made the only comparison worth making,
which skills fire most and where, impossible, because the counts sat at a
different depth from the names and never lined up in a column. One flat table,
a row per skill-and-project pair, is 40 lines down to 24 and sorts by eye.

`⚑ repo-local` was a yellow flag. It is the entire point of the view — a skill
that looks cold globally only because it is hot in one repo — so it is a word
in a column now. `1 project(s)` reads `1 project`.

## `route` wraps the two lines that stop you doing the wrong thing

Its advisory and its closing "this is a plan — nothing is applied" caveat ran
to 108 and 109 columns. Those are the two sentences that stop someone applying
a plan they should not, which made them the worst lines in the tool to leave
running off the edge. They wrap.

The routed section also gave each leaf router its own table, with its own
header and its own column widths, so three routers meant three `NAME USES RATE`
headers and three different places the numbers landed — nothing could be
compared across them. It is one table now with the category as a divider
inside it, and no header at all, because the exposed table five lines above
already carries one for identical columns.

## What did not change

`NO_COLOR`, `--no-color` and non-terminal detection were already correct before
this work and are untouched: `report-before-piped.txt` contains no escape codes
either. What a pipe got wrong was width — 378 columns — and that is what is
fixed. The banner, the footer, the star CTA, every flag, every subcommand,
every exit code and every machine-readable format are as they were.

Two things were left deliberately alone. `reap apm` emits a proposed `apm.yml`
that a tool consumes, so its long comment lines stay long. And `reap why`
prints `used 11 time(s) in the window` — the same `(s)` construction fixed
elsewhere in this pass — because that sentence is also the `summary` field of
`reap why --json`. Correcting the grammar there would change a machine-readable
string, which costs more than the grammar is worth. The same reasoning holds
for `reap manifest`.
