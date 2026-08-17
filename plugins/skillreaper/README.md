# skillreaper plugin

Two skills over the `reap` CLI:

- `/skillreaper:reap` — what is loaded and never used, and what it costs
- `/skillreaper:gap` — how much of each category is ever actually touched

```
/plugin marketplace add thousandflowers/skillreaper
/plugin install skillreaper@skillreaper
```

## Design notes

**Ships no binary.** `reap` is a Go program already distributed through
Homebrew, npm/npx and `go install`. Bundling it would mean committing six
GOOS/GOARCH builds; downloading it here would duplicate the checksum-verified
fetch that `npm/lib/release.js` already performs. The skills assume `reap` is on
`PATH` and print a runnable install block when it is not — `npx skillreaper`
first, since that needs no install at all.

**Ships no hooks.** `reap install-hook` writes a `SessionStart` nudge into the
user's own `settings.json`, keyed by the marker comment
`skillreaper-weekly-nudge`. A `hooks/hooks.json` here would be a second,
independent copy: anyone who already ran `install-hook` would be nudged twice,
and `reap uninstall-hook` can only remove the `settings.json` copy, never the
plugin's.

> **TODO** — if hooks are ever wanted in this plugin, the plugin must *detect*
> the `skillreaper-weekly-nudge` marker and supersede the manual hook, rather
> than coexist with it.

**Skills consume `reap --json`, never the plain report.** The human report is a
30-line ANSI table; injecting it into the context only to have the model repeat
it is exactly the dead weight this tool exists to find. Each `SKILL.md` carries
an explicit render spec so every invocation looks the same.
