# Launch Posts

> Drafts from the first launch, kept for their wording. Their figures are
> the June 2026 measurement of the author's own stack (187 items scanned,
> 142 never used = 76%, 45 left, ~8,000 tokens a session). The README now
> carries the 2026-08-23 one (382 loaded, 13 fired, ~19,755 dead tokens a
> session): post with the newest measured numbers. reap does not measure
> latency or wrong tool picks (README, roadmap #55), so the lines that
> promise those need a source or have to go.

## Hacker News

**Title**: Show HN: Reap – Find and prune unused AI agent skills from your transcripts

**Body**:

Every Claude Code session loads every skill description into context. After
a few months of installing plugins and trying tools, you end up with 150-300
items your agent reads but never uses.

I built `reap` - a CLI tool that scans your session transcripts, finds what's
actually used, and quarantines the rest:

```
brew install thousandflowers/tap/skillreaper
reap
reap prune
```

The output groups items by verdict (REAP -> REVIEW -> KEEP) with token
weights, dead token costs, and monthly spend - all from your own transcript data.

`reap prune` deletes nothing. `reap restore --all` undoes everything.

100% local, zero deps, single binary. Measures Claude Code, Codex CLI and
OpenCode; inventories Cursor, Gemini CLI, Hermes and OpenClaw. Written in Go.

https://github.com/thousandflowers/skillreaper

---

## X Thread (5 posts)

**Post 1** (with GIF from README):

Your AI agent reads 187 skill descriptions every session.

You use 45.

The other 142 are dead weight - consuming context, increasing latency,
and causing wrong tool picks.

I built `reap` to find and quarantine them.

[attach reap-demo.gif]

---

**Post 2**:

How it works:

1. `reap` - scans your transcripts, groups skills by verdict (REAP/REVIEW/KEEP)
2. `reap prune` - moves unused items to a quarantine folder
3. `reap restore --all` - undos everything

100% local. Zero config. `reap prune` deletes nothing.

---

**Post 3**:

The before/after:

Before: 187 items loaded, 76% never used, 8000 tok/session wasted
After: 45 actively used items, full context budget for real work

Your agent stops scrolling through irrelevant tools. Wrong picks drop.
First-token latency improves.

---

**Post 4**:

Measures Claude Code, Codex CLI, OpenCode. Inventories Cursor, Gemini CLI, Hermes, OpenClaw.
Homebrew, npm, Go install - take your pick.

```
brew install thousandflowers/tap/skillreaper
npx skillreaper
go install github.com/thousandflowers/skillreaper/cmd/reap@latest
```

---

**Post 5**:

MIT, single static binary, zero dependencies.
Written in Go.

github.com/thousandflowers/skillreaper

Try it and let me know what you think

---

## Reddit (r/claudeai, r/ClaudeCode)

**Title**: I made a CLI tool that finds and prunes unused AI skills from your agent's context

**Body**:

If you use Claude Code (or any agent IDE), you've probably accumulated
dozens of skills, MCP servers, and agent definitions over time. Every
session, your agent loads ALL of them into context.

Most are never used.

I wrote `reap` - it scans your session transcripts, identifies what's
actually getting used, and quarantines the rest. The output shows you:

- Which items are dead weight (REAP)
- How many tokens they waste per session
- What that costs you monthly

Then `reap prune` moves them to a quarantine folder. Reversible -
`reap restore --all` puts everything back.

100% local, zero dependencies, open source (MIT).
https://github.com/thousandflowers/skillreaper
