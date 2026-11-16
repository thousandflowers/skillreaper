# Security Policy

skillreaper is local - it reads config files and session transcripts on
disk, computes verdicts, and prints results. The binary has no
dependencies, and no command makes a network connection except two you
start by hand:

- `reap reinvest` (experimental) sends Claude, through `claude -p` on your
  own Claude Code account, excerpts of your sessions (with the keys it
  recognises removed first) and your installed skills' names and
  descriptions; with `--online` it also sends search words drawn from them
  to skills.sh (and, with `--mcp`, the MCP registry). It says so on its
  first line.
- The npm package downloads the release binary the first time it runs,
  and checks it against the release's checksums.

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
