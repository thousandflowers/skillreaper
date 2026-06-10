# Security Policy

skillreaper is 100% local — it reads config files and session transcripts
on disk, computes verdicts, and prints results. The binary has no
dependencies and makes no outbound network connections.

## Reporting a Vulnerability

If you discover a security issue, please open a
[GitHub Issue](https://github.com/thousandflowers/skillreaper/issues)
with the label `security`. Do not open a public issue if the vulnerability
could affect other users.

Given the tool's local-only design, most security concerns involve
transcript parsing. We aim to triage all reports within 7 days.
