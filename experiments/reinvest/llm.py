"""What reinvest asks Claude for: group sessions, check coverage, verify a candidate, write a skill."""
from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
import sys
import tempfile

from sessions import canon, cut

MODEL = "sonnet"
# A clean `claude -p`: no user hooks, plugins, MCP servers, skills or CLAUDE.md (they cost ~25 s per call and
# leak persona text into the answer), and no saved session, so reinvest never reads its own calls back.
LEAN = ["--setting-sources", "", "--strict-mcp-config", "--tools", "", "--disable-slash-commands",
        "--no-session-persistence", "--system-prompt",
        "You do exactly what the request asks and reply only in the requested format."]
THINK_TOKENS = "16000"  # verification needs it: without, skills written for someone else's repo get through
FAILED = "verification failed"
DOC_CHARS = 15000       # how much of a SKILL.md / README the verifier reads (almost always all of it)


def ask_json(prompt: str, body: str, think: bool = True, effort: str | None = None):
    """Run one `claude -p` call and parse the JSON in its answer (one retry). None if it never parses."""
    claude = shutil.which("claude")
    if not claude:
        sys.exit("reinvest needs the `claude` CLI on PATH.")
    cmd = [claude, "-p", prompt, *LEAN, "--model", MODEL] + (["--effort", effort] if effort else [])
    # --setting-sources "" also drops the thinking setting, so it is chosen here, per call
    env = {**os.environ, "MAX_THINKING_TOKENS": THINK_TOKENS if think else "0"}
    for _ in range(2):
        out = subprocess.run(cmd, input=body, capture_output=True, text=True, timeout=900,
                             cwd=tempfile.gettempdir(), env=env).stdout
        # the outermost bracket first: an object holding a list must not be cut down to the list
        for open_, close in sorted((("[", "]"), ("{", "}")), key=lambda p: out.find(p[0]) % (len(out) + 1)):
            i, j = out.find(open_), out.rfind(close)
            if i >= 0 and j > i:
                try:  # fast models sometimes leave a trailing comma
                    return json.loads(re.sub(r",\s*([\]}])", r"\1", out[i:j + 1]))
                except ValueError:
                    pass
    return None


def words(text: str) -> set[str]:
    return set(re.findall(r"[a-z]{4,}", text.lower()))


def closest_installed(text: str, installed: list[dict], n: int) -> list[dict]:
    """Installed skills sharing the most words with `text`: names alone mislead (agent-sort sorts plugins, not files)."""
    tw = words(text)
    return sorted(installed, key=lambda s: -len(tw & words(s["name"] + " " + s["desc"])))[:n]


def skill_lines(skills: list[dict], width: int = 160) -> str:
    return "\n".join(f"- {s['name']}: {cut(s['desc'], width)}" for s in skills) or "none"


# ---------------------------------------------------------------------------
# 1. Kinds of work
# ---------------------------------------------------------------------------
# Embeddings group prompts by project or by phrasing, never by kind of work (tried thresholds 0.55-0.75,
# with and without centering). One call over all sessions does, and returns ids, so counts are exact.

GROUP_PROMPT = (
    "Each line is the start of one conversation a developer had with an AI coding assistant, with its id. "
    "Group the conversations by the KIND of work they ask for, NOT by project: different projects asking for the "
    "same kind of work (e.g. releasing a new version) belong together; the same project asking for different kinds "
    "of work belongs apart. A group that fits only one project is wrong: merge it into the general kind of work it "
    "is an instance of (fixing reported UI bugs, releasing, editing a website...). Each conversation goes in at "
    "most one group; leave out conversations that fit no recurring kind of work. For each group give: 'name': 2-4 "
    "words naming the kind of work, no project names; 'sessions': the ids; 'delegable': true only if a reusable "
    "agent skill could take over this work (not chit-chat, one-off questions, generic 'continue'); 'task': one "
    "sentence describing the work the way a reusable skill would (inputs, steps, output), with no names of the "
    "user's OWN projects but KEEPING names of public platforms, stores, languages and tools (Raycast Store, Chrome "
    "Web Store, Homebrew, Figma, Swift); 'queries': 5 short search queries (1-3 words) phrased like the NAME a skill "
    "for this work would have (e.g. 'file organizer', 'bug triage'), specific to broad; 'mcp_queries': 3 single "
    "words naming the app, platform or resource an MCP server for this work would connect to (e.g. 'figma', "
    "'filesystem', 'wifi'). Reply with ONLY a JSON array: [{\"name\": \"...\", \"sessions\": [0, 4], "
    "\"delegable\": true, \"task\": \"...\", \"queries\": [\"...\"], \"mcp_queries\": [\"...\"]}].")


