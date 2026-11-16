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

A first Ctrl-C while the menu applies stops it after the item in hand and
shows what was done; a second one ends it at once. A crash gives the terminal
back as it was.

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
- Removing, deleting or restoring an MCP server checks `~/.claude.json` again
  just before writing it: if Claude Code changed it meanwhile, nothing is
  written and the command says to run again, instead of losing that change.
- On macOS and Linux the prune lock is the system's own (flock): a run that
  dies drops it, and two runs finding a lock left by a dead one can no longer
  both get in.
- `reap unmute <path>` undoes that one copy and leaves a namesake muted in
  another tool as it is.
- A YAML description block whose line is indented less than the rest keeps
  that line's text; its first letters, or half an accented one, were cut.
- A skill or agent of your own is dated by its file, so one written or edited
  in the last `--grace-days` is REVIEW(grace), not REAP: it had no date at all,
  and a skill made today was offered as safe to prune.
- `reap mute` works on a SKILL.md saved with a byte-order mark, and keeps the
  mark.
- The report's `DO` line for `reap prune` counts what a bare `reap prune`
  takes, not every unused item.
- `reap uninstall-hook` says when there was no hook to remove, and
  `reap install-hook` repairs a hook that names a reap binary which moved,
  and says when there was nothing to change.
- `reap mute` skips an item another prune moved meanwhile, as prune does,
  instead of failing on it.
- Model ids with a release date (`claude-haiku-4-5-20251001`) get their
  model's price: they were left out of the blended `$/month`.
- `--days` and `--grace-days` above 36500 are refused: past a century the date
  arithmetic wrapped and the window came out wrong. `--price` must be finite.
- A relative `--claude-dir` or `--claude-json` is made absolute first: mute
  refused it, and what prune and mute recorded named nothing from another
  folder.
- `reap uninstall-hook` leaves a hook group that is someone else's alone, even
  an empty one; `"hooks": null` or `[]` in settings.json counts as no hooks.
- The weekly nudge says what grew, items to prune or to mute; `reap diff`
  counts the snapshots still missing; naming a muted item in `reap prune`
  says it is muted.
- Under the `skillreaper` name, `help` keeps its columns.
- After a cleanup the weekly nudge's baseline follows the counts down, so it
  speaks again when new unused items appear.
- A menu apply that partly failed shows what went well first, then the
  failure's cause without the paths that pushed it off the screen.
- Restore, unmute and `keep --list` on a `--claude-dir` that is not there say
  so, instead of "Nothing to restore."
- `--claude-dir` alone finds the `.claude.json` inside or beside it: every MCP
  server used to drop out.
- `--source own` and `--source usr` work, as the report labels those sources;
  a mute with no match lists the sources there are.
- A joined emoji in a name shows as its emoji in the menu, not with
  replacement marks.
- Pruning an agent whose file is named `SKILL.md` moves that file, not the
  whole agents folder.
- MCP servers of one name in several projects are each listed; one of them,
  picked at random, used to stand for all.
