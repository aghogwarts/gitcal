# Step 3: combining one month's activity

We can already discover repositories and read commits from one repository. This
increment connects those pieces, selects a calendar month, removes duplicate
commits, and groups the results by local day. The output is a daily agenda; the
traditional month grid will consume the same structured data in a later piece.

## Try it

From the folder containing `go.mod`, run:

```powershell
go run . activity --month 2026-09 "C:\path\to\projects"
```

On macOS, use your corresponding folder path, such as `"$HOME/projects"`. Unlike
`history`, `activity` takes the parent folder containing your repositories, just
like `scan`. You may also point it directly at one repository.

Leave out `--month` to use the current local month. Put options before the folder:
Go's standard flag parser stops interpreting flags when it reaches a positional
argument. `go run . activity --help` explains the syntax.

## 1. Keep the old command, make its reader reusable

`readHistory(ctx, folder)` now calls `readCommits(ctx, folder, historyLimit)`.
The original 20-commit behavior is preserved. `readCommits` accepts a limit; zero
means we do not pass a count limit to Git. All other Git retrieval logic is reused.

A monthly view cannot be built from only the latest 20 commits. A busy repository
might have hundreds of commits in the month. A past month's commits might also
be entirely outside the latest sample. Reading all reachable commits avoids
silently showing an incomplete month.

For now we filter the returned author timestamps ourselves. Author timestamps
can differ from committer timestamps and need not follow log order, so we do not
stop when we encounter the first apparently old author date. The tests create a
matching commit beyond the preview limit and outside the committer-date month.

This has a cost: full reachable history is loaded into memory for each repository,
one repository at a time. We retain only matching entries in the final activity
result. A future streaming reader/cache can improve this without changing the
calendar's data model. This increment does not claim an optimized large-repo scan.

## 2. Add a data model for combined activity

`Activity` contains the month, counts of discovered/read repositories, grouped days,
and warnings. Each `ActivityDay` contains a date and entries. Each `ActivityEntry`
contains one `Commit` and a list of repository paths.

Why a list of paths for one commit? A clone or a second worktree can expose the
same commit. We want to count that commit once and still remember all the places
where it was found. Full paths also distinguish projects with identical names in
different parent folders.

No terminal output is produced by `collectActivity`. As before, data preparation
and presentation are separate, allowing the eventual calendar to reuse the result.

## 3. Define the month using calendar boundaries

We construct midnight on the first day in the requested timezone, then use:

```go
end := start.AddDate(0, 1, 0)
```

The three arguments are years, months, and days. This advances by one calendar
month. Adding a fixed number of hours would mishandle months with different
lengths and timezones with daylight-saving changes.

A commit belongs to the month when its instant is at or after `start` and before
`end`. In code we skip anything outside that interval:

```go
if commit.AuthoredAt.Before(start) || !commit.AuthoredAt.Before(end) {
    continue
}
```

The next month's midnight is excluded, preventing the same instant from appearing
in two adjacent months. Time comparisons compare instants even if their original
timezone offsets differ. Only after filtering do we convert to local time to
choose a date string.

For example, September at UTC+05:30 starts at August 31, 18:30 UTC. Filtering on
the commit's original written date alone would put some boundary commits in the
wrong month. The tests cover exact start/end boundaries, leap day, and a month
with a daylight-saving transition.

## 4. Use a map to remove duplicate commits

```go
byHash := make(map[string]*ActivityEntry)
```

A map associates a key with a value. Here the key is the full commit hash and the
value is a pointer to an activity entry. The full hash is used for identity even
though terminal output shortens it.

```go
if existing, ok := byHash[commit.Hash]; ok {
    existing.Repositories = append(existing.Repositories, repo)
} else {
    byHash[commit.Hash] = &ActivityEntry{
        Commit: commit,
        Repositories: []string{repo},
    }
}
```

Map lookup returns the value and a boolean saying whether the key exists. `ok` is
that boolean. `&ActivityEntry{...}` creates a value and takes its address; the `*`
in the map's value type means it stores a pointer. Updating `existing.Repositories`
therefore updates the entry already in the map.

This distinguishes storage copies from distinct commits. Matching messages are
not enough to deduplicate: two different commits can have the same subject.
Rebased or cherry-picked commits with different hashes remain separate entries.

## 5. Group by date, then sort explicitly

For every unique entry, we obtain its local date:

```go
date := entry.Commit.AuthoredAt.In(start.Location()).Format("2006-01-02")
```

A second map, `map[string][]ActivityEntry`, gathers entries under that date.
Each key has a slice of commits belonging to its day.

Go does not guarantee map iteration order. We sort the result explicitly:

- Date groups ascend from the earliest to latest day.
- Entries ascend by author timestamp.
- Equal timestamps use the full hash as a deterministic tie-breaker.
- Repository paths are sorted too.

Fixed-width `YYYY-MM-DD` strings sort chronologically, which makes date-key sorting
straightforward. This stage uses author-time order, unlike `history`, which keeps
Git's normal log order. The calendar cares about when each event belongs.

## 6. Continue when one repository fails

An invalid scan root is a fatal error. A broken repository discovered alongside
healthy ones is a warning: we retain the healthy data and keep processing.

We track how many valid repositories were discovered and how many histories were
successfully read. Empty repositories count as successful reads. Discovery warnings
are also retained, so inaccessible paths or invalid `.git` candidates are visible.

When warnings exist, the CLI prints an explicit incomplete-results message and
returns exit code 1. It never quietly presents a partial result as complete. Ctrl+C
cancellation remains a fatal return, rather than a warning to continue past.

## 7. Introduce flags with the standard library

`activity_cli.go` uses `flag.NewFlagSet` for this command's options. `options.String`
registers a string flag with a default and help text, returning a pointer to its
value. `*monthText` dereferences that pointer to read the parsed value.

`flag.ContinueOnError` makes parsing return errors instead of ending the process.
Our command decides the exit status, just as it does for discovery and history.
Invalid options or months return 2; help returns 0. The standard library is enough
for this small command, so we have not introduced Cobra yet.

`time.ParseInLocation` parses `YYYY-MM` using local time, rather than assuming UTC.
It validates month values, and an additional check excludes year zero.

## 8. Understand what the agenda includes

The activity scope is the history reachable from each discovered checkout's HEAD.
It includes merged history, all authors, and merge commits. Unmerged branches are
excluded unless another discovered checkout/worktree has that branch checked out.
Shallow clones can only contribute the history they contain locally. We never fetch.

These are explicit rules for the current learning increment; work/personal groups,
identity filters, saved repository selections, and final calendar options are still
future steps. Date grouping is separate from repository groups: a date groups
events for display, while work/personal will classify which repositories are selected.

This command prints only dates with commits. The planned traditional calendar will
also need empty dates, weekday placement, month navigation, and limited event space
inside each cell. Those are presentation concerns built on top of this result.

## What to check on your machine

Run `go test ./...`, then choose a month with known work and a small projects folder.
Check a few entries against familiar commits. If the folder contains multiple clones,
find a shared commit and confirm it is shown once with multiple repository paths.
The summary's unique count is therefore not necessarily the sum of every checkout's
individual commit count.

The main concepts in this increment are composition (reusing existing functions),
maps, pointers, calendar boundaries, explicit sorting, and partial-error reporting.
