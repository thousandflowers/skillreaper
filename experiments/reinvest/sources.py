"""Where candidates come from: skills.sh search and the official MCP registry, plus their SKILL.md / README."""
from __future__ import annotations

import json
import os
import re
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

from sessions import canon, clean

SKILLS_SH = "https://skills.sh"
MCP_REGISTRY = "https://registry.modelcontextprotocol.io/v0/servers"
def _cache_base() -> Path:
    """The user cache directory as Go's os.UserCacheDir picks it, so reap reads what reinvest leaves here."""
    if sys.platform == "darwin":
        return Path.home() / "Library" / "Caches"
    if os.name == "nt":
        return Path(os.environ.get("LocalAppData") or Path.home() / "AppData" / "Local")
    return Path(os.environ.get("XDG_CACHE_HOME") or Path.home() / ".cache")


CACHE = _cache_base() / "skillreaper" / "reinvest"
SEARCH_TTL = 86400  # skills.sh rate-limits hard (429): reuse searches for a day
PER_QUERY = 12
MCP_PER_TASK = 5
# Where a repo keeps a skill; the skills CLI reads SKILL.md from raw.githubusercontent.com the same way.
SKILL_PATHS = ["skills/{s}/SKILL.md", "{s}/SKILL.md", ".claude/skills/{s}/SKILL.md", ".agents/skills/{s}/SKILL.md",
               "skill/{s}/SKILL.md", "SKILL.md"]
RISKY = [
    (r"curl[^\n|]{0,200}\|\s*(ba|z)?sh", "downloads and runs code"),
    (r"wget[^\n|]{0,200}\|\s*(ba|z)?sh", "downloads and runs code"),
    (r"ignore (all )?(previous|prior) instructions", "tries to override instructions"),
    (r"base64\s+(-d|--decode)", "decodes hidden content"),
    (r"(webhook|pastebin|ngrok|requestbin)", "may send data out"),
    (r"(\.env|id_rsa|\.ssh/|keychain|api[_-]?key)[^\n]{0,80}(curl|post|upload|send|fetch)", "may send secrets out"),
    (r"rm\s+-rf\s+(/|~)(\s|$)", "destructive command"),
    (r"xattr\s+-c", "bypasses macOS quarantine"),
]
# An MCP server's tools run on every call without the agent reading them first: one that runs any command or
# script hands the model a shell under a friendlier name. Matched against tool names and capability phrases.
MCP_RISKY = [
    (r"\b(run|execute|exec)_(command|shell|script|applescript|jxa|code|terminal|process)s?\b",
     "has a tool that runs any command or script"),
    (r"\b(shell_exec|exec_command|run_shell|eval_code)\b", "has a tool that runs any command or script"),
    (r"\b(execute|run)s? (arbitrary|any|shell|terminal|system) (commands?|code|scripts?)",
     "runs arbitrary commands"),
    (r"\b(execute|run)s? (commands or )?applescript|applescript (execution|runner)",
     "runs arbitrary AppleScript"),
]

SKILLSSH_TRIES = 4      # a 429 waits Retry-After and retries, instead of giving up on skills.sh for the run
SKILLSSH_MAX_WAIT = 30  # seconds; longer Retry-After values are capped
_skillssh = threading.Lock()  # one skills.sh request at a time: parallel ones are what trips the rate limit


def http_get(url: str, timeout: int = 15) -> bytes:
    req = urllib.request.Request(url, headers={"User-Agent": "skillreaper-reinvest/0.1"})
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return r.read()


def http_json(url: str, tries: int = 3, retry_on: tuple[int, ...] = (429, 500, 502, 503, 504), max_wait: int = 8):
    """GET JSON, waiting out the given statuses (Retry-After, else backoff). Raises what the last try raised."""
    for attempt in range(tries):
        try:
            return json.loads(http_get(url).decode("utf-8"))
        except urllib.error.HTTPError as e:
            if e.code not in retry_on or attempt == tries - 1:
                raise
            try:
                wait = int(e.headers.get("Retry-After") or 0)
            except ValueError:  # Retry-After may be an HTTP date
                wait = 0
            time.sleep(min(wait or 2 ** (attempt + 1), max_wait))


def frontmatter(text: str) -> dict:
    """The YAML frontmatter of a SKILL.md, flat keys only (enough for name/description)."""
    lines = text.splitlines()
    if not lines or lines[0].strip() != "---":
        return {}
    out, key, buf = {}, None, []
    for line in lines[1:]:
        if line.strip() == "---":
            break
        m = re.match(r"^([A-Za-z_-]+):\s*(.*)$", line)
        if m and not line[:1].isspace():
            if key:
                out[key] = " ".join(buf).strip()
            key, val = m.group(1), m.group(2).strip()
            buf = [] if val in (">", "|", ">-", "|-", ">+", "|+", "") else [val.strip("\"'")]
        elif key and line[:1].isspace():
            buf.append(line.strip().strip("\"'"))
    if key:
        out[key] = " ".join(buf).strip()
    return out


def risks(doc: str, mcp: bool = False) -> list[str]:
    low = doc.lower()
    return sorted({why for pattern, why in RISKY + (MCP_RISKY if mcp else []) if re.search(pattern, low)})


