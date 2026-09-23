# Step 5: making the calendar interactive

Step 4 drew a month and exited. This increment lets you move around it: arrow
keys select a date, `[` and `]` change month, and Enter descends into a date's
commits and then into a single commit. It is the first part of the project with
an event loop and application state, rather than a command that runs once.

## Try it

```powershell
go run . calendar "C:\path\to\projects"
```

Press `?` for the key reference. `q` or Ctrl+C exits.

The same command still prints a single static grid when its output is
redirected, so existing scripts keep working:

```sh
go run . calendar "path/to/projects" > september.txt
```

## 1. One command, two behaviours

`gitcal calendar` now decides at startup whether to be interactive. The check is
the one `supportsHighlight` already used in step 4 — `term.IsTerminal` — applied
to **both** stdout and stdin, because keyboard control needs a real input device,
not just a terminal to draw on.

This mirrors how `git log` decides whether to page and colourize, and it means
the useful thing happens without you having to remember a flag.

The cost is that one command now has two behaviours based on context you cannot
see in the line you typed. The standard mitigation, which we follow, is that
explicit flags always beat detection: `--interactive` forces the full interface
even when redirected, and `--static` forces plain text even on a terminal. The
latter is what you want for screenshots and for debugging layout.

`--day` continues to print its list and exit, since it already names one date.

## 2. The Elm architecture

Bubble Tea structures a program around three methods:

- `Init() tea.Cmd` — what to do on startup.
- `Update(tea.Msg) (tea.Model, tea.Cmd)` — given a message, return a **new**
  model and optionally more work.
- `View() string` — render the current model.

Two consequences are worth internalising. `Update` is the only place state
changes, so every behaviour in the application is reachable by sending it a
message — which is exactly why the tests in `tui_test.go` need no terminal.
And `View` returns a plain string, which is why step 4's decision to keep
`renderCalendar` pure paid off: the grid renderer needed a `Selected` field and
a replaceable footer, and nothing else.

Our model holds the folder, the current month, the selected date, which of the
three views is active, the index of the selected commit, the loaded activity,
loading and error state, and the terminal size.

## 3. Work that takes time cannot happen in Update

`Update` must return promptly or the interface freezes. Reading every
repository's history takes seconds, so it cannot run there.

A `tea.Cmd` is a function returning a message. Bubble Tea runs it on its own
goroutine and feeds the result back into `Update` as an ordinary message. So
loading becomes: set `loading = true`, return a command, render a status line,
and handle the `activityLoadedMsg` whenever it arrives.

`Init` cannot modify the model — it only returns a command — so the first load
is triggered by having `Init` emit a `reloadMsg`. Startup then flows through the
same path as pressing `r`, instead of being a special case.

## 4. Two problems that only appear once months are navigable

Holding `]` moves through months faster than each month can be read. That
creates two distinct bugs, and they need two different fixes.

**Wrong data on screen.** A scan for October can finish after you have already
moved to November, and rendering it would show October's commits under a
November heading. Every scan carries a request number, and results whose number
is not the current one are dropped:

```go
if message.request != m.request {
	return m, nil
}
```

**Wasted work.** Dropping a result does not stop the scan that produced it, so
several full history reads would compete for the disk. Each load therefore gets
a cancellable context, and starting a new one cancels its predecessor.
`collectActivity` already accepted a `ctx` and checked it between repositories,
so this needed no changes to the scanning code.

This is why we chose to keep per-month loading rather than reading everything
once at startup: the cost is visible and cancellable, and caching stays a
self-contained later step.

## 5. Selection stops at the month's edge

Pressing ← on the 1st does nothing, rather than rolling into the previous month.

The reason is the previous section. With per-month loading, rolling over would
mean a key that usually costs nothing occasionally triggers a multi-second
rescan. Keeping `[` and `]` as the only way to change month makes the expensive
action always deliberate. If caching later makes month changes cheap, this is
worth revisiting.

Clamping also applies when changing month: moving from 31 January to February
lands on the 28th, because the selected date must exist.

## 6. Three views, one model

`viewGrid`, `viewDay`, and `viewCommit` are a small state machine, with Enter
descending and Esc ascending. Enter on a date with no commits does nothing, so
the day view is never empty.

The two list views are new pure functions in `calendar.go`. `renderDayList`
shows one compact line per commit and scrolls to keep the selection visible on a
busy date, reporting how many entries are hidden above and below.
`renderCommitDetail` shows one commit in full. The verbose `renderDay` used by
`--day` is untouched; the interactive list is deliberately terser because detail
now has a level of its own.

## 7. A dependency conflict, and what it teaches

Adding Bubble Tea broke the build. Its rendering stack expects
`charmbracelet/x/ansi` v0.10.x, where `Style.Italic()` takes no argument; step 4
had pinned v0.11.8, where it takes a `bool`. Go's module resolution picks the
highest requested version of a shared dependency, so Bubble Tea's own code was
compiled against an `ansi` it did not support.

The fix was to pin `ansi` back to v0.10.1. Our own use of it — `StringWidth` and
`Truncate` — is identical in both versions, and step 4's alignment tests passing
unchanged is the evidence.

The lesson is about "just take the newest version". Newest is not free: it
constrains what you can add later, and the constraint surfaces at the worst
moment, in someone else's code. A test suite that verifies behaviour rather than
versions is what makes a downgrade like this safe to do quickly.

## Intentional limits of this increment

No repository or group filtering, no author filtering, and no saved settings —
those are steps 6 and 7. Every month change still re-reads all history, so a
large projects folder is slow; caching is step 8. Commit bodies are not read,
only subjects, so the detail view shows the subject line alone. The grid itself
does not scroll: a month of very busy dates can exceed a short terminal.
