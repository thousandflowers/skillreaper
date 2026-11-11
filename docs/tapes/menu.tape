# menu.tape - the interactive menu (`reap menu`) on a fake stack, for the README
# and for posts.
#
#   cd docs/tapes && vhs menu.tape
#
# or, while the reap on PATH is a release from before the menu, with a build of
# this checkout that lives outside the repo:
#
#   go build -o /tmp/reap-menu ./cmd/reap           (from the repo root)
#   cd docs/tapes && REAP=/tmp/reap-menu vhs menu.tape
#
# Writes ../menu-demo.gif, which is docs/menu-demo.gif: tracked, next to
# reap-demo.gif, because the README embeds it. About 22 seconds, under 1 MB.
#
# The conventions are hero.tape's:
#   - no absolute path to anyone's home: the repo root comes from git
#   - the binary is resolved once and checked for the menu command, so the
#     recording cannot silently capture a reap that answers "unknown command"
#   - the Output path is quoted
#
# What the menu is shown is ../gif-helpers/menu-fixture.sh, rebuilt from nothing
# under /tmp/menu-claude on every run. The tape answers the receipt with y, which
# really prunes and mutes: the reap() wrapper below pins every call to that
# directory, and the durable evidence record is sent there too, so nothing of the
# machine that records is read or written.
#
# Three things about the keys:
#   - Shift+Enter (apply) is sent as Escape then Enter. The menu reads ESC CR as
#     Shift+Enter when the two arrive within 200 ms, hence the @10ms.
#   - After a lone Escape the menu waits those same 200 ms to be sure it is not
#     the start of a sequence, so the next key is never sooner than that.
#   - On macOS the menu asks the system whether Shift is down. Keep your hands
#     off Shift while this records, or the tape's plain Enter opens the receipt.
#     The receipt takes y only 0.6 s after it appears, hence the Sleep before it.
#
# Numbers: the menu writes amounts the way the Mac's region does, so a Mac set
# to Italy would draw 1.836 and a euro sign. The reap() wrapper sets
# LC_MONETARY=en_US.UTF-8 for the menu alone, which the menu honours, so the GIF
# reads 1,836 and $ wherever it is recorded. An exported LC_ALL would win over
# it: record from a shell that does not set one.
#
# Geometry: the menu wants 110 columns and 30 rows to look like itself, and an
# opened "suggest to prune" is 15 rows under a 7-row header. 1200x720 at 16pt
# measures 114x35.
#
# Framerate 10, where the other tapes ask for 20: vhs stamps every frame as if
# it had kept the rate it was asked for, and at this size it captures about 14 a
# second (measured, M2 Pro). Asked for 20, the 21 seconds below played back in
# 15, every pause a third shorter than written. 10 it can hold.

Output "../menu-demo.gif"

Set Theme "Ayu Mirage"
Set FontSize 16
Set Width 1200
Set Height 720
Set Padding 16
Set Framerate 10
Set PlaybackSpeed 1.0

Hide
Type 'REPO="$(git rev-parse --show-toplevel)"' Enter
Type 'REAP="${REAP:-$(command -v reap)}"' Enter
Type '"$REAP" menu </dev/null 2>&1 | grep -q "cannot open the menu" || { echo "recording aborted: $REAP has no menu command"; exit 1; }' Enter
Type 'export SOURCE_DATE_EPOCH="$("$REPO/docs/gif-helpers/menu-fixture.sh" /tmp/menu-claude)"' Enter
Type 'export SKILLREAPER_STATE_DIR=/tmp/menu-claude/.state' Enter
Type 'reap() { LC_MONETARY=en_US.UTF-8 "$REAP" "$@" -claude-dir /tmp/menu-claude -claude-json /tmp/menu-claude/.claude.json; }' Enter
Type 'clear' Enter
Sleep 1s
Show

# The list: the big number, and what reap suggests.
Sleep 500ms
Type "reap menu" Sleep 400ms Enter
Sleep 2500ms

# Open "suggest to prune" and choose three rows: the number falls, the names
# are struck through.
Enter
Sleep 900ms
Down
Sleep 500ms
Space
Sleep 800ms
Down
Sleep 500ms
Space
Sleep 800ms
Down
Sleep 500ms
Space
Sleep 900ms

# One row's card, and back.
Down
Sleep 600ms
Enter
Sleep 2200ms
Escape
Sleep 800ms

# Close the group, then space on its header: with three of fifteen chosen it
# chooses the rest, and the big number runs down to 0.
Left
Sleep 800ms
Space
Sleep 2200ms

# The whole of "suggest to mute" at once: the number has nothing left to lose,
# the green "you saved" line grows.
Down
Sleep 600ms
Space
Sleep 1400ms

# The receipt, y, and the lighter list.
Escape@10ms
Enter
Sleep 1s
Type "y"
Sleep 3s

Hide
Type "q"
Sleep 500ms
