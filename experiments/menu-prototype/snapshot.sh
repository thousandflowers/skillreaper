#!/bin/bash
# Writes the local data the prototypes read. These files hold personal paths and
# are ignored by git: do not commit them. A failed refresh leaves the last good files
# as they were: everything is written aside and only moved in at the very end.
set -eu
umask 077  # the prune manifest can hold what an MCP server had in its env
here=$(python3 -c 'import os, sys; print(os.path.dirname(os.path.realpath(sys.argv[1])))' "$0")
work=$(mktemp -d "$here/.snapshot.XXXXXX")
trap 'rm -rf "$work"' EXIT

# REAP=path reads with a build of this branch. A path may hold spaces; anything else may carry arguments.
if [ -x "${REAP:-}" ]; then reap_cmd=("$REAP"); else read -r -a reap_cmd <<< "${REAP:-reap}"; fi
# Run from where you are, not from this folder: reap reports the project of the current directory.
"${reap_cmd[@]}" --json > "$work/data.json"

python3 - "$work" <<'PY'
import json, os, sys
work = sys.argv[1]
home = os.path.expanduser("~")


def read(path):
    """A dictionary from a file that may be absent, broken, or something else."""
    try:
        with open(path, encoding="utf-8") as f:
            found = json.load(f)
    except (OSError, ValueError):
        return {}
    return found if isinstance(found, dict) else {}


report = read(os.path.join(work, "data.json"))
if "Rows" not in report:
    sys.exit("snapshot: reap did not print a report, keeping the previous data")
rows = report["Rows"] or []
if rows and "PricePerToken" not in report:
    print("snapshot: this reap is too old to report PricePerToken. Use REAP=<a build of this branch>.", file=sys.stderr)
# The list of what is pruned never needs what was in it, and the payload can hold secrets.
manifest = read(os.path.join(home, ".claude", "reaped", "manifest.json"))
entries = manifest.get("entries") if isinstance(manifest.get("entries"), list) else []
pruned = [{k: v for k, v in e.items() if k != "payload"} for e in entries if isinstance(e, dict)]
with open(os.path.join(work, "pruned.json"), "w", encoding="utf-8") as f:
    json.dump({"entries": pruned}, f)
account = read(os.path.join(home, ".claude.json")).get("oauthAccount")
billing = account.get("billingType") if isinstance(account, dict) else None
with open(os.path.join(work, "plan.json"), "w", encoding="utf-8") as f:
    json.dump({"billingType": billing}, f)
PY
python3 "$here/extras.py" "$work/extras.json"
python3 "$here/currency.py" > "$work/rate.json"
python3 -c 'import json, sys; json.load(open(sys.argv[1], encoding="utf-8"))' "$work/rate.json"
for f in data pruned plan extras rate; do mv -f "$work/$f.json" "$here/$f.json"; done
