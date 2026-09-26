# reap reinvest — prototype

Python prototype of a possible `reap reinvest` step: after `reap prune` frees context, look at the
user's own Claude Code sessions, find the kinds of work that recur, and propose at most 3 skills from
skills.sh that would do that work — or nothing, which is the common and correct answer.

Not part of the Go build. Kept here to decide whether and how to port it.

## Run

```bash
python -m venv .venv && .venv/bin/pip install sentence-transformers scikit-learn
.venv/bin/python reinvest_prova.py                  # report in prove_laya/reinvest.md (~30-110 s)
.venv/bin/python reinvest_prova.py --rifai-gruppi   # regroup sessions instead of reusing the 1-day cache
```

Needs the `claude` CLI. Reads `~/.claude` only; installs nothing. `prove_laya/` holds excerpts of the
user's prompts and is gitignored: never commit it.

`sentence-transformers` / `scikit-learn` are only needed for `--catalogo tutto`; the default path uses none.
`laya_prove_mac.py` is the shared library (session reading, skills.sh fetching); its Laya experiments
are not used by `reinvest_prova.py` — Laya lost to simpler alternatives in every earlier test.

## Pipeline (default)

1. **Sessions** — only ones a person wrote: drop `entrypoint: sdk-*` (`claude -p`), subagent
   transcripts, sessions opened in this folder, and the session still in progress.
2. **Kinds of work** — one `claude -p` call groups all sessions by kind of work (not by project) and
   returns session ids, a general task sentence and 5 search queries per group. Cached for a day.
   Groups under 3 sessions, or mostly covered by a skill the user already invokes, are skipped.
3. **Candidates** — skills.sh search with those queries, ranked by position in the results of that
   task's queries. On the first 429 the run switches to GitHub raw only.
4. **Filters** — already installed, too few installs, risky patterns in SKILL.md.
5. **Verification** — every (task, candidate) pair is checked alone by Claude with the full SKILL.md
   and the names of all installed skills. Rejects: written for another project or agent, needs a
   paid service or advertises one, wrong platform, generic advice, narrow special case, duplicate of
   an installed skill. One pick per task, max 3.

## What the experiments showed

20 runs on one real user's data (73 sessions, 327 installed skills, 7 ever used).

- **Embeddings cannot find kinds of work.** Clustering prompts (thresholds 0.55–0.75, with and
  without mean-centering) groups by project or by phrasing. One Claude call groups by kind of work
  correctly and gives exact session counts.
- **Embeddings cannot find duplicates either.** A 0.85 similarity threshold caught 0 of 10 duplicate
  pairs a Claude judge found.
- **Instability comes from grouping, not selection.** Fresh groups every run: 0–18% overlap of
  proposals across runs. Fixed groups: 100%. Hence the group cache.
- **Per-candidate verification beats one batched choice.** Faster (~40 s vs ~100 s), stricter, and
  its reasons are right; the batched choice let through a skill tuned to another company's repo, a
  Codex-only skill and a paid-service ad.
- **Most weak proposals duplicate installed-but-unused skills** (feature-dev, code-reviewer,
  webapp-testing…). Proposing the twin of a skill that prune just removed is pointless, so the
  verifier gets all installed names.
- **GitHub catalogs + embeddings add nothing** over skills.sh search alone: same final proposal.
  The Go port needs no ML.
- **More candidates don't help once ranking is right.** For most of this user's recurring work
  there is no good skill in the catalog; "no proposal" is usually correct.
- **`claude -p` cost:** the user's hooks/plugins/MCP/CLAUDE.md add ~25 s per call and leak persona
  text into answers; `--setting-sources "" --strict-mcp-config --tools "" --disable-slash-commands
  --no-session-persistence` plus a minimal system prompt fixes both. Extended thinking off for
  grouping (235 s → ~18 s, same quality), on for verification.
- **skills.sh search is wording-sensitive** ("file organization tool" → expo-router, "file
  organizer" → file-organizer), so queries are asked for as skill-name-style phrases.

## Open questions before porting

- How long to cache groups (a day, a week, until N new sessions).
- Where it lives: its own `reap reinvest` command or a question at the end of `reap prune`.
- What it shows: only proposals, or also the kinds-of-work table (the most reliable output so far).
- When several candidates pass verification for a task, pick the best one, not the first.
- Does `reap prune` count `claude -p` sessions as usage? Worth checking separately.
