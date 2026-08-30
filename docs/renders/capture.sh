#!/usr/bin/env bash
# Regenerate the before/after captures beside this script.
#
# Both sides run the same fixture stack through two binaries: "before" built
# from a released tag, "after" built from the working tree. The input is
# identical on both sides on purpose - these files are evidence about the FORM
# of the report, not about the size of anyone's waste, and a before/after taken
# against two different stacks measures the stack instead of the layout.
#
#   docs/renders/capture.sh <outdir> [before-ref]
#
# Colour captures need a character device, because reap enables colour only for
# a terminal. script(1) cannot provide one when this runs from a CI job or an
# agent with no tty of its own, so the pty comes from python's pty.spawn, which
# needs no controlling terminal; the carriage returns it inserts are stripped.
set -euo pipefail

# The machine's zone and locale must not reach the captures: reap renders the
# LAST column as a calendar date, and a run in one zone can land it on a
# different day than the same run in another. LC_ALL pins collation too, since
# the report sorts names. hero-fixture.sh exports the same pair for its own
# timestamps; both are set because either script can be run on its own.
export TZ=UTC LC_ALL=C

out=${1:?usage: capture.sh <outdir> [before-ref]}
before_ref=${2:-v0.7.0}
root=$(cd "$(dirname "$0")/../.." && pwd)
work=$(mktemp -d)
cleanup() {
  git -C "$root" worktree remove --force "$work/before" 2>/dev/null || true
  git -C "$root" worktree prune
  rm -rf "$work"
}
trap cleanup EXIT

mkdir -p "$out"

git -C "$root" worktree add --detach "$work/before" "$before_ref" >/dev/null
(cd "$work/before" && go build -o "$work/reap-before" ./cmd/reap)
(cd "$root" && go build -o "$work/reap-after" ./cmd/reap)

# A fixed path, not a temp one: one warning quotes the fixture directory, and
# reap wraps that warning to the terminal width before anything downstream could
# normalise the string. A temp path is a different length on every machine -
# /var/folders/... on macOS, /tmp/tmp.XXXX on Linux - so the same binary wrapped
# the same warning at different points and the captures could never match across
# platforms. CI found exactly that. The path is part of the control now.
fixture=/tmp/skillreaper-fixture
# The fixture prints the epoch of its own anchor, and reap is pinned to it, so
# the window it measures and the ages it renders no longer depend on when this
# runs. The v0.7.0 binary predates the variable and ignores it - see the note on
# the before captures in the Makefile.
SOURCE_DATE_EPOCH="$("$root/docs/gif-helpers/hero-fixture.sh" "$fixture")"
export SOURCE_DATE_EPOCH

pty() {                                   # run "$@" with a pty on stdout
  python3 -c 'import pty, sys; sys.exit(pty.spawn(sys.argv[1:]))' "$@"
}

# Fold the width-labelled captures at their stated width. A terminal hard-wraps
# a long line at the right edge and puts no newline in the bytes, so a capture
# that stored raw output would record "before at 80" and "before at 120" as the
# same file - and that difference is the whole subject. Folding counts display
# columns and steps over ANSI escapes, which byte-based fold(1) cannot do. It is
# a no-op for output that already fits, which is itself the result being shown.
fold_at() {
  python3 -c '
import re, sys
width = int(sys.argv[1])
ansi = re.compile(r"\x1b\[[0-9;]*m")
for line in sys.stdin:
    line = line.rstrip("\n")
    out, col = [], 0
    for part in ansi.split(line) if False else [line]:
        pass
    i = 0
    while i < len(line):
        m = ansi.match(line, i)
        if m:
            out.append(m.group()); i = m.end(); continue
        if col == width:
            out.append("\n"); col = 0
        out.append(line[i]); col += 1; i += 1
    print("".join(out))
' "$1"
}

run() {                                   # run <binary> <width|pipe> <color|nocolor> [subcommand]
  local bin=$1 width=$2 color=$3; shift 3
  # -no-banner: with a pty attached the wordmark prints above the report and
  # would sit in the capture as if it were part of it
  local args=(-claude-dir "$fixture" -claude-json "$fixture/.claude.json" -no-nudge -no-banner "$@")
  if [ "$color" = nocolor ]; then args+=(-no-color); fi
  if [ "$width" = pipe ]; then
    env -u COLUMNS "$bin" "${args[@]}"
  elif [ "$color" = color ]; then
    COLUMNS="$width" pty "$bin" "${args[@]}" | tr -d '\r'
  else
    COLUMNS="$width" "$bin" "${args[@]}"
  fi
}

# capture <side> <view> <width|pipe> <color|nocolor> <outfile> [subcommand...]
#
# The size table is measured on the raw output, before folding: "widest line"
# is the number the redesign is about, and folding is what hides it.
capture() {
  local side=$1 view=$2 width=$3 color=$4 file=$5; shift 5
  local raw="$work/raw.txt"
  run "$work/reap-$side" "$width" "$color" "$@" > "$raw"
  if [ "$width" = pipe ]; then cp "$raw" "$out/$file"; else fold_at "$width" < "$raw" > "$out/$file"; fi
  python3 -c '
import re, sys
ansi = re.compile(r"\x1b\[[0-9;]*m")
lines = [ansi.sub("", l.rstrip("\n")) for l in open(sys.argv[5], encoding="utf-8")]
print("%-11s %-6s %-5s %-7s %5d lines  widest %4d" % (
    sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4], len(lines), max((len(l) for l in lines), default=0)))
' "$view" "$side" "$width" "$color" "$raw" >> "$out/summary.txt"
}

: > "$out/summary.txt"
for side in before after; do
  for w in 80 120; do
    for c in color nocolor; do
      capture "$side" report "$w" "$c" "report-$side-$w-$c.txt"
    done
    capture "$side" gap        "$w" nocolor "gap-$side-$w-nocolor.txt"        gap
    capture "$side" by-project "$w" nocolor "by-project-$side-$w-nocolor.txt" by-project
    capture "$side" route      "$w" nocolor "route-$side-$w-nocolor.txt"      route
  done
  capture "$side" gap    80   color   "gap-$side-80-color.txt" gap
  capture "$side" report pipe nocolor "report-$side-piped.txt"
done

# one line per capture, so the size claims in the changelog and in DECISIONS.md
# can be checked against the files rather than trusted
sort -o "$out/summary.txt" "$out/summary.txt"

echo "captures written to $out (before=$before_ref, after=working tree)"
