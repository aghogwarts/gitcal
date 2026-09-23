# Step 2: turning Git history into Go data

We now have two commands: `scan` discovers repositories; `history` reads commits
from one repository you choose. Keeping this increment to one repository lets us
learn data retrieval and parsing before combining repositories or building the UI.

## Try the change

Extract the updated ZIP into a new folder. Open the inner `gitcal` folder (the one
containing `go.mod`) in your editor. Your earlier copy can remain as a reference.

Run `go run . scan "your-projects-folder"`, pick one repository path from the
output, then run `go run . history "that-repository-path"`.

The path after `history` must be the repository root, containing its `.git` file
or directory. The folder containing our program is where you run `go run .`.
They are usually different folders.

## 1. Start with a data model

```go
type Commit struct {
    Hash        string
    AuthorName  string
    AuthorEmail string
    AuthoredAt  time.Time
    Subject     string
}
```

This struct defines the information one commit carries. The hash identifies the
commit; the author fields will eventually support identity filters; the timestamp
will put it on a calendar; the subject is the short text shown as an event.

`time.Time` represents a date and time, including timezone information. It is more
useful than keeping a date as an arbitrary string: we can convert its timezone,
compare instants, and later determine which calendar day it belongs to.

The scanner returned a list of paths. `readHistory` returns `[]Commit`: a slice of
structured commit values. Neither function needs to know how a calendar is drawn.

## 2. Route the command in main.go

Previously, `run` accepted only `scan <folder>`. It now recognizes `history` too
and calls `runHistory`, which asks `readHistory` for data and prints the result.

The direction of dependencies is deliberate: command handling calls retrieval;
retrieval returns data. Printing stays out of retrieval. That means the eventual
calendar can call `readHistory` without trying to recover data from terminal text.

For invalid usage the CLI returns exit code 2. For retrieval failures it returns 1.
A valid empty branch returns 0 with an explanatory message.

## 3. Validate the exact repository

`readHistory` resolves the supplied path, looks for its own `.git` marker, finds
the installed Git executable, and validates the metadata using our existing
validation function. Both `.git` directories and worktree/submodule `.git` files
go through Git's own validation.

This exact-root requirement prevents a subtle surprise. Git normally searches
parent directories for a repository when invoked inside a subfolder. For this
increment we want the precise repository that the user selected from `scan`.

## 4. Resolve HEAD and recognize an empty branch

`HEAD` identifies the current checkout. Usually it refers to a branch; in a detached
checkout it refers directly to a commit. We ask Git to resolve `HEAD` to a commit
hash, then pass that hash to the history command. Using the resolved hash keeps
the starting point consistent even if another process changes branches afterward.

A brand-new repository has a branch name but no first commit. Git calls this an
unborn branch. A failed HEAD lookup alone is insufficient to conclude that the
repository is empty; the metadata could also be broken.

`resolveHead` checks whether HEAD is symbolic and whether that exact branch ref
is absent. Only the missing-branch case becomes an empty result. Other failures
remain errors. `hasExitCode` uses `errors.As` to find a Git process error even after
we wrapped it with explanatory text using `%w`.

This helper is supporting edge-case handling. When first reading the code, focus
on the normal path: validate root, resolve HEAD, read log, parse records.

## 5. Ask Git for a predictable format

Regular `git log` output is designed for people, and its appearance can be
configured. We request explicit fields:

| Placeholder | Value                                      |
| ----------- | ------------------------------------------ |
| `%H`        | Full commit hash                           |
| `%an`       | Author name as recorded in the commit      |
| `%ae`       | Author email as recorded in the commit     |
| `%aI`       | Author timestamp in strict ISO 8601 format |
| `%s`        | Subject, rather than the full message body |
| `%x00`      | A NUL byte separating fields               |

The format is `%H%x00%an%x00%ae%x00%aI%x00%s`. The `-z` option makes Git terminate
each record with NUL as well. Names and subjects can contain spaces, punctuation,
and Unicode, so splitting them on spaces or pipes would be unreliable. Normal
Git-created commit metadata does not contain NUL separators; malformed output is
rejected instead of partially displayed.

