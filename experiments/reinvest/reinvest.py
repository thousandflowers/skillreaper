#!/usr/bin/env python3
"""reap reinvest (prototype): trade skills you never use for ones that do work you do and have nothing for.

  1. sessions   the ones you wrote (no `claude -p`, no subagents, not the open one)
  2. kinds      Claude groups them by kind of work; kinds a skill already covers drop out: what is left are gaps
  3. write      for each gap Claude writes a skill from your own sessions: the steps you repeat, the rules you
                had to repeat (with --online: search skills.sh instead, with --mcp the official MCP registry too)
  4. verify     every (gap, candidate) read in full by Claude, alone; most are rejected, with a reason
  5. swaps      each fit is paired with REAP skills of about the same weight; pick them with the arrow keys,
                then reinvest installs the new one and runs `reap mute` on the old ones (reversible)

Weights and verdicts come from `reap --json`, so the numbers match the reap report.
Usage:  python3 reinvest.py [--dry-run] [--regroup] [--online [--mcp]] [--days N]
"""
from __future__ import annotations

import argparse
import hashlib
import json
import shutil
import statistics
import subprocess
import sys
import time
from collections import Counter
from concurrent.futures import ThreadPoolExecutor

import llm
import sources
import swaps
from sessions import canon, cut, read_sessions, week

REGROUP_EVERY = 7 * 86400  # a full regroup changes the groups (0-18% overlap run to run), so it runs weekly;
                           # in between, only new sessions are placed into the groups that exist
MEMORY_SESSIONS = 1000  # about 1 MB at most: what the file keeps of each session is capped below
MEMORY_PROMPTS = 10
MEMORY_PROMPT_CHARS = 500
MIN_SESSIONS = 3      # a kind of work seen fewer times is not recurring
MAX_GAPS = 6
CANDIDATES_PER_GAP = 20
MIN_INSTALLS = 20     # skills.sh: fewer installs than this is too little signal to trust
VERDICTS_TTL = 7 * 86400  # a verdict is keyed by everything it read, so it only goes stale with the candidate
DENIED_FILE = "denied.json"  # documents a verifier flagged, by hash: a risk belongs to the text, whatever the task
VERIFY_RUNS = 2       # a candidate is kept only if every run keeps it: borderline ones flipped between runs
WRITE_SESSIONS = 8    # sessions a written skill is drawn from: the longest ones, where corrections pile up
TERM = sys.__stdout__  # progress lines and the result go here; everything else goes to the run log
LOG = sources.CACHE / "run.log"
REPORT = sources.CACHE / "report.md"


def reap_tokens(slug: str, doc: str) -> int:
    """What reap will report for this skill once installed, computed the way reap does (internal/scan/skills.go,
    internal/scan/frontmatter.go, internal/cost/cost.go): bytes of the directory name plus the description, / 3.7
    rounded up. The description is read whole, as Claude Code injects it: block scalars and continued lines too."""
    lines = doc.splitlines()
    if not lines or lines[0].strip() != "---":
        return (len(slug.encode("utf-8")) * 10 + 36) // 37
    header = []
    for line in lines[1:]:
        if line.strip() == "---":
            break
        header.append(line)
    if any(l.split(":", 1)[0] == "disable-model-invocation" and l.split(":", 1)[1].strip().lower() == "true"
           for l in header if ":" in l):
        return 0  # run only by hand: Claude Code never lists it, as reap counts it
    chars = len((slug + description(header)).encode("utf-8", "replace"))  # JSON escapes can decode to a lone surrogate
    return (chars * 10 + 36) // 37


def description(header: list[str]) -> str:
    """The unindented `description:` value, as reap's topLevelValue reads it."""
    for i, line in enumerate(header):
        if line != line.lstrip(" \t") or not line.startswith("description:"):
            continue
        raw = line.rstrip(" \t\r")[len("description:"):].strip()
        cont = []
        for c in header[i + 1:]:
            if c.strip() and c == c.lstrip(" \t"):
                break
            cont.append(c)
        ind = raw.split(" #")[0].strip()
        if ind[:1] in ("|", ">") and all(ch in "+-0123456789" for ch in ind[1:]):
            return block(cont, ind[0], "-" if "-" in ind else "+" if "+" in ind else "")
        if len(raw) >= 2 and raw[0] == raw[-1] == '"':
            try:
                return json.loads(raw)
            except ValueError:
                return raw.strip("\"'")
        if not raw or raw[0] in "\"'":
            return raw.strip("\"'")
        return " ".join([raw.strip("\"'")] + [c.strip() for c in cont if c.strip()])
    return ""


