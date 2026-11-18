#!/bin/bash
# menu-fixture.sh: builds a throwaway ~/.claude lookalike sized for the menu's
# demo GIF (docs/tapes/menu.tape). A copy of hero-fixture.sh's method at a scale
# one screen can show: every row of an opened group has to fit a 35-row window.
#
# Shape: 20 skills and 3 subagents over 40 sessions. Twelve skills and the three
# subagents never fire (the menu's "suggest to prune"), four skills fire in
# fewer than one session in five ("suggest to mute"), four fire often ("suggest
# to lock"). Invented, neutral names; nothing here is anyone's install.
#
#   docs/gif-helpers/menu-fixture.sh /tmp/menu-claude
#   reap menu -claude-dir /tmp/menu-claude -claude-json /tmp/menu-claude/.claude.json
#
# The menu changes what it is pointed at once its receipt is answered with y, so
# this directory is rebuilt from nothing on every run and is the only thing the
# tape ever lets the menu see.
set -euo pipefail

# UTC and the C locale, for the reasons hero-fixture.sh gives: neither the
# machine's zone nor its locale may reach a timestamp, an mtime or a sort order.
export TZ=UTC LC_ALL=C

# One literal instant every timestamp is measured from, so the fixture is the
# same on every machine and every day. reap measures its window from the wall
# clock unless SOURCE_DATE_EPOCH says otherwise: whoever runs it against this
# fixture exports the epoch printed on stdout below. Midday keeps every
# whole-day offset far from a boundary in any zone.
ANCHOR="2026-08-30T12:00:00Z"

# python3 rather than date(1): BSD and GNU date share no spelling for "format
# this instant", and this has to run on both. One call prints every stamp the
# fixture needs, one line per day back: the day, its ISO instant for a
# transcript line, its touch(1) stamp for an mtime. In UTC a day is exactly
# 86400 seconds, so whole-day offsets cannot drift.
SPAN=26        # days the sessions spread over
INSTALLED=60   # days since every item was installed
ISO=()
STAMP=()
ANCHOR_EPOCH=""
while read -r day epoch iso stamp; do
	ANCHOR_EPOCH="${ANCHOR_EPOCH:-$epoch}"
	ISO[day]="$iso"
	STAMP[day]="$stamp"
