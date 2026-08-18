# hero.tape — `reap --agent` against the hero fixture, for posts and threads.
#
#   cd docs/tapes && vhs hero.tape
#
# Writes /tmp/skillreaper-hero.gif, deliberately outside the repo: this GIF is
# for posts, not for the README, and docs/gifs/ is gitignored anyway.
#
# Three things this fixes versus reap-prune.tape:
#   - no absolute path to anyone's home: the repo root comes from git
#   - output goes to /tmp, not to a gitignored directory inside the repo
#   - the binary is resolved once and its version asserted, so the recording
#     cannot silently capture whatever `reap` happens to be on PATH
#
# Geometry: the report's longest line is 71 columns, and every one of them has to
# fit or the "To prune:" line wraps its closing paren onto a line of its own.
# 800px wide caps the font at 16pt — 17 measured at 70 usable columns, one short.
# reap-prune.tape's 26pt would need ~1150px, so the size differs and the theme
# and font do not.

# Quoted: the parser in vhs 0.11 rejects an unquoted path that starts with "/".
Output "/tmp/skillreaper-hero.gif"

Set Theme "Ayu Mirage"
Set FontSize 16
Set Width 800
Set Height 520
Set Padding 16
Set Framerate 20
Set PlaybackSpeed 1.0

Hide
Type 'WANT=0.5.0' Enter
Type 'REPO="$(git rev-parse --show-toplevel)"' Enter
Type 'REAP="$(command -v reap)"' Enter
Type '"$REAP" version | grep -q "reap $WANT" || { echo "recording aborted: need reap $WANT, found $("$REAP" version)"; exit 1; }' Enter
Type '"$REPO/docs/gif-helpers/hero-fixture.sh" /tmp/hero-claude' Enter
Type 'reap() { "$REAP" -claude-dir /tmp/hero-claude -claude-json /tmp/hero-claude/.claude.json -no-nudge "$@"; }' Enter
Type 'clear' Enter
Show

Sleep 600ms
Type "reap --agent" Sleep 400ms Enter
Sleep 2s
