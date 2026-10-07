# reap reinvest — prototype

`reap` finds the skills you never use. `reinvest` looks at the work you actually do, finds the kinds of work
no skill covers, and proposes **swaps**: mute some never-used skills and create a skill for that work, written
from your own sessions, for about the same tokens per session. With `--online` it searches skills.sh and the
MCP registry for one instead. You pick the swaps with the arrow keys.

Python, standard library only. Not part of the Go build: kept here to decide whether and how to port it.

## Run

```bash
python3 reinvest.py --dry-run   # pick swaps, print what would run; installs and mutes nothing
python3 reinvest.py             # same, then asks before installing and muting
python3 reinvest.py --regroup   # group every session from scratch now, instead of weekly
python3 reinvest.py --online    # search skills.sh and the MCP registry instead of writing skills
python3 token_cost.py           # tokens per request per kind of work, with vs without each skill you used
python3 -m unittest test_reinvest   # offline checks, no network, no Claude calls
```

Needs `reap` and `claude` on PATH. Takes about 1–2 minutes (Claude does the grouping and the verification).
A dry run still asks Claude through `claude -p`, which updates Claude Code's own `~/.claude.json` as any session does (no transcript is kept: `--no-session-persistence`).

Cache, report and log live in `skillreaper/reinvest/` under the user cache directory reap uses (`~/Library/Caches` on macOS, `~/.cache` on Linux); nothing is written in the repo. The last proposals go to `swaps.json` there, and reap's text report points at them in one line; applying stays in reinvest's picker.

## Picker

```
✓  mute     ecc:deployment-patterns   ~51 tok/session · never used
   install  ◂ web-design-reviewer ▸   ~95 tok/session · skill   2/3
            for Portfolio Website Editing (9 sessions)
```

↑↓ next swap · ←→ other candidates for this swap · enter accept/undo · p read the whole SKILL.md/README in
`$PAGER` · m mute nothing / mute again · q done. A written skill shows as `create … ✎ new · written from your
sessions` and is offered even when no unused skill weighs about the same.

A written skill is created in `~/.claude/skills-reinvest/<name>/` and linked as `~/.claude/skills/<name>`, so
Claude Code loads it at once and reap weighs it like any other. A skill or link of that name already there is
never replaced. When it goes unused, `reap prune` deletes it, folder included (`reap restore` brings it back).

Applying installs the candidate (`npx skills add … -a claude-code` / `claude mcp add -s user`), then runs
`reap mute` on what it replaces — reversible with `reap unmute`, and works for plugin skills too. Each swap is
all or nothing: a failed install mutes nothing, and a failed mute unmutes what that swap muted and uninstalls
the new one. Tested for real (skill and MCP, success and rollback) with `HOME` pointed at a scratch folder.

## Pipeline

| file | does |
|---|---|
| `sessions.py` | reads the sessions you wrote: drops `claude -p`/SDK runs, subagents, this folder, the open session |
| `llm.py` | the Claude calls: group sessions by kind of work, check coverage, verify a candidate, write a skill |
| `sources.py` | skills.sh search + SKILL.md, official MCP registry + README, risky-pattern check |
| `swaps.py` | pairs each fit with REAP skills of similar weight, the picker, applying |
| `reinvest.py` | runs it all; weights and verdicts come from `reap --json` |

1. **Kinds of work** — one Claude call groups all sessions by kind of work, not by project. That full grouping
   runs weekly (or with `--regroup`); in between, one small call places only the new sessions into the groups
   that exist, or into new ones, so proposals stay put and new work still counts. A session placed nowhere is
   not sent again; a call that never answered is retried next run.
2. **Gaps** — kinds seen in ≥3 sessions, delegable, and covered by no skill: neither one you use in most of
   those sessions nor an installed one (a second, small call judges the 12 closest installed skills by what
   they do). Installed-but-unused coverage is out of scope here: that is a future `reap reuse`.