- `--agent` says items were held back from REAP only when some were.
- `--mute-threshold` is described as what it counts: uses ÷ sessions.
- Paths reap prints (delete's confirmation among them) carry no control
  characters from a folder's name.
- `install-hook` repairs every stale hook of ours, not the last one.
- `reap <flag> reinvest` says reinvest comes first, instead of calling it
  unknown.
- In the menu, Option+arrow (Option as Meta) is no key, as other Alt keys
  are; its tail no longer arrived as `[` and `a`, which chose every
  suggestion. Esc pressed twice quickly is one Esc, not nothing.
- The README lists Hermes as inventory only, as the code has it: its history
  is not parsed yet, so its items are REVIEW, never REAP.
- `reap --agent` and `reap gap --agent` say `n/a`, not `0%`, when the window
  holds no session, and the gap view says so in a line: nothing was read, so
  nothing is measured.
- `reap mute` and `reap unmute` leave the file's date as it was. A personal
  skill is dated by its file, so one muted and unmuted read as installed
  today and sat in the grace period again.
- INSTALL.md no longer offers `~/.claude/reaped` as leftovers to clear after
  an uninstall: it holds the pruned items and the mute backups. It now says
  to restore and unmute first.
- Ctrl-C pressed while the menu waits behind another run's lock stops the
  apply. It used to be lost, and everything went through once the lock came
  free.
- Two projects whose folders share a name are two groups in the menu, each
  named by its whole path; they used to share one name and open together.
- In a narrow window the menu's legend keeps `q quit`: it takes the place of
  the last keys shown instead of being the one cut.
- `reap why` on an item held for a use before the window says so, and how to
  overrule it, instead of asking for more sessions that change nothing.
- A command reap prints to be pasted (`reap unmute`, `reap keep --remove`,
  the hints of `reap why`) carries a name or a path with a space as one
  quoted word.
- The README's `reap why` and `reap share` examples are generated with the
  other blocks now; both had drifted from what reap prints.
- The plugin's `reap` skill no longer says a piped word answers the prune
  question: without a terminal prune and mute stop and ask for `--yes`.
- A skill named in a heading of your CLAUDE.md is protected like one named in
  its text. Lines starting with `#` were skipped as comments; they are
  headings, and a session loads them.
- `reap keep` and `reap keep --remove` take the lock the other commands
  share: two run at once used to lose one of the two entries.
- Paths printed by `reap unmute` and `reap restore`, in their listings and
  their refusals, carry no control characters from a folder's name.
- The refusal for a kept item gives a `reap keep --remove` that can be pasted
  for a name with a space.
- The weekly nudge stays quiet where it cannot write down that it spoke; it
  used to speak at the start of every session.
- `reap snapshot --list --json` prints JSON.
- An integer flag written in another base (`--days 010`, `0x10`, `1_000`) is
  refused. `010` used to be read as octal: eight days, without a word.
- `reap why` counts the days left in the grace period to the date the verdict
  uses, and its rate line reads `uses/session 2%` instead of running the
  label into the figure.
- `reap gap` in a narrow window gives up the bar and the token columns
  before its category names, which were cut to `…` on every row.
- In the menu: a selection's footer says `space chooses` (it never only
  pruned); the question before a plugin is turned off reads `[y/n]`, since
  Enter does not answer it; a shared folder named on the receipt is cleaned
  of control characters; counts stand clear of the names before them in the
  narrowest window; the card at its smallest keeps the line that explains
  the choice; on a flat plan the saved line reads as a sentence.
- `docs/tapes/README.md` and `hero-fixture.sh` give a rebuild command that
  sets `SOURCE_DATE_EPOCH`; CONTRIBUTING and `docs/decisions.md` say the
  plugin's version is set to the tag before a release; the plugin's skills
  pass a flag together with its value.
- `CLAUDE_CONFIG_DIR` written with a trailing slash or as a relative path is
  the same folder: it used to open a second, empty record of past uses.
- A command printed to be pasted (`reap restore 001`, `reap unmute`, `reap
  keep --remove`) repeats `--claude-dir` when the run was given one. Ids
  start at 001 in every folder, so the pasted undo acted on another folder.
- `reap restore` takes an id typed without its zeros (`reap restore 1`), and
  its listing prints a server's name with no control characters.
- `reap unmute <path>` takes a relative path.
- `reap mute <server>` says mute is for skills and agents and points to
  `reap prune`, instead of saying to edit the config by hand.
- `reap <flags> reinvest` gives a command that keeps the reinvest flags that
  were typed; it used to drop them, so the pasted line was not a dry run.
- In the menu: Ctrl-C while another run holds the lock ends the wait at
  once; the other rows of a plugin already switched off are not reported as
  interrupted; a selection's footer counts what space will choose; two rows
  of one name from two marketplaces say which plugin each is; in the
  narrowest window the status and a group's name are cut short of the apply
  label and of the count, and the column title is not cut mid-word.