def mcp_where(server: dict) -> str:
    """'runs locally' for a package, 'remote, <host>' for a hosted server: a remote one sees what you send it."""
    if server.get("packages"):
        return "runs locally"
    hosts = {urllib.parse.urlparse(r.get("url", "")).hostname for r in server.get("remotes") or []} - {None}
    return f"remote, {', '.join(sorted(hosts))}" if hosts else "unknown origin"


# ---------------------------------------------------------------------------
# skills.sh
# ---------------------------------------------------------------------------

def search_skills(queries: list[str], skip_names: set[str]) -> list[dict]:
    """Search results only (no SKILL.md yet): name, repo, installs and each query's rank."""
    CACHE.mkdir(parents=True, exist_ok=True)
    found = {}
    for q in queries:
        f = CACHE / f"search_{re.sub(r'[^a-z0-9]+', '_', q.lower())}.json"
        if f.exists() and time.time() - f.stat().st_mtime < SEARCH_TTL:
            data = json.loads(f.read_text(encoding="utf-8"))
        else:
            try:
                data = _skillssh_json(f"{SKILLS_SH}/api/search?" + urllib.parse.urlencode({"q": q, "limit": PER_QUERY}))
            except Exception as e:  # noqa: BLE001  (a failed query just contributes nothing)
                print(f"skills.sh search {q!r}: {e}")
                continue
            f.write_text(json.dumps(data), encoding="utf-8")
        for rank, sk in enumerate(data.get("skills", [])):
            item = found.setdefault(sk["id"], {
                "id": sk["id"], "kind": "skill", "source": sk.get("source", ""), "slug": sk.get("skillId", ""),
                "name": sk.get("name", sk.get("skillId", "")), "installs": int(sk.get("installs") or 0), "hits": {}})
            item["hits"][q] = rank
    return [it for it in found.values() if canon(it["name"]) not in skip_names]


def skill_doc(item: dict) -> str:
    """The full SKILL.md, cached for a day: GitHub raw first (a CDN, no rate limit), skills.sh as fallback."""
    f = CACHE / f"doc_{re.sub(r'[^a-z0-9]+', '_', item.get('id', '').lower())}.md"
    if f.exists() and time.time() - f.stat().st_mtime < SEARCH_TTL:
        return f.read_text(encoding="utf-8")
    doc = _fetch_skill_doc(item)
    if doc:  # a miss may be a transient failure: not cached
        CACHE.mkdir(parents=True, exist_ok=True)
        f.write_text(doc, encoding="utf-8")
    return doc


def _skillssh_json(url: str):
    """GET skills.sh one request at a time, waiting out 429s."""
    with _skillssh:
        return http_json(url, SKILLSSH_TRIES, (429,), SKILLSSH_MAX_WAIT)


def _fetch_skill_doc(item: dict) -> str:
    src, slug = item.get("source", ""), item.get("slug", "")
    if src.count("/") == 1 and "." not in src.split("/")[0]:
        for pattern in SKILL_PATHS:
            try:
                doc = http_get(f"https://raw.githubusercontent.com/{src}/HEAD/"
                               + pattern.format(s=urllib.parse.quote(slug)), timeout=10).decode("utf-8", "replace")
            except Exception:  # noqa: BLE001
                continue
            meta = frontmatter(doc)
            # a repo-root SKILL.md only counts if it is this skill, not another one in a multi-skill repo
            if meta.get("description") and (pattern != "SKILL.md" or canon(meta.get("name", "")) == canon(slug)):
                return doc
    owner, _, repo = src.partition("/")
    try:
        data = _skillssh_json(f"{SKILLS_SH}/api/download/{owner}/{repo}/{urllib.parse.quote(slug)}")
    except Exception as e:  # noqa: BLE001  (logged: a silent miss is what hid the 429s)
        print(f"skills.sh download {item.get('id')}: {e}")
        return ""
    return next((x["contents"] for x in data.get("files", []) if x.get("path", "").endswith("SKILL.md")), "")


# ---------------------------------------------------------------------------
# MCP registry
# ---------------------------------------------------------------------------

def readme(repo_url: str) -> str:
    m = re.match(r"https?://github\.com/([^/]+/[^/#?]+)", repo_url or "")
    if not m:
        return ""
    for name in ("README.md", "readme.md"):
        try:
            return http_get(f"https://raw.githubusercontent.com/{m.group(1).removesuffix('.git')}/HEAD/{name}",
                            timeout=10).decode("utf-8", "replace")
        except Exception:  # noqa: BLE001
            continue
    return ""


def search_mcp(words: list[str]) -> list[dict]:
    """MCP servers for these words (the registry search is literal: single words work, phrases don't)."""
    found = {}
    for w in words:
        try:
            data = http_json(f"{MCP_REGISTRY}?" + urllib.parse.urlencode({"search": w, "limit": 20}), tries=2)
        except Exception:  # noqa: BLE001
            continue
        for x in data.get("servers", []):
            server = x.get("server", {})
            found.setdefault(server.get("name"), server)  # the registry lists every version: keep the first
    servers = [s for n, s in found.items() if n][: MCP_PER_TASK * 2]
    with ThreadPoolExecutor(8) as ex:
        docs = list(ex.map(lambda s: readme((s.get("repository") or {}).get("url", "")), servers))
    return [{"id": s["name"], "kind": "mcp", "name": s.get("title") or s["name"].split("/")[-1],
             "desc": clean(s.get("description", "")), "server": s, "installs": 0, "doc": doc}
            for s, doc in zip(servers, docs) if doc][:MCP_PER_TASK]
