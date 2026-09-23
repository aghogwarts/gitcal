# Step 3 verification

Checked in a Linux workspace with Go 1.27.1:

- `gofmt` applied to changed Go source and test files.
- `go test -v ./...`: all 16 test functions passed, including two calendar-month
  subtests, all seven step-2 history tests, and the four original scanner tests.
  No tests were skipped in this run.
- `go vet ./...`: passed.
- Compilation passed for Linux amd64, Windows amd64, macOS amd64, and macOS arm64.
- The built Linux executable successfully ran `activity --month 2026-09` against
  a temporary projects folder with spaces in its path, showing the expected
  commit under the machine-local date and retaining its repository path.

The build commands used `-buildvcs=false` because workspace VCS metadata is not
part of this downloadable project. Go build outputs and the temporary compiler
are not included in the ZIP; it contains source and documentation.

New activity tests cover: multiple repositories, identical-hash deduplication with
all source paths preserved, same-named projects, matches beyond the 20-commit
preview, author/committer month differences, unmerged-branch exclusion, exact
local month boundaries, timezone rollover, hash ordering for tied timestamps,
leap day, daylight-saving changes, empty repositories, cancellation, missing roots,
invalid flags/months, and partial results with explicit failure exit status.

Existing history tests cover: the 20-commit limit, Unicode subjects, names with spaces, author
timestamps and offsets, unmerged-branch exclusion, merges and multiple authors,
empty and orphan branches, detached worktrees, invalid paths, inherited Git
environment overrides, broken objects, cancellation, malformed log records,
local-time date rollover, command exit statuses, terminal control-character
handling, and unchanged repository status after reading history.

Cross-compilation confirms that code builds for the target; it does not establish
native runtime behavior. This increment still needs a run on your Windows or Mac
machine. Permission behavior and cancellation during a long-running scan have not
been exhaustively tested on those platforms. No large-repository performance
benchmark was performed; activity currently reads full reachable history into
memory for each repository before filtering by month.
