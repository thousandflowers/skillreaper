# Contributing

Small, focused project. PRs welcome for:

- **New platform support** — add a struct in `internal/platform/`
- **Model pricing updates** — edit `internal/cost/cost.go`
- **Bug fixes** — open an issue first

## Guidelines

- Keep the stdlib-only constraint. No external dependencies.
- Tests go next to the code they cover (`*_test.go`).
- Run `go test ./...` before opening a PR.
- One change per commit, clear commit message.

## Releases

Tagged automatically via GoReleaser on `v*` push. No manual release
steps needed.

## Questions

Open an issue or discussion on GitHub.
