# Installing skillreaper

## macOS - Homebrew (recommended)

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

The npm package downloads the correct platform binary the first time
it runs, checks it against the release's checksums, and wraps it as a
Node CLI. Same binary either way. It keeps the binary next to the
package, or, when that folder is not yours to write (a global install
made with sudo), in `skillreaper/<version>/` under `$XDG_CACHE_HOME`, else
under `%LOCALAPPDATA%` on Windows, else under `~/.cache`.

**Verify**:
```bash
skillreaper version
```

---

## Go install (any platform)

```bash
go install github.com/thousandflowers/skillreaper/cmd/reap@latest
```

Requires Go >= 1.24. Binary goes to `$GOPATH/bin` (default
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

The example is macOS Apple Silicon: swap the filename for your row in the
table above. `latest` resolves to the newest release, so the block cannot go
stale. It is one chain: a download that fails, or a checksum that does not
match, stops it there and installs nothing. The checksum line must say `OK`
(on Linux write `sha256sum` in place of `shasum -a 256`). The archive ships
the binary as `skillreaper`; the `mv` renames it to `reap`.

```bash
curl -fLO https://github.com/thousandflowers/skillreaper/releases/latest/download/skillreaper_darwin_arm64.tar.gz &&
curl -fLO https://github.com/thousandflowers/skillreaper/releases/latest/download/checksums.txt &&
shasum -a 256 -c --ignore-missing checksums.txt &&
tar xzf skillreaper_darwin_arm64.tar.gz &&
mv skillreaper /usr/local/bin/reap &&
rm skillreaper_darwin_arm64.tar.gz checksums.txt
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

`~/.claude/reaped` is not a cache. It holds every item you pruned (the only
copy), the descriptions `reap mute` took out, your keep list and the backups
of `.claude.json`. Before you uninstall, put back what you want to keep. The
third line is for those who ran `install-hook`: without it `settings.json`
goes on calling a binary that is gone. `--yes` is there so that the block
can be pasted whole: without it the first command asks, and what was pasted
after it is dropped.
```bash
reap restore --all --yes
reap unmute --all --yes
reap uninstall-hook
```
After that the folder can go. Whatever is still in it goes for good.
