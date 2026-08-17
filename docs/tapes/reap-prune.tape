Output ../gifs/reap-prune.gif
Set Theme "Ayu Mirage"
Set FontSize 26
Set Width 1400
Set Height 466
Set Padding 48
Set Framerate 20
Set PlaybackSpeed 1.0
Hide
Type '"$HOME/Desktop/Progetti dev/skillreaper/docs/gif-helpers/demo-fixture.sh" /tmp/demo-claude' Enter
Type 'reap() { command reap -claude-dir /tmp/demo-claude -claude-json /tmp/demo-claude/.claude.json -no-nudge "$@"; }' Enter
Type "clear" Enter
Show

Type "reap prune" Sleep 400ms Enter
Sleep 2s
Type "n" Enter
Sleep 3s
