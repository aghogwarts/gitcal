# Verification

This document records how the current project is checked. The individual step
walkthroughs remain historical learning notes; this file describes the combined
application.

## Automated checks

Run these commands from the folder containing `go.mod`:

```sh
gofmt -l .
go vet ./...
go test -count=1 ./...
```

`gofmt -l .` should print nothing. The test suite creates temporary repositories
and points `GITCAL_CONFIG` at temporary files, so it does not read or modify the
user's repositories or gitcal configuration. Git must be available on `PATH`.
A directory-symlink test may skip on Windows accounts that cannot create
symlinks.

Cross-platform compilation is checked for:

- Windows on amd64 and arm64;
- macOS on amd64 and arm64; and
- Linux on amd64.

Cross-compilation verifies that the code builds for each target. It does not
replace running the executable on that operating system.

## What the tests cover

- Repository discovery: ordinary repositories, nested repositories, linked
  worktrees, directory links, invalid roots, partial failures, and cancellation.
- History: complete fields, author timestamps, merge commits, worktrees, branch
  scope, malformed Git output, empty repositories, and read-only behaviour.
- Activity: month boundaries, daylight-saving transitions, stable ordering,
  duplicate commit hashes, partial results, streaming history, and timing output.
- Calendar rendering: week placement, six-week months, responsive cell content,
  display-width alignment, overflow summaries, adaptive date states, repository
  colours, and the complete selected-date accent outline.
- Interactive calendar: movement, month changes, loading and error states,
  cancellation, detail navigation, help, group cycling, mine-only filtering,
  refreshes, and the six-view in-memory cache.
- Repository picker: group editing, exclusions, reserved characters while
  typing, cancellation, scan interruption, cache invalidation, and state
  preservation when returning to the calendar.
- Configuration: atomic save/load round trips, Windows path handling, multiple
  roots, groups, exclusions, identities, explicit-folder overrides, and command
  argument validation.

## Native checks

The application has been exercised natively on Windows 11 with Git on `PATH`,
including repository discovery, saved roots, grouping, exclusions, identities,
activity timing, and the interactive calendar. The terminal UI and selection
states have also been reviewed there.

Native macOS terminal verification is still pending. In particular, it should
confirm the configuration location, keyboard handling, colour rendering, and
path matching on the user's actual filesystem.

## Limits of verification

- A successful cross-build does not establish native runtime behaviour.
- The atomic configuration write is tested as a round trip, but no test kills
  the process during the rename. Concurrent configuration edits are not
  coordinated; the last writer wins.
- Terminal colour tests validate emitted styles and display width, not every
  terminal emulator or theme.
- `activity --timings` is a diagnostic for comparing changes on the same
  machine, not a portable benchmark.
- Persistent caching and progress reporting remain deferred work, as documented
  in the README.
