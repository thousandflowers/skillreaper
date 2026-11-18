# Changelog

Notable changes to skillreaper. This file starts at 0.8.0; notes for earlier
releases are on the [GitHub releases
page](https://github.com/thousandflowers/skillreaper/releases).

## 0.9.0

**`skillreaper` now opens a menu: every item on one screen, a choice per row or
group, and a receipt before anything changes. `reap` alone still prints the
report, and every command still works from the command line.**

### Upgrading from 0.8

What behaves differently for someone who had 0.8:

- `reap prune <name>` prunes that one item. In 0.8 the name was ignored and
  the whole plan was queued.
- A muted item is no longer REAP: its verdict is `-` with the reason `muted`,
  so the counts of the report and the rows of `--json` change for anyone who
  had muted something. `--json` also gained `MutedCount`, `Platforms`,
  `PriceBasis`, `PricePerToken`, `MonthlyBasisDays`, `PlatformSessions` and
  `PlatformSessionsPerMonth`.
- The session count and the measured tokens can be lower than 0.8 printed
  for the same folder: a session counts by when it happened, and a message
  is measured once. Monthly figures are scaled from the days of history
  there really are, so with under a month of it the money can read higher.
- The questions default to no. 0.8 asked `[Y/n]`, and Enter went ahead; 0.9
  asks `[y/N]`, and Enter is `aborted`.
- `reap mute <name>` asks first, as prune does. In 0.8 it muted at once.
- `echo y | reap prune` is refused: with no terminal to ask on, prune and
  mute need `--yes`. A declined prompt exits 3, and so does `--quiet`
  without `--yes` when there was something to do (it exited 0 in silence).
- A skill or agent of your own written, edited or copied in the last 14
  days is REVIEW (`grace`), not REAP: it is dated by its file. After a
  restore from a backup, or a fresh checkout of your dotfiles, most of a
  stack reads REVIEW for two weeks.
- `reap manifest <name>` prints a `Platform:` line and a relative install
  path, and no longer a `Restore path`.
- In `--json`, a row of an item never used has no `LastUsed`, `LastAttempt`
  or `InstalledAt` where 0.8 wrote a date in year 1, and rows gained
  `FrontmatterName`, `HookLoaded` and `LinkTarget`. Every other JSON output
  only gained keys.
- `--dry-run` on `prune` and `mute` exits 2: they show their plan and ask
  already.

### The menu

```bash
skillreaper
```

`reap menu` opens the same screen.

The number at the top is what you re-send in every session. It falls as you
choose, in tokens or in money. Rows are grouped by what reap suggests (prune,
mute, not sure, lock, already muted, already pruned), by plugin, or flat.

- `space` takes reap's suggestion for a row, a group or a selection; `p`, `m`,
  `l` and `d` choose prune, mute, lock or delete yourself; `a` fills in reap's
  suggestions for the row, group or selection and, pressed again, puts back
  what was there.
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
- A command counts as a use where the transcript records it being run, not
  where its tag only stands in pasted text, and a skill a plugin's SessionStart
  hook loads every session is no longer called unused.
- A skill named in your `CLAUDE.md`, an import or your rules is protected
  wherever reap runs from, not only in the directory that file lives in.
- A tool whose history cannot be read is no longer judged on other tools'
  sessions.
- Mute savings leave out the name, which stays loaded, and money figures follow
  each platform's own session count.

### reap reinvest (experimental)

With `--dry-run` you pick swaps and it prints what would run, changing
nothing; without it, it asks before creating and muting.

```bash
reap reinvest --dry-run
reap reinvest
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

It is not in the menu yet.

### Every fix, by area

The 275 fixes of this release, most of them found by reviewing it before
it went out. Each says what was wrong where that is not plain.

<details>
<summary>Open the list</summary>

#### Verdicts and numbers

- A YAML description block whose line is indented less than the rest keeps
  that line's text; its first letters, or half an accented one, were cut.
- A skill or agent of your own is dated by its file, so one written or edited
  in the last `--grace-days` is REVIEW(grace), not REAP: it had no date at all,
  and a skill made today was offered as safe to prune.
- The report's `DO` line for `reap prune` counts what a bare `reap prune`
  takes, not every unused item.
- Model ids with a release date (`claude-haiku-4-5-20251001`) get their
  model's price: they were left out of the blended `$/month`.
- MCP servers of one name in several projects are each listed; one of them,
  picked at random, used to stand for all.
- A skill named in a heading of your CLAUDE.md is protected like one named in
  its text. Lines starting with `#` were skipped as comments; they are
  headings, and a session loads them.
- A skill used under another letter case than its folder's is found in the
  record of past uses. It read as never used and could come out REAP.
- A project folder linked to another one in the same `projects` folder is
  counted once, and the measured token totals count only the window.
- A parse is counted from the start of the day the window opens on (UTC), so
  a cached parse and a fresh one agree at the window's edge.
- An MCP server whose name holds `__` is found in the record of past uses.
  Its calls were filed under the part before the first `__`, so once its
  transcripts aged out it came out REAP and a bulk prune took it.
- `reap share` counts nothing for an item put back by hand, and `reap restore
  001` means the item named 001 when the entry 001 is already back.
- `reap why`, `reap manifest` and `reap route` count an item against the
  sessions of its own tool, as its verdict does. `reap why` says when a
  skill's weight is what the last session's listing kept of its description.
- An MCP server whose name ends in a character tool names drop (`docs.`, a
  lone `é`) is seen as used: its calls were credited to another name, and it
  came out REAP. So is a server named with `__` that was only reached
  through the resource tools, once its transcripts are gone.
- A session counts in the window when one of its turns happened in it. The
  file's date counted it before: a folder restored from a backup, or copied
  to a new machine, made every old session a recent one with no use in it.
- A skill an agent names under `skills:` in its frontmatter is held at KEEP
  (`agent-loaded`). Claude Code loads it into the agent when the agent
  starts, no transcript records that, and it came out REAP.
- An MCP server used through its prompts (`/mcp__server__prompt`) is seen as
  used. The prompt was filed as a skill of that name and the server came
  out REAP.
- The measured token totals count a message once. Claude Code writes one
  line per content block, each with the message's whole usage, and every
  line was counted.
- CLAUDE.md protects a skill named in any letter case, and `CLAUDE.local.md`
  is read like `CLAUDE.md`.
- A server is not kept by the calls of another whose name begins with its
  own (`a` and `a__b`, `docs` and `docs.`): in the record of past uses a
  call goes to the longest configured name that begins it.
- More things at work with no use of their own on record are held, each with
  its reason: the agent `settings.json` runs the session as (`main-agent`),
  the agent a skill names under `agent:` (`skill-agent`), a skill one of your
  own SessionStart hooks reads (`hook-loaded`), and a skill in
  `~/.claude/skills` that OpenCode used from there (REVIEW,
  `used-elsewhere`). An agent's `skills:` list is read however YAML writes
  it, and names a skill by its frontmatter name too.
- A transcript that cannot be opened makes the evidence incomplete: nothing
  is REAP or MUTE on it, as the README says.
- A server whose name ends in a character tool names drop no longer shows a
  second, transcript-only row; a server's prompt counts with a note after
  its name.
- The headline's numbers add up when a muted skill has fired: it was taken
  off the never-fired, among which it never was, and "held back" came out
  one short for each.
- An agent that a plugin's own `settings.json` runs the session as is KEEP
  (`main-agent`), and `settings.local.json` has the last word on which
  agent that is.
- A SessionStart hook of yours is followed into the scripts beside the one
  it names, and through `$CLAUDE_CONFIG_DIR`; a hook path that is relative
  is the project's and is not read against the folder reap runs in.
- Three things no longer protect a skill or an agent that nothing uses: a
  README at a plugin's root that lists the plugin's skills, the word
  `agent:` inside a skill's description, and `skills: null` in an agent.
- An item whose name holds a character reap rewrites to print it (a
  direction mark, a control character) is found under the name its uses
  are filed under. It read as never used, and came out REAP.
- A hook's script is followed when punctuation is glued to its path
  (`start.sh;`, `start.sh&`), when a quote stands between the variable and
  the rest (`"$HOME"/.claude/hooks/x.sh`), and when the script beside it
  is a link to a file. The skill each of them loads was REAP.
- The money after `reap prune` and `reap mute` is priced with the sessions
  of each moved item's own tool. An item of a tool with no session on
  record claimed a monthly saving from Claude Code's.
- `--model` takes an id with a date or `[1m]` after it for the weight and
  for the price alike; `--mute-min-tokens 0` turns MUTE off, and the help
  says so; `agent:` and list items in a header take a comment after a tab.
- A quoted description that runs over several lines is weighed whole, as
  `reap mute` strips it whole. Weighed as its first line, a heavy skill
  came out a few tokens, never MUTE, and the saving claimed for muting it
  was a hundredth of the real one.
- A hook script whose path is in quotes is followed whatever the path
  holds (a space, a bracket, an `&`).
- `reap apm`, `reap manifest` and `reap route` find where a skill fired
  the way its uses are counted: whatever case the transcript wrote, and
  under the name in its header. `apm --diff` told you to drop a skill that
  had fired in the repo.
- `reap share` counts as dead what the report counts as dead: the weight
  of skills that fired and are only heavy was in its `dead context`.
- `reap gap` and the weekly reminder count as mutable what `reap mute`
  takes: a heavy, rarely used skill inside a plugin was promised as
  `recoverable with reap mute`, which then found nothing to mute.
- Only a `---` at the start of a line closes a skill's header, for the
  weight as for `reap mute`: an indented one inside a description is
  text. The header ended there, and the description weighed a fraction of
  what mute then took out.
- A comment after a value in a header (`description: "x" # note`) is not
  part of it, and `''` inside single quotes is one quote: both were
  weighed and shown as written. The body of a file with CRLF line ends
  is counted exactly.
- A hook path with a space written `\ ` is followed.
- The report's DO block and the plan of `reap prune` give a server's
  measured weight, and say `weight not measured` or `at least` where it
  was not: the DO block said `~0 tok/session reclaimed`, the plan `?` for
  every server. `reap why --json` marks such a weight (`weight_unknown`).
- Every reader of a skill's header ends it at the same line. A key written
  after an indented `---` in the description was missed by one of them, so
  a manual-only skill was weighed as if Claude loaded it. A value that is
  only a comment (`allowed-tools: # none yet`) is no value.
- A skill whose name is one plain word is protected by your CLAUDE.md only
  where the word is written as a name: `/review`, between backticks,
  asterisks, quotes or as a link's text, beside the word "skill", in a
  list item or a quoted line that begins with names (`- review: …`,
  `- review, deploy`, `> review`) or a heading of its own, or alone in a
  table cell. "always review the
  diff" protected a skill named `review` that nothing used; so did
  "docs/review" and "review skills". A word is one in any alphabet, and a
  longer word is not the name it begins with (`обзорный` for `обзор`). A
  name with a dash, a colon, an underscore or a digit counts wherever it
  stands, as before. `reap why` says when a name is in your CLAUDE.md as a
  plain word that protects nothing, and speaks of CLAUDE.md for skills
  only: a server of a name written there read "referenced" under REAP.
- A description that begins on the line after `description:` is weighed:
  it read as empty, a few tokens for 600 characters, while `reap mute`
  took all of it. `allowed-tools` written one per line is that many
  tools, not `all`.
- A skill in `~/.claude/skills` is held at REVIEW when another tool that
  loads that folder could not be read (OpenCode's history without the
  `sqlite3` CLI): it may be in use there, unseen. It came out REAP and went
  into the prune plan. The warning about that tool says so.
- A CLAUDE.md saved with a byte order mark is read from its first line: a
  list of skills that began there protected nothing. One long line that
  repeats a skill's name no longer takes seconds to read.
- "Monthly figures are extrapolated from N days of history" takes N from
  the tools the figures come from, not from one with nothing unused in it.
- With two tools, `reap --agent` and `--md` say that the dead-token figure
  is an average over all their sessions, as the report does: the rows under
  it did not add up to it. `reap gap --agent` has no row for a kind of item
  there is none of, and `reap route` tells apart two tools' skills of one
  name (`shared (codex)`, and `platform` in its JSON).
- A key of a nested map in a skill's header (`metadata:` then an indented
  `allowed-tools:`) is not the skill's own: it counted as its tools, or
  made it run only by hand.
