# Step 1 walkthrough: finding repositories

This explains the original discovery increment. Step 2 adds a `history` command
and moves the shared Git process setup into `git.go`; see `STEP-2-WALKTHROUGH.md`.
The scanner's traversal behavior is unchanged.

The purpose of this increment is to answer one question: **which Git working
repositories exist beneath a folder?** Everything the calendar does later depends
on that answer. Keeping discovery separate lets us understand and verify it first.

## 1. How Go sees the project

`go.mod` defines a module: a collection of Go packages that belong together.
`module gitcal` is our temporary module name. The `go` line sets the minimum Go
language version; it is not a requirement to install that exact compiler release.

Both production files begin with `package main`. Files in the same directory and
package are compiled together, so `main.go` can call `scan` in `scan.go` without
importing it. A `main` package with a `main()` function builds into an executable.

`go run .` compiles and runs the package in the current directory. The remaining
arguments are passed to our application. `go build` creates an executable we can
run directly. There is no `go.sum` because this increment has no external modules.

## 2. The command enters through main.go

For `gitcal scan /some/folder`, `os.Args` contains the executable name, `scan`,
and `/some/folder`. `os.Args[1:]` is a slice expression that removes the executable
name and passes the remaining arguments to `run`.

We accept a small command grammar: `scan` followed by exactly one folder, or help.
Manual parsing is sufficient for these two cases. Cobra can come in when groups,
filters, and multiple options make a command framework worthwhile.

`run` accepts two `io.Writer` values. An interface describes behavior that a value
must provide. Here, anything that can receive written bytes will do. Real runs use
the terminal; tests can use an in-memory buffer. That keeps the command easy to
exercise without starting a second copy of our executable.

Normal output goes to stdout; errors and warnings go to stderr. This means a future
shell script can redirect repository paths separately from diagnostic messages.
Our current normal output also includes a human-readable count, so it is not yet
a formal machine-readable format.

`run` returns an integer exit status. `main` gives it to `os.Exit`. Zero means
success; nonzero indicates a problem. When invoked through `go run`, the Go launcher
may present the child's failure as `exit status N`; use the built executable when
testing exact process exit codes.

## 3. Cancellation is carried in a context

`signal.NotifyContext` creates a context that becomes cancelled when Ctrl+C sends
an interrupt. A context carries cancellation between functions; it is not a thread.

We pass it to `scan`, check it while traversing, and pass it to
`exec.CommandContext`. This lets an interrupt stop both discovery and a Git command.
Cancellation does not make an already-blocked filesystem operation instantly return.

After `run` returns, `stop()` unregisters the interrupt handler before `os.Exit`
ends the process. This cleanup is explicit because `os.Exit` does not run deferred
functions (functions scheduled with Go's `defer` keyword).

## 4. scan.go turns a folder into a result

```go
type scanResult struct {
    Repositories []string
    Warnings     []error
}
```

A `struct` groups related fields. `[]string` and `[]error` are slices: lists whose
length can change. `append` adds elements and returns the updated slice.

`scan` returns `(scanResult, error)`. Multiple return values are common in Go.
The error is `nil` when the operation completes; otherwise callers decide what to
do. We distinguish a fatal error (the chosen folder cannot be scanned) from warnings
(one candidate or child folder could not be checked, but others might work).

Before traversal, we resolve the root to an absolute path, resolve any root symlink,
ensure it is a working folder, and locate Git. Returning early on failures keeps
the main traversal logic less deeply nested.

The `%w` in `fmt.Errorf` wraps an underlying error while adding context. That keeps
the original cause available to future code that wants to inspect it.

## 5. Walking the filesystem

`filepath.WalkDir` visits entries beneath the chosen folder. We pass it a callback:
a function called for each entry, receiving its path, directory-entry information,
and any traversal error. The callback can capture and update `result` from the
surrounding function. That is a closure.

The first callback checks are cancellation and traversal errors. We must handle
an error before accessing the entry because entry information can be absent.

For most entries, the callback returns `nil`, meaning continue. When it sees
`.git`, it takes the parent directory as a repository candidate and asks Git to
validate it. A directory named `.git` alone is insufficient evidence: it could be
an empty folder or damaged metadata.

After checking a `.git` directory, returning `filepath.SkipDir` skips its contents.
It does **not** skip the entire project folder. That distinction lets us discover
repositories nested inside another project's working files.

A `.git` file can refer to Git metadata elsewhere, as with linked worktrees and
submodules. Git understands that format, so we use the same validation command
for both files and directories. The actual file contents do not need a custom parser.

We use `filepath` rather than manually joining names with `/` or `\`. It follows
the target operating system's path rules. WalkDir does not follow directory symlinks
inside the traversal, helping avoid repeated scans through linked directories.

References: [Go filepath documentation](https://pkg.go.dev/path/filepath#WalkDir)
and [Git repository layout](https://git-scm.com/docs/gitrepository-layout).

## 6. Letting Git check a candidate

We invoke the equivalent of:

```sh
git --git-dir <marker-path> --work-tree <project-path> rev-parse --git-dir
```

`exec.CommandContext` receives the executable and every argument separately.
It does not run a shell. Paths containing spaces remain single arguments, and
characters inside paths do not become shell commands.

We explicitly identify the candidate's metadata, avoiding Git's usual parent-folder
search. Otherwise a broken nested candidate could accidentally be confused with
the enclosing repository.

Git-specific environment overrides inherited from the caller are removed for this
child process so they do not redirect the validation to another repository. Other
environment variables remain available. This changes only the child process.

If Git exits unsuccessfully, we retain the error and its diagnostic text as a
warning. If it succeeds, we add the absolute repository path to the result.

## 7. Why these tests exist

The tests create actual repositories in temporary folders. They exercise the
boundary between our traversal and Git, where merely mocking a response could
hide a mistake. The fixtures cover:

- An ordinary repository, including a path with spaces.
- An empty nested repository and a linked worktree with a `.git` file.
- A repository inside Git metadata, which must not be traversed.
- Invalid `.git` metadata, which must produce a warning.
- An inherited `GIT_DIR` override, which must not redirect discovery.
- Empty and invalid scan roots, cancellation, and directory symlinks.
- Incorrect command arguments and their exit status.

`t.TempDir()` creates temporary test storage with automatic cleanup. `t.Helper()`
makes failures in the Git fixture helper point back to the calling test. `t.Setenv`
restores changed environment variables after the test. The symlink test skips when
the host does not allow creating symlinks; it is not silently counted as a pass.

## What to try yourself

Run the scanner on one small projects folder, then run it on a repository directly.
Check that nested repositories are still listed. Try a nonexistent path and a path
containing spaces. These are useful experiments for connecting the code to behavior.

The next increment, after this one is understood, is reading commits from a single
repository. It will introduce a commit data type and parsing Git's output. Groups
and the calendar come after that foundation.
