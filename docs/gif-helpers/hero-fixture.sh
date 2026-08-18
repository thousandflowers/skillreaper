#!/bin/bash
# hero-fixture.sh: builds a throwaway ~/.claude lookalike at the scale a real
# accumulated stack reaches, so the README hero can show `reap --agent` output
# without publishing anyone's install. Neutral generated names, neutral path.
#
# Shape: ~300 skills of which 6 fire, 68 subagents that never fire, 12 MCP
# servers of which 1 fires, 34 sessions in the window.
#
#   docs/gif-helpers/hero-fixture.sh /tmp/hero-claude
#   reap -claude-dir /tmp/hero-claude -claude-json /tmp/hero-claude/.claude.json \
#        -no-nudge --agent
set -euo pipefail

ROOT="${1:-/tmp/hero-claude}"
rm -rf "$ROOT"
mkdir -p "$ROOT/skills" "$ROOT/agents" "$ROOT/projects/acme-platform"

VERBS=(audit compress deploy export extract format import lint migrate parse
	publish render resize review scaffold summarise sync translate validate verify)
NOUNS=(backlog catalog changelog contract dataset invoice ledger manifest
	playlist receipt report roster schema sitemap timesheet transcript)
ROLES=(builder checker inspector migrator planner reviewer)
SERVERS=(billing calendar chat crm email filestore maps metrics search tickets
	warehouse wiki)

OLD="$(date -v-60d +%Y%m%d%H%M)"

# A description in the size range real skills land in. Which of the six trailing
# clauses appear is read off the bits of the index, so the token column gets the
# spread a real inventory has rather than the same number three hundred times;
# every 37th skill also carries the long preamble that a handful of real ones do.
# ponytail: no ${var^} here — macOS ships bash 3.2, which has no case operators.
CLAUSES=(
	"Validates the input first."
	"Reports what it could not handle."
	"Leaves the original file untouched."
	"Handles batches as well as single files."
	"Stops at the first malformed record instead of writing half a result."
	"Keeps column types and encoding as they were unless told otherwise."
)

describe() {
	printf 'Turn a %s into the working format. Use when the user asks to %s a ' "$2" "$1"
	printf '%s or points at one on disk.' "$2"
	local bits=$(($3 % 64)) c=0
	for c in "${!CLAUSES[@]}"; do
		[ $((bits / (1 << c) % 2)) -eq 1 ] && printf ' %s' "${CLAUSES[$c]}"
	done
	[ $(($3 % 37)) -eq 0 ] || return 0
	printf ' This one is the heavy end of the library: it carries the full '
	printf 'conversion matrix, every dialect quirk it has ever had to work '
	printf 'around, the fallbacks for truncated input, the retry policy for '
	printf 'partial reads, and the notes on which of those behaviours are safe '
	printf 'to change. All of it loads on every session whether or not anyone '
	printf 'ever asks for a conversion.'
	return 0
}

skills=()
for verb in "${VERBS[@]}"; do
	for noun in "${NOUNS[@]}"; do
		[ "${#skills[@]}" -lt 298 ] || break 2
		name="$verb-$noun"
		skills+=("$name")
		mkdir -p "$ROOT/skills/$name"
		{
			printf -- '---\nname: %s\ndescription: %s\n---\n\n' \
				"$name" "$(describe "$verb" "$noun" "${#skills[@]}")"
			printf 'Validate the input, do the work, report what was skipped.\n'
		} > "$ROOT/skills/$name/SKILL.md"
		touch -t "$OLD" "$ROOT/skills/$name/SKILL.md"
	done
done

agents=0
for role in "${ROLES[@]}"; do
	for noun in "${NOUNS[@]}"; do
		[ "$agents" -lt 68 ] || break 2
		name="$noun-$role"
		agents=$((agents + 1))
		{
			printf -- '---\nname: %s\n' "$name"
			printf 'description: Reviews %s changes and reports what looks wrong.\n' "$noun"
			printf -- '---\n\nRead the change, list the problems, propose nothing.\n'
		} > "$ROOT/agents/$name.md"
		touch -t "$OLD" "$ROOT/agents/$name.md"
	done
done

# 12 MCP servers in the user config; only the first one ever gets called.
{
	printf '{\n  "mcpServers": {\n'
	for i in "${!SERVERS[@]}"; do
		[ "$i" -eq 0 ] || printf ',\n'
		printf '    "%s": {"command": "npx", "args": ["-y", "%s-mcp"]}' \
			"${SERVERS[$i]}" "${SERVERS[$i]}"
	done
	printf '\n  }\n}\n'
} > "$ROOT/.claude.json"

# Evidence: 34 sessions across the 30-day window. Six skills fire, at the
# uneven rates real use produces; one MCP tool fires; no subagent ever does.
fired=("${skills[0]}" "${skills[1]}" "${skills[2]}" "${skills[3]}" "${skills[4]}" "${skills[5]}")
for i in $(seq 1 34); do
	# 1..29: day 30 would sit on the window edge and drop out of the count
	day=$(( (i * 28 / 34) + 1 ))
	T="$ROOT/projects/acme-platform/session-$i.jsonl"
	ts="$(date -u -v-${day}d +%Y-%m-%dT%H:%M:%SZ)"
	: > "$T"
	for f in "${!fired[@]}"; do
		# skill 0 fires most often, skill 5 barely: i % (f+2) spreads the rates
		if [ $((i % (f + 2))) -eq 0 ]; then
			printf '{"type":"user","timestamp":"%s","message":{"role":"user","content":"<command-name>/%s</command-name>"}}\n' \
				"$ts" "${fired[$f]}" >> "$T"
		fi
	done
	if [ $((i % 4)) -eq 0 ]; then
		printf '{"type":"assistant","timestamp":"%s","message":{"role":"assistant","content":[{"type":"tool_use","name":"mcp__%s__query","input":{}}]}}\n' \
			"$ts" "${SERVERS[0]}" >> "$T"
	fi
	printf '{"type":"user","timestamp":"%s","message":{"role":"user","content":"routine work"}}\n' "$ts" >> "$T"
	touch -t "$(date -v-${day}d +%Y%m%d%H%M)" "$T"
done

printf 'built %s: %d skills, %d agents, %d mcp servers, 34 sessions\n' \
	"$ROOT" "${#skills[@]}" "$agents" "${#SERVERS[@]}" >&2