- `docs/tapes/README.md` says which two tapes need changing before they
  record again, and `docs/decisions.md` is newest first again.
- A credential after `Authorization: Basic` or `Token`, after `curl -u`, or in
  a `Cookie:` header is masked in the report, `--json` and `--md`. Only the
  scheme was masked before, and the credential was printed.
- A skill used under another letter case than its folder's is found in the
  record of past uses. It read as never used and could come out REAP.
- `--claude-json` given without `--claude-dir` is the file read and edited.
  It was dropped without a word, and `reap prune <server>` edited the usual
  file.
- A server defined for every project and again for one project, in one file,
  is not guessed at by name: `reap prune` and `reap delete` took the first.
- A project folder linked to another one in the same `projects` folder is
  counted once, and the measured token totals count only the window.
- A parse is counted from the start of the day the window opens on (UTC), so
  a cached parse and a fresh one agree at the window's edge.
- A warning about the plugin registry is printed once, not three times.
- With `--claude-dir` and no session in the window, the report says none was
  found, not that Claude Code keeps no transcripts.
- `reap why` counts the sessions still needed against the minimum the verdict
  uses for an item installed inside the window.
- `reap restore 7` brings back the item named 7 when there is one, never an
  entry already restored, and says what it restored (`restored 002 (name)`).
- A printed command for a name that starts with a dash puts it after `--`;
  the undo of `install-hook` and delete's restore hint repeat `--claude-dir`.
- Run as `skillreaper`, every hint says `skillreaper`: the weekly nudge said
  ``Run `reap` ``, a command an npm install does not have.
- `install-hook` on a settings file whose `hooks.SessionStart` is not a list
  says so and names the file.
- In the menu: with nothing chosen the status has the whole row (a failure's
  reason was cut off); unlocking two rows that share a lock is no failure; a
  stray byte no longer eats the keys typed after it; a skill and an agent of
  one name say which is which; a narrow receipt keeps a gap before the
  amount; rows and groups of equal weight keep one order at every launch.
- The README describes the gap view, snapshots and the nudge line as they
  are; `docs/renders/summary.txt` matches its captures and `make
  renders-check` now checks it; INSTALL.md names every place the npm binary
  can be kept.
- `reap mute` leaves a file whose `---` header never closes as it is: it has
  no frontmatter, as the scanner reads it.
- The npm wrapper gives up on a download only when it stops arriving, not
  after 30 seconds of a slow one.
- Installed with npm into a folder you cannot write (`sudo npm i -g`), the
  binary goes to `~/.cache/skillreaper/<version>/` instead of failing.

### Behaviour changes on the command line

- `echo y | reap prune` is refused, and a declined prompt exits 3.
- `--dry-run` on `prune` and `mute` exits 2: they show their plan and ask
  already.

### reap reinvest (experimental)

```bash
reap reinvest --dry-run    # pick swaps, print what would run, change nothing
reap reinvest              # the same, then ask before creating and muting
```

`reap` finds what you never use. `reinvest` looks at the work you actually do,
finds the kinds of work no skill of yours covers, and proposes swaps: a skill
for that work, written from your own sessions, against mutes of unused skills
of about the same weight. You pick the swaps with the arrow keys.

It is the Python prototype from `experiments/reinvest`, now in the binary, with
the same cache files and the same checks. What to know before running it:

- It asks Claude, through `claude -p`, to group your sessions, write the skill
  and read every candidate: about twenty calls and a minute on a first run.
  It says so on its first line, before any call. What it sends is excerpts
  of your sessions, with the keys it recognises removed before anything is
  kept or sent, and your installed skills' names and descriptions. Nothing else reap
  does calls a model or the network.
- Every candidate is read in full by Claude, twice, and by a set of risk rules
  first; one refusal is enough, and a refused document stays refused. A swap is
  all or nothing, and an existing skill of the same name is never replaced.
- `--online` searches skills.sh instead of writing. Its search and its checks
  were run against the live sites; installing what it finds was tested with
  stand-ins only, so try it with `--dry-run` first.
