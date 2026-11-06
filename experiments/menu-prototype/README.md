# Interactive menu prototypes

Read-only layout experiments for an interactive `reap`, in Python with curses. They are not part of the
Go build, and they never prune, mute or install anything: confirming only prints what would run.

`reap_proto10.py` is the current list, `menu_logic.py` what it knows about rows and choices, `menu_receipt.py` its other screens, and `reinvest_views.py` the step after the receipt. `terminal.py` holds
how they read keys and set up the terminal. `reap_proto.py` to `reap_proto9.py` are the earlier rounds they
build on, and `notes.md` records what each round was judged on. `reap_proto.py` takes a version from 1 to 5
and `reap_proto2.py` one from 6 to 10; the others take none.

```bash
REAP=<a build of this branch> ./snapshot.sh   # released reap lacks fields the menu reads
TERM=xterm-256color python3 reap_proto10.py
python3 test_menu.py                          # no terminal needed
```

What running these does:

- `snapshot.sh` runs reap from the directory you are in, so the report is about that project. It reads
  `~/.claude`, asks macOS for the currency through `osascript` (`currency.py`, no network) and writes
  `data.json`, `pruned.json`, `plan.json`, `extras.json` and `rate.json` here, all private to your user. A
  failed run leaves the previous files alone. `pruned.json` leaves out what pruned MCP servers had in their config.
- `rates.json` is the fixed exchange-rate table and is tracked. The other JSON files hold personal paths and are ignored.
- In `reinvest_views.py`, Enter opens a skill's page in the browser.
- Python leaves a `__pycache__` folder here.

Reading Shift+Up/Down and Shift+Enter relies on macOS: Terminal.app sends them as plain keys, so the prototype asks
the system for the modifier state. Elsewhere the menu understands the sequences those terminals send, and where Shift
cannot be read (off macOS, over ssh) the footer offers the plain key `x` instead.
