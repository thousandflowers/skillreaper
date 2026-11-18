#!/usr/bin/env python3
"""Writes the tables the Go port of reinvest needs and the Go standard library does not have.

  nfkc.txt   Unicode NFKC: full compatibility decompositions (D), canonical combining classes (C),
             canonical composition pairs (P). Hangul syllables are left out: they are computed.
  html5.txt  What html.unescape knows: named references (E), the replacements for invalid numeric
             references (R) and the code points a numeric reference may not produce (X).
  casing.txt What str.lower() needs to choose between the two small sigmas: the ranges of characters
             it skips over (I, "case-ignorable") and of the others that count as letters with a case (K).

Everything comes from this Python's own unicodedata and html modules, so the Go code folds text the way
the Python prototype did. Run:  python3 gen.py   (rewrites the three files next to this script)
"""
from __future__ import annotations

import html
import sys
import unicodedata
from html.entities import html5
from pathlib import Path

HERE = Path(__file__).resolve().parent
HANGUL = range(0xAC00, 0xD7A4)
SURROGATES = range(0xD800, 0xE000)


def hexes(text: str) -> str:
    return " ".join(f"{ord(ch):X}" for ch in text)


def nfkc_lines() -> list[str]:
    lines = [f"# Unicode {unicodedata.unidata_version}, from Python {sys.version.split()[0]}"]
    points = [cp for cp in range(sys.maxunicode + 1) if cp not in SURROGATES]
    for cp in points:
        full = unicodedata.normalize("NFKD", chr(cp))
        if full != chr(cp) and cp not in HANGUL:
            lines.append(f"D {cp:X} {hexes(full)}")
    for cp in points:
        ccc = unicodedata.combining(chr(cp))
        if ccc:
            lines.append(f"C {cp:X} {ccc}")
    for cp in points:
        parts = unicodedata.decomposition(chr(cp)).split()
        if len(parts) != 2 or parts[0].startswith("<") or cp in HANGUL:
            continue
        # a composition exclusion does not come back from its own decomposition
        if unicodedata.normalize("NFC", unicodedata.normalize("NFD", chr(cp))) == chr(cp):
            lines.append(f"P {parts[0]} {parts[1]} {cp:X}")
    return lines


def html_lines() -> list[str]:
    lines = [f"# html.unescape tables, from Python {sys.version.split()[0]}"]
    lines += [f"E {name} {hexes(value)}" for name, value in sorted(html5.items())]
    lines += [f"R {num:X} {hexes(value)}" for num, value in sorted(html._invalid_charrefs.items())]
    lines += [f"X {num:X}" for num in sorted(html._invalid_codepoints)]
    return lines


SIGMA, FINAL_SIGMA = "\u03a3", "\u03c2"


def ranges(points: list[int]) -> list[tuple[int, int]]:
    out: list[tuple[int, int]] = []
    for cp in points:
        if out and out[-1][1] == cp - 1:
            out[-1] = (out[-1][0], cp)
        else:
            out.append((cp, cp))
    return out


def casing_lines() -> list[str]:
    """Read off str.lower() itself: a capital sigma takes its final form after a cased letter, with any
    number of ignorable characters between, so two probes tell what each character is."""
    lines = [f"# Unicode {unicodedata.unidata_version}, from Python {sys.version.split()[0]}"]
    ignorable, cased = [], []
    for cp in (cp for cp in range(sys.maxunicode + 1) if cp not in SURROGATES):
        right_after = (chr(cp) + SIGMA).lower().endswith(FINAL_SIGMA)
        after_a_letter = ("a" + chr(cp) + SIGMA).lower().endswith(FINAL_SIGMA)
        if right_after:
            cased.append(cp)
        elif after_a_letter:
            ignorable.append(cp)
    lines += [f"I {lo:X} {hi:X}" for lo, hi in ranges(ignorable)]
    lines += [f"K {lo:X} {hi:X}" for lo, hi in ranges(cased)]
    return lines


def main() -> None:
    (HERE / "nfkc.txt").write_text("\n".join(nfkc_lines()) + "\n", encoding="utf-8")
    (HERE / "html5.txt").write_text("\n".join(html_lines()) + "\n", encoding="utf-8")
    (HERE / "casing.txt").write_text("\n".join(casing_lines()) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