- Before the question that installs a skill, its whole text is on the
  screen: it is loaded in every session from then on.
- Mutes and their roll back name each file by its path, so undoing a failed
  swap never unmutes a copy you muted yourself in another tool.
- Keys removed before anything is sent or kept: Anthropic, OpenAI (project and
  service keys too), GitHub, GitLab, npm, Google, Stripe, Hugging Face, AWS,
  Slack, bearer tokens, JWTs, and private keys with their whole body.
- A skill holding text a reader cannot see (Unicode tag characters, direction
  overrides, a carriage return that redraws a line) is refused, and the screen
  never carries those characters; nor a failing program's escape sequences.
- A swap that failed stays on offer in `reap`'s report; one applied after
  being changed in the picker leaves it.
- Session text and skill descriptions go to Claude inside a marker they
  cannot guess, said to be data, in every call.
- A skill written for a gap that drops out of a run stays cached, so it comes
  back with the same text and verdicts.
- An installed skill must match what was verified whole: a file cut short,
  emptied or left out is not what was checked, and the install is taken out.
- It reads sessions and writes skills under `CLAUDE_CONFIG_DIR` when that is
  set, as Claude Code and reap do.
- `--days` outside 1–36500, `--max` under 1, and `--mcp` without `--online` are
  refused; with `--online` the first line also names skills.sh (and the MCP
  registry).
- A step that runs out of time, or is cut short by Ctrl-C, ends with
  everything it started (npx, node, git), and a skill a failed step half
  wrote is taken out again.
- Claude Code's compaction summaries are not read as messages you wrote.
- A cached written skill whose name is no slug (a path) is dropped.
- A config folder kept under a folder named `subagents` has its sessions
  read; only a `subagents` folder inside `projects` marks a subagent's.
- With no session in the window it asks Claude nothing. It used to send the
  grouping question twice, empty, and report the reply as a failure.
- The picker scrolls. On a 24-row terminal a fifth or sixth swap was drawn
  below the screen and could be accepted unseen.
- A kill or a closed window while the picker is open gives the terminal
  back, as Ctrl-C does.
- A skill that could not be written is reported (`✗ 1 of 2 skills not
  written`); it used to read as `0 skills written` with no reason.
- The help says what `--days` bounds: the transcripts read. Sessions
  remembered from earlier runs still count.
- Each Claude config folder (`CLAUDE_CONFIG_DIR`) keeps its own session
  memory and cache, and `reap`'s hint reads the proposals of the folder it
  reports on. The usual folder keeps the places it had. One memory for all
  sent one profile's prompts to Claude on behalf of another.
- Ctrl-C in the middle of a swap says what the swap had already changed
  before reinvest ends; it ended without a word.
- The picker draws no swap, and accepts none, in a window too small to show
  one (under 9 rows or 24 columns).
- `N with no skill` counts them all, and says when only the first few are
  taken up.
- A relative `CLAUDE_CONFIG_DIR` works: it broke every swap, left a link
  pointing nowhere and said `rolled back: nothing changed`.
- Each swap is printed, and leaves `reap`'s hint, as it lands; a Ctrl-C
  during the second one used to hide that the first was in place.
- `reap --json` exiting with an error is an error. A failed scan prints JSON
  with no rows, and that was read as "no skill installed".
- A stop while a failed swap is being undone says so, and the summary says
  `no swap proposed for` the gaps left out, not `no fit found`.
- The steps reinvest runs inherit `CLAUDE_CONFIG_DIR` as an absolute path: a
  relative one still sent `reap mute` to the wrong folder.
- `reap --claude-dir X reinvest` is answered with a command that keeps the
  folder (`CLAUDE_CONFIG_DIR=X reap reinvest`).
- A candidate is weighed, and its description read, by reap's own reader of
  the header, so the two agree on every quirk (a byte-order mark, an unclosed
  header, a quoted or nested `disable-model-invocation`).
- It has run on macOS. On Linux it builds and has not been run.

It is not in the menu yet.

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
