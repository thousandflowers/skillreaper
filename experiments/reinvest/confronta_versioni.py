#!/usr/bin/env python3
"""Confronta le versioni di prove_versioni.sh: tempo medio, stabilità delle proposte tra giri, unione delle proposte."""
import json
from collections import Counter, defaultdict
from itertools import combinations
from pathlib import Path

runs = defaultdict(list)
for f in sorted(Path("prove_laya").glob("reinvest_debug_*_*.json")):
    name = f.stem.removeprefix("reinvest_debug_").rsplit("_", 1)[0]
    runs[name].append(json.loads(f.read_text()))


def jaccard(a, b):
    a, b = set(a), set(b)
    return len(a & b) / len(a | b) if a | b else 1.0


for name, rs in runs.items():
    props = [r.get("proposte", []) for r in rs]
    pairs = list(combinations(props, 2))
    stab = sum(jaccard(a, b) for a, b in pairs) / len(pairs) if pairs else None
    count = Counter(p for ps in props for p in set(ps))
    print(f"\n## {name}: {len(rs)} giri, tempo medio {sum(r.get('secondi', 0) for r in rs) // max(1, len(rs))}s, "
          f"stabilità proposte {stab if stab is None else f'{stab:.0%}'}, scartate in verifica "
          f"{sum(len(r.get('scartate', [])) for r in rs)}")
    for i, ps in enumerate(props, 1):
        print(f"  giro {i}: {', '.join(ps) or '(nessuna)'}")
    print("  presenze: " + ", ".join(f"{p} {n}/{len(rs)}" for p, n in count.most_common()))
