#!/bin/bash
# Writes the local data the prototypes read. These files hold personal paths and
# are ignored by git: do not commit them.
set -eu
cd "$(dirname "$0")"
reap --json > data.json
if [ -f ~/.claude/reaped/manifest.json ]; then cp ~/.claude/reaped/manifest.json pruned.json; else echo '{"entries":[]}' > pruned.json; fi
python3 extras.py extras.json
python3 - <<'PY'
import json, os
account = {}
try:
    account = json.load(open(os.path.expanduser("~/.claude.json"))).get("oauthAccount") or {}
except OSError:
    pass
json.dump({"billingType": account.get("billingType")}, open("plan.json", "w"))
json.dump({"cur": "USD", "sym": "$", "rate": 1.0}, open("rate.json", "w"))
PY
