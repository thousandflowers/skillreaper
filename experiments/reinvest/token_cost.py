#!/usr/bin/env python3
"""Tokens per request inside each kind of work, with vs without each skill you invoked.

Reads the groups reinvest cached today (run reinvest.py first). A comparison needs a few sessions on both
sides: skill presence is confounded with session size, so thin sides are shown as such, not as a ratio.
"""
from __future__ import annotations

import json
import statistics
import sys

import sources
from reinvest import reap_state
from sessions import canon, read_sessions

MIN_SIDE = 2


def per_request(s: dict) -> float:
    return s["tokens"] / max(1, len(s["prompts"]))


def median(xs) -> int:
    xs = list(xs)
    return int(statistics.median(xs)) if xs else 0


def main():
    cache = sources.CACHE / "groups.json"
    if not cache.exists():
        sys.exit("no cached groups: run reinvest.py first")
    state = reap_state()
    by_id = {s["id"]: s for s in read_sessions(120, {canon(s["name"]) for s in state["installed"]})}
    for g in sorted(json.loads(cache.read_text()), key=lambda g: -len(g["session_ids"])):
        ss = [by_id[i] for i in g["session_ids"] if i in by_id]
        if len(ss) < 3:
            continue
        print(f"\n  {g['name']} · {len(ss)} sessions · median {median(s['tokens'] for s in ss):,} tok/session · "
              f"{median(per_request(s) for s in ss):,} tok/request")
        for skill in sorted({k for s in ss for k in s["skills"]}):
            with_ = [s for s in ss if skill in s["skills"]]
            without = [s for s in ss if skill not in s["skills"]]
            if min(len(with_), len(without)) < MIN_SIDE:
                print(f"    {skill:<28} {len(with_)} with / {len(without)} without · too few to compare")
                continue
            a, b = median(per_request(s) for s in with_), median(per_request(s) for s in without)
            print(f"    {skill:<28} {a:,} vs {b:,} tok/request ({len(with_)} vs {len(without)}) · {a / max(1, b):.1f}x")


if __name__ == "__main__":
    main()
