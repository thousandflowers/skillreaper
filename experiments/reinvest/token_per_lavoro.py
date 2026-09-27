#!/usr/bin/env python3
"""Token per tipo di lavoro, con e senza ciascuna skill: esperimento naturale sulle sessioni già raggruppate."""
import json
import statistics
from collections import defaultdict
from pathlib import Path

import reinvest_prova as R
from laya_prove_mac import canon, read_installed, unique_by_name

names = {canon(s["name"]) for s in unique_by_name(read_installed())}
S = R.session_stats(120, names)
groups = json.loads(Path("prove_laya/gruppi.json").read_text())


def per_req(s):  # token per richiesta: le sessioni lunghe non pesano solo perché lunghe
    return s["tokens"] / max(1, len(s["prompts"]))


def med(xs):
    return int(statistics.median(xs)) if xs else 0


for g in sorted(groups, key=lambda g: -len(g.get("sid", []))):
    by_id = {s["id"]: s for s in S}
    ss = [by_id[x] for x in g.get("sid", []) if x in by_id]
    if len(ss) < 3:
        continue
    print(f"\n## {g.get('name')} — {len(ss)} sessioni, mediana {med([s['tokens'] for s in ss]):,} token/sessione, "
          f"{med([per_req(s) for s in ss]):,} token/richiesta, attrito medio "
          f"{sum(s['errors'] + 2 * s['fixes'] + 3 * s['interrupts'] for s in ss) / len(ss):.0f}")
    by_skill = defaultdict(list)
    for s in ss:
        for k in s["skills"]:
            by_skill[k].append(s)
    for k, with_ in sorted(by_skill.items(), key=lambda kv: -len(kv[1])):
        without = [s for s in ss if k not in s["skills"]]
        if len(with_) < 2 or len(without) < 2:
            print(f"   {k}: {len(with_)} con / {len(without)} senza, troppo poche per confrontare")
            continue
        a, b = med([per_req(s) for s in with_]), med([per_req(s) for s in without])
        print(f"   {k}: con {a:,} token/richiesta ({len(with_)}) vs senza {b:,} ({len(without)}) -> {a / max(1, b):.1f}x")
