"""Read the Claude Code sessions a person actually wrote, with per-session stats."""
from __future__ import annotations

import json
import re
from datetime import datetime, timedelta, timezone
from pathlib import Path

PROJECTS = Path.home() / ".claude" / "projects"
TOOL_DIR = Path(__file__).resolve().parent
LIVE_MINUTES = 10  # a transcript written this recently is a session still open (e.g. the one running reinvest)

NOISE_PREFIX = ("<task-notification", "<bash-input", "<bash-stdout", "<bash-stderr", "<local-command",
                "Caveat:", "<command-message", "<user-prompt-submit-hook", "[Request interrupted")
COMMAND_RE = re.compile(r"<command-name>\s*/?([^<\s]+)\s*</command-name>")
REMINDER_RE = re.compile(r"<system-reminder>.*?</system-reminder>", re.S)


def clean(text: str) -> str:
    return re.sub(r"\s+", " ", REMINDER_RE.sub("", text or "")).strip()


def cut(text: str, n: int) -> str:
    text = clean(text)
    return text if len(text) <= n else text[: n - 1] + "…"


def canon(name: str) -> str:
    """'plugin:skill' and '/skill' both become 'skill'."""
    return name.strip().lstrip("/").split(":")[-1].lower()


def blocks(entry: dict) -> list[dict]:
    content = (entry.get("message") or {}).get("content")
    if isinstance(content, str):
        return [{"type": "text", "text": content}]
    return [b for b in content if isinstance(b, dict)] if isinstance(content, list) else []


def user_text(entry: dict) -> str | None:
    """Text the person typed, or None for tool results, meta and sidechain entries."""
    if entry.get("type") != "user" or entry.get("isMeta") or entry.get("isSidechain"):
        return None
    parts = []
    for b in blocks(entry):
        if b.get("type") == "tool_result":
            return None
        if b.get("type") == "text":
            parts.append(b.get("text", ""))
    return "\n".join(parts).strip() or None


def is_automated(entries: list[dict]) -> bool:
    """Not written by a person: launched with `claude -p` / the SDK (entrypoint "sdk-*"),
    or opened inside this tool's folder (work on the tool is not the user's work)."""
    first = next((e for e in entries if e.get("type") == "user" and not e.get("isMeta")), None)
    if first is None or str(first.get("entrypoint", "")).startswith("sdk"):
        return True
    cwd = first.get("cwd")
    return bool(cwd) and Path(cwd).resolve().is_relative_to(TOOL_DIR)


def last_activity(entries: list[dict]) -> datetime | None:
    """When the session was last used: its newest timestamped entry."""
    for e in reversed(entries):
        try:
            return datetime.fromisoformat(e["timestamp"].replace("Z", "+00:00"))
        except (KeyError, AttributeError, ValueError):
            continue
    return None


def read_transcripts(days: int):
    now = datetime.now(timezone.utc)
    oldest, newest = now - timedelta(days=days), now - timedelta(minutes=LIVE_MINUTES)
    for path in sorted(PROJECTS.rglob("*.jsonl")):
        if "subagents" in path.parts:  # subagent transcripts hold no user prompts
            continue
        try:
            # mtime only rules out old files: Claude Code later appends untimestamped metadata lines
            # ("atis-latch", "pr-link") to old transcripts in batches, so mtime says nothing about activity.
            if datetime.fromtimestamp(path.stat().st_mtime, tz=timezone.utc) < oldest:
                continue
            entries = []
            with path.open(encoding="utf-8", errors="replace") as f:
                for line in f:
                    try:
                        entries.append(json.loads(line))
                    except json.JSONDecodeError:
                        continue
        except OSError:
            continue
        last = last_activity(entries)
        if last and oldest <= last <= newest and not is_automated(entries):
            yield path, entries


def read_sessions(days: int, skill_names: set[str]) -> list[dict]:
    """One dict per human session: prompts, tokens, friction signals and the skills it invoked."""
    out = []
    for path, entries in read_transcripts(days):
        s = {"id": path.stem, "prompts": [], "tools": 0, "errors": 0, "interrupts": 0,
             "tokens": 0, "skills": set(), "ts": ""}
        for e in entries:
            s["ts"] = s["ts"] or e.get("timestamp", "")
            raw = user_text(e)
            if raw is not None:
                if raw.startswith("[Request interrupted"):
                    s["interrupts"] += 1
                elif m := COMMAND_RE.search(raw):
                    if canon(m.group(1)) in skill_names:
                        s["skills"].add(canon(m.group(1)))
                elif not raw.lstrip().startswith(NOISE_PREFIX) and len(clean(raw)) >= 12:
                    s["prompts"].append(clean(raw))
            elif e.get("type") == "user":
                s["errors"] += sum(1 for b in blocks(e) if b.get("type") == "tool_result" and b.get("is_error"))
            elif e.get("type") == "assistant":
                u = (e.get("message") or {}).get("usage") or {}
                s["tokens"] += sum(int(u.get(k) or 0) for k in
                                   ("input_tokens", "output_tokens", "cache_creation_input_tokens"))
                for b in blocks(e):
                    if b.get("type") == "tool_use":
                        s["tools"] += 1
                        if b.get("name") == "Skill":
                            inp = b.get("input") or {}
                            s["skills"].add(canon(inp.get("skill") or inp.get("command") or inp.get("name") or "?"))
        if s["prompts"]:
            out.append(s)
    return out


def week(ts: str) -> str:
    try:
        y, w, _ = datetime.fromisoformat(ts.replace("Z", "+00:00")).isocalendar()
        return f"{y}-{w:02d}"
    except ValueError:
        return "?"
