# Interactive menu prototypes

Read-only layout experiments for an interactive `reap`, in Python with curses. Nothing here runs any
command or changes any file: confirming only prints what would run. They are not part of the Go build.

`reap_proto10.py` is the current one. `reap_proto.py` to `reap_proto9.py` are the earlier rounds it
builds on, and `notes.md` records what each round was judged on.

```bash
./snapshot.sh                 # writes the local data, needs reap on PATH
TERM=xterm-256color python3 reap_proto10.py
```

Reading Shift+Up/Down relies on macOS: Terminal.app sends them as plain arrows, so the prototype asks
the system for the modifier state.