- `reap why` on a skill that runs only by hand
  (`disable-model-invocation: true`) says it weighs `0 tok`, and why: it
  said `weight unknown` of a weight that is known.
- A history read only in part counts as not read: a database too large for
  one query, or one record over the limit, left sessions behind, and a skill
  used in the part that was lost came out REAP. A long list of names after
  `Skills:` protects its last name as it does its first.
- The sessions an item installed inside the window is judged on grow with
  the time it has existed. The scale ran the other way: a skill 29 days old
  needed one session to come out REAP, and one 31 days old ten.
- More ways for evidence to be missing are said, and hold the verdicts
  instead of passing for "never used": a project folder that is a link to a
  disk that is not there (it was skipped without a word); a record of past
  uses that cannot be read (it is set aside as `….unreadable`, never
  written over); a CLAUDE.md that is there and cannot be read; a tool that
  loads `~/.claude/skills` too and is not read because `--claude-dir` was
  given. A projects folder that is itself a link is read. A parse is cached
  only when it was whole and the record of past uses is there, so a verdict
  no longer depends on the cache. A second config folder's skills are not
  held on OpenCode's account: it loads `~/.claude/skills` only.
- `reap why` on a hook or a prose file has no use lines: they read as
  "never used". A tool's name in the payload table is printed without
  control characters.
