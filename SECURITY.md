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

If you discover a security issue, please open a
[GitHub Issue](https://github.com/thousandflowers/skillreaper/issues)
with the label `security`. Do not open a public issue if the vulnerability
could affect other users.

Given the tool's local-only design, most security concerns involve
transcript parsing. We aim to triage all reports within 7 days.
