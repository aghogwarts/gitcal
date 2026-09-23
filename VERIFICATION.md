# Step 2 verification

Checked in a Linux workspace with Go 1.27.1:

- `gofmt` applied to changed Go source and test files.
- `go test -v ./...`: all 11 test functions passed, including the four original
  scanner tests. No tests were skipped in this run.
- `go vet ./...`: passed.
- Compilation passed for Linux amd64, Windows amd64, macOS amd64, and macOS arm64.
- The built Linux executable successfully ran both `history` and `scan` against
  a temporary repository with spaces in its path. History displayed a known
  timestamp correctly with `TZ=Asia/Kolkata`. A non-repository path returned exit 1.

The build commands used `-buildvcs=false` because workspace VCS metadata is not
part of this downloadable project. Go build outputs and the temporary compiler
are not included in the ZIP; it contains source and documentation.

New tests cover: the 20-commit limit, Unicode subjects, names with spaces, author
timestamps and offsets, unmerged-branch exclusion, merges and multiple authors,
empty and orphan branches, detached worktrees, invalid paths, inherited Git
environment overrides, broken objects, cancellation, malformed log records,
local-time date rollover, command exit statuses, terminal control-character
handling, and unchanged repository status after reading history.

Cross-compilation confirms that code builds for the target; it does not establish
native runtime behavior. This increment still needs a run on your Windows or Mac
machine. Permission behavior and cancellation during a long-running scan have not
been exhaustively tested on those platforms.