- A skill is protected by the name in its header too, the one it is called
  by: CLAUDE.md naming `ship-it` did not protect `skills/dirfoo` whose
  header says `name: ship-it`. The memory files, the agents and the
  SessionStart hooks of every project your sessions ran in are read,
  wherever reap is run from: a skill named or loaded only in one project was
  kept from inside it and REAP from anywhere else.
- An item is dated by when reap first found it in the stack, where that is
  later than its own date. A server added to a config had no date and was
  REAP the same day; an item restored from quarantine, or unmuted, was
  judged at once on sessions it was not in; a plugin's server is as new as
  the plugin. What was there at reap's first look is not affected.
- A transcript read only in part is not marked as read in the record of past
  uses: it was never read again, and a use in it was lost when it left the
  window. Settings or an agent's file that cannot be read hold the verdicts
  of skills and agents; `reap prune` and `reap delete` stop on a mute list
  they cannot read, as `reap mute` does. A plan made on under half the
  window's days of history says so above it.
- For a project your sessions ran in, the CLAUDE.md of the folders above it
  is read too (a repository's, for a session in one of its packages), and a
  project's hook is followed when it names its script by
  `$CLAUDE_PROJECT_DIR` or by a path from the project's root. An unreadable
  agent or settings file in a project holds the verdicts as one in your own
  folder does. The dates reap writes down are real ones under a pinned
  clock. A pipe named like a settings or agent file no longer hangs a run.

#### Prune, mute, restore, delete and keep

- Quarantine paths are unique. Two items with the same name used to overwrite
  each other there, and `restore` could not bring both back.
- A name two items share is refused rather than guessed: select with
  `codex:skill:x` or a path.
- Named prune and delete reach a plugin's item or a link, and a named mute a
  plugin's item; a bulk prune or mute never does. A named mute of a skill
  reached through a link outside the folders reap edits is refused before
  any question.
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
- `reap mute` works on a SKILL.md saved with a byte-order mark, and keeps the
  mark.
- `reap mute` skips an item another prune moved meanwhile, as prune does,
  instead of failing on it.
- Pruning an agent whose file is named `SKILL.md` moves that file, not the
  whole agents folder.
- `reap mute` and `reap unmute` leave the file's date as it was. A personal
  skill is dated by its file, so one muted and unmuted read as installed
  today and sat in the grace period again.
- `reap keep` and `reap keep --remove` take the lock the other commands
  share: two run at once used to lose one of the two entries.
- `reap restore` takes an id typed without its zeros (`reap restore 1`), and
  its listing prints a server's name with no control characters.
- `reap unmute <path>` takes a relative path.
- `--claude-json` given without `--claude-dir` is the file read and edited.
  It was dropped without a word, and `reap prune <server>` edited the usual
  file.
- A server defined for every project and again for one project, in one file,
  is not guessed at by name: `reap prune` and `reap delete` took the first.
- `reap restore 7` brings back the item named 7 when there is one, never an
  entry already restored, and says what it restored (`restored 002 (name)`).
- `--claude-dir` on a folder with no `.claude.json` of its own takes no other
  profile's: the file next to the folder is read only for a folder named
  `.claude`. `reap prune --claude-dir ~/.claude-work` edited `~/.claude.json`.
- A name that begins like a selector (`codex:foo`, `agent:bar`) is found by
  prune, mute and delete; the way out of a kept item whose name begins with
  a dash is printed by its key (`reap keep --remove skill:-name`).
- A mute or unmute keeps the file's date with no gap: a run killed right
  after the write left today's date, and the skill read as newly installed.
- Pruning a server whose key is written twice in the config takes every
  spelling; the earlier one used to become the live definition.
- A typed name that is one item's whole name and, read as
  `platform:category:name`, another item, is not guessed: reap lists both
  forms, each quoted as one word. It took the other item, `--yes` included.
- The keep list is read again after the question is answered. An item kept
  from another terminal while `Prune…? [y/N]` was open is left alone by
  prune, mute and delete.
- `reap mute` waiting behind another run stops at once on Ctrl-C; `reap mute
  mcp:<name>` says the item is a server; the `reap unmute` command inside an
  error works for a name that begins with a dash.
- A delete that fails on the way says that part of the item is already gone.
- `reap keep` writes the key by the item's name on disk, and takes the name
  as the report shows it or ending in a space. For a name the report shows
  cleaned it said `excluded from prune` and protected nothing.
- The path of a config file is not the name of a server in it: typed where a
  name goes it took the first server of the file, `--yes` and `delete`
  included. The list of a shared name offers a quoted path for a skill or
  an agent and none for a server.
- `reap mute` leaves a file whose `---` header never closes as it is: it has
  no frontmatter, as the scanner reads it.
- `reap unmute` after one that was cut short finds the description back
  and clears the entry. It failed on the missing copy at every run, and
  `unmute --all` could not finish.
- `reap mute` takes all of a quoted description whose lines are not
  indented; the first line alone left a header that no longer parsed.
  A file that begins with a byte order mark and was edited while muted
  gets its description back.
- `reap restore` of a server into a config whose `mcpServers` is `null` or
  a list says so, where it said `not found`; `reap prune` and `reap mute`
  with nothing to do say when the report has warnings.
- `reap mute` reads a quote that closes on the description's own line as
  the end of the value: an apostrophe inside single quotes, or a comment
  holding a quote, made it say the file had no description, or take the
  next key along.
- `reap unmute` with its copy of the description gone says what to do
  (write one back, run it again) in place of the system's `no such file`.
- `reap delete`, from the command line and from the menu, leaves an item
  whose file is another one, or was written, since reap read it: a pull or
  a reinstall can put a different skill under the same name while the
  question waits, and a delete cannot be undone.
- A mute list that cannot be read is named, once, with what to do, before
  any plan or question; the report warns that it cannot tell what is
  muted. `reap mute` printed the JSON reader's error once per item.
- The check before a delete is on what the delete takes, the skill's
  folder, and not only on the file reap read in it: another folder of
  that name with no `SKILL.md` read as `gone`, and was deleted.
- `reap delete` says in its question that a skill's whole folder goes, and
  how many files are in it; a file put in that folder after reap read it
  stops the delete, as a changed `SKILL.md` does.
- `reap prune --quiet` and `reap mute --quiet` without `--yes`, with
  something to do, exit 3 and say `nothing done: --quiet needs --yes` on
  stderr. They exited 0 in silence, and a script took that for done.
- The undo printed after a mute names the file when another tool has an
  item of that name: `reap unmute <name>` brought back every copy. A named
  mute that would save no token (a description that is empty, or weighs no
  more than the name) is refused: it rewrote the file to save nothing.
- A skill that runs only by hand is not reap's to mute: nothing of it is
  in the context, and the mute took the text its slash menu shows. A named
  `reap mute` that cannot be done (already muted, no description, only by
  hand) says so before it asks, where the answer to a yes was an error.
- A bulk `reap prune` no longer says "pruned only by name" of a skill whose
  whole skills folder is a link: by name it is refused too. The note and
  `--json` say to remove it by hand, as `reap why` does.
- `reap restore` tells apart two projects' servers of one name, in its list
  and when it asks for an id. A bulk mute leaves out an item whose mute
  would save nothing, as a mute by name refuses it. The undo line of a mute
  never prints a control character from a path.
- A skill that is a link into the folder other tools read too
  (`~/.agents/skills`) is left out of a bulk mute, as a bulk prune leaves
  it; a mute by name says whose file it edits. `reap restore --all` and
  `reap unmute --all` with nothing to bring back write nothing: they left a
  `reaped/` folder in whatever folder they were pointed at.

#### What reap accepts and prints

- `--days` and `--grace-days` above 36500 are refused: past a century the date
  arithmetic wrapped and the window came out wrong. `--price` must be finite.
- A relative `--claude-dir` or `--claude-json` is made absolute first: mute
  refused it, and what prune and mute recorded named nothing from another
  folder.
- Under the `skillreaper` name, `help` keeps its columns.
- Restore, unmute and `keep --list` on a `--claude-dir` that is not there say
  so, instead of "Nothing to restore."
- `--claude-dir` alone finds the `.claude.json` inside or beside it: every MCP
  server used to drop out.
- `--source own` and `--source usr` work, as the report labels those sources;
  a mute with no match lists the sources there are.
- `--agent` says items were held back from REAP only when some were.
- `--mute-threshold` is described as what it counts: uses ÷ sessions.
- Paths reap prints (delete's confirmation among them) carry no control
  characters from a folder's name.
- `reap <flag> reinvest` says reinvest comes first, instead of calling it
  unknown.
- `reap --agent` and `reap gap --agent` say `n/a`, not `0%`, when the window
  holds no session, and the gap view says so in a line: nothing was read, so
  nothing is measured.
- `reap why` on an item held for a use before the window says so, and how to
  overrule it, instead of asking for more sessions that change nothing.
- A command reap prints to be pasted (`reap unmute`, `reap keep --remove`,
  the hints of `reap why`) carries a name or a path with a space as one
  quoted word.
- Paths printed by `reap unmute` and `reap restore`, in their listings and
  their refusals, carry no control characters from a folder's name.
- The refusal for a kept item gives a `reap keep --remove` that can be pasted
  for a name with a space.
- `reap snapshot --list --json` prints JSON.
- An integer flag written in another base (`--days 010`, `0x10`, `1_000`) is
  refused. `010` used to be read as octal: eight days, without a word.
- `reap why` counts the days left in the grace period to the date the verdict
  uses, and its rate line reads `uses/session 2%` instead of running the
  label into the figure.
- `reap gap` in a narrow window gives up the bar and the token columns
  before its category names, which were cut to `…` on every row.
- `CLAUDE_CONFIG_DIR` written with a trailing slash or as a relative path is
  the same folder: it used to open a second, empty record of past uses.
- A command printed to be pasted (`reap restore 001`, `reap unmute`, `reap
  keep --remove`) repeats `--claude-dir` when the run was given one. Ids
  start at 001 in every folder, so the pasted undo acted on another folder.
- `reap mute <server>` says mute is for skills and agents and points to
  `reap prune`, instead of saying to edit the config by hand.
- `reap <flags> reinvest` gives a command that keeps the reinvest flags that
  were typed; it used to drop them, so the pasted line was not a dry run.
- A warning about the plugin registry is printed once, not three times.
- With `--claude-dir` and no session in the window, the report says none was
  found, not that Claude Code keeps no transcripts.
- `reap why` counts the sessions still needed against the minimum the verdict
  uses for an item installed inside the window.
- A printed command for a name that starts with a dash puts it after `--`;
  the undo of `install-hook` and delete's restore hint repeat `--claude-dir`.
- Run as `skillreaper`, every hint says `skillreaper`: the weekly nudge said
  ``Run `reap` ``, a command an npm install does not have.
- A command inside an error (`reap restore 001`, `reap unmute <name>`,
  `reap prune <server>`) repeats `--claude-dir` when the run named a folder.
- A mistyped `--claude-json`, `--claude-dir` on a file and `snapshot --list`
  on a missing folder are errors, not an empty stack.
- A scan that could read nothing is said as a failed scan by `snapshot`,
  `share`, `gap` and `prune` too, and no snapshot of it is stored.
- `reap why` takes a whole name before a namespaced one, finds a name with a
  control character by what it is on disk, and for an item with a use on
  record no longer asks for more sessions that change nothing.
- `--claude-json` is checked by the commands that read it, not by `version`
  or `install-hook`; the answer to a misplaced `reinvest` is no longer under
  a page of usage; a name in the Markdown report cannot read as a tag or a
  link.
- `--claude-dir ""` and `--claude-json ""` are refused: read as not
  given, the command ran on the usual folder.
- A mute stopped because the item was kept meanwhile says so; the menu lets
  no C1 control character through; a backslash in a name no longer breaks a
  cell of the Markdown report.
- A failed scan is said by `route`, `by-project`, `apm` and `mute` too;
  `reap manifest ""` is refused; a prune that leaves an item alone because
  it was kept meanwhile names it.
- A `platform:category:name` that matches nothing is `not found`; it was
  answered with a line about another tool's item of that name. A name not
  found beside a report with warnings points at them: a file reap could
  not read holds items it cannot find.
- One day is `1 day` and one session `1 session` in every view; a few
  items fired among very many is `<1%` in every view, not `0%` in some;
  the muted line says how many of the muted never fired, so that the
  headline's figures close; the masthead puts what does not fit on a
  second line, where it was cut before the words `warnings below`.
- `reap why` has a sentence for `rare`; `reap apm` says its session count
  is of all projects; `--json` leaves out the date of an item never used
  or with no install date, where it wrote year 1.
- Run through `npx`, a printed hint names `npx skillreaper`: neither
  `reap` nor `skillreaper` is on the PATH there.
- After a prune of MCP servers alone the line says `weight not measured`,
  not `saving ~0 tok/session`; with a measured item among them it says
  `at least`.
- `reap prune --json` marks a weight that was never measured
  (`weight_unknown`, `unmeasured`), and the plan's table by source prints
  `?` for it, not `~0 tok`; `--price NaN` is told it is not a number.
- What the report says can be done about a MUTE row is what reap does:
  `reap why` no longer sends you to `reap mute` for a skill reached through
  a link outside the folders reap edits, which `reap mute` refuses; and the
  line of `reap gap` for skills a bulk `reap mute` leaves alone points at
  `reap why` instead of saying reap cannot mute them.
- A hook's Windows path keeps its backslashes; `reap keep --list` prints
  names without control characters; `reap route --md` escapes names; the
  warnings count `1 day`; the line on weight never measured names MCP
  servers alone, since hooks are not judged; `reap why` on a prose file
  gives the file's size, not a `description`.
- The line asking for a star after a report says what reap `can save you`:
  it said `saved you` before anything was pruned. After a prune it says
  what that prune saved.
- `reap why` does not send a skill to `reap prune` when its whole skills
  folder is a link, which prune refuses; `reap prune`, `reap delete` and a
  named `reap mute` refuse such a case before any note, plan or question.
- `reap --agent` names `reap prune` only when it would take something,
  and says that its dead-token figure is a floor when some unused items
  were never measured; the headline does not say `~0 removable` for items
  that are removable and unmeasured; with sessions of two tools the
  dead-token figure says it is an average over both; `reap gap` counts
  the MUTE rows a bulk mute leaves alone beside the ones it takes.
- A clock pinned by `SOURCE_DATE_EPOCH` is said when it cannot be right for
  this stack: a transcript is newer than it, or it lies in the future. A
  shell that exports the variable for its own reasons (nix) had every age
  and window measured against a date decades back, with no word of it.
- `reap prune --json` after an apply counts `unmeasured` among what was
  moved; `reap keep --remove` and `reap apm` print names without control
  characters; INSTALL.md says what to do with the Windows archive.
- `reap why` on a skill held because another tool's history could not be
  read says whose history is missing: it said reap had no record of its use,
  above `uses 2`. Its advice for a name that is a plain word in CLAUDE.md
  gives the full key (`reap keep skill:<name>`), and is not given for an
  item that is kept.
- One cause, one warning: a history read in part was said twice, and a
  folder whose only transcript was over the limit was also said to have no
  transcripts. `reap restore <name>` names each entry of a shared name with
  its project (`004 (…/.claude.json, project /work/a)`). `reap --agent` says
  `held back from a REAP or MUTE verdict`: a used skill can be held too.

#### The menu

- A key or a mouse report that arrives in two pieces on a slow link is read
  whole: its tail could read as a key, and the letter that ends an arrow
  marked the row to delete.
- Keys pressed while an apply is at work are dropped: a `q` pressed during a
  slow one left the menu before its failures could be read.
- On a narrow window the receipt's big figure gives way to a short one, as
  the header's does, so that the words beside it stay whole.
- A stray `ESC [` (Alt+[) ends at the next Esc or Enter, where it swallowed
  the arrow after it and let its last letter through as a key.
- A name made of accents alone is shown as `(no name)`.
- A choice that could not be carried out stays chosen, so that the same key
  applies it again once its cause is put right.
- An apply started from the screen after the last row shows what came of it,
  where it showed nothing, and that screen counts what was just applied.
- A delete of a row whose file the menu itself changed through another row
  is not refused.
- The header says `you saved` for what was applied and `would save` for what
  is chosen or could not be done, each with its own figure, and the question
  before an apply says `to save`.
- A sum under one unit of the currency is `<$1` in the short figure, where
  it was `$0`.
- A lock applied from the screen between lists is counted there.
- After the menu's own mute of a file, every row that is that file can still
  be deleted, unless somebody else changed it in the meantime: that change
  is still refused.
- The Linux console's F1 to F5 type nothing, where they chose or marked a
  row.
- In plain text a title is cut at the window only.
- Where the locale cannot draw a currency's symbol the amount carries the
  currency's code, where a sum in yuan read `JPY`.
- The green line and the question's words are said in fewer words before
  they are cut by a narrow window.
- An agent the menu muted can be deleted, through a link to it too.
- Where the locale cannot draw the big font (`LC_ALL=C`), figures and titles
  are plain text, where they were blocks of `#`.
- A name cut in its middle no longer leaves an accent on the `…`.
- Where the locale cannot draw a wide character, the columns after it stay
  in line.
- The screen with no rows says `q quit`.
- A menu apply that partly failed shows what went well first, then the
  failure's cause without the paths that pushed it off the screen.
- A joined emoji in a name shows as its emoji in the menu, not with
  replacement marks.
- In the menu, Option+arrow (Option as Meta) is no key, as other Alt keys
  are; its tail no longer arrived as `[` and `a`, which chose every
  suggestion. Esc pressed twice quickly is one Esc, not nothing.
- Ctrl-C pressed while the menu waits behind another run's lock stops the
  apply. It used to be lost, and everything went through once the lock came
  free.
- Two projects whose folders share a name are two groups in the menu, each
  named by its whole path; they used to share one name and open together.
- In a narrow window the menu's legend keeps `q quit`: it takes the place of
  the last keys shown instead of being the one cut.
- In the menu: a selection's footer says `space chooses` (it never only
  pruned); the question before a plugin is turned off reads `[y/n]`, since
  Enter does not answer it; a shared folder named on the receipt is cleaned
  of control characters; counts stand clear of the names before them in the
  narrowest window; the card at its smallest keeps the line that explains
  the choice; on a flat plan the saved line reads as a sentence.
- In the menu: Ctrl-C while another run holds the lock ends the wait at
  once; the other rows of a plugin already switched off are not reported as
  interrupted; a selection's footer counts what space will choose; two rows
  of one name from two marketplaces say which plugin each is; in the
  narrowest window the status and a group's name are cut short of the apply
  label and of the count, and the column title is not cut mid-word.
- In the menu: with nothing chosen the status has the whole row (a failure's
  reason was cut off); unlocking two rows that share a lock is no failure; a
  stray byte no longer eats the keys typed after it; a skill and an agent of
  one name say which is which; a narrow receipt keeps a gap before the
  amount; rows and groups of equal weight keep one order at every launch.
- A lock from the menu is written by the item's name on disk. For a name with
  a control character in it the menu said locked and nothing was protected.
- In the menu, a lock is every copy's lock (the keep list goes by category
  and name): the other copies show locked at once and the receipt says so.
  Two rows in quarantine under one name say where each was and its id; the
  card names the marketplace when two plugins share a name; a receipt line
  keeps the name beside an amount too wide for the window.
- In the menu: a lock on a name that ends in a space comes off again; a
  paste that pauses is still swallowed whole; a lock is applied before the
  rest, so a prune of another copy in the same apply is always the one
  refused; twins of one name and weight keep one order at every launch.
- In the menu, `q` on the screen of a platform with nothing left asks before
  it drops choices made on another platform.
- Where the locale cannot draw them, the marks inside a figure are plain
  too: a no-break space between thousands came out `1?344`. The green line
  never shows part of a figure (`you would save US$1` stood for
  US$1,200.00): it takes a short figure, and where that has no room either,
  it is left out. On the narrowest receipt what comes back heavier is said
  whole.
- The words beside the big figure are whole whenever the figure was judged
  to fit (the check was one cell short); a short money figure is rounded to
  the nearest unit, where 1.99 read `1`; the receipt asks for the size it
  needs, where a window of 9 rows was told it needed 29; its summary fits
  the narrowest receipt with long figures too.

#### Secrets in the report

- A credential after `Authorization: Basic` or `Token`, after `curl -u`, or in
  a `Cookie:` header is masked in the report, `--json` and `--md`. Only the
  scheme was masked before, and the credential was printed.
- A secret in a hook or server command is masked in more shapes: in quotes
  (`--token "…"`, `TOKEN="…"`, `{"api_key":"…"}`), after `=` when it
  begins with a dash, as the user part of a URL, after `--user` or glued to
  `-u`, with a space in it, and in a Digest `Authorization` header. A server's
  argument with a space in it stays one word, so all of it is masked.
- The masking also finds a secret's name behind another flag's `=`
  (`--env=TOKEN=…`), after what joins commands (`;`, `|`), in a connection
  string, and under more names (cookie, session, passphrase, `--pass`,
  `--pwd`, `--sig`, `--jwt`); the value is masked whole, whatever characters
  it holds. SECURITY.md says what the masking reads and what it cannot.
- The masking takes a password with no user in a URL (`redis://:…@host`), a
  value with an escaped space, text glued after a closing quote, and a
  connection string's value with a space in it.

#### Hook, weekly reminder, npm and plugin

- `reap uninstall-hook` says when there was no hook to remove, and
  `reap install-hook` repairs a hook that names a reap binary which moved,
  and says when there was nothing to change.
- `reap uninstall-hook` leaves a hook group that is someone else's alone, even
  an empty one; `"hooks": null` or `[]` in settings.json counts as no hooks.
- The weekly nudge says what grew, items to prune or to mute; `reap diff`
  counts the snapshots still missing; naming a muted item in `reap prune`
  says it is muted.
- After a cleanup the weekly nudge's baseline follows the counts down, so it
  speaks again when new unused items appear.
- `install-hook` repairs every stale hook of ours, not the last one.
- The plugin's `reap` skill no longer says a piped word answers the prune
  question: without a terminal prune and mute stop and ask for `--yes`.
- The weekly nudge stays quiet where it cannot write down that it spoke; it
  used to speak at the start of every session.
- `docs/tapes/README.md` and `hero-fixture.sh` give a rebuild command that
  sets `SOURCE_DATE_EPOCH`; CONTRIBUTING and `docs/decisions.md` say the
  plugin's version is set to the tag before a release; the plugin's skills
  pass a flag together with its value.
- `install-hook` on a settings file whose `hooks.SessionStart` is not a list
  says so and names the file.
- `install-hook` reads `"SessionStart": null` as an empty list, and a run
  stopped by SIGTERM exits 143, not Ctrl-C's 130.
- The plugin's skill prunes the one item that was named, not everything;
  INSTALL.md checks the downloaded archive against `checksums.txt`.
- Two first runs of the npm package at once both succeed: each downloads
  into a folder of its own and moves the binary into place whole.
- The npm package passes on an interrupt sent to it alone, clears a download
  folder a killed run left, names the URL that failed, and ships only what
  it runs.
- The npm wrapper gives up on a download only when it stops arriving, not
  after 30 seconds of a slow one.
- Installed with npm into a folder you cannot write (`sudo npm i -g`), the
  binary goes to `~/.cache/skillreaper/<version>/` instead of failing.
- The weekly reminder counts what `reap prune` would take: unused items
  inside a plugin were counted as `to prune`.
- `install-hook --dry-run` on a `settings.json` that is a link leading
  nowhere says so, as the real run does; the counts that still stood in a
  fixed plural (`1 items never fired`, `1 uses`, `1 days ago`) agree in
  number; under `npx` the agent view's `run reap for details` names
  `npx skillreaper` too.
- The first weekly reminder does not say "more than at the last check",
  and a later one names only what rose.

#### reap reinvest

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
- A skill with characters that draw nothing (zero-width space, word joiner,
  soft hyphen, variation selectors) is refused: the text shown before
  `apply?` left them out and the file was written with them. Joiners and the
  emoji selector stay.
- Slack app and rotating tokens and a PGP private key block are removed from
  what is sent, with the other keys.
- Keys typed while the run works are dropped when the picker opens: an
  impatient Enter accepted the first swap.
- `reap --days 5 reinvest --max 2` is answered with the command to type, and
  the step lines agree in number (`1 kind of work`, `1 skill written`).
- A `y` typed before `apply?` is on screen is dropped: it answered for a
  skill whose text had not been shown yet.
- A mute cut off for taking too long is undone like the ones before it; it
  could stay muted under a line saying nothing had changed.
- The `reap unmute` command printed after a swap carries
  `CLAUDE_CONFIG_DIR` when the run was pointed at a folder through it.
- With `--online --mcp`, a package has to name the server's repository
  itself, not one whose name only begins the same way.
- The undo command printed after a swap carries no control character from a
  skill's path, and a mute that was killed is undone like one cut off for
  time.
- With `--online`, the command that installs what was found is on screen
  before `apply?`. For an MCP server the question came with its name alone,
  and the package that would run at every launch was never shown.
- With `--online`, only a few short words go out as a search, an npm package
  is taken only at a whole version, and a page asked for over https is not
  read from http.
- A candidate is weighed, and its description read, by reap's own reader of
  the header, so the two agree on every quirk (a byte-order mark, an unclosed
  header, a quoted or nested `disable-model-invocation`).
- With `--online`, each kind of work gets its own searches. The limit was
  on all of them together, in the order of the alphabet, and with several
  kinds most were never searched.
- The command `--dry-run` prints is quoted word by word, so pasting it
  runs that command and nothing else.
- A step that got no answer says what `claude -p` said and where the log
  is: `got no answer` alone hid a login that had run out.
- A written skill whose link was deleted is linked again when offered with
  the same text. It was shown in full, accepted and then refused, at every
  run.
- The mute line of `--dry-run` names the item's path and the config folder
  the run was pointed at; with no terminal to choose on, the list of swaps
  ends by saying that nothing was changed.
- The reason on a failed step is that step's own: one left by a call that
  failed and then answered is not shown, and what `claude -p` said on
  stdout is read when stderr is empty.
- A swap that fails after linking again a folder that was already in
  `skills-reinvest` takes the link away and leaves the folder: it removed
  the folder, with whatever else was in it, under `nothing changed`.
- With `--online --mcp`, a server installed with nothing to mute is no
  longer listed as a swap still to make, and a registry name is printed
  without the control characters it may carry.
- The reason on a failed step survives a call beside it that answered,
  does not outlive its step, is shown without the keys it may hold, and
  `run.log` is the person's alone to read. A call placing new sessions
  that got no answer says so.
- Stopped with Ctrl-C after the new skill is in place, reinvest prints the
  `reap mute` commands that finish the swap by hand.
- A folder left in `skills-reinvest` is linked again only when it holds
  that skill and nothing else; a server that could not be noted down is
  not promised a later mute.
- Candidates that fit but have nothing unused to trade for them are said
  to be that, not `nothing verified`.
- A grouping answer that places no session is no answer: saved as one, it
  gave `0 kinds of work` for a week with no call made. A key in what
  `claude -p` said is masked before the line is cut, not after.
- A folder left in `skills-reinvest` may hold the `.DS_Store` Finder
  leaves, and any other file in it is named; the text after a stop tells
  two skills of one name apart, and gives no command to finish a swap that
  was being undone.
- The mute in hand when a stop comes is one skill, by its path: with two
  skills of one name in a swap, the one not reached was named nowhere.
- An answer that places no new session is no answer, and says why: saved
  as one, the sessions were marked as placed. A search answer that is an
  error is asked for again, not kept for a week.
- A `reap --json` answer that is not this version's report stops the run:
  read as an empty stack, the sessions were sent to be grouped and a new
  skill was checked against no installed one. An answer about none of the
  kinds of work asked about, `assign` as text, or `skills` that is no list,
  is no answer either; a rejection with no reason says `no reason given`.
- With `--online`, a search or a download that could not be made is said,
  with the reason: a 403 from skills.sh read as `0 candidates`. A projects
  folder that cannot be read is said, where it read as `0 sessions`.
- Ctrl-C typed in the pager no longer ends the picker when the pager
  closes. A candidate kept with no reason given says so, where its line
  was blank.
- No file of reinvest's own is written through a link, and the picker cuts
  wide characters at the window. A mute that gave no answer in time is
  still named when a stop comes while it is undone.
- `run.log` is not written through a link (the file it led to was emptied
  and made private); a refusal names a few of a folder's files and counts
  the rest; a `.DS_Store` that is a folder or a link is not Finder's.
- It has run on macOS. On Linux it builds and has not been run.
- A kind of work or a skills.sh result with no name goes by its first words
  or by its slug: the screen and the report had a blank there. With `--mcp`,
  a README or a package's metadata that could not be fetched is counted with
  the searches that failed; the reason printed is cut short, and says when
  the answer was a web page. A request to end (SIGTERM) that came while the
  pager was open is obeyed. A folder of sessions that cannot be read is
  said: the count was merely low.
- A transcript that cannot be opened is said, like a folder that cannot be:
  the count of sessions was low without a word. Two kinds of work that
  would go by one name are told apart: the second was written, verified,
  and never offered. The prototype's README says it is not maintained, and
  how it differs from `reap reinvest`.
- The fenced form of a command that runs when a skill loads (three
  backticks and `!`) is refused like the inline one. The name in a skill's
  header counts as taken, not only its folder's. A key pasted in a skill's
  description is removed before the description goes to Claude. Before the
  question the screen says where the skill is written and which files are
  muted; with `--mcp` the first line names npm, PyPI and GitHub too; a stop
  after the skill was created says how to take it out again.
- A skill is offered for a mute only when `reap mute` takes it plainly: not
  one linked into the folder other tools read, not one in a skills folder
  that is a link (the swap failed, and came back at every run). A written
  skill whose name an installed one has says so, and is not written again
  at every run. A swap rolled back leaves no empty folder.

</details>

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
