# Demo tapes

The README GIFs are generated with [VHS](https://github.com/charmbracelet/vhs),
so they can be rebuilt from source instead of re-recorded by hand.

    brew install vhs gifsicle
    mkdir -p ../gifs
    cd docs/tapes && vhs reap-prune.tape

Each tape writes into `docs/gifs/`, which is git-ignored — hence the `mkdir`.
`gifsicle` is only needed to post-process a result that came out too heavy; a
tape sized for the README should not need it.

`../gif-helpers/demo-fixture.sh` builds a throwaway `~/.claude` lookalike under
`/tmp/demo-claude`: five skills and twelve sessions, two of which fire. The
tapes call it before recording, so every GIF shows the same neutral fixture and
never a real machine's skill names or paths.