ASSIGN_PROMPT = (
    "Below are the KINDS of work a developer already does with an AI coding assistant (G0, G1...), then new "
    "conversations, each with its id. Put each new conversation in the existing kind of work it asks for. When "
    "several new conversations share a kind of work no existing one covers, make a new group for them. Group "
    "them " + GROUP_PROMPT.split("Reply with ONLY")[0].split("Group the conversations ")[1] +
    "Reply with ONLY a JSON object: {\"assign\": {\"<conversation id>\": <G number without the G>}, "
    "\"new\": [{\"name\": \"...\", \"sessions\": [3], \"delegable\": true, \"task\": \"...\", "
    "\"queries\": [\"...\"], \"mcp_queries\": [\"...\"]}]}.")


def numbered(sessions: list[dict]) -> str:
    return "\n".join(f"[{i}] {cut(' | '.join(s['prompts'][:3]), 500)}" for i, s in enumerate(sessions))


def new_groups(out, sessions: list[dict], taken: set[int]) -> list[dict]:
    """Groups from the model's answer, by session id, not position: new sessions shift positions."""
    groups = []
    for g in out if isinstance(out, list) else []:
        if not isinstance(g, dict):
            continue
        ids = sorted({int(i) for i in g.get("sessions", []) if str(i).isdigit() and int(i) < len(sessions)} - taken)
        taken |= set(ids)
        if ids:
            groups.append({**{k: v for k, v in g.items() if k != "sessions"},
                           "session_ids": [sessions[i]["id"] for i in ids]})
    return groups


def group_sessions(sessions: list[dict]) -> list[dict]:
    # without extended thinking: 235 s -> ~20 s, same groups
    return new_groups(ask_json(GROUP_PROMPT, numbered(sessions), think=False, effort="medium"), sessions, set())


def assign_sessions(groups: list[dict], new: list[dict]) -> list[dict] | None:
    """`groups` with `new` sessions placed in them, or in new groups. None if the call never answered, so the
    caller can retry those sessions next run; a session placed nowhere was judged and is not sent again."""
    kinds = "\n".join(f"[G{i}] {g.get('name', '')}: {g.get('task', '')}" for i, g in enumerate(groups))
    out = ask_json(ASSIGN_PROMPT, f"Kinds of work:\n{kinds}\n\nNew conversations:\n{numbered(new)}",
                   think=False, effort="medium")
    if not isinstance(out, dict):
        return None
    placed, taken = [{**g, "session_ids": list(g["session_ids"])} for g in groups], set()
    for k, gi in (out.get("assign") or {}).items():
        if str(k).isdigit() and int(k) < len(new) and isinstance(gi, int) and 0 <= gi < len(placed) \
                and int(k) not in taken:
            taken.add(int(k))
            placed[gi]["session_ids"].append(new[int(k)]["id"])
    return placed + new_groups(out.get("new"), new, taken)


# ---------------------------------------------------------------------------
# 2. Coverage by an installed skill
# ---------------------------------------------------------------------------

COVER_PROMPT = (
    "For each numbered kind of work below, the developer's installed skills that look closest are listed with what "
    "they do. Answer with the name of the installed skill that ALREADY does this kind of work well enough to be "
    "used for it, or null if none does (a skill for a different platform, a narrow part, or only similar wording "
    "does not count). Reply with ONLY a JSON object mapping each number to a name or null.")


def covering_skills(kinds: list[dict], installed: list[dict]) -> list[str | None]:
    if not kinds:
        return []
    body = "\n\n".join(f"[{i}] {k['task']}\n" + skill_lines(
        closest_installed(k["task"] + " " + " ".join(k["examples"]), installed, 12))
        for i, k in enumerate(kinds))
    out = ask_json(COVER_PROMPT, body, think=False, effort="medium")
    out = out if isinstance(out, dict) else {}
    names = {canon(s["name"]): s["name"] for s in installed}
    return [names.get(canon(str(out.get(str(i)) or ""))) for i in range(len(kinds))]


# ---------------------------------------------------------------------------
# 3. Verification of one candidate
# ---------------------------------------------------------------------------
# One call per (task, candidate), in parallel. A single batched choice among ~20 candidates was slower and let
# through a skill tuned to another company's repo, a Codex-only skill and a paid-service ad.