def block(lines: list[str], style: str, chomp: str) -> str:
    first = next((l for l in lines if l.strip()), None)
    if first is None:
        return ""
    indent = len(first) - len(first.lstrip(" \t"))
    body = ["" if not l.strip() else l[indent:].rstrip("\r") if len(l) >= indent else l.strip() for l in lines]
    trailing = 0
    while body and body[-1] == "":
        body.pop()
        trailing += 1
    if style == "|":
        out = "\n".join(body)
    else:
        out, breaks = "", 0
        for i, l in enumerate(body):
            if not l:
                breaks += 1
                continue
            if i:
                out += "\n" * breaks if breaks else " "
            out += l
            breaks = 0
    return out + ("" if chomp == "-" else "\n" * (trailing + 1) if chomp == "+" else "\n")


def failed(msg: str):
    TERM.write(("\r\033[K" if TERM.isatty() else "") + f"  ✗ {msg}\n")
    TERM.flush()


def step(msg: str, done: bool = False):
    if done:
        TERM.write(("\r\033[K" if TERM.isatty() else "") + f"  ✓ {msg}\n")
    elif TERM.isatty():
        TERM.write(f"  ⠋ {msg}…")
    TERM.flush()


# ---------------------------------------------------------------------------
# Inputs
# ---------------------------------------------------------------------------

def reap_state() -> dict:
    """Installed skills, which ones fired, the REAP pool and MCP weights, straight from `reap --json`."""
    if not shutil.which("reap"):
        sys.exit("reinvest needs `reap` on PATH (brew install thousandflowers/tap/skillreaper).")
    data = json.loads(subprocess.run(["reap", "--json", "--no-nudge"], capture_output=True, text=True).stdout)
    skills = [{"name": r["Name"], "desc": r.get("Description", ""), "tokens": r.get("Tokens", 0),
               "uses": r.get("Uses", 0), "verdict": r.get("Verdict", "")}
              for r in data.get("Rows", []) if r.get("Category") == "skill"]
    mcp = {r["Name"]: None if r.get("WeightUnknown") else r.get("Tokens", 0)
           for r in data.get("Rows", []) if r.get("Category") == "mcp"}
    return {"installed": skills, "used": [s for s in skills if s["uses"] > 0], "mcp": mcp,
            "reap": [s for s in skills if s["verdict"] == "REAP"], "dead": data.get("DeadTokensPerSession", 0)}


def remember_sessions(current: list[dict]) -> list[dict]:
    """The sessions on disk plus those already remembered: Claude Code deletes old transcripts, the work they show stays."""
    path = sources.STATE / "sessions_memory.json"
    try:
        raw = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        raw = []
    good = lambda r: isinstance(r, dict) and isinstance(r.get("id"), str) and isinstance(r.get("prompts"), list) \
        and all(isinstance(p, str) for p in r["prompts"])
    old = {r["id"]: {"tools": 0, "errors": 0, "interrupts": 0, "tokens": 0, "ts": "", **r, "skills": set(r.get("skills") or [])}
           for r in (raw if isinstance(raw, list) else []) if good(r)}
    known = {s["id"]: s for s in current}
    merged = [*current, *(r for i, r in old.items() if i not in known)]
    trim = lambda s: {**s, "skills": sorted(s["skills"]), "prompts": [p[:MEMORY_PROMPT_CHARS] for p in s["prompts"][:MEMORY_PROMPTS]]}
    newest = sorted(merged, key=lambda s: str(s["ts"]), reverse=True)[:MEMORY_SESSIONS]
    sources.write_private(path, json.dumps([trim(s) for s in newest], ensure_ascii=False))
    return merged


