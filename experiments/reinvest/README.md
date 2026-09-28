# reap reinvest — prototype

`reap` finds the skills you never use. `reinvest` looks at the work you actually do, finds the kinds of work
no skill covers, and proposes **swaps**: mute some never-used skills, install one skill or MCP server that does
that work, for about the same tokens per session. You pick the swaps with the arrow keys.

Python, standard library only. Not part of the Go build: kept here to decide whether and how to port it.

## Run

```bash
python3 reinvest.py --dry-run   # pick swaps, print what would run, change nothing
python3 reinvest.py             # same, then asks before installing and muting
python3 reinvest.py --regroup   # regroup sessions instead of reusing today's groups
python3 token_cost.py           # tokens per request per kind of work, with vs without each skill you used
```

Needs `reap` and `claude` on PATH. Takes about 1–2 minutes (Claude does the grouping and the verification).
Cache, report and log live in `~/.cache/skillreaper/reinvest/`; nothing is written in the repo.

## Picker

```
✓  mute     ecc:deployment-patterns   ~51 tok/session · never used
   install  ◂ web-design-reviewer ▸   ~95 tok/session · skill   2/3
            for Portfolio Website Editing (9 sessions)
```

↑↓ next swap · ←→ other candidates for this swap · enter accept/undo · q done.
Applying installs the candidate (`npx skills add` / `claude mcp add`), then runs `reap mute` on what it
replaces — reversible with `reap unmute`, and works for plugin skills too. A failed install mutes nothing.

## Pipeline

| file | does |
|---|---|
| `sessions.py` | reads the sessions you wrote: drops `claude -p`/SDK runs, subagents, this folder, the open session |
| `llm.py` | the three Claude calls: group sessions by kind of work, check coverage, verify a candidate |
| `sources.py` | skills.sh search + SKILL.md, official MCP registry + README, risky-pattern check |
| `swaps.py` | pairs each fit with REAP skills of similar weight, the picker, applying |
| `reinvest.py` | runs it all; weights and verdicts come from `reap --json` |

1. **Kinds of work** — one Claude call groups all sessions by kind of work, not by project. Groups are cached
   for a day by session id.
2. **Gaps** — kinds seen in ≥3 sessions, delegable, and covered by no skill: neither one you use in most of
   those sessions nor an installed one (a second, small call judges the 12 closest installed skills by what
   they do). Installed-but-unused coverage is out of scope here: that is a future `reap reuse`.
3. **Candidates** — skills.sh results ranked by position in that gap's searches (≥20 installs), plus MCP
   servers from the official registry (single-word searches). SKILL.md / README fetched only for these.
4. **Verification** — every (gap, candidate) read in full by Claude, alone, with the skills you use and the
   installed skills closest to it. Rejects: written for another project or agent, paid service or ad, wrong
   platform, generic advice, wraps standard CLI tools, narrow special case, duplicate of what you have.
5. **Swaps** — the first fit per gap (max 3), paired with 1–3 REAP skills of about the same weight.
   MCP weight is an estimate (≈200 tokens per tool).

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
- **Remaining instability**: when two candidates for one gap are both borderline, verification keeps one or
  the other across runs.

## Known issues (next session starts here)

1. **Real apply never run.** `npx skills add … -g -y`, `claude mcp add` and `reap mute` were only exercised
   in `--dry-run`. Check which agents `skills add` installs to without `-a`, and that a failure mid-swap
   leaves nothing half-done.
2. **MCP safety is weak.** `risks()` scans the README text only; a server like `mcp-macos` (runs arbitrary
   AppleScript/commands) passed verification. MCP servers need their own checks (what the tools can do,
   remote vs local, required env vars).
3. **MCP weight is a guess** (~200 tokens per tool from the README). A 2800-token server paired with 706 muted
   tokens shows a large net cost; measure real tool-schema size or say "unknown".
4. **Borderline candidates flip between runs** (verification is not deterministic). Option: keep a candidate
   only if it passes two verifications (≈2× verify time).
5. **First fit per gap, not the best one.** When several candidates pass for one gap, the first in search
   order becomes the default; the others are only reachable with ←/→.
6. **Candidate weight is chars/4**, reap uses its own estimate; small mismatches in the net figure.
7. **Session count once read 68, then 61 minutes later**, not reproduced; worth a look at `read_transcripts`.
8. **skills.sh 429s** still cut the SKILL.md fallback for the rest of a run.

## Open questions before porting

- An appendix in the plain `reap` report showing the last cached swaps (fast), vs only this command (slow).
- How long to cache groups.
- `reap reuse`: kinds of work covered by an installed skill you never invoke.