done < <(python3 -c '
import datetime, sys
utc = datetime.timezone.utc
anchor = datetime.datetime.strptime(sys.argv[1], "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=utc)
for day in range(int(sys.argv[2]) + 1):
    t = anchor - datetime.timedelta(days=day)
    print(day, int(anchor.timestamp()), t.strftime("%Y-%m-%dT%H:%M:%SZ"), t.strftime("%Y%m%d%H%M"))
' "$ANCHOR" "$INSTALLED")
[ -n "$ANCHOR_EPOCH" ] || { echo "menu-fixture: python3 printed no dates" >&2; exit 1; }

SESSIONS=40
ROOT="${1:-/tmp/menu-claude}"
rm -rf "$ROOT"
mkdir -p "$ROOT/skills" "$ROOT/agents" "$ROOT/projects/acme-platform"

# Older than --grace-days, so every item is judged rather than held for review.
OLD="${STAMP[INSTALLED]}"

# Trailing clauses, chosen by the bits of the row's own "extra" number, so the
# token column gets the spread a real stack has instead of one figure repeated.
CLAUSES=(
	"Validates the input first and reports what it could not handle."
	"Leaves the original files untouched."
	"Handles batches as well as single files."
	"Stops at the first malformed record instead of writing half a result."
	"Asks before overwriting anything that already exists."
	"Prints a short summary of what changed when it is done."
)

# The few fat rows every accumulated stack has: a description that carries its
# whole manual, loaded on every session whether or not anyone asks for it.
MANUAL="This one carries its full manual: every option it accepts, the quirks \
of each format it has ever had to work around, the fallbacks for truncated \
input, the retry policy for partial reads, the known edge cases with the \
workaround for each, a worked example of a typical run from start to finish, \
and the notes on which of those behaviours are safe to change. All of it loads \
on every session, whether or not anyone asks for it."

describe() {                          # describe <base sentence(s)> <extra>
	printf '%s' "$1"
	local bits=$(($2 % 64)) c=0
	for c in "${!CLAUSES[@]}"; do
		[ $((bits / (1 << c) % 2)) -eq 1 ] && printf ' %s' "${CLAUSES[$c]}"
	done
	[ "$2" -ge 64 ] && printf ' %s' "$MANUAL"
	return 0
}

# The stack, one row per item:
#   kind | name | every | extra | description
# every: fires in every Nth session, 0 for never. With 40 sessions, 2 to 5 is
#        used often and 7 or more is under the one-in-five that earns a mute.
# extra: which trailing clauses the description gets (bits 0-5), plus 64 for the
#        manual. It only moves the weight.
fired=()
count_skills=0
count_agents=0
while IFS='|' read -r kind name every extra base; do
	[ -n "$kind" ] || continue
	desc="$(describe "$base" "$extra")"
	if [ "$kind" = skill ]; then
		count_skills=$((count_skills + 1))
		mkdir -p "$ROOT/skills/$name"
		file="$ROOT/skills/$name/SKILL.md"
	else
		count_agents=$((count_agents + 1))
		file="$ROOT/agents/$name.md"
	fi
	printf -- '---\nname: %s\ndescription: %s\n---\n\nDo the work, then report what was skipped.\n' \
		"$name" "$desc" > "$file"
	touch -t "$OLD" "$file"
	[ "$every" -eq 0 ] || fired+=("$name:$every")
done <<'STACK'
skill|invoice-parser|0|71|Pull totals, dates and tax numbers out of an invoice. Use when the user supplies an invoice as a PDF, a photo or a scan and wants the figures as structured data.
skill|changelog-writer|0|93|Turn a range of commits into a changelog entry. Use when the user asks what changed between two versions or wants release notes drafted.
skill|sitemap-builder|0|5|Crawl a site folder and write its sitemap. Use when the user adds pages and asks for the sitemap to be rebuilt.
skill|timesheet-export|0|94|Export tracked hours as a timesheet. Use when the user asks for hours per project, per week or per client, as a table or a file to send.
skill|contract-review|0|107|Read a contract and list the clauses worth a second look. Use when the user shares an agreement and asks what to watch out for before signing.
skill|playlist-sync|0|2|Keep two playlists in step. Use when the user asks to copy, merge or compare playlists.
skill|receipt-scanner|0|15|Read receipts into an expense list. Use when the user photographs a receipt or forwards one and wants it filed.
skill|schema-migrate|0|121|Write the migration between two versions of a database schema. Use when the user changes a table, renames a column or asks how to move the data across.
skill|roster-planner|0|22|Lay out a shift roster from availability. Use when the user lists who can work when and asks for a fair schedule.
skill|banner-resize|0|1|Resize a banner to a set of target sizes. Use when the user needs the same artwork for several placements.
skill|transcript-cleanup|0|116|Tidy an automatic transcript. Use when the user pastes a raw transcript and wants filler words, repeats and timestamps removed.
skill|newsletter-draft|0|11|Draft a newsletter from a list of links and notes. Use when the user collects items for an issue and asks for a first version.
agent|ledger-auditor|0|6|Reviews ledger changes and reports the entries that do not balance.
agent|release-notes-checker|0|9|Checks release notes against the commits they claim to describe and lists what is missing.
agent|dataset-inspector|0|3|Opens a dataset and reports empty columns, mixed types and duplicated rows.
skill|spreadsheet-import|7|88|Load a spreadsheet into the working dataset. Use when the user points at a spreadsheet file, pastes delimited text or asks to import an export.
skill|diagram-render|9|25|Render a diagram from its text description. Use when the user describes boxes and arrows or asks for a flowchart of some code.
skill|translation-pass|11|14|Translate a document while keeping its formatting. Use when the user asks for a file, a page or a set of strings in another language.
skill|meeting-summary|13|37|Summarise a meeting into decisions and open questions. Use when the user pastes notes or a transcript and asks what was agreed.
skill|pdf-export|2|12|Export a report to a print-ready PDF. Use when the user asks for a PDF, a print version or a handout.
skill|commit-helper|3|4|Write a commit message for the staged changes. Use when the user asks to commit or wants a message drafted.
skill|test-runner|4|10|Run the test suite and explain what failed. Use when the user asks to run the tests or why one is red.
skill|code-explainer|5|8|Explain what a piece of code does, line by line. Use when the user points at a function and asks how it works.
STACK

# Billed on a plan, as most Claude Code accounts are: the menu then opens on
# tokens, the unit a plan is spent in, rather than on money.
printf '{\n  "oauthAccount": {"billingType": "stripe_subscription"}\n}\n' > "$ROOT/.claude.json"

# Evidence: 40 sessions, each row firing at its own rate. They stop at 26 days
# back: a corpus that reaches nine tenths of Claude Code's 30-day retention gets
# a warning about evidence being swept away, which is true of an old install and
# is not what this GIF is about.
for i in $(seq 1 "$SESSIONS"); do
	day=$(( (i * (SPAN - 1) / SESSIONS) + 1 ))
	T="$ROOT/projects/acme-platform/session-$i.jsonl"
	ts="${ISO[day]}"
	: > "$T"
	for f in "${fired[@]}"; do
		if [ $((i % ${f##*:})) -eq 0 ]; then
			printf '{"type":"user","timestamp":"%s","message":{"role":"user","content":"<command-name>/%s</command-name>"}}\n' \
				"$ts" "${f%%:*}" >> "$T"
		fi
	done
	printf '{"type":"user","timestamp":"%s","message":{"role":"user","content":"routine work"}}\n' "$ts" >> "$T"
	touch -t "${STAMP[day]}" "$T"
done

printf 'built %s: %d skills, %d agents, %d sessions\n' \
	"$ROOT" "$count_skills" "$count_agents" "$SESSIONS" >&2

# The anchor, on stdout, as the epoch reap wants in SOURCE_DATE_EPOCH.
printf '%s\n' "$ANCHOR_EPOCH"
