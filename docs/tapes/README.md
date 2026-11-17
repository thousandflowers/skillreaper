# Demo tapes

The README GIFs are generated with [VHS](https://github.com/charmbracelet/vhs),
so they can be rebuilt from source instead of re-recorded by hand.

    brew install vhs gifsicle
    mkdir -p docs/gifs
    cd docs/tapes && vhs reap-prune.tape

`reap-prune.tape` writes into `docs/gifs/`, which is git-ignored - hence the
`mkdir`. `hero.tape` writes `/tmp/skillreaper-hero.gif`, and `menu.tape` writes
`docs/menu-demo.gif`, the one the README embeds.
`gifsicle` is only needed to post-process a result that came out too heavy; a
tape sized for the README should not need it.

`../gif-helpers/demo-fixture.sh` (macOS only: it uses BSD `date -v`) builds a throwaway `~/.claude` lookalike under
`/tmp/demo-claude`: five skills and twelve sessions, two of which fire. The
tapes call it before recording, so every GIF shows the same neutral fixture and
never a real machine's skill names or paths.

`../gif-helpers/hero-fixture.sh` does the same at the scale an accumulated stack
reaches - 298 skills, 68 subagents, 12 MCP servers, 34 sessions, 7 items firing.
It feeds the text block at the top of the README, regenerated with:

    export SOURCE_DATE_EPOCH="$(docs/gif-helpers/hero-fixture.sh /tmp/hero-claude)"
    reap -claude-dir /tmp/hero-claude -claude-json /tmp/hero-claude/.claude.json \
         -no-nudge --agent

(`make readme-numbers` does exactly this.) The demo fixture stamps its files
relative to today. The hero and menu fixtures are dated from a fixed day and
print it: reap measures its window from the clock, so without
`SOURCE_DATE_EPOCH` set to that day it finds no session in the window.

`../gif-helpers/menu-fixture.sh` is the stack the menu is recorded on: 20 skills
and 3 subagents over 40 sessions, with invented names, landing in the prune,
mute and lock groups. `menu.tape` drives the menu through it and writes
`docs/menu-demo.gif`, the one GIF the README embeds from a tape:

    go build -o /tmp/reap-menu ./cmd/reap
    cd docs/tapes && REAP=/tmp/reap-menu vhs menu.tape

Two tapes need a hand before they record again. `hero.tape` was written for
0.5.0: it stops unless `reap version` says 0.5.0, and it does not set
`SOURCE_DATE_EPOCH`, without which the hero fixture shows no session (the README
hero is the text block above, which `make readme-numbers` regenerates, not this
tape). `reap-prune.tape` names the fixture script by the path of the checkout it
was recorded from. Change both to your build and your checkout first.

`menu.tape` records with `LC_MONETARY=en_US.UTF-8`, so the numbers read the same whatever
the region of the Mac it is run on. Keep your hands off Shift while it runs: on
macOS the menu asks the system whether Shift is down.