VERIFY_PROMPT = (
    "You check ONE agent skill or MCP server proposed to a developer who uses Claude Code, for the TASK below. "
    "Read the whole SKILL.md or README. Reject it if ANY of these is true: it is written for one specific other "
    "project or codebase (its own file names, commands, CI job names, internal tools); it is written for another "
    "AI agent (Codex, Cursor, OpenAI tools) or relies on skills/tools it does not include; it needs an account or "
    "a paid third-party service, or advertises a product or paid upgrade; it targets a different platform than the "
    "TASK; it is thin or generic advice an assistant would follow anyway; it only wraps what the assistant already "
    "does with standard command-line tools (gh, git, curl, the filesystem); it does not actually do the TASK, or "
    "covers only a narrow special case of it (e.g. only invoices when the TASK is organizing any files); it does "
    "the same job as a skill the developer already uses or has installed (judge by what the closest installed "
    "skills DO, not by similar names); for an MCP server, it can run arbitrary commands, code or scripts (shell, "
    "AppleScript, eval), or read and write files anywhere rather than in folders the developer chooses. Otherwise "
    "keep it. Reply with ONLY a JSON object: {\"keep\": true or false, \"reason\": \"one short sentence\", "
    "\"use\": \"if kept: one short sentence saying what it does in this work, addressed to the developer\", "
    "\"fit\": if kept, how completely it does the TASK, 1 (a small part) to 5 (all of it), else 0}.")


def verify(task: str, doc: str, used: list[dict], installed: list[dict], own: bool = False) -> dict:
    # a skill written from the developer's own sessions names their own tools and habits on purpose
    origin = ("ORIGIN: written for this developer from their own conversations; naming their own tools, projects "
              "and conventions is expected. Reject it if any step or rule holds for only ONE of their projects (that "
              "project's domain, features or files) instead of the whole kind of work.\n\n" if own else "")
    body = (f"{origin}TASK: {task}\n\nSkills the developer already uses:\n{skill_lines(used, 120)}\n\n"
            f"Installed skills closest to this one (read what they do, not just the name):\n"
            f"{skill_lines(closest_installed(doc[:3000], installed, 8))}\n\nDOCUMENT:\n{doc[:DOC_CHARS]}")
    out = ask_json(VERIFY_PROMPT, body, effort="high")
    return out if isinstance(out, dict) and "keep" in out else {"keep": False, "reason": FAILED}


# ---------------------------------------------------------------------------
# 4. A skill written from the developer's own sessions
# ---------------------------------------------------------------------------

WRITE_PROMPT = (
    "Below is a kind of work a developer does again and again with Claude Code, and the messages they wrote in "
    "several of those conversations, in order. Write ONE Claude Code skill that takes this work over for THIS "
    "developer. Take from the messages: the steps that repeat, and every correction, preference or rule they had "
    "to say more than once (what not to do without asking, language, format, checks before calling it done); "
    "those become explicit rules. Keep it general across their projects: every step and rule must hold for ANY "
    "project of this kind, so drop what came from one of them (its domain, e.g. chess moves in a chess extension; "
    "its features, file names, paths). Leave out what Claude Code already does unasked. Do not repeat what the installed skills listed "
    "below already do. 'name': lowercase words joined by hyphens, 2-4 words, the kind of work. 'description': "
    "one or two sentences, at most 300 characters (it is loaded in every session): what it does and the "
    "situations that should trigger it, in the developer's own words where possible. 'body': the skill in "
    "Markdown, at most 60 lines: when to use it, numbered steps, rules, how to check it is done. Reply with ONLY "
    "a JSON object: {\"name\": \"...\", \"description\": \"...\", \"body\": \"...\"}.")
SLUG_RE = re.compile(r"[a-z0-9]+(-[a-z0-9]+){0,5}")
MAX_DESC = 400  # the prompt asks for 300; a little over is fine, a page is not


def write_skill(gap: dict, conversations: list[list[str]], installed: list[dict]) -> tuple[str, str] | None:
    """(slug, SKILL.md) for one gap, or None if the answer is unusable. The frontmatter is built here, not by
    the model, so it always parses: the description is a JSON string, which is valid YAML."""
    convs = "\n\n".join(f"Conversation {i + 1}:\n" + "\n".join(f"- {cut(p, 400)}" for p in ps[:10])
                        for i, ps in enumerate(conversations))
    body = (f"KIND OF WORK: {gap['name']}\nTASK: {gap['task']}\n\nInstalled skills closest to this work:\n"
            f"{skill_lines(closest_installed(gap['task'], installed, 8))}\n\n{convs}")
    out = ask_json(WRITE_PROMPT, body, effort="high")
    if not isinstance(out, dict):
        return None
    slug, desc, text = (str(out.get(k) or "").strip() for k in ("name", "description", "body"))
    if not SLUG_RE.fullmatch(slug) or not 40 <= len(desc) <= MAX_DESC or len(text) < 200:
        return None
    return slug, f"---\nname: {slug}\ndescription: {json.dumps(desc, ensure_ascii=False)}\n---\n\n{text}\n"
