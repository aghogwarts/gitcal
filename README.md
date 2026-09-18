# gitcal — step 1: repository discovery

A learning project for a local Git calendar, written in Go.

The planned interface is a traditional monthly calendar with commits as events
inside each day, optional repository groups, and filters. **This first increment
only discovers repositories.** It does not collect commits or save settings yet.
`gitcal` is a working name, not a checked or reserved public project name.

## Requirements

- Install a currently supported Go release from https://go.dev/dl/.
- Install Git and ensure `git --version` works in your terminal.
- The module uses Go 1.22 language features or earlier; use a supported newer
  toolchain rather than installing an old release just to match `go.mod`.

## Run

Extract the project and open a terminal inside the `gitcal` folder containing
`go.mod`. First check `go version` and `git --version`.

Windows PowerShell:

```powershell
go run . scan "$env:USERPROFILE\source\repos"
```

macOS:

```sh
go run . scan "$HOME/projects"
```

Replace the example path with a folder that actually contains your repositories.
The scanner includes the selected folder itself. Quote paths with spaces.
Use your shell's home variable as above; the CLI does not expand a literal `~`.

To build and run an executable on Windows:

```powershell
go build -o gitcal.exe .
.\gitcal.exe scan "C:\path\to\projects"
```

On macOS:

```sh
go build -o gitcal .
./gitcal scan "$HOME/projects"
```

Run `go test ./...` for tests and `go vet ./...` for static checks.
The tests use temporary repositories and require Git. A symlink test may skip
on Windows when the current account cannot create symlinks.

## Behavior

- Lists absolute repository paths, sorted for repeatable output.
- Recognizes ordinary checkouts, nested repositories, and valid `.git` files
  used by linked worktrees and checked-out submodules.
- Validates candidates with Git; malformed metadata produces a warning.
- Skips `.git` directory contents while continuing through working folders.
- Does not follow directory symlinks encountered under the scan root. An
  explicitly selected root is resolved first. This is traversal behavior, not
  a filesystem security boundary.
- Reports unreadable child paths and continues where possible. A scan with
  warnings exits with status 1 so partial results are visible to scripts.
- A missing root, unreadable root, non-directory root, or missing Git is an error.
- Ctrl+C cancels the scan, including any running Git validation command.
- Makes no repository changes and performs no network requests.

Exit codes: `0` successful scan/help, `1` failure or incomplete scan,
`2` invalid command usage.

## Intentional limits of this increment

One scan root per invocation; no remembered roots, groups, commit extraction,
calendar, caching, progress indicator, or automatic dependency-folder exclusions.
Large directory trees may take time to scan. Bare repositories are not discovered
because this increment looks for working checkouts with a `.git` marker. Separate
worktrees appear separately; commit deduplication belongs to the history stage.
Native Windows/macOS behavior still needs verification on those machines.

## Verification for this increment

Verified with Go 1.27.1 in a Linux workspace: all four test functions passed,
`go vet ./...` passed, and the built CLI passed success and invalid-path checks.
Compilation also passed for Windows amd64 and macOS arm64/amd64. Cross-compilation
does not replace running the program on those operating systems. Build checks in
the temporary workspace used `-buildvcs=false` to omit unavailable VCS metadata.

## Learn the code

Read [WALKTHROUGH.md](WALKTHROUGH.md). Production code is split between
`main.go` (CLI behavior) and `scan.go` (discovery). `scan_test.go` checks discovery
against real, temporary Git repositories. There are no third-party Go dependencies.

The module name is deliberately local for now. When a GitHub repository is chosen,
we can change it to that repository's import path. Publishing and a license choice
are separate future steps.
