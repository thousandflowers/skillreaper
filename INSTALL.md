# Installing skillreaper

## macOS — Homebrew (recommended)

```bash
brew install thousandflowers/tap/skillreaper
```

The formula auto-updates with `brew upgrade`. This is the easiest
option for macOS users.

**Verify**:
```bash
reap version
```

---

## npm / npx (any platform)

**One-shot** (no install):
```bash
npx skillreaper
```

**Installed** (requires Node >= 18):
```bash
npm install -g skillreaper
skillreaper
```

The npm package downloads the correct platform binary at install
time and wraps it as a Node CLI. Same binary either way.

**Verify**:
```bash
skillreaper version
```

---

## Go install (any platform)

```bash
go install github.com/thousandflowers/skillreaper/cmd/reap@latest
```

Requires Go >= 1.22. Binary goes to `$GOPATH/bin` (default
`~/go/bin/reap`). If `~/go/bin` is not in PATH:

```bash
echo 'export PATH=$PATH:$HOME/go/bin' >> ~/.zshrc
source ~/.zshrc
```

**Verify**:
```bash
reap version
```

---

## Binary download (macOS, Linux, Windows)

Download from the
[releases page](https://github.com/thousandflowers/skillreaper/releases).

| OS | Architecture | File |
|---|---|---|
| macOS | Intel | `skillreaper_darwin_amd64.tar.gz` |
| macOS | Apple Silicon | `skillreaper_darwin_arm64.tar.gz` |
| Linux | amd64 | `skillreaper_linux_amd64.tar.gz` |
| Linux | arm64 | `skillreaper_linux_arm64.tar.gz` |
| Windows | amd64 | `skillreaper_windows_amd64.tar.gz` |
| Windows | arm64 | `skillreaper_windows_arm64.tar.gz` |

Single static binary per archive. No dependencies.

```bash
# Example: macOS Apple Silicon v0.1.0
curl -LO https://github.com/thousandflowers/skillreaper/releases/download/v0.1.0/skillreaper_darwin_arm64.tar.gz
tar xzf skillreaper_darwin_arm64.tar.gz
mv skillreaper /usr/local/bin/
rm skillreaper_darwin_arm64.tar.gz
```

**Verify**:
```bash
reap version
```

---

## Upgrading

| Method | Command |
|---|---|
| Homebrew | `brew upgrade skillreaper` |
| npm | `npm update -g skillreaper` |
| Go | `go install github.com/thousandflowers/skillreaper/cmd/reap@latest` |
| Binary | Download new release, replace binary |

---

## Uninstalling

| Method | Command |
|---|---|
| Homebrew | `brew uninstall skillreaper` |
| npm | `npm uninstall -g skillreaper` |
| Go | `rm $(which reap)` |
| Binary | `rm /usr/local/bin/reap` (or wherever placed) |

Clean up quarantine manifests (optional):
```bash
rm -rf ~/.claude/reaped
```
