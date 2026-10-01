#!/usr/bin/env python3
"""reap reinvest (prototype): trade skills you never use for ones that do work you do and have nothing for.

  1. sessions   the ones you wrote (no `claude -p`, no subagents, not the open one)
  2. kinds      Claude groups them by kind of work; kinds a skill already covers drop out: what is left are gaps
  3. search     skills.sh and the official MCP registry, with queries written for each gap
  4. verify     every (gap, candidate) read in full by Claude, alone; most are rejected, with a reason
  5. swaps      each fit is paired with REAP skills of about the same weight; pick them with the arrow keys,
                then reinvest installs the new one and runs `reap mute` on the old ones (reversible)

Weights and verdicts come from `reap --json`, so the numbers match the reap report.
Usage:  python3 reinvest.py [--dry-run] [--regroup] [--no-mcp] [--days N]
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

GROUPS_TTL = 86400    # regrouping every run made proposals change run to run (0-18% overlap vs 100% cached)
MIN_SESSIONS = 3      # a kind of work seen fewer times is not recurring
MAX_GAPS = 6
CANDIDATES_PER_GAP = 10
MIN_INSTALLS = 20     # skills.sh: fewer installs than this is too little signal to trust
VERDICTS_TTL = 7 * 86400  # a verdict is keyed by everything it read, so it only goes stale with the candidate
VERIFY_RUNS = 2       # a candidate is kept only if every run keeps it: borderline ones flipped between runs
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
    chars = len((slug + description(header)).encode("utf-8"))
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


def load_groups(sessions: list[dict], regroup: bool) -> list[dict]:
    cache = sources.CACHE / "groups.json"
    if not regroup and cache.exists() and time.time() - cache.stat().st_mtime < GROUPS_TTL:
        return json.loads(cache.read_text(encoding="utf-8"))
    groups = llm.group_sessions(sessions)
    cache.write_text(json.dumps(groups, ensure_ascii=False), encoding="utf-8")
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
            "name": str(g.get("name", "")), "task": str(g.get("task", "")), "delegable": bool(g.get("delegable")),
            "queries": g.get("queries") or [], "mcp_queries": g.get("mcp_queries") or [],
            "sessions": len(ss), "weeks": len({week(s["ts"]) for s in ss}),
            "tokens": sum(s["tokens"] for s in ss), "median_tokens": int(statistics.median(s["tokens"] for s in ss)),
            "friction": sum(s["errors"] + 3 * s["interrupts"] for s in ss),
            "covered_by": top if n / len(ss) >= 0.5 else None,  # a skill you use in most of these sessions
            "examples": [cut(s["prompts"][0], 200) for s in ss[:6]]})
    return kinds


def find_gaps(kinds: list[dict], installed: list[dict]) -> list[dict]:
    """Recurring, delegable kinds of work that no skill covers, used or merely installed (that is `reap reuse`)."""
    open_ = [k for k in kinds if k["sessions"] >= MIN_SESSIONS and k["delegable"] and k["task"] and not k["covered_by"]]
    for k, name in zip(open_, llm.covering_skills(open_, installed)):
        k["covered_by"] = name
    gaps = [k for k in open_ if not k["covered_by"]]
    score = lambda k: k["sessions"] * (1 + k["friction"] / k["sessions"]) * (1 + k["median_tokens"] / 200_000)
    return sorted(gaps, key=score, reverse=True)[:MAX_GAPS]


# ---------------------------------------------------------------------------
# Candidates
# ---------------------------------------------------------------------------

def candidates_for(gap: dict, found: list[dict], no_mcp: bool) -> list[dict]:
    """Skills ranked by their best position in this gap's searches (installs reward off-topic skills), then MCP."""
    mine = set(gap["queries"])
    ranked = sorted((c for c in found if mine & set(c["hits"]) and c["installs"] >= MIN_INSTALLS),
                    key=lambda c: (min(r for q, r in c["hits"].items() if q in mine), -c["installs"]))
    ranked = ranked[:CANDIDATES_PER_GAP]
    with ThreadPoolExecutor(10) as ex:
        docs = list(ex.map(sources.skill_doc, ranked))
    out = []
    for c, doc in zip(ranked, docs):
        desc = sources.frontmatter(doc).get("description", "")
        if len(desc) >= 40 and not sources.risks(doc):
            out.append({**c, "doc": doc, "desc": desc, "cost": reap_tokens(c["slug"], doc)})
    if not no_mcp:
        # cost None: an MCP server's weight is its tool schemas, known only once it runs (reap reports it as unknown
        # too); running an unreviewed server to measure it is not acceptable before the user said yes
        out += [{**m, "cost": None} for m in sources.search_mcp(gap["mcp_queries"])
                if not sources.risks(m["doc"], mcp=True)]
    return [{**c, "gap": f"{gap['name']} ({gap['sessions']} sessions)", "task": gap["task"]} for c in out]


def combine(runs: list[dict]) -> dict:
    """Several verdicts on one candidate -> one: kept only if every run kept it, with the lowest fit."""
    no = next((v for v in runs if not v.get("keep")), None)
    if no:
        return {"keep": False, "reason": no.get("reason", "")}
    return {**runs[0], "fit": min(int(v.get("fit") or 1) for v in runs)}


