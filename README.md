# gitcal — step 5: an interactive month calendar

A learning project for a local Git calendar, written in Go.

The planned interface is a traditional monthly calendar with commits as events
inside each day, optional repository groups, and filters. **This version discovers
repositories, reads their history, and presents one month at a time as a calendar
you can move around with the keyboard.** Repository groups, author filtering, and
saved settings are not implemented yet.
`gitcal` is a working name, not a checked or reserved public project name.

## Requirements

- Install Go 1.26 or newer from https://go.dev/dl/.
- Install Git and ensure `git --version` works in your terminal.
- Steps 4 and 5 added third-party dependencies; `go.mod` declares a Go 1.26 floor
  because of them. `go run` and `go build` download them on first use.

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
- This is the data preparation step for the calendar grid below. It prints only
  days with activity; `calendar` renders every date in the month.

**Performance:** this learning increment reads a repository's entire reachable
history into memory before filtering, one repository at a time. Large histories
and broad scan roots can be slow. Start with a small projects folder. Streaming,
caching, progress reporting, and saved repository selections are future work.

## Browse the month as a calendar

`calendar` reads the same data as `activity` and lays it out as a month grid.
On a terminal it is interactive; redirected output is printed once and exits.
Pass the same parent folder, with options before it.

```powershell
# Windows PowerShell
go run . calendar --month 2026-09 "C:\path\to\projects"
```

```sh
# macOS
go run . calendar --month 2026-09 "$HOME/projects"
```

Omit `--month` for the current local month. To read one date completely,
including commits the grid had to summarise:

```sh
go run . calendar --day 2026-09-18 "path/to/projects"
```

`go run . calendar --help` lists the options.

| Option          | Effect                                                        |
| --------------- | ------------------------------------------------------------- |
| `--month`       | Month to draw, `YYYY-MM`; defaults to the current local month  |
| `--day`         | List one date's commits in full and exit                       |
| `--per-day`     | Commits shown in each date cell before `+N more`; default 3    |
| `--width`       | Grid width in columns; `0` (default) detects the terminal      |
| `--interactive` | Force the keyboard interface even when output is redirected    |
| `--static`      | Force a single printed grid even on a terminal                 |

### Keys

| Key            | Action                                                   |
| -------------- | --------------------------------------------------------- |
| `← → ↑ ↓`      | Move between dates; stops at the month's edge             |
| `h l k j`      | The same without arrow keys                               |
| `[` `]`        | Previous / next month, also `PgUp` and `PgDn`             |
| `Enter`        | Open the selected date, then the selected commit          |
| `Esc`          | Back up one level                                         |
| `t`            | Jump to today                                             |
| `r`            | Re-read the current month                                 |
| `?`            | Toggle the key reference                                  |
| `q`, `Ctrl+C`  | Quit                                                      |

- Every month change re-reads all history, so a large projects folder takes a
  moment. The footer reports progress, and superseded scans are cancelled.
- Weeks start on **Monday**. A configurable Sunday start is future work.
- The grid sizes itself to the terminal, clamped between 78 and 204 columns.
  Redirecting output to a file or pipe uses 80 columns.
- Cells carry the commit subject, adding the repository name and then the time
  as the terminal grows. Text is shortened by **display width**, so emoji and
  East Asian characters stay aligned.
- Colours adapt to light and dark terminal backgrounds, and degrade on
  terminals with limited colour support. Redirected output and `NO_COLOR`
  produce plain text.
- Today is a filled badge, the selected date a lighter one, and a date that is
  both adds an underline. Weekends are tinted, and dates with no commits are
  dimmed so active dates stand out.
- Each repository keeps its own colour, derived from its name so it stays the
  same as months change and as other repositories come and go.
- `--day` and `--month` cannot be combined: a date already identifies its month.
- The summary below the grid counts every commit in the month, not the visible
  ones. Use `--day` or raise `--per-day` to see the rest.
- Month selection, timezone handling, deduplication, author inclusion, and
  warning behavior are identical to `activity`.

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
caching, or automatic dependency-folder exclusions. The interactive calendar
re-reads every repository on each month change and does not scroll the grid
itself. Large directory trees may take time to scan. Bare repositories are
not discovered because this increment looks for working checkouts with a `.git`
marker. Separate worktrees appear separately in `scan`, while `activity` and
`calendar` deduplicate their commits. The `scan`, `activity`, and `calendar`
commands have been run natively on Windows; macOS still needs verification.

## Verification for this increment

See `VERIFICATION.md` for the checks performed on this increment and their limits.

## Learn the code

Read [WALKTHROUGH.md](WALKTHROUGH.md) for discovery,
[STEP-2-WALKTHROUGH.md](STEP-2-WALKTHROUGH.md) for single-repository history,
[STEP-3-WALKTHROUGH.md](STEP-3-WALKTHROUGH.md) for monthly activity,
[STEP-4-WALKTHROUGH.md](STEP-4-WALKTHROUGH.md) for the month grid, and
[STEP-5-WALKTHROUGH.md](STEP-5-WALKTHROUGH.md) for the interactive interface.

| File               | Purpose                                                         |
| ------------------ | --------------------------------------------------------------- |
| `main.go`          | Command routing and human-readable output                       |
| `scan.go`          | Existing repository discovery                                   |
| `git.go`           | Shared read-only Git process execution                          |
| `history.go`       | Commit data, HEAD resolution, history retrieval and parsing     |
| `activity.go`      | Month filtering, deduplication, provenance and date grouping    |
| `activity_cli.go`  | Activity flags and agenda output                                |
| `calendar.go`      | Month arithmetic and all layout, with no terminal access        |
| `calendar_cli.go`  | Calendar flags, terminal detection, static and day output       |
| `styles.go`        | Adaptive colour palette and per-repository colour assignment    |
| `tui.go`           | Bubble Tea model: state, keys, loading and the three views      |
| `scan_test.go`     | Original discovery tests                                        |
| `history_test.go`  | Real-repository integration tests and parser checks             |
| `activity_test.go` | Multiple repositories, date boundaries, duplicates and failures |
| `calendar_test.go` | Layout alignment, week placement, overflow and highlighting     |
| `tui_test.go`      | Key handling, month clamping, stale scans and view transitions  |

Four direct dependencies: `golang.org/x/term` for terminal size,
`github.com/charmbracelet/x/ansi` for display-width truncation,
`github.com/charmbracelet/bubbletea` for the interactive interface, and
`github.com/charmbracelet/lipgloss` for adaptive colour. `ansi` is pinned to
v0.10.x because Bubble Tea's rendering stack is incompatible with v0.11.
All four commands remain available.

The module name is deliberately local for now. When a GitHub repository is chosen,
we can change it to that repository's import path. Publishing and a license choice
are separate future steps.
