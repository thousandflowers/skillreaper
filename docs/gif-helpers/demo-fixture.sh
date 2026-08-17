#!/bin/bash
# demo-fixture.sh: builds a throwaway ~/.claude lookalike for the VHS demo.
# Neutral skill names and a neutral path (/tmp/demo-claude) so the GIF carries
# no personal data. Two skills fire in the transcripts, three never do.
set -euo pipefail

ROOT="${1:-/tmp/demo-claude}"
rm -rf "$ROOT"
mkdir -p "$ROOT/skills" "$ROOT/projects/acme-invoices"

make_skill() {
	mkdir -p "$ROOT/skills/$1"
	cat > "$ROOT/skills/$1/SKILL.md" <<SKILL
---
name: $1
description: $2
---

$3
SKILL
	# older than --grace-days so it is judged, not held at REVIEW(grace)
	touch -t "$(date -v-60d +%Y%m%d%H%M)" "$ROOT/skills/$1/SKILL.md"
}

make_skill pdf-export \
	"Export a report to a print-ready PDF. Use when the user asks for a PDF, a print version, a handout, or a file to send to a printer. Handles page size, bleed, embedded fonts, and colour profile, and warns when an image is below 300 dpi. Covers single reports and batches." \
	"Render the report, embed the fonts, set the colour profile, then write the PDF."

make_skill csv-import \
	"Load a CSV into the working dataset. Use when the user points at a .csv or .tsv file, pastes delimited text, or asks to import a spreadsheet export. Sniffs the delimiter, coerces column types, reports malformed rows instead of dropping them silently, and keeps the original file untouched." \
	"Sniff the delimiter, coerce the types, report the bad rows."

make_skill invoice-parser \
	"Pull totals, dates, and VAT numbers out of an invoice. Use when the user supplies an invoice as PDF, photo, or scan and wants the figures as structured data. Locates the grand total even when the layout puts it mid-page, normalises the date format, and validates the VAT number against its country pattern." \
	"Locate the total, normalise the date, validate the VAT number."

make_skill image-resize \
	"Resize images to a target width or height. Use when the user asks to shrink, scale, or batch-convert images for the web, for email, or to fit an upload limit. Scales proportionally, preserves the colour profile and EXIF orientation, and refuses to upscale past the source resolution." \
	"Scale proportionally, keep the colour profile, never upscale."

make_skill slack-digest \
	"Summarise a Slack channel into a digest. Use when the user asks what they missed, wants a daily or weekly recap, or needs a channel condensed before a meeting. Collects the messages in the window, groups them by thread, separates decisions from chatter, and lists who is waiting on a reply." \
	"Collect the window's messages, group by thread, split decisions from chatter."

# Evidence: 12 sessions in the window. pdf-export and csv-import fire;
# the other three never do, which is what earns them a REAP verdict.
mkdir -p "$ROOT/projects/acme-invoices"
i=0
for d in 2 3 4 6 7 9 11 13 16 19 22 26; do
	i=$((i + 1))
	T="$ROOT/projects/acme-invoices/session-$i.jsonl"
	ts="$(date -u -v-${d}d +%Y-%m-%dT%H:%M:%SZ)"
	: > "$T"
	if [ $((i % 2)) -eq 1 ]; then
		printf '{"type":"user","timestamp":"%s","message":{"role":"user","content":"<command-name>/pdf-export</command-name>"}}\n' "$ts" >> "$T"
	fi
	if [ $((i % 5)) -eq 0 ]; then
		printf '{"type":"user","timestamp":"%s","message":{"role":"user","content":"<command-name>/csv-import</command-name>"}}\n' "$ts" >> "$T"
	fi
	printf '{"type":"user","timestamp":"%s","message":{"role":"user","content":"routine work"}}\n' "$ts" >> "$T"
	touch -t "$(date -v-${d}d +%Y%m%d%H%M)" "$T"
done

printf '{}\n' > "$ROOT/.claude.json"