def verify_all(cands: list[dict], state: dict) -> tuple[list[dict], list[tuple[str, str]]]:
    """Each candidate verified VERIFY_RUNS times; verdicts cached by everything the verifier read."""
    cache_file = sources.CACHE / "verdicts.json"
    try:
        cache = json.loads(cache_file.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        cache = {}
    context = "\n".join(sorted(s["name"] for s in state["installed"])) + "\n" + \
              "\n".join(sorted(s["name"] for s in state["used"]))
    keys = [hashlib.sha256("\0".join((c["task"], c["id"], c["doc"], context)).encode()).hexdigest() for c in cands]
    verdicts = {k: cache[k] for k in keys if k in cache and time.time() - cache[k]["at"] < VERDICTS_TTL}
    todo = list({k: c for k, c in zip(keys, cands) if k not in verdicts}.items())
    with ThreadPoolExecutor(8) as ex:
        runs = list(ex.map(lambda kc: llm.verify(kc[1]["task"], kc[1]["doc"], state["used"], state["installed"]),
                           [kc for kc in todo for _ in range(VERIFY_RUNS)]))
    for i, (k, _) in enumerate(todo):
        verdicts[k] = {**combine(runs[i * VERIFY_RUNS:(i + 1) * VERIFY_RUNS]), "at": time.time()}
        if verdicts[k].get("reason") != llm.FAILED:  # a call that never answered is not a verdict: not cached
            cache[k] = verdicts[k]
    cache_file.write_text(json.dumps({k: v for k, v in cache.items() if time.time() - v["at"] < VERDICTS_TTL}),
                          encoding="utf-8")
    passing, rejected = [], {}
    for k, c in zip(keys, cands):
        v = verdicts[k]
        if not v.get("keep"):
            rejected[c["name"]] = v.get("reason", "")
            continue
        kind = f"MCP server, {sources.mcp_where(c['server'])}" if c["kind"] == "mcp" else "skill"
        passing.append({**c, "kind": kind, "fit": int(v.get("fit") or 1), "why": v.get("use") or v.get("reason", "")})
    return passing, list(rejected.items())


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
                  f"  {swaps.verb(c):<9} {c['name']}   {swaps.weight(c['cost'], c.get('installed', False))} · {c['kind']}",
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
        f"- {c['name']} ({c['kind']}, {swaps.weight(c['cost'])}, fit {c['fit']}/5) for {c['gap']}: {c['why']}"
        for c in passing]
    lines += ["", "## Rejected", ""] + [f"- {n}: {r}" for n, r in rejected]
    REPORT.write_text("\n".join(lines) + "\n", encoding="utf-8")


def choose_and_apply(swap_list: list[dict], dry_run: bool):
    chosen = swaps.pick(swap_list)
    if not chosen:
        print("  nothing accepted · nothing changed")
        return
    for s in chosen:
        print(f"  {s['cands'][s['idx']]['name']}  in place of  {swaps.names(s['mute'])}")
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
    ap.add_argument("--regroup", action="store_true", help="regroup sessions even if today's groups are cached")
    ap.add_argument("--no-mcp", action="store_true", help="skills only")
    ap.add_argument("--dry-run", action="store_true", help="pick swaps but only print what would run")
    args = ap.parse_args()
    sources.CACHE.mkdir(parents=True, exist_ok=True)
    sys.stdout = LOG.open("w", encoding="utf-8")  # library chatter goes to the log; the terminal gets steps

    step("reading sessions")
    state = reap_state()
    sessions = read_sessions(args.days, {canon(s["name"]) for s in state["installed"]})
    step(f"{len(sessions)} sessions read", done=True)

    step("grouping your work")
    kinds = kinds_of_work(load_groups(sessions, args.regroup), sessions)
    gaps = find_gaps(kinds, state["installed"])
    step(f"{len(kinds)} kinds of work · {len(gaps)} with no skill", done=True)

    step("searching skills and MCP servers")
    queries = sorted({q for g in gaps for q in g["queries"]})
    found = sources.search_skills(queries, {canon(s["name"]) for s in state["installed"]})
    with ThreadPoolExecutor(max(1, len(gaps))) as ex:
        cands = [c for cs in ex.map(lambda g: candidates_for(g, found, args.no_mcp), gaps) for c in cs]
    step(f"{len(cands)} candidates", done=True)

    step(f"verifying {len(cands)} candidates")
    passing, rejected = verify_all(cands, state)
    step(f"{len(passing)} fit", done=True)

    write_report(kinds, gaps, passing, rejected)
    settled = swaps.settle(state["mcp"], state["reap"])
    pool = [x for x in state["reap"] if not any(x in s["mute"] for s in settled)]
    swap_list = settled + swaps.build(best_per_gap(passing, args.max), passing, pool)
    sys.stdout.close()
    sys.stdout = sys.__stdout__
    swaps.save_proposals(swap_list)
    print()
    if not swap_list:
        print(f"  no swaps: nothing verified for your gaps · details: {REPORT}")
    elif sys.stdin.isatty() and TERM.isatty():
        choose_and_apply(swap_list, args.dry_run)
    else:
        print(summary(swap_list, gaps, state["dead"]))


if __name__ == "__main__":
    main()
