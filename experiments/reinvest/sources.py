"""Where candidates come from: skills.sh search and the official MCP registry, plus their SKILL.md / README."""
from __future__ import annotations

import hashlib
import html
import json
import os
import re
import sys
import threading
import time
import unicodedata
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


def default_state_dir() -> Path:
    """Where what cannot be fetched again lives. A cache folder can be emptied at any time, by the system or a cleaner."""
    if sys.platform == "darwin":
        return Path.home() / "Library" / "Application Support" / "skillreaper" / "reinvest"
    if os.name == "nt":
        return Path(os.environ.get("AppData") or Path.home() / "AppData" / "Roaming") / "skillreaper" / "reinvest"
    return Path(os.environ.get("XDG_DATA_HOME") or Path.home() / ".local" / "share") / "skillreaper" / "reinvest"


STATE = default_state_dir()
SEARCH_TTL = 7 * 86400  # skills.sh rate-limits hard (429): reuse searches and documents for a week; install compares again
PER_QUERY = 12
MCP_PER_TASK = 5
MAX_RESPONSE = 5 * 1024 * 1024  # bytes read from any one answer; a bigger one fails to parse and is skipped
# What may reach a command line: anything else is dropped where it comes in, so it can never become a flag or a URL.
SOURCE_RE = re.compile(r"[a-z0-9][a-z0-9-]*/[a-z0-9_][\w.-]*", re.I)  # owner/repo
SLUG_RE = re.compile(r"[a-z0-9][\w.-]*", re.I)
MCP_NAME_RE = re.compile(r"[a-z0-9][\w.-]*(/[a-z0-9][\w.-]*)*", re.I)
PACKAGE_ID_RE = re.compile(r"(@[a-z0-9][\w.-]*/)?[a-z0-9][\w.-]*", re.I)
VERSION_RE = re.compile(r"[0-9][\w.+-]*")
SUBFOLDER_RE = re.compile(r"[\w-][\w.-]*(/[\w-][\w.-]*)*")
# https only, and a shape every URL parser reads the same way: no userinfo, no backslash, no control characters.
REMOTE_RE = re.compile(r"https://[a-z0-9.-]+(:\d+)?(/[\w.~%!$&'()*+,;=:@/?#-]*)?", re.I)
# How each package type is run, pinned to the version the registry lists, and where its own metadata lives.
PACKAGES = {"npm": {"runner": ["npx", "-y"], "pin": "{id}@{v}", "meta": "https://registry.npmjs.org/{id}/{v}"},
            "pypi": {"runner": ["uvx"], "pin": "{id}=={v}", "meta": "https://pypi.org/pypi/{id}/{v}/json"}}
TRANSPORTS = {"streamable-http": "http", "sse": "sse"}
OFFICIAL_META = "io.modelcontextprotocol.registry/official"
FILE_MARK = "===== FILE {} ====="  # how each bundled file appears after SKILL.md in what the verifier reads
SKILL_FILE = "SKILL.md"
FETCH = r"(?:curl|wget|iwr|invoke-webrequest)\b"
SH = r"(?:ba|z|k|da|fi)?sh"
SECRETS = [r"sk-ant-[\w-]{20,}", r"\bsk-[a-z0-9]{32,}", r"\bgh[pousr]_[a-z0-9]{30,}", r"\bgithub_pat_\w{20,}",
           r"\bakia[a-z0-9]{16}\b", r"\bxox[abprs]-[\w-]{10,}", r"-----begin [a-z ]*private key-----"]
