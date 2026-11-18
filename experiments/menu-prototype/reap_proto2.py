#!/usr/bin/env python3
"""Round 2: five different concepts, gradient bars, more data. Read-only: nothing is ever run."""
import collections, curses, json, locale, os, sys, textwrap
import reap_proto
from reap_proto import DATA, DEAD, MONEY, ROWS, WHY, C, DIM, HL, load_json, pair, put, window


def load_extras():
    """The day counts are decoration: anything odd in them is left out, never fatal."""
    raw = load_json("extras.json", {})
    raw = raw if isinstance(raw, dict) else {}
    byday = raw.get("byday") if isinstance(raw.get("byday"), dict) else {}
    return {"dates": [], "days": [], "fires": [], **raw,
            "byday": {k: v for k, v in byday.items() if isinstance(v, list) and all(type(n) is int and n >= 0 for n in v)}}


EXTRA = load_extras()
M = DATA["Measured"] if isinstance(DATA.get("Measured"), dict) else {}
SESSIONS = DATA.get("Sessions", 0)
PRICE = MONEY / DEAD if DEAD else 0
WARM = [57, 93, 129, 165, 201, 205, 209, 215, 221, 227]
HEALTH = [196, 202, 208, 214, 220, 226, 190, 154, 118, 46]
RAMP_BASE = {}


def setup():
    reap_proto.setup()
    next_pair = 20
    if getattr(curses, "COLORS", 0) >= 256:
        for c in WARM + HEALTH:
            pair(next_pair, c)
            pair(next_pair + 100, 16, c)
            RAMP_BASE[c] = next_pair
            next_pair += 1


def rc(ramp, t, bg=False):
    c = ramp[min(len(ramp) - 1, int(t * len(ramp)))]
    if c not in RAMP_BASE:
        return curses.A_REVERSE if bg else 0
    return curses.color_pair(RAMP_BASE[c] + (100 if bg else 0))


def grad(w, y, x, width, frac, ramp):
    """A bar whose colour depends on the position of the cell, so a fuller bar reaches further along the ramp."""
    filled = frac * width
    for i in range(width):
        t = i / max(1, width - 1)
        if i + 1 <= filled:
            put(w, y, x + i, "█", rc(ramp, t))
        elif i < filled:
            put(w, y, x + i, " ▏▎▍▌▋▊▉"[int((filled - i) * 8)], rc(ramp, t))
        else:
            put(w, y, x + i, "░", curses.A_DIM)


