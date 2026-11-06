#!/usr/bin/env python3
"""Prints the amounts' currency as JSON: the one set on this Mac, at the fixed rate in rates.json. No network."""
import json
import math
import os
import re
import subprocess
import datetime
import sys

ASK_SYSTEM = """ObjC.import("Foundation"); var l = $.NSLocale.currentLocale; var f = $.NSNumberFormatter.alloc.init;
f.numberStyle = $.NSNumberFormatterCurrencyStyle; f.locale = l;
JSON.stringify({code: l.currencyCode.js, sym: l.currencySymbol.js, decimal: l.decimalSeparator.js, group: l.groupingSeparator.js, digits: f.maximumFractionDigits})"""
USD = {"cur": "USD", "sym": "US$", "rate": 1.0, "decimal": ".", "group": ",", "digits": 2}  # a fallback must read as foreign where $ is local
MAX_RATE_AGE_DAYS = 90
CODE = re.compile(r"[A-Z]{3}")
MAX_DIGITS = 4
BIDI_MARKS = re.compile("[‎‏؜‪-‮⁦-⁩]")


def parse_answer(out):
    """What the system said: its JSON, or the older 'CODE SYMBOL' line. None if it makes no sense."""
    try:
        ans = json.loads(out)
    except ValueError:
        parts = out.strip().split(None, 1)
        ans = {"code": parts[0], "sym": parts[1]} if len(parts) == 2 else None
    if not isinstance(ans, dict) or not isinstance(ans.get("code"), str) or not CODE.fullmatch(ans["code"]) or not isinstance(ans.get("sym"), str):
        return None
    sym = BIDI_MARKS.sub("", ans["sym"]).strip()
    style = {k: ans[k] for k in ("decimal", "group") if isinstance(ans.get(k), str) and len(ans[k]) == 1}
    digits = ans.get("digits")
    if type(digits) is int and 0 <= digits <= MAX_DIGITS:
        style["digits"] = digits
    return {"code": ans["code"], "sym": sym or ans["code"], "style": style}


def system_currency():
    """The system's currency and number style, or None off macOS or if it cannot answer."""
    try:
        out = subprocess.run(["osascript", "-l", "JavaScript", "-e", ASK_SYSTEM], capture_output=True, text=True, errors="replace", timeout=10, check=True).stdout
    except (OSError, subprocess.SubprocessError):
        return None
    return parse_answer(out)


def usable_rate(v):
    return isinstance(v, (int, float)) and not isinstance(v, bool) and math.isfinite(v) and v > 0


def fresh(date, today):
    """A table whose date is unreadable or too old prints no converted cents: a wrong amount is worse than a foreign one."""
    try:
        age = today - datetime.date.fromisoformat(date)
    except (TypeError, ValueError):
        return False
    return age.days <= MAX_RATE_AGE_DAYS


def read_table(path, today=None):
    """The fixed rates, or nothing: an unreadable or stale table means every currency is unknown."""
    try:
        with open(path, encoding="utf-8") as f:
            found = json.load(f)
        table, date = found["per_eur"], found.get("date")
    except (OSError, ValueError, KeyError, TypeError, AttributeError):
        return {}
    return table if isinstance(table, dict) and fresh(date, today or datetime.date.today()) else {}


def currency(found, per_eur):
    """Prices are in USD. A currency with no usable rate stays in USD: a wrong amount is worse than a foreign one.
    The separators are the system's either way."""
    style = found["style"] if found else {}
    if not found or not usable_rate(per_eur.get(found["code"])) or not usable_rate(per_eur.get("USD")):
        return {**USD, **{k: v for k, v in style.items() if k != "digits"}}
    return {**USD, **style, "cur": found["code"], "sym": found["sym"], "rate": per_eur[found["code"]] / per_eur["USD"]}


if __name__ == "__main__":
    eur = {"code": "EUR", "sym": "€", "style": {"decimal": ",", "group": "."}}
    assert currency(None, {"USD": 2.0}) == USD
    assert currency({"code": "XXX", "sym": "x", "style": {}}, {"USD": 2.0}) == USD
    assert currency(eur, {"USD": 2.0, "EUR": 1.0}) == {**USD, "cur": "EUR", "sym": "€", "rate": 0.5, "decimal": ",", "group": "."}
    assert all(currency(eur, {"USD": 2.0, "EUR": bad})["cur"] == "USD" for bad in (0, -1, float("nan"), "1", None))
    assert all(currency(eur, {"USD": bad, "EUR": 1.0})["cur"] == "USD" for bad in (0, -1.1, float("inf"), None))
    assert fresh("2026-01-01", datetime.date(2026, 2, 1)) and not fresh("2019-01-01", datetime.date(2026, 2, 1)) and not fresh(None, datetime.date(2026, 2, 1))
    assert parse_answer("EUR €") == {"code": "EUR", "sym": "€", "style": {}}
    assert parse_answer("CHF Fr .") == {"code": "CHF", "sym": "Fr .", "style": {}}
    assert parse_answer("nope") is None
    assert parse_answer('{"code":"JPY","sym":"¥","decimal":".","group":",","digits":0}')["style"]["digits"] == 0
    table = read_table(os.path.join(os.path.dirname(os.path.abspath(__file__)), "rates.json"))
    json.dump(currency(system_currency(), table), sys.stdout, ensure_ascii=False)
