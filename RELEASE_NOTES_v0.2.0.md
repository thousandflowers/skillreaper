## v0.2.0

### MUTE verdict — a third state between KEEP and REAP

Skills that are rarely used but carry heavy token weight now get a `MUTE`
verdict. `reap mute <name>` strips the description from `SKILL.md`, keeping
the skill available but removing its context cost. Original description is
backed up to `~/.claude/reaped/muted/<name>.md.bak`.

`reap unmute <name>` and `reap unmute --all` restore from backup.

### CLAUDE.md protection

Skills referenced in `CLAUDE.md` are automatically protected with
`KEEP(claude-md-ref)`, regardless of usage data. Scans `./CLAUDE.md`,
`~/CLAUDE.md`, and `./.claude/CLAUDE.md`.

### Weekly nudge — passive SessionStart hook

`reap install-hook` installs a hook that checks once a week whether your
REAP or MUTE count has grown. One line to stderr if it has, nothing otherwise.
`reap uninstall-hook` removes only the skillreaper entry — other hooks intact.

### Broken-vs-cold distinction

A skill that was invoked but errored now shows as `REAP(broken)` in bright
red, separate from `REAP(unused)`. Broken skills get their own section in
`reap gap` with error counts.

### Credits

Ideas for MUTE, CLAUDE.md protection, the weekly nudge, and broken-vs-cold
were inspired by [groundskeeper](https://github.com/zvoque/groundskeeper),
[optimize](https://github.com/codeprakhar25/optimize), and discussion in
r/claudeskills. Full credits in README.