Other options limit output to 20 commits, request UTF-8, and disable color,
decorations, notes, signature displays, and patch text. An ending `--` separates
revision arguments from any possible path filters; we supply no path filters.

The code passes arguments separately to `exec.CommandContext`. It does not run a
shell, so paths containing spaces do not require handmade shell escaping.

References: [git log](https://git-scm.com/docs/git-log) and
[Git pretty formats](https://git-scm.com/docs/pretty-formats).

## 6. Share process handling without changing discovery

The original scanner had its Git process setup inside `validateRepository`.
`git.go` now provides `runGit`, which both discovery and history use.

`args ...string` is a variadic parameter: callers can supply several command
arguments, and the function receives them as a slice. `commandArgs...` expands
that slice back into arguments when starting the process.

The helper preserves our explicit metadata path, working directory, cancellation,
and removal of inherited `GIT_` environment overrides. A new distinction matters:
stdout contains the data we parse, while stderr contains diagnostics. We capture
them separately so an error message cannot be mistaken for a commit field.

On failure, the helper adds Git's diagnostic message and preserves the underlying
error. On cancellation, it returns the context's cancellation error.

## 7. Parse bytes into Commit values

`parseHistory` verifies the final NUL terminator, splits on NUL, and checks that
the number of fields is a multiple of five. It then processes five fields at a
time, turning each group into a `Commit`.

`time.Parse(time.RFC3339, value)` converts the ISO timestamp into `time.Time`.
If the timestamp is invalid, parsing returns an error identifying the commit.
The function never prints; malformed input cannot leave a partial successful
history on screen.

`make([]Commit, 0, len(fields)/5)` creates an empty slice with enough capacity for
the expected commits. Length is the number currently present; capacity is the
space available before growth needs another allocation. `append` adds each value.

## 8. Format for the user's machine

The stored `AuthoredAt` retains the original timestamp offset. Only display uses
`AuthoredAt.In(time.Local)`. For example, 23:30 at UTC-07:00 becomes 12:00 the next
day at UTC+05:30. That day change is important for our eventual calendar.

Go formats dates using a reference date rather than tokens such as YYYY:

```go
commit.AuthoredAt.In(time.Local).Format("2006-01-02 15:04 -07:00")
```

The reference components represent year, month, day, 24-hour time, and UTC offset.
The full commit hash stays in the struct; `%.12s` displays just its first 12
characters. We replace terminal control characters in displayed text with spaces
without modifying the stored subject or author values.

## 9. What this history means

This command follows the current checkout's history, including merged ancestors,
up to 20 commits. It includes all authors and merges. It does not combine unmerged
branches or all repositories yet, and these are not finalized calendar count rules.

The displayed date is the author date. Git's normal log order is generally based
on committer dates, so displayed author times can appear out of order. An author
timestamp describes when a change was authored; the committer timestamp records
when this commit was created/applied, which can differ after a rebase or cherry-pick.
We preserve Git's order rather than silently sorting the limited sample by another
field. Later calendar grouping will use the chosen timestamp policy explicitly.

## 10. How we verified it

Tests use real temporary repositories to check: the 20-commit cap, unmerged-branch
exclusion, merged history, multiple authors, author-vs-committer dates, timezone
rollover, Unicode subjects, detached worktrees, empty/orphan branches, errors,
cancellation, malformed records, and unchanged repository status. Original scanner
tests continue to run. Check `VERIFICATION.md` for the actual verification results.

Run `go test ./...` yourself. Then compare `go run . history "repo-path"` with
`git -C "repo-path" log -20`. Choose a familiar repository so you can recognize
the commit subjects and connect the output to the code.

Pause here before adding more functionality. The main concepts to understand are
the Commit struct, the explicit Git output format, the parser, and presentation
being separate from data retrieval.
