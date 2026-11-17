# Security Policy

skillreaper is local - it reads config files and session transcripts on
disk, computes verdicts, and prints results. The binary has no
dependencies, and no command makes a network connection except two you
start by hand:

- `reap reinvest` (experimental) sends Claude, through `claude -p` on your
  own Claude Code account, excerpts of your sessions (with the keys it
  recognises removed first) and your installed skills' names and
  descriptions; with `--online` it also sends search words drawn from them
  to skills.sh (and, with `--mcp`, the MCP registry), reads what it found
  from GitHub and from the npm and PyPI registries to check it, and, once
  you answer yes to `apply?`, installs it with `npx`, `uvx` or
  `claude mcp add`, the command shown before the question. It says so on its
  first line.
- The npm package downloads the release binary the first time it runs,
  and checks it against the release's checksums.

## Secrets in what reap prints

The hook and MCP rows of the report show the command each one runs. What looks
like a credential there is masked: the value after a name that says so (key,
token, secret, password, auth, cookie, session and the like), the user part of
a URL, `Authorization` and `Cookie` headers, `-u user:password`. This reads
shapes, not secrets. A credential under a name that says nothing, or passed as
a bare argument, is shown as it is written in your config: look before you
share `--json` output or a snapshot file.

Two places hold your config as it is, unmasked, because that is their job:
`reap install-hook --dry-run` prints the whole `settings.json` it would write,
and `~/.claude/reaped` keeps what restore needs to put a pruned MCP server
back (its full definition in `manifest.json`, and copies of `.claude.json` in
`backups/`, readable by you alone).

## Reporting a Vulnerability

If the issue could affect other users, report it in private: on the
repository's **Security** tab choose
[Report a vulnerability](https://github.com/thousandflowers/skillreaper/security/advisories/new).
Nothing sent there is public until a fix is out.

For anything else security-related, open a
[GitHub Issue](https://github.com/thousandflowers/skillreaper/issues)
with the label `security`. An issue is public from the moment it is opened.

Given the tool's local-only design, most security concerns involve
transcript parsing. We aim to triage all reports within 7 days.