RISKY = [
    (rf"{FETCH}[^\n]{{0,200}}\|[^\n]{{0,80}}(?<![\w.-]){SH}\b", "downloads and runs code"),
    (rf"{FETCH}[^\n]{{0,200}}\|\s*(?:sudo\s+)?(?:python3?|perl|ruby|node)\s*(?:-\s*)?$", "downloads and runs code"),
    (rf"\b{SH}\s+(?:-c\s+)?[\"']?(?:<\(|\$\()\s*{FETCH}", "downloads and runs code"),
    (rf"\beval\b[^\n]{{0,20}}\$\(\s*{FETCH}", "downloads and runs code"),
    (rf"{FETCH}[^\n]{{0,200}}(?:\s-o\s*|\s-O\s*|>\s*)\S+\.sh\b[\s\S]{{0,300}}(?:\b{SH}\s+\S*\.sh|chmod\s+\+x)",
     "downloads and runs code"),
    (r"(?:ignore|disregard|forget|override)\s+(?:(?:all|any|of|the|your|my)\s+){0,3}"
     r"(?:previous|prior|above|earlier|system)\s+(?:instructions|rules|prompts?)", "tries to override instructions"),
    (r"ignora\s+(?:(?:tutte|le)\s+){0,2}istruzioni\s+(?:precedenti|sopra)", "tries to override instructions"),
    (r"[\"']risk[\"']\s*:[\s\S]{0,400}[\"']keep[\"']\s*:|[\"']keep[\"']\s*:[\s\S]{0,400}[\"']risk[\"']\s*:",
     "carries a ready-made verdict for its own check"),
    (r"base64\s+(?:-d|--decode)|b64decode|openssl\s+(?:enc\s+|base64\s+)[^\n]{0,40}-d\b", "decodes hidden content"),
    (r"(pastebin|ngrok|requestbin)", "may send data out"),
    # the word alone is common and harmless, and Claude reads the rest: only sending to one counts here
    (r"\b(post|send|upload|forward|curl|wget|push)\b[^\n]{0,80}webhook", "may send data out"),
    (r"(\.env|id_rsa|\.ssh/|keychain|api[_-]?key)[^\n]{0,80}(curl|post|upload|send|fetch)", "may send secrets out"),
    (r"\b(?:curl|wget|scp|rsync|nc)\b[^\n]{0,120}(?:\.env\b|id_rsa|\.ssh/|\.aws/|\bkeychain\b)", "may send secrets out"),
    (r"\brm\s+(?:-[a-z]+\s+)+[\"']?(?:/|~|\$home|\$\{home\})/?\*?[\"']?(?:\s|$|;)", "destructive command"),
    (r"xattr\s+-c", "bypasses macOS quarantine"),
    (r"(?:^|\s)!`", "runs a command as soon as it loads"),
    ("|".join(SECRETS), "holds what looks like a pasted secret"),
]
# Control characters a pager or terminal acts on (escape sequences, the clipboard one, backspace): never in a real doc.
CONTROL_RE = re.compile(r"[\x00-\x08\x0b\x0c\x0e-\x1f\x7f-\x9f]")
# An MCP server's tools run on every call without the agent reading them first: one that runs any command or
# script hands the model a shell under a friendlier name. Matched against tool names and capability phrases,
# with camelCase and hyphens first turned into underscores. A bare word counts only where it stands as a tool name.
MCP_RISKY = [
    (r"\b(run|execute|exec|start|spawn|eval|evaluate)_(command|shell|script|applescript|jxa|code|terminal|process|"
     r"python|bash)s?\b", "has a tool that runs any command or script"),
    (r"\b(shell_exec|exec_command|run_shell|eval_code|browser_evaluate|python_repl|send_keys|osascript)\b",
     "has a tool that runs any command or script"),
    (r"(?m)(?:^[\s*|-]*|(?<!`)`)(?:bash|shell|exec|terminal|spawn|eval)(?:`(?!`)|\s*(?::|\||$))",
     "has a tool that runs any command or script"),
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
        return r.read(MAX_RESPONSE)


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


def printable(text: str) -> str:
    """Third-party text made safe to store and print: no lone surrogates, no control characters."""
    return CONTROL_RE.sub("", text.encode("utf-8", "replace").decode("utf-8"))


def one_line(text: str) -> str:
    return clean(printable(text))


def _normalized(doc: str) -> str:
    """The text as a reader or shell takes it: entities decoded, lookalikes folded, invisibles out, lines joined."""
    text = unicodedata.normalize("NFKC", html.unescape(doc))
    text = "".join(ch for ch in text if unicodedata.category(ch) != "Cf")
    return re.sub(r"\\\r?\n[ \t]*", " ", text)


def _tool_form(text: str) -> str:
    return re.sub(r"(?<=\w)-(?=\w)", "_", re.sub(r"([a-z0-9])([A-Z])", r"\1_\2", text)).lower()


def search_queries(group: dict) -> list[str]:
    """What to search for a kind of work: its queries, then its name and each pair of words in it, which are broader."""
    queries = [q for q in group.get("queries") or [] if isinstance(q, str)]
    name = group.get("name")
    words = name.lower().split() if isinstance(name, str) else []
    pairs = [" ".join(words[i:i + 2]) for i in range(len(words) - 1)]
    terms = queries + ([" ".join(words)] if words else []) + pairs
    return list({t.lower(): t for t in reversed(terms)}.values())[::-1]


def risks(doc: str, mcp: bool = False) -> list[str]:
    text = _normalized(doc)
    low = text.lower()
    found = {why for pattern, why in RISKY if re.search(pattern, low, re.M)}
    if mcp:
        tools = _tool_form(text)
        found |= {why for pattern, why in MCP_RISKY if re.search(pattern, tools) or re.search(pattern, low)}
    if CONTROL_RE.search(doc):
        found.add("hides control sequences from the reader")
    if "hooks" in frontmatter(doc):
        found.add("installs hooks that run on their own")
    return sorted(found)


def redact(text: str) -> str:
    """Pasted credentials out of session text, before anything is written from it."""
    return re.sub("|".join(SECRETS), "[secret removed]", text, flags=re.I)


def write_private(path: Path, text: str):
    """A cache file only this user can read: verdicts, reports and written skills quote session text."""
    path.parent.mkdir(parents=True, exist_ok=True)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    os.fchmod(fd, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as f:
        f.write(text)


def _cache_file(prefix: str, key: str, suffix: str) -> Path:
    """By hash: ids differing only in punctuation or case get their own file, and no id is too long for a name."""
    return CACHE / f"{prefix}_{hashlib.sha256(key.encode('utf-8', 'replace')).hexdigest()}{suffix}"


def install_target(server: dict) -> dict | None:
    """What installing this server would actually run: a pinned package, else an https remote. None if neither.
    The one place that chooses, so the label the user sees and the command that runs cannot disagree."""
    for pkg in server.get("packages") or []:
        if isinstance(pkg, dict) and pkg.get("registryType") in PACKAGES \
                and PACKAGE_ID_RE.fullmatch(str(pkg.get("identifier"))) and VERSION_RE.fullmatch(str(pkg.get("version"))):
            return {"package": pkg}
    for remote in server.get("remotes") or []:
        if isinstance(remote, dict) and remote.get("type") in TRANSPORTS and REMOTE_RE.fullmatch(str(remote.get("url"))):
            return {"remote": remote}
    return None


def mcp_where(server: dict) -> str:
    """'runs locally' for a package, 'remote, <host>' for a hosted server: a remote one sees what you send it."""
    target = install_target(server)
    if not target:
        return "unknown origin"
    if "package" in target:
        return "runs locally"
    return f"remote, {urllib.parse.urlparse(target['remote']['url']).hostname}"


# ---------------------------------------------------------------------------
# skills.sh
# ---------------------------------------------------------------------------

def _skill_item(sk) -> dict | None:
    """One search result, or None if it is not a shape we can name, fetch and install."""
    if not isinstance(sk, dict) or not isinstance(sk.get("id"), str):
        return None
    source, slug = sk.get("source"), sk.get("skillId")
    if not (isinstance(source, str) and SOURCE_RE.fullmatch(source) and isinstance(slug, str) and SLUG_RE.fullmatch(slug)):
        return None
    try:
        installs = int(sk.get("installs") or 0)
    except (TypeError, ValueError):
        return None
    return {"id": printable(sk["id"]), "kind": "skill", "source": source, "slug": slug,
            "name": one_line(str(sk.get("name") or slug)), "installs": installs, "hits": {}}


def _search_page(q: str) -> dict | None:
    f = _cache_file("search", q.lower(), ".json")
    if f.exists() and time.time() - f.stat().st_mtime < SEARCH_TTL:
        try:
            return json.loads(f.read_text(encoding="utf-8"))
        except (OSError, ValueError):
            pass
    try:
        data = _skillssh_json(f"{SKILLS_SH}/api/search?" + urllib.parse.urlencode({"q": q, "limit": PER_QUERY}))
    except Exception as e:  # noqa: BLE001  (a failed query just contributes nothing)
        print(f"skills.sh search {q!r}: {e}")
        return None
    f.write_text(json.dumps(data), encoding="utf-8")
    return data


def search_skills(queries: list[str], skip_names: set[str]) -> list[dict]:
    """Search results only (no SKILL.md yet): name, repo, installs and each query's rank."""
    CACHE.mkdir(parents=True, exist_ok=True)
    found = {}
    for q in queries:
        data = _search_page(q)
        skills = data.get("skills") if isinstance(data, dict) else None
        for rank, sk in enumerate(skills if isinstance(skills, list) else []):
            item = _skill_item(sk)
            if item is None:
                print(f"skills.sh search {q!r}: skipped a malformed result")
                continue
            found.setdefault(item["id"], item)["hits"][q] = rank
    # by name and by folder: a result named differently can still land on a skill the user already has
    return [it for it in found.values() if not {canon(it["name"]), canon(it["slug"])} & skip_names]


def skill_doc(item: dict) -> str:
    """Everything `skills add` would install, as text, cached for a week. '' when it cannot be read: that one
    candidate is skipped, and a miss may be a transient failure, so it is not cached."""
    try:
        f = _cache_file("doc", str(item.get("id", "")), ".md")
        if f.exists() and time.time() - f.stat().st_mtime < SEARCH_TTL:
            return f.read_text(encoding="utf-8")
        doc = _fetch_skill_doc(item)
        if doc:
            CACHE.mkdir(parents=True, exist_ok=True)
            f.write_text(doc, encoding="utf-8")
        return doc
    except Exception as e:  # noqa: BLE001  (one bad document costs only that candidate)
        print(f"skill doc {item.get('id')!r}: {e}")
        return ""


def _skillssh_json(url: str):
    """GET skills.sh one request at a time, waiting out 429s."""
    with _skillssh:
        return http_json(url, SKILLSSH_TRIES, (429,), SKILLSSH_MAX_WAIT)


def render_tree(tree: dict[str, str]) -> str:
    """SKILL.md, then every other file under its own mark: the whole of what gets installed."""
    rest = sorted(p for p in tree if p != SKILL_FILE)
    return "\n".join([tree[SKILL_FILE], *(f"\n{FILE_MARK.format(p)}\n{tree[p]}" for p in rest)])


def _same_name(name: str, slug: str) -> bool:
    return re.sub(r"[^a-z0-9]+", "-", name.lower()).strip("-") == re.sub(r"[^a-z0-9]+", "-", slug.lower()).strip("-")


def _fetch_skill_doc(item: dict) -> str:
    """The skill's whole folder from skills.sh, the only source that lists every file. A folder with a file we cannot
    read as text, or whose own name is not the slug, is refused: what is checked must be what gets installed."""
    src, slug = item.get("source", ""), item.get("slug", "")
    if not (SOURCE_RE.fullmatch(src) and SLUG_RE.fullmatch(slug)):
        return ""
    try:
        data = _skillssh_json(f"{SKILLS_SH}/api/download/{src}/{slug}")
    except Exception as e:  # noqa: BLE001  (logged: a silent miss is what hid the 429s)
        print(f"skills.sh download {item.get('id')}: {e}")
        return ""
    files = data.get("files") if isinstance(data, dict) else None
    if not isinstance(files, list):
        return ""
    tree = {x["path"]: x["contents"] for x in files
            if isinstance(x, dict) and isinstance(x.get("path"), str) and isinstance(x.get("contents"), str)}
    if len(tree) != len(files) or SKILL_FILE not in tree:
        return ""
    tree = {p: text.encode("utf-8", "replace").decode("utf-8") for p, text in tree.items()}
    return render_tree(tree) if _same_name(frontmatter(tree[SKILL_FILE]).get("name", ""), slug) else ""


# ---------------------------------------------------------------------------
# MCP registry
# ---------------------------------------------------------------------------

def readme(repo_url: str, subfolder: str = "") -> str:
    """The README of the server itself: its own folder when it has one, with no falling back to the repo's."""
    m = re.match(r"https?://github\.com/([^/]+/[^/#?]+?)(?:\.git)?(?:/tree/[^/]+/([^#?]+?))?/?(?:[#?].*)?$", repo_url or "")
    if not m:
        return ""
    folder = (subfolder or m.group(2) or "").strip("/")
    if folder and not SUBFOLDER_RE.fullmatch(folder):
        return ""
    for name in ("README.md", "readme.md"):
        path = f"{folder}/{name}" if folder else name
        try:
            return http_get(f"https://raw.githubusercontent.com/{m.group(1)}/HEAD/{path}",
                            timeout=10).decode("utf-8", "replace")
        except Exception:  # noqa: BLE001
            continue
    return ""


def _is_current(entry: dict) -> bool:
    meta = (entry.get("_meta") or {}).get(OFFICIAL_META) or {}
    return meta.get("status") == "active" and meta.get("isLatest") is True


def package_matches_repo(server: dict) -> bool:
    """A package must name the repo whose README was read: otherwise a clean README vouches for any package.
    Only a remote has nothing to compare: the verifier and the user see its host instead."""
    target = install_target(server)
    if not target or "package" not in target:
        return bool(target)
    pkg = target["package"]
    repo = re.sub(r"^https?://|\.git$", "", str((server.get("repository") or {}).get("url", ""))).strip("/").lower()
    try:
        meta = http_json(PACKAGES[pkg["registryType"]]["meta"].format(id=pkg["identifier"], v=pkg["version"]), tries=2)
    except Exception as e:  # noqa: BLE001  (not confirmed is not a match)
        print(f"package metadata {pkg['identifier']}: {e}")
        return False
    return bool(repo) and repo in json.dumps(meta).lower()


def _server_doc(s: dict) -> str:
    repo = s.get("repository") or {}
    if not package_matches_repo(s):
        return ""
    return readme(repo.get("url", ""), str(repo.get("subfolder") or ""))


def search_mcp(words: list[str]) -> list[dict]:
    """MCP servers for these words (the registry search is literal: single words work, phrases don't).
    Only the active latest version of each: an old or deleted entry is never a candidate."""
    found = {}
    for w in words:
        try:
            data = http_json(f"{MCP_REGISTRY}?" + urllib.parse.urlencode({"search": w, "limit": 20, "version": "latest"}),
                             tries=2)
        except Exception as e:  # noqa: BLE001
            print(f"MCP registry {w!r}: {e}")
            continue
        for x in data.get("servers", []) if isinstance(data, dict) else []:
            server = x.get("server") if isinstance(x, dict) else None
            if isinstance(server, dict) and _is_current(x) and MCP_NAME_RE.fullmatch(str(server.get("name"))) \
                    and isinstance(server.get("repository") or {}, dict):
                found.setdefault(server["name"], server)
    servers = list(found.values())[: MCP_PER_TASK * 2]
    with ThreadPoolExecutor(8) as ex:
        docs = list(ex.map(_server_doc, servers))
    return [{"id": s["name"], "kind": "mcp", "name": one_line(str(s.get("title") or s["name"].split("/")[-1])),
             "desc": one_line(str(s.get("description", ""))), "server": s, "installs": 0, "doc": doc}
            for s, doc in zip(servers, docs) if doc][:MCP_PER_TASK]