def spark(w, y, x, vals, ramp):
    top = max(vals) or 1
    for i, v in enumerate(vals):
        ch = "▁▂▃▄▅▆▇█"[min(7, v * 8 // (top + 1))] if v else "·"
        put(w, y, x + i, ch, rc(ramp, v / top) if v else curses.A_DIM)


def human(n):
    for lim, s in ((1e9, "B"), (1e6, "M"), (1e3, "k")):
        if n >= lim:
            return f"{n / lim:.1f}{s}"
    return str(int(n))


def src(r):
    s = r["Source"]
    if s.startswith("plugin:"):
        return s[7:].split("@")[0]
    return {"personal": "your own", "user-config": "your config"}.get(s, "this project" if s.startswith("project:") else s)


def sources():
    by = collections.defaultdict(list)
    for r in ROWS:
        by[src(r)].append(r)
    out = []
    for name, items in by.items():
        items = sorted(items, key=lambda r: -r["Tokens"])
        fired = sum(1 for r in items if r["Uses"] > 0)
        dead = sum(r["Tokens"] for r in items if r["Verdict"] == "REAP")
        out.append({"name": name, "items": items, "fired": fired, "rate": fired / len(items), "tok": sum(r["Tokens"] for r in items),
                    "dead": dead, "money": dead * PRICE, "own": any(r["Removable"] for r in items)})
    return sorted(out, key=lambda s: -s["tok"])


SRC = sources()
TOTAL = sum(s["tok"] for s in SRC) or 1


# 6 ─ Inbox: like an email cleaner. A plugin is a sender, firing is opening.
def inbox(w):
    i, cleaned, msg = 0, {}, "demo: nothing is run"
    while True:
        w.erase()
        h, wd = w.getmaxyx()
        saved = sum(SRC[n]["dead"] for n in cleaned)
        put(w, 1, 3, "Your context inbox", curses.A_BOLD)
        put(w, 1, wd - 34, f"cleaned ~{saved:,} tok · ${saved * PRICE:.2f}/mo", rc(HEALTH, 0.95))
        put(w, 2, 3, f"{len(SRC)} senders · {len(ROWS)} items · {sum(s['fired'] for s in SRC)} ever opened in 30 days", DIM())
        put(w, 4, 3, f"{'SENDER':<20}{'OPENED':<26}{'ITEMS':>6}{'TOK':>8}{'/MONTH':>9}", curses.A_UNDERLINE)
        view = h - 14
        top = window(i, view, len(SRC))
        for n, s in enumerate(SRC[top:top + view]):
            y = 5 + n
            idx = top + n
            put(w, y, 1, ">" if idx == i else " ", curses.A_BOLD)
            done = idx in cleaned
            put(w, y, 3, f"{s['name'][:18]:<20}", curses.A_DIM if done else (curses.A_BOLD if idx == i else 0))
            grad(w, y, 23, 16, max(s["rate"], 0.02), HEALTH)
            put(w, y, 40, f"{s['rate'] * 100:>3.0f}%")
            put(w, y, 49, f"{len(s['items']):>6}{s['tok']:>8,}{'$' + format(s['money'], '.2f'):>9}", curses.A_DIM if done else 0)
            if done:
                put(w, y, 73, "cleaned", C(5))
        s = SRC[i]
        put(w, h - 8, 3, "─" * (wd - 6), DIM())
        put(w, h - 7, 3, f"{s['name']}", curses.A_BOLD)
        put(w, h - 6, 3, f"{s['fired']} of {len(s['items'])} items fired · ~{s['dead']:,} tok never used · heaviest: " + ", ".join(r["Name"] for r in s["items"][:3]), DIM())
        put(w, h - 4, 3, " enter ", HL()); put(w, h - 4, 11, "go through them")
        put(w, h - 4, 30, " c ", HL()); put(w, h - 4, 34, "clean up")
        put(w, h - 4, 46, " k ", HL()); put(w, h - 4, 50, "keep")
        put(w, h - 2, 3, f"↑↓ move · q quit   {msg}", DIM())
        w.refresh()
        k = w.getch()
        if k == ord("q"):
            return
        if k == curses.KEY_UP:
            i = max(0, i - 1)
        elif k == curses.KEY_DOWN:
            i = min(len(SRC) - 1, i + 1)
        elif k == ord("c"):
            cleaned[i] = True
            msg = f"demo: would mute or prune every unused item from {s['name']}"
        elif k == ord("k"):
            cleaned.pop(i, None)
        elif k in (10, 13):
            cards(w, s)


def cards(w, s):
    i, kept, gone = 0, 0, 0
    while i < len(s["items"]):
        r = s["items"][i]
        w.erase()
        h, wd = w.getmaxyx()
        pw = min(wd - 8, 66)
        x = (wd - pw) // 2
        put(w, 1, x, f"{s['name']} · {i + 1} of {len(s['items'])}", DIM())
        grad(w, 2, x, pw, (i + 1) / len(s["items"]), WARM)
        put(w, 4, x, "╭" + "─" * (pw - 2) + "╮", DIM())
        for y in range(5, 18):
            put(w, y, x, "│", DIM()); put(w, y, x + pw - 1, "│", DIM())
        put(w, 18, x, "╰" + "─" * (pw - 2) + "╯", DIM())
        put(w, 6, x + 3, r["Name"], curses.A_BOLD)
        put(w, 7, x + 3, f"{r['Category']} · costs ${r['Tokens'] * PRICE:.3f}/month", DIM())
        for n, line in enumerate(textwrap.wrap(r.get("Description") or "no description", pw - 6)[:4]):
            put(w, 9 + n, x + 3, line)
        put(w, 14, x + 3, "weight"); grad(w, 14, x + 11, pw - 24, r["Tokens"] / max(1, s["items"][0]["Tokens"]), WARM); put(w, 14, x + pw - 11, f"{r['Tokens']:>5} tok")
        put(w, 15, x + 3, f"used   {r['Uses']} times in 30 days")
        put(w, 16, x + 3, WHY.get(r.get("Reason"), ""), DIM())
        put(w, 20, x, " ← keep ", C(7)); put(w, 20, x + pw - 11, " clean → ", rc(HEALTH, 0, True))
        put(w, 22, x, f"kept {kept} · cleaned {gone} · esc back · demo: nothing is run", DIM())
        w.refresh()
        k = w.getch()
        if k == 27 or k == ord("q"):
            return
        if k == curses.KEY_LEFT:
            kept, i = kept + 1, i + 1
        elif k == curses.KEY_RIGHT:
            gone, i = gone + 1, i + 1


# 7 ─ Gauge: a health screen, the numbers first.
def gauge(w):
    i = 0
    rows = sorted([r for r in ROWS if r["Verdict"] == "REAP"], key=lambda r: -r["Tokens"])
    vals = [d["sessions"] for d in EXTRA["days"]]
    cache = [d["cache"] for d in EXTRA["days"]]
    while True:
        w.erase()
        h, wd = w.getmaxyx()
        bw = wd - 6
        used = sum(r["Tokens"] for r in ROWS if r["Uses"] > 0)
        put(w, 1, 3, "CONTEXT USE", curses.A_BOLD)
        put(w, 1, 16, f"{used:,} of {TOTAL:,} tok fired", DIM())
        grad(w, 2, 3, bw, used / TOTAL, HEALTH)
        put(w, 4, 3, "DEAD WEIGHT", curses.A_BOLD)
        put(w, 4, 16, f"~{DEAD:,} tok/session", DIM())
        grad(w, 5, 3, bw, DEAD / TOTAL, WARM)
        tiles = [("sessions", str(SESSIONS)), ("messages", f"{M.get('messages', 0):,}"), ("re-read from cache", human(M.get("cache_read_tokens", 0))),
                 ("written to cache", human(M.get("cache_creation_tokens", 0))), ("dead cost/month", f"${MONEY:.2f}")]
        tw = bw // len(tiles)
        for n, (label, val) in enumerate(tiles):
            x = 3 + n * tw
            put(w, 7, x, val, rc(WARM, (n + 1) / len(tiles)) | curses.A_BOLD)
            put(w, 8, x, label[:tw - 1], DIM())
        put(w, 10, 3, "sessions, last 30 days", DIM()); spark(w, 11, 3, vals, WARM)
        put(w, 10, 38, "cache re-read per day", DIM()); spark(w, 11, 38, [int(c / 1e6) for c in cache], HEALTH)
        put(w, 13, 3, "HEAVIEST NEVER FIRED", curses.A_BOLD)
        view = h - 17
        top = window(i, view, len(rows))
        for n, r in enumerate(rows[top:top + view]):
            y = 14 + n
            put(w, y, 3, f"{r['Name'][:30]:<31}", HL() if top + n == i else 0)
            grad(w, y, 35, 22, r["Tokens"] / max(1, rows[0]["Tokens"]), WARM)
            put(w, y, 58, f"{r['Tokens']:>5} tok  ${r['Tokens'] * PRICE:.2f}/mo  {src(r)[:14]}", DIM())
        put(w, h - 1, 3, "↑↓ move · q quit · demo: nothing is run", DIM())
        w.refresh()
        k = w.getch()
        if k == ord("q"):
            return
        if k == curses.KEY_UP:
            i = max(0, i - 1)
        elif k == curses.KEY_DOWN:
            i = min(len(rows) - 1, i + 1)


# 8 ─ Map: weight as area, the bigger block is what matters.
def treemap(w):
    i = 0
    big = SRC[:7]
    while True:
        w.erase()
        h, wd = w.getmaxyx()
        put(w, 0, 2, f"Where {TOTAL:,} tok/session go", curses.A_BOLD)
        mw, mh = wd - 40, h - 4
        tot = sum(s["tok"] for s in big) or 1
        y = 2
        for n, s in enumerate(big):
            bh = max(1, round(s["tok"] / tot * mh))
            dead_w = int(s["dead"] / max(1, s["tok"]) * mw)
            color = rc(WARM, 1 - n / len(big), True)
            for b in range(bh):
                if y + b >= h - 2:
                    break
                put(w, y + b, 2, " " * dead_w, color)
                put(w, y + b, 2 + dead_w, "░" * (mw - dead_w), rc(WARM, 1 - n / len(big)))
            label = f" {'>' if n == i else ' '} {s['name']}  {s['tok']:,} tok "
            put(w, y, 3, label[:mw - 2], color | curses.A_BOLD)
            y += bh
        s = big[i]
        x = mw + 5
        put(w, 2, x, s["name"], curses.A_BOLD)
        put(w, 3, x, f"{s['tok'] * 100 / TOTAL:.0f}% of all your weight", DIM())
        put(w, 5, x, "fired"); grad(w, 5, x + 7, 20, max(s["rate"], 0.02), HEALTH)
        put(w, 6, x, f"{s['fired']} of {len(s['items'])} items", DIM())
        put(w, 8, x, f"dead  ~{s['dead']:,} tok")
        put(w, 9, x, f"cost  ${s['money']:.2f}/month")
        put(w, 11, x, "heaviest", DIM())
        for n, r in enumerate(s["items"][:h - 16]):
            put(w, 12 + n, x, f"{r['Tokens']:>5}  {r['Name'][:26]}")
        put(w, h - 1, 2, "solid = never fired · shaded = alive · ↑↓ move · q quit · demo", DIM())
        w.refresh()
        k = w.getch()
        if k == ord("q"):
            return
        if k == curses.KEY_UP:
            i = max(0, i - 1)
        elif k == curses.KEY_DOWN:
            i = min(len(big) - 1, i + 1)


# 9 ─ Calendar: when things actually happened.
def calendar(w):
    day = 29
    rows = [("sessions", [d["sessions"] for d in EXTRA["days"]]), ("messages", [d["messages"] for d in EXTRA["days"]]),
            ("cache re-read", [d["cache"] for d in EXTRA["days"]]), ("skills fired", [d["fires"] for d in EXTRA["days"]])]
    rows += [(k, v) for k, v in EXTRA["byday"].items()]
    while True:
        w.erase()
        h, wd = w.getmaxyx()
        put(w, 1, 2, "Last 30 days", curses.A_BOLD)
        put(w, 1, 16, f"{SESSIONS} sessions · {sum(1 for d in EXTRA['days'] if d['sessions'])} active days", DIM())
        x0 = 28
        for n in range(30):
            if EXTRA["dates"][n].endswith(("01", "08", "15", "22")) or n == 29:
                put(w, 3, x0 + n * 2, EXTRA["dates"][n][8:], DIM())
        for ri, (name, vals) in enumerate(rows):
            y = 4 + ri + (1 if ri >= 4 else 0)
            put(w, y, 2, name[:25], curses.A_BOLD if ri < 4 else 0)
            top = max(vals) or 1
            for n, v in enumerate(vals):
                attr = rc(WARM if ri < 4 else HEALTH, v / top) if v else curses.A_DIM
                ch = "██" if v else "··"
                put(w, y, x0 + n * 2, ch, attr | (curses.A_REVERSE if n == day and not v else 0))
            put(w, y, x0 + day * 2, "▐▌" if vals[day] else "▏▕", curses.A_BOLD)
        d = EXTRA["days"][day]
        y = 6 + len(rows)
        put(w, y, 2, EXTRA["dates"][day], curses.A_BOLD)
        put(w, y + 1, 2, f"{d['sessions']} sessions · {d['messages']:,} messages · {human(d['cache'])} re-read from cache · {d['fires']} skills fired")
        never = sum(1 for r in ROWS if r["Uses"] == 0)
        put(w, y + 3, 2, f"{never} of {len(ROWS)} items did not fire once in these 30 days.", C(1))
        grad(w, y + 4, 2, wd - 6, (len(ROWS) - never) / len(ROWS), HEALTH)
        put(w, h - 1, 2, "←→ move day · q quit · demo: nothing is run", DIM())
        w.refresh()
        k = w.getch()
        if k == ord("q"):
            return
        if k == curses.KEY_LEFT:
            day = max(0, day - 1)
        elif k == curses.KEY_RIGHT:
            day = min(29, day + 1)


# 10 ─ Receipt: your context as a monthly bill, strike out what you do not want.
def receipt(w):
    i, off = 0, set()
    lines = [s for s in SRC if s["dead"] > 0]
    while True:
        w.erase()
        h, wd = w.getmaxyx()
        pw = 58
        x = max(1, (wd - pw) // 2)
        put(w, 0, x, f"{'SKILLREAPER':^{pw}}", curses.A_BOLD)
        put(w, 1, x, f"{'unused context · last 30 days · ' + str(SESSIONS) + ' sessions':^{pw}}", DIM())
        put(w, 2, x, "┄" * pw, DIM())
        view = h - 14
        top = window(i, view, len(lines))
        for n, s in enumerate(lines[top:top + view]):
            idx = top + n
            struck = idx in off
            text = f"{s['name'][:22]:<23}{len(s['items']):>4} items{s['dead']:>8,} tok{'$' + format(s['money'], '.2f'):>9}"
            a = HL() if idx == i else (curses.A_DIM if struck else 0)
            if struck:
                text = "".join(c + "̶" for c in text[:pw])
            put(w, 3 + n, x, text, a)
        total = sum(s["money"] for s in lines)
        saved = sum(lines[n]["money"] for n in off)
        savedtok = sum(lines[n]["dead"] for n in off)
        y = h - 10
        put(w, y, x, "┄" * pw, DIM())
        put(w, y + 1, x, f"{'re-read from cache, all sessions':<40}{human(M.get('cache_read_tokens', 0)):>18}", DIM())
        put(w, y + 2, x, f"{'unused context':<40}{'$' + format(total, '.2f') + '/mo':>18}")
        put(w, y + 3, x, f"{'struck out':<40}{'-$' + format(saved, '.2f') + '/mo':>18}", rc(HEALTH, 0.95))
        put(w, y + 4, x, f"{'YOU WOULD PAY':<40}{'$' + format(total - saved, '.2f') + '/mo':>18}", curses.A_BOLD)
        grad(w, y + 6, x, pw, saved / total if total else 0, HEALTH)
        put(w, y + 7, x, f"{savedtok:,} tok/session saved", DIM())
        put(w, h - 1, x, "↑↓ move · space strike out · q quit · demo: nothing is run", DIM())
        w.refresh()
        k = w.getch()
        if k == ord("q"):
            return
        if k == curses.KEY_UP:
            i = max(0, i - 1)
        elif k == curses.KEY_DOWN:
            i = min(len(lines) - 1, i + 1)
        elif k == ord(" "):
            off ^= {i}


VERSIONS = {"6": inbox, "7": gauge, "8": treemap, "9": calendar, "10": receipt}


def main(w, fn):
    setup()
    w.keypad(True)
    fn(w)


if __name__ == "__main__":
    if len(sys.argv) < 2 or sys.argv[1] not in VERSIONS:
        sys.exit(f"usage: {sys.argv[0]} {{{'|'.join(VERSIONS)}}}")
    os.environ.setdefault("ESCDELAY", "25")
    curses.wrapper(main, VERSIONS[sys.argv[1]])
