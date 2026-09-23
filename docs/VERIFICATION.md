# Step 6a verification

Checked natively on Windows 11 (amd64) with Go 1.27.1 and Git on `PATH`.

- `gofmt -l .` reported no files needing formatting.
- `go vet ./...`: passed.
- `go test ./...`: 43 test functions passed. None failed and none were skipped,
  so the symlink test that can skip on accounts without symlink privilege did
  run in this environment.
- Compilation passed for `windows/amd64`, `windows/arm64`, `darwin/amd64`,
  `darwin/arm64`, and `linux/amd64`.

## Ran natively, not just compiled

Against a real projects folder containing nine repositories, using
`GITCAL_CONFIG` so the check could not disturb a real configuration:

- `roots` with nothing configured printed the `roots add` suggestion.
- `activity` with nothing configured and redirected output exited 1 and
  explained the `roots add` command instead of waiting for input.
- `roots add` then `repos` listed all nine repositories as `ungrouped`.
- `repos set` assigned `work` and `personal`, and `repos exclude` excluded one.
  `repos` then reported `9 repositories: 1 excluded, 1 personal, 1 work,
  6 ungrouped`.
- The written file used TOML literal strings with unescaped Windows paths and
  carried its explanatory header.
- `calendar --static --group personal` drew the month and reported
  `7 unique commits on 2 days; personal only · read 1 of 1 selected
  repositories (9 discovered)`, confirming that filtering reaches the summary
  line and that discovery still counts every repository.

## What the new tests cover

Configuration: save and load round trip through a folder that does not exist
yet, unescaped Windows paths in the output, the explanatory header, and a
malformed file reported with its path.

Path matching: case differences, trailing separators, redundant `..` elements,
reassignment not leaving a duplicate entry under an old spelling, clearing a
group, and adding the same root twice under different spellings.

Filtering: group selection, case-insensitive group names, the `ungrouped`
bucket behaving as a real group, exclusion overriding a matching group,
un-excluding, and the zero filter including everything.

Commands: `roots add`/`remove`/`list` including a non-existent folder and
removing something not configured, `repos set`/`exclude`/`include`/`list`,
and wrong argument counts returning usage exit code 2.

Selection: an explicit folder overriding configured roots and not inheriting
saved exclusions, and a non-terminal run declining to prompt.

Multi-root scanning: one unreadable root warning while the readable one still
returns its repositories, overlapping roots not duplicating a repository, and
losing every root still being an error.

End to end: groups deciding which commits are counted, `SelectedRepositories`
and `DiscoveredRepositories` reported separately, excluded repositories never
contributing commits, and the group travelling with each commit for the
detail view.

Earlier increments' tests were kept and still pass, which is the evidence that
filtering did not disturb deduplication, timezone handling, grid alignment, or
the interactive key handling.

## Limits of this check

Cross-compilation confirms the code builds for a target; it does not establish
native runtime behaviour. **macOS has still not been run natively**, so
`os.UserConfigDir` resolving to `~/Library/Application Support`, and the
case-insensitive path matching that this step deliberately applies only on
Windows, remain unverified there. macOS filesystems are usually
case-insensitive, so a repository referred to with different capitalisation
may not match its saved group on a Mac; `pathKey` would need to fold case on
Darwin too if that proves to be a problem in practice.

The atomic save was reasoned about rather than tested by interruption; no test
kills the process mid-write. Concurrent runs of `gitcal` editing the
configuration at the same time are not coordinated, so the last writer wins.

No large-repository performance benchmark was performed. Exclusion now happens
before history is read, so excluding a repository does save that work, but the
selected repositories are still read in full on every month change. Caching is
step 8.