3. **Candidates** — by default one skill per gap, written by Claude from that gap's longest sessions (up to 8):
   the steps you repeat and the rules you had to repeat. reinvest builds the frontmatter itself, so it always
   parses. It must hold for every project of that kind; cached by the gap, its sessions and the prompt.
   With `--online`: skills.sh results ranked by position in that gap's searches (≥20 installs), plus MCP
   servers from the official registry (single-word searches). SKILL.md / README fetched only for these.
4. **Verification** — every (gap, candidate) read in full by Claude, alone, with the skills you use and the
   installed skills closest to it. Rejects: written for another project or agent, paid service or ad, wrong
   platform, generic advice, wraps standard CLI tools, narrow special case, duplicate of what you have, and
   MCP servers that run arbitrary commands or scripts (also caught before Claude by tool-name patterns).
   Each candidate is verified twice and kept only if both runs keep it; verdicts are cached for a week, keyed
   by everything the verifier read, so the same inputs give the same answer. A written skill is told it is
   yours (your tools and habits are fine) but is rejected when a step holds for only one of your projects.
5. **Swaps** — the best fit per gap (the verifier's 1–5 fit, then the lighter), max 3, paired with 1–3 REAP
   skills of about the same weight. Skill weight uses reap's own formula (matches reap on all 301 installed
   skills). An MCP server's weight is unknown until it runs, so an MCP swap installs and mutes nothing; the
   server is remembered (`weigh_later.json`), and once reap has weighed it from the transcripts (its tool names
   and instructions, as Claude Code injected them) the next run offers the mute as an `installed` swap.
   A server no longer installed is forgotten. Needs reap with MCP weights (branch `feat/mcp-weight`).

## What the experiments showed

Measured on one real install (~70 human sessions, 327 skills, 7 ever used), over ~30 runs.

- **Embeddings cannot find kinds of work or duplicates.** Clustering grouped by project or phrasing at every
  threshold; a 0.85 similarity caught 0 of 10 duplicates a Claude judge found. One Claude call groups correctly.
- **Instability comes from grouping.** Fresh groups per run: 0–18% overlap of proposals. Cached: 100%.
- **Per-candidate verification beats one batched choice**: faster, stricter, right reasons.
- **Most weak proposals duplicated installed-but-unused skills**, and name-only checks misfire
  (`agent-sort` sorts plugin components, not files): the verifier reads what the closest installed skills do.
- **`claude -p` cost**: user hooks/plugins/MCP/CLAUDE.md add ~25 s per call and leak persona text;
  the flags in `llm.LEAN` remove both. Extended thinking off for grouping (235 s → ~20 s), on for verification.
- **skills.sh search is wording-sensitive** ("file organization tool" → expo-router, "file organizer" →
  file-organizer): queries are asked for as skill-name-style phrases. Downloading every result's description
  cost ~80 s and 429s; only candidates are fetched now.
- **Tokens with vs without a skill** (`token_cost.py`) are only comparable with a few sessions on each side:
  releases with `ship-release` used ~30% fewer tokens per request (5 vs 6 sessions); elsewhere too few.
  Savings for a skill never run cannot be read from history.
- **Borderline candidates flipped between runs**; now fixed by two verifications per candidate plus a verdict
  cache.
- **Session counts moved (68 → 61)** because Claude Code appends untimestamped lines (`atis-latch`, `pr-link`)
  to old transcripts in batches (6 files in one minute), so mtime put old sessions inside the "still open, last
  10 minutes" window. Sessions are now windowed by their last timestamped entry.
- **`skills add` without `-a`** installs for ~20 agents (`~/.agents/skills` + a symlink for Claude Code) and
  exits 0 even when one of them fails; a repo that no longer exists makes it clone forever, even with
  `GIT_TERMINAL_PROMPT=0`.

- **Multi-line descriptions** (`description: >`, `|`, or a line continued below) were weighed by their first
  line only, by reap and so by reinvest: 29 installed skills at 3-8 tokens instead of 140-230. Both now read
  the description whole (reap branch `feat/mcp-weight`); reinvest matches reap on all 301 installed skills.

## Open questions before porting

- `reap reuse`: kinds of work covered by an installed skill you never invoke.
