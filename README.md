# gitcal — step 3: monthly activity across repositories

A learning project for a local Git calendar, written in Go.

The planned interface is a traditional monthly calendar with commits as events
inside each day, optional repository groups, and filters. **This version discovers
repositories, reads recent history, and combines one month's commits across
repositories into a daily agenda.** It does not display the calendar grid or
save settings yet.
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

## Read one repository's history

Use a repository root printed by `scan`, not the parent folder containing many
repositories. Run from the project folder containing `go.mod`:

```powershell
# Windows PowerShell: replace this example with one of your repository paths.
go run . history "C:\path\to\your-repository"
```

```sh
# macOS: replace this example with one of your repository paths.
go run . history "$HOME/projects/your-repository"
```

The output contains the author timestamp with local UTC offset, the first 12
characters of the commit hash, the subject, and the author's name and email.

- Up to 20 commits reachable from the current checkout's `HEAD` are shown. This
  includes merged history but excludes unmerged branches. Detached HEAD works too.
- Git's normal log ordering is preserved (generally reverse committer-date order).
  The displayed author dates are not used to sort the output and may appear out of
  order. Author and committer timestamps are different fields in Git.
- All authors and merge commits are included in this learning increment. Author
  filtering, branch selection, and calendar counting rules remain future work.
- Original author timestamps and offsets are parsed into `time.Time`; only display
  converts them to the machine's local timezone. That can change the calendar date.
- Empty branches report `No commits yet on the current branch.` successfully.
- A folder must have its own `.git` marker. A random subfolder of a repository is
  rejected instead of silently showing the parent repository's history.
- Linked worktrees and checked-out submodules using `.git` files are supported.
- Full commit hashes are kept in memory; messages are subjects, not full bodies.
- Terminal control characters are replaced with spaces for display only.
- No history is stored on disk. Nothing is fetched, committed, or changed.

To compare with Git, run `git -C "your-repository-path" log -20`. It should select
the same commits, although formatting and displayed timezone can differ.

## Combine activity across repositories

Pass the parent folder you previously scanned. Options go **before** the folder.

```powershell
# Windows PowerShell
go run . activity --month 2026-09 "C:\path\to\projects"
```

```sh
# macOS
go run . activity --month 2026-09 "$HOME/projects"
```

Omit `--month` for the current local month:

```sh
go run . activity "path/to/projects"
```

`go run . activity --help` shows the activity options. Month values must be
`YYYY-MM`, including the leading zero for months 01–09. A repository root itself
also works as the scan folder, and nested repositories are included.

The output is a chronological agenda grouped by date. Each entry includes its
local author time and UTC offset, short hash, subject, author, and full repository
path(s). The final line gives unique-commit, active-day, and repository counts.

- The selected month uses **author timestamps in the machine's local timezone**.
  The first instant of the month is included; the first instant of the next month
  is excluded. UTC offsets and daylight-saving changes are respected.
- Activity reads the full history reachable from each checkout's HEAD, with no
  20-commit cap. The existing `history` command still shows only its 20-commit
  preview. Only history available locally is readable (for example, shallow clones
  contain less history); the tool does not fetch missing objects.
- All authors and merges remain included. Unmerged branches are not included
  unless they are the current checkout of a discovered repository/worktree.
- Identical full commit hashes are counted once across repositories. Every source
  repository is retained and shown, so clones and worktrees do not lose provenance.
  Similar messages, rebases, and cherry-picks with different hashes remain distinct.
- Days and commits are sorted chronologically by local author date/time. Equal
  instants use the full hash as a stable tie-breaker; repository paths are sorted.
- Empty repositories are valid. With no matching activity, an explicit message
  appears. A failed repository produces a warning while successful results remain
  visible, with exit code 1 and an incomplete-results message.
- This is the data preparation step for the planned traditional calendar, with
  commits as events in date cells. It currently prints only days with activity.

**Performance:** this learning increment reads a repository's entire reachable
history into memory before filtering, one repository at a time. Large histories
and broad scan roots can be slow. Start with a small projects folder. Streaming,
caching, progress reporting, and saved repository selections are future work.

## Scanner behavior

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

Exit codes: `0` success/help (including empty results), `1` failure or incomplete results,
`2` invalid command usage.

## Intentional limits of this increment

One scan root or history repository per invocation; no remembered roots, groups,
calendar grid, caching, progress indicator, or automatic dependency-folder exclusions.
Large directory trees may take time to scan. Bare repositories are not discovered
because this increment looks for working checkouts with a `.git` marker. Separate
worktrees appear separately in `scan`, while `activity` deduplicates their commits.
The user verified the first scanner on their machine (OS not recorded). Native
execution of the newer history and activity commands still needs Windows/macOS verification.

## Verification for this increment

See `VERIFICATION.md` for the checks performed on this increment and their limits.

## Learn the code

Read [WALKTHROUGH.md](WALKTHROUGH.md) for discovery,
[STEP-2-WALKTHROUGH.md](STEP-2-WALKTHROUGH.md) for single-repository history,
and [STEP-3-WALKTHROUGH.md](STEP-3-WALKTHROUGH.md) for monthly activity.

| File               | Purpose                                                         |
| ------------------ | --------------------------------------------------------------- |
| `main.go`          | Command routing and human-readable output                       |
| `scan.go`          | Existing repository discovery                                   |
| `git.go`           | Shared read-only Git process execution                          |
| `history.go`       | Commit data, HEAD resolution, history retrieval and parsing     |
| `activity.go`      | Month filtering, deduplication, provenance and date grouping    |
| `activity_cli.go`  | Activity flags and agenda output                                |
| `scan_test.go`     | Original discovery tests                                        |
| `history_test.go`  | Real-repository integration tests and parser checks             |
| `activity_test.go` | Multiple repositories, date boundaries, duplicates and failures |

There are no third-party Go dependencies. All three commands remain available.

The module name is deliberately local for now. When a GitHub repository is chosen,
we can change it to that repository's import path. Publishing and a license choice
are separate future steps.