def load_groups(sessions: list[dict], regroup: bool) -> list[dict]:
    cache = sources.CACHE / "groups.json"
    try:
        saved = json.loads(cache.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        saved = None
    if regroup or not isinstance(saved, dict) or time.time() - saved.get("at", 0) >= REGROUP_EVERY:
        fresh = llm.group_sessions(sessions)
        if fresh is None:  # a call that never answered is not an answer: nothing saved, the next run asks again
            failed("grouping your work got no answer · trying again next run")
            return saved["groups"] if isinstance(saved, dict) and isinstance(saved.get("groups"), list) else []
        groups, at, seen = fresh, time.time(), {s["id"] for s in sessions}
    else:
        groups, at, seen = saved["groups"], saved["at"], set(saved.get("seen", []))
        new = [s for s in sessions if s["id"] not in seen]
        placed = llm.assign_sessions(groups, new) if new else groups
        if placed is not None:  # a call that never answered leaves the new sessions for the next run
            groups, seen = placed, seen | {s["id"] for s in new}
    sources.write_private(cache, json.dumps({"at": at, "groups": groups, "seen": sorted(seen)}, ensure_ascii=False))
    return groups


def kinds_of_work(groups: list[dict], sessions: list[dict]) -> list[dict]:
    by_id = {s["id"]: s for s in sessions}
    kinds = []
    for g in groups:
        ss = [by_id[i] for i in g.get("session_ids", []) if i in by_id]
        if not ss:
            continue
        fired = Counter(k for s in ss for k in s["skills"])
        top, n = fired.most_common(1)[0] if fired else (None, 0)
        kinds.append({
            "name": sources.one_line(str(g.get("name", ""))), "task": sources.one_line(str(g.get("task", ""))),
            "delegable": g.get("delegable") is True,
            "queries": sources.search_queries(g),
            "mcp_queries": [q for q in g.get("mcp_queries") or [] if isinstance(q, str)],
            "ids": [s["id"] for s in ss], "sessions": len(ss), "weeks": len({week(s["ts"]) for s in ss}),
            "tokens": sum(s["tokens"] for s in ss), "median_tokens": int(statistics.median(s["tokens"] for s in ss)),
            "friction": sum(s["errors"] + 3 * s["interrupts"] for s in ss),
            "covered_by": top if n / len(ss) >= 0.5 else None,  # a skill you use in most of these sessions
            "examples": [cut(s["prompts"][0], 200) for s in ss[:6]]})
    return kinds


def find_gaps(kinds: list[dict], installed: list[dict]) -> list[dict]:
    """Recurring, delegable kinds of work that no skill covers, used or merely installed (that is `reap reuse`)."""
    open_ = [k for k in kinds if k["sessions"] >= MIN_SESSIONS and k["delegable"] and k["task"] and not k["covered_by"]]
    covering = llm.covering_skills(open_, installed)
    if covering is None:  # not knowing what covers this work is not the same as nothing covering it
        failed("checking your installed skills got no answer · nothing proposed this run")
        return []
    gaps = [k for k, name in zip(open_, covering) if not name]
    score = lambda k: k["sessions"] * (1 + k["friction"] / k["sessions"]) * (1 + k["median_tokens"] / 200_000)
    return sorted(gaps, key=score, reverse=True)[:MAX_GAPS]


# ---------------------------------------------------------------------------
# Candidates
# ---------------------------------------------------------------------------

def namespaced(m: dict) -> dict:
    """A registry title is the server's own to choose: 'GitHub' from anyone. Its namespace is shown beside it."""
    return {**m, "name": m["name"] if m["name"] == m["id"] or "/" not in m["id"] else f"{m['name']} ({m['id'].split('/')[0]})"}


def candidates_for(gap: dict, found: list[dict], no_mcp: bool, refused: list | None = None) -> list[dict]:
    """Skills ranked by their best position in this gap's searches (installs reward off-topic skills), then MCP.
    What the rules turn down goes in refused, as (id, name, reason), so the report can say so."""
    refused = refused if refused is not None else []
    mine = set(gap["queries"])
    ranked = sorted((c for c in found if mine & set(c["hits"]) and c["installs"] >= MIN_INSTALLS),
                    key=lambda c: (-len(mine & set(c["hits"])), min(r for q, r in c["hits"].items() if q in mine), -c["installs"]))
    ranked = ranked[:CANDIDATES_PER_GAP]
    with ThreadPoolExecutor(10) as ex:
        docs = list(ex.map(sources.skill_doc, ranked))
    out = []
    for c, doc in zip(ranked, docs):
        desc = sources.frontmatter(doc).get("description", "")
        found_risks = sources.risks(doc) if len(desc) >= 40 else []
        if found_risks:
            refused.append((c["id"], c["name"], f"{llm.SECURITY}{', '.join(found_risks)}"))
        elif len(desc) >= 40 and reap_tokens(c["slug"], doc) == 0:
            # Weighs nothing because Claude never lists it: it cannot take over any work, and any document can claim this.
            refused.append((c["id"], c["name"], "runs only by hand: Claude could never start it"))
        elif len(desc) >= 40:
            out.append({**c, "doc": doc, "desc": desc, "cost": reap_tokens(c["slug"], doc)})
    if not no_mcp:
        # cost None: an MCP server's weight is its tool schemas, known only once it runs (reap reports it as unknown
        # too); running an unreviewed server to measure it is not acceptable before the user said yes
        for m in sources.search_mcp(gap["mcp_queries"]):
            found_risks = sources.risks(m["doc"], mcp=True)
            if found_risks:
                refused.append((m["id"], m["name"], f"{llm.SECURITY}{', '.join(found_risks)}"))
            else:
                out.append({**namespaced(m), "cost": None})
    return [{**c, "gap": f"{gap['name']} ({gap['sessions']} sessions)", "task": gap["task"]} for c in out]


def created_for(gaps: list[dict], sessions: list[dict], installed: list[dict], refused: list | None = None) -> list[dict]:
    """One skill written per gap. Cached by the gap, its sessions and the prompt: rewritten only when one changes,
    so its verdicts stay cached too. A cached one the rules flag leaves the cache, so the next run writes it again."""
    refused = refused if refused is not None else []
    cache_file = sources.CACHE / "written.json"
    try:
        cache = json.loads(cache_file.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        cache = {}
    by_id = {s["id"]: s for s in sessions}
    keys = [hashlib.sha256("\0".join([llm.WRITE_PROMPT, g["name"], g["task"], *sorted(g["ids"])]).encode()).hexdigest()
            for g in gaps]  # a new prompt rewrites every skill

    def write(gap: dict) -> list | None:
        ss = sorted((by_id[i] for i in gap["ids"] if i in by_id), key=lambda s: -len(s["prompts"]))
        return llm.write_skill(gap, [s["prompts"] for s in ss[:WRITE_SESSIONS]], installed)
    todo = [(k, g) for k, g in zip(keys, gaps) if k not in cache]
    with ThreadPoolExecutor(max(1, len(todo))) as ex:
        for (k, _), out in zip(todo, ex.map(lambda kg: write(kg[1]), todo)):
            if out:  # an unusable answer is not cached: the next run tries again
                cache[k] = list(out)
    flagged = {k: sources.risks(cache[k][1]) for k in keys if k in cache and sources.risks(cache[k][1])}
    refused.extend((f"new:{cache[k][0]}", cache[k][0], f"{llm.SECURITY}{', '.join(r)}") for k, r in flagged.items())
    sources.write_private(cache_file, json.dumps({k: cache[k] for k in keys if k in cache and k not in flagged}, ensure_ascii=False))
    taken = {canon(s["name"]) for s in installed}  # a cached skill may share a name with one installed since
    out = []
    for k, g in zip(keys, gaps):
        if k in cache and k not in flagged and canon(cache[k][0]) not in taken:
            slug, doc = cache[k]
            out.append({"id": f"new:{slug}", "name": slug, "slug": slug, "doc": doc, "kind": "skill",
                        "created": True, "desc": sources.frontmatter(doc).get("description", ""),
                        "cost": reap_tokens(slug, doc), "gap": f"{g['name']} ({g['sessions']} sessions)",
                        "task": g["task"]})
    return out


def reason_rank(reason: str) -> int:
    """Which reason to keep: a security finding first, a call that never answered last."""
    return 0 if reason.startswith(llm.SECURITY) else 2 if reason == llm.FAILED else 1


def combine(runs: list[dict]) -> dict:
    """Several verdicts on one candidate -> one: kept only if every run kept it, with the lowest fit. A security
    finding in any run is the reason, whatever the other runs said, and so it is cached."""
    no = [v for v in runs if v.get("keep") is not True]
    if no:
        return {"keep": False, "reason": min((v.get("reason", "") for v in no), key=reason_rank)}
    if not all(llm.valid_fit(v.get("fit")) for v in runs):
        return {"keep": False, "reason": llm.FAILED}
    return {**runs[0], "fit": min(v["fit"] for v in runs)}


def load_cache(path) -> dict:
    """A JSON object from the cache, or nothing: a cache that is damaged or not ours is ignored, not trusted."""
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return {}
    return data if isinstance(data, dict) else {}


def usable_verdict(v) -> bool:
    if not isinstance(v, dict) or not isinstance(v.get("at"), (int, float)) or time.time() - v["at"] >= VERDICTS_TTL:
        return False
    if v.get("keep") is False:
        return isinstance(v.get("reason", ""), str)
    return v.get("keep") is True and llm.valid_fit(v.get("fit")) and isinstance(v.get("use", ""), str) \
        and isinstance(v.get("reason", ""), str)


def identity(c: dict) -> str:
    """Who a candidate says it is and what installing it runs: the verifier checks these against the document."""
    if c.get("created"):
        return ""
    if c["kind"] == "mcp":
        return json.dumps({"registry name": c["id"], "installs": sources.install_target(c.get("server") or {})})
    return f"named {c['name']}, from the repo {c.get('source', '')}, folder {c.get('slug', '')}"


def doc_hash(doc: str) -> str:
    return hashlib.sha256(doc.encode("utf-8", "replace")).hexdigest()


def rejection_lines(rejected: dict) -> list[tuple[str, str]]:
    """Per candidate, every reason once, the most serious first."""
    return [(name, "; ".join(sorted(dict.fromkeys(reasons), key=reason_rank))) for name, reasons in rejected.values()]


def verify_all(cands: list[dict], state: dict) -> tuple[list[dict], list[tuple[str, str]]]:
    """Each candidate verified VERIFY_RUNS times; verdicts cached by everything the verifier read. A document flagged
    once stays flagged: one lucky roll on another task or another day must not pass it."""
    cache_file, denied_file = sources.CACHE / "verdicts.json", sources.CACHE / DENIED_FILE
    cache = {k: v for k, v in load_cache(cache_file).items() if usable_verdict(v)}
    denied = {k: v for k, v in load_cache(denied_file).items() if isinstance(v, str)}
    keys = [llm.verify_key(c["task"], c["doc"], state["used"], state["installed"], c.get("created", False), identity(c))
            for c in cands]
    verdicts = {k: cache[k] for k in keys if k in cache}
    todo = list({k: c for k, c in zip(keys, cands) if k not in verdicts and doc_hash(c["doc"]) not in denied}.items())
    with ThreadPoolExecutor(8) as ex:
        runs = list(ex.map(lambda kc: llm.verify(kc[1]["task"], kc[1]["doc"], state["used"], state["installed"],
                                                 own=kc[1].get("created", False), about=identity(kc[1])),
                           [kc for kc in todo for _ in range(VERIFY_RUNS)]))
    for i, (k, _) in enumerate(todo):
        verdicts[k] = {**combine(runs[i * VERIFY_RUNS:(i + 1) * VERIFY_RUNS]), "at": time.time()}
        if verdicts[k].get("reason") != llm.FAILED:  # a call that never answered is not a verdict: not cached
            cache[k] = verdicts[k]
    sources.write_private(cache_file, json.dumps(cache))
    for k, c in zip(keys, cands):
        reason = verdicts.get(k, {}).get("reason", "")
        if reason.startswith(llm.SECURITY):
            denied.setdefault(doc_hash(c["doc"]), reason)
    sources.write_private(denied_file, json.dumps(denied))
    passing, rejected = [], {}
    for k, c in zip(keys, cands):
        v = verdicts.get(k, {})
        reason = denied.get(doc_hash(c["doc"])) or v.get("reason", "")
        if c["kind"] == "mcp" and not sources.install_target(c.get("server") or {}):
            reason = reason or "no safe way to install it"
            v = {"keep": False}
        if doc_hash(c["doc"]) in denied or v.get("keep") is not True:
            rejected.setdefault(c["id"], (c["name"], []))[1].append(reason)
            continue
        where = {"where": sources.mcp_where(c["server"])} if c["kind"] == "mcp" else {}
        passing.append({**c, **where, "fit": v["fit"], "why": v.get("use") or v.get("reason", "")})
    return passing, rejection_lines(rejected)


def best_per_gap(passing: list[dict], limit: int) -> list[dict]:
    """The best fit for each gap; on a tie the lighter one (an unknown MCP weight ranks after any known one)."""
    ranked = sorted(passing, key=lambda c: (-c["fit"], c["cost"] is None, c["cost"] or 0))
    picks, gaps, names = [], set(), set()
    for c in ranked:
        if c["gap"] not in gaps and c["name"] not in names and len(picks) < limit:
            gaps.add(c["gap"])
            names.add(c["name"])
            picks.append(c)
    return picks


# ---------------------------------------------------------------------------
# Output
# ---------------------------------------------------------------------------

def summary(swap_list: list[dict], gaps: list[dict], dead: int) -> str:
    """The non-interactive view (no terminal): the same swaps, as text."""
    lines = [f"  reinvest · ~{dead:,} dead tok/session per reap · swaps for work you do with no skill", ""]
    for s in swap_list:
        c = s["cands"][s["idx"]]
        lines += [f"  mute      {swaps.mute_text(s)}",
                  f"  {swaps.verb(c):<9} {c['name']}   {swaps.weight(c['cost'], c.get('installed', False))} · {swaps.kind_text(c)}",
                  f"            for {c['gap']}", f"            {c['why']}", ""]
    solved = {s["cands"][s["idx"]]["gap"] for s in swap_list}
    missing = [g["name"] for g in gaps if f"{g['name']} ({g['sessions']} sessions)" not in solved]
    if missing:
        lines += ["  no fit found for: " + ", ".join(missing), ""]
    return "\n".join(lines + [f"  details: {REPORT}"])


def write_report(kinds: list[dict], gaps: list[dict], passing: list[dict], rejected: list[tuple[str, str]]):
    lines = ["# reinvest", "", "## Kinds of work", ""]
    lines += [f"- {k['name']}: {k['sessions']} sessions, {k['weeks']} weeks"
              + (f", covered by {k['covered_by']}" if k["covered_by"] else "") for k in kinds]
    lines += ["", "## Gaps", ""] + [f"- {g['name']}: {g['task']}" for g in gaps]
    lines += ["", "## Verified candidates", ""] + [
        f"- {c['name']} ({swaps.kind_text(c)}, {swaps.weight(c['cost'])}, fit {c['fit']}/5) for {c['gap']}: {c['why']}"
        for c in passing]
    lines += ["", "## Rejected", ""] + [f"- {n}: {r}" for n, r in rejected]
    sources.write_private(REPORT, "\n".join(lines) + "\n")
    sources.write_private(sources.CACHE / "verified.json", json.dumps(
        [{"id": c.get("id"), "name": c["name"], "gap": c["gap"].rsplit(" (", 1)[0], "fit": c["fit"], "why": c["why"],
          "cost": c["cost"], "installs": c.get("installs", 0)} for c in passing], ensure_ascii=False))


def refusal_lines(rejected: list[tuple[str, str]]) -> list[str]:
    """What the user is told about refusals: a failed check is not the same as nothing fitting."""
    failed = sum(1 for _, reason in rejected if llm.FAILED in reason)
    unsafe = [(n, r) for n, r in rejected if llm.SECURITY in r]
    lines = [f"  refused for security: {sources.printable(n)}: {sources.printable(r)}" for n, r in unsafe]
    if failed:
        lines.append(f"  the check could not run for {failed} · they count as not checked, not as no fit")
    return lines + ([f"  details: {REPORT}"] if rejected else [])


def choose_and_apply(swap_list: list[dict], dry_run: bool):
    chosen = swaps.pick(swap_list)
    if not chosen:
        print("  nothing accepted · nothing changed")
        return
    for s in chosen:
        print(f"  {swaps.named(s['cands'][s['idx']])}  in place of  {swaps.names(s['mute'])}")
    if not dry_run and input("  apply? installs the new ones, mutes the old ones (reap unmute undoes) [y/N] "
                             ).strip().lower() != "y":
        print("  nothing changed")
        return
    print("\n".join(swaps.apply(chosen, dry_run)))
    if not dry_run:
        swaps.drop_proposals(chosen)
    if dry_run:
        print("  dry run · nothing installed or muted")


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--days", type=int, default=120, help="sessions to read (longer than reap's window: "
                                                          "recurring work needs history)")
    ap.add_argument("--max", type=int, default=3, help="swaps proposed at most")
    ap.add_argument("--regroup", action="store_true", help="group every session from scratch now, instead of weekly")
    ap.add_argument("--online", action="store_true", help="search skills.sh instead of "
                                                          "writing skills from your sessions")
    ap.add_argument("--mcp", action="store_true", help="with --online: also offer MCP servers. Off by default: the registry "
                                                       "gives no signal of trust, and a server runs code")
    ap.add_argument("--no-mcp", action="store_true", help=argparse.SUPPRESS)  # what was once needed to get today's default
    ap.add_argument("--dry-run", action="store_true", help="pick swaps but only print what would run: installs and mutes nothing")
    args = ap.parse_args()
    sources.CACHE.mkdir(parents=True, exist_ok=True)
    sys.stdout = LOG.open("w", encoding="utf-8")  # library chatter goes to the log; the terminal gets steps

    step("reading sessions")
    state = reap_state()
    sessions = remember_sessions(read_sessions(args.days, {canon(s["name"]) for s in state["installed"]}))
    step(f"{len(sessions)} sessions read", done=True)

    refused = []  # what the rules turned down before any verifier saw it
    step("grouping your work")
    kinds = kinds_of_work(load_groups(sessions, args.regroup), sessions)
    gaps = find_gaps(kinds, state["installed"])
    step(f"{len(kinds)} kinds of work · {len(gaps)} with no skill", done=True)

    if args.online:
        step("searching skills and MCP servers")
        queries = sorted({q for g in gaps for q in g["queries"]})
        found = sources.search_skills(queries, {canon(s["name"]) for s in state["installed"]})
        with ThreadPoolExecutor(max(1, len(gaps))) as ex:
            cands = [c for cs in ex.map(lambda g: candidates_for(g, found, args.no_mcp or not args.mcp, refused), gaps) for c in cs]
        step(f"{len(cands)} candidates", done=True)
    else:
        step(f"writing a skill for {len(gaps)} gaps from your sessions")
        cands = created_for(gaps, sessions, state["installed"], refused)
        step(f"{len(cands)} skills written", done=True)

    step(f"verifying {len(cands)} candidates")
    passing, rejected = verify_all(cands, state)
    rejected = rejected + rejection_lines({i: (n, [r]) for i, n, r in refused})
    step(f"{len(passing)} fit", done=True)

    write_report(kinds, gaps, passing, rejected)
    settled = swaps.settle(state["mcp"], state["reap"])
    pool = [x for x in state["reap"] if not any(x in s["mute"] for s in settled)]
    swap_list = settled + swaps.build(best_per_gap(passing, args.max), passing, pool)
    sys.stdout.close()
    sys.stdout = sys.__stdout__
    swaps.save_proposals(swap_list)
    print()
    told = refusal_lines(rejected)
    if told:
        print("\n".join(told))
    if not swap_list:
        print("  no swaps: nothing verified for your gaps" + ("" if told else f" · details: {REPORT}"))
    elif sys.stdin.isatty() and TERM.isatty():
        choose_and_apply(swap_list, args.dry_run)
    else:
        print(summary(swap_list, gaps, state["dead"]))


if __name__ == "__main__":
    main()
