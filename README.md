# GitFinder

Find every local Git repository on your Windows drives and see, in one page,
which ones have uncommitted work, unpushed commits or no remote at all.

GitFinder scans the drives you pick, checks each repository with `git`, and
opens a single-file HTML report. It is one small `.exe` with no dependencies
other than Git.

## Features

- **Drive picker**: lists all drives (fixed, removable, network) and lets you
  toggle which ones to scan, or type a folder path to scan just that folder.
- **Fast scanning**: walks folders in parallel and skips system and package
  cache folders (`Windows`, `Program Files`, `node_modules`, `AppData`, ...).
  If [Everything](https://www.voidtools.com/) is running, its index is used
  instead, so finding repositories takes seconds instead of minutes.
- **Per-repository status**: current branch, every local branch with its
  ahead/behind state, remotes, commit count, last commit, stashes, and
  working tree changes (staged, modified, untracked, conflicts).
- **At risk panel**: repositories with no remote or with commits that no
  remote has. This work is lost if the disk fails.
- **Stale and duplicate detection**: no commit in 6+ months; the same remote
  cloned in several places.
- **Cache**: results are saved, so the next run can refresh known
  repositories without scanning the disks (`Q` in the menu or `-quick`).
- **Optional fetch**: `F` in the menu or `-fetch` runs `git fetch` first, so
  ahead/behind reflects the remote right now instead of the last fetch.
- **Copyable commands**: each repository has an Actions dialog (commit, push,
  pull, switch or create branch, add a remote, stash, open in Explorer or
  VS Code). Click a command to copy it, then paste it into CMD or PowerShell;
  it works from any folder.

## Requirements

- Windows 10 or 11
- [Git for Windows](https://git-scm.com/download/win). If Git is missing,
  GitFinder tells you how to install it:
  `winget install --id Git.Git -e --source winget`

## Usage

Double-click `gitfinder.exe`, or run it from a terminal:

```text
  Drives  (type a number to toggle, e.g. 2 or 1 3)
   [x]  1  C:\  —                Fixed      16.3 GB free of 357.4 GB
   [ ]  2  E:\  32-VAHID         Removable  7.1 GB free of 29.0 GB
   [x]  3  F:\  HDD              Fixed      19.6 GB free of 931.5 GB

  Options
   [ ] F  Fetch from remotes first  (exact sync state; slower, needs network)
   [x] E  Use Everything index      (instant scan)

  Commands
   Q      quick refresh of 74 cached repositories (no disk scan)
   A / N  select all / none drives      X  exit

  Enter = scan selected drives   ·   or type/paste a folder path to scan only that folder
```

The report opens in your browser when the scan finishes. It is saved to
`%LOCALAPPDATA%\GitFinder\report.html`, next to the cache.

### Command-line flags

| Flag | Description |
| --- | --- |
| `-path "D:\code;E:\work"` | Scan only these folders (separate several with `;`) |
| `-quick` | Re-check cached repositories only, without scanning disks |
| `-fetch` | Run `git fetch` on every repository first |
| `-yes` | Skip the menu and scan the default (or last used) drives |
| `-out report.html` | Write the report to this file |
| `-no-open` | Do not open the report in the browser |
| `-no-everything` | Always walk the disks, even if Everything is running |
| `-version` | Print the version |

Example: a daily report with Windows Task Scheduler:

```bat
gitfinder.exe -quick -no-open -out "%USERPROFILE%\Documents\git-report.html"
```

## Notes

- Without `-fetch`, "in sync", "ahead" and "behind" compare against each
  repository's last fetch. Run with `-fetch` for the current remote state.
- Fetching never asks for credentials; repositories that need a login show
  "fetch failed" instead of blocking the scan.
- Everything's index can lag behind brand-new folders. GitFinder adds
  repositories it found in earlier scans, and you can press `E` to switch to
  a full disk walk at any time.

## Build

Requires [Go](https://go.dev/dl/) 1.23 or newer (`winget install GoLang.Go`).

```bat
build.bat
```

or `go build -ldflags "-s -w" -o gitfinder.exe .`

## Author

Made by **vahidzahani** · [vahidzahani.ir](https://vahidzahani.ir) ·
[github.com/vahidzahani](https://github.com/vahidzahani)
