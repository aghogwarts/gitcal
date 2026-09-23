# Step 4: the static month grid

Step 3 gave us commits grouped by local date. That is already the calendar's
data; what is missing is the calendar. This increment draws a traditional month
grid — seven weekday columns, dates in their correct places, and a few commits
as events inside each date's cell — plus a way to read one date in full.

There is no keyboard navigation yet. The grid is printed once and the program
exits. Interactivity is step 5.

## Try it

From the folder containing `go.mod`:

```powershell
go run . calendar --month 2026-09 "C:\path\to\projects"
```

```sh
go run . calendar --month 2026-09 "$HOME/projects"
```

Omit `--month` for the current month. To read a single date completely:

```sh
go run . calendar --day 2026-09-18 "path/to/projects"
```

`--per-day N` changes how many commits each cell shows before it summarises the
rest, and `--width N` overrides terminal detection, which is useful for
screenshots and for checking narrow layouts.

## 1. A new command rather than a new flag

`activity` still prints its chronological agenda, unchanged. `calendar` is a
separate command reading the same `collectActivity` result.

Two commands over one data source cost slightly more code than one command with
a `--calendar` flag, but each command's help text stays truthful about what it
does, and neither accumulates flags belonging to the other. It also gives step 5
a clear home: `calendar` grows keyboard handling while `activity` stays a plain,
scriptable list.

The split inside step 4 matters as much. `calendar.go` turns an `Activity` value
into a string and never touches the terminal, a file, or the clock.
`calendar_cli.go` does all of that and passes the answers in:

```go
type calendarOptions struct {
	Width         int
	Today         string
	Highlight     bool
	EntriesPerDay int
}
```

This is the same separation as step 1's "the scanner returns data, the command
decides how to display it", applied one level further in. Its practical payoff
appears in section 8.

## 2. Our first third-party dependencies

Until now `go.mod` required nothing. Drawing a grid needs two answers the
standard library does not give.

**How wide is the terminal?** `golang.org/x/term` provides `term.GetSize`, which
reads the real console size on Windows and Unix alike. The obvious alternative,
the `COLUMNS` environment variable, is unreliable and specifically unreliable on
Windows, where PowerShell does not export it.

**How much space does this text occupy?** Go offers three different notions of
length, and only one of them is right here. `len(s)` counts *bytes*, so `é` is
two and truncating by `len` can cut a character in half.
`utf8.RuneCountInString(s)` counts *code points*, which is correct for English
but still wrong for `日`: one rune, two terminal columns. What we need is
*display width*, which needs Unicode tables and grapheme clustering.
`github.com/charmbracelet/x/ansi` supplies `StringWidth` and `Truncate` for
exactly this.

Why alignment deserves a dependency: in a seven-column grid, one mismeasured
cell shifts every column to its right for that entire row. A single emoji in a
commit subject would visibly break the calendar. `ansi` also ignores ANSI escape
sequences when measuring, which section 7 depends on.

These versions raise the module's `go` directive to 1.26.0, because a module must
declare at least the highest Go version its dependencies require. Older releases
of both libraries support much older Go if that floor ever becomes a problem.

`go.mod` now distinguishes what we import from what our dependencies pulled in:
`require` blocks marked `// indirect` are transitive. `go mod tidy` maintains
this, and `go.sum` records checksums so future builds get identical code.

## 3. Dividing the terminal into seven columns

Seven cells and the eight vertical rules between them share the width:

```go
width := (total - (calendarColumns + 1)) / calendarColumns
```

The result is clamped between 10 and 28 columns. The lower bound keeps `+N more`
readable and makes the grid 78 columns wide, which fits a standard console. The
upper bound stops a wide monitor from stretching one month across the screen.

Each cell reserves one space of padding on each side, so its usable text area is
two columns narrower than the cell.

## 4. Calendar arithmetic

Two calculations place every date, and both avoid hardcoded month lengths.

Go numbers weekdays with Sunday as 0. We want Monday first:

```go
offset := (int(first.Weekday()) + 6) % 7
```

Adding 6 before taking the remainder rotates the week by one position, mapping
Monday to 0 and Sunday to 6.

For the month's length we ask the calendar instead of a lookup table:

```go
daysInMonth := first.AddDate(0, 1, -1).Day()
```

One month forward, one day back, from the first of the month. This is correct
for 28, 29, 30, and 31-day months without a leap-year rule anywhere in our code.

The loop then walks in steps of seven starting from `1 - offset`, so the first
week begins on a possibly negative number and the out-of-range cells simply stay
empty:

```go
for start := 1 - offset; start <= daysInMonth; start += 7 {
```

The worst case is a 31-day month beginning on a Sunday — March 2026 — which needs
six week rows. Nothing in the loop assumes five.

## 5. Weeks are built column-first, then printed row by row

A cell's height depends on how many commits that date has, but every cell in a
week must be the same height or the vertical rules will not line up. So each
week is built as seven lists of lines, the tallest is measured, and then the
week is printed one row at a time with missing lines rendered as blanks.

Weeks with no commits collapse to a single row, which keeps a quiet month short.

## 6. Fitting an event into a cell

Each entry is built by adding fields only while they still leave room for the
subject:

- always the commit subject;
- the repository name once the text area reaches 12 columns;
- the time once it reaches 20.

The repository name is `filepath.Base` of the first source path, so a cell shows
`api` rather than `/home/you/projects/api`. When a date has more commits than
`--per-day` allows, the remainder becomes `+N more`. The summary line below the
grid always counts every commit, not the visible ones, so nothing is silently
lost — and `--day` shows the rest.

`ansi.Truncate` shortens to the text area and appends `…`, and the remaining gap
is padded by display width rather than by character count.

## 7. Highlighting today without breaking the grid

Today's date number is wrapped in `\x1b[7m` and `\x1b[0m`, the ANSI codes for
reverse video and reset. Those bytes are invisible on screen but present in the
string, so any width function that counted them would push that cell's contents
out of alignment. `ansi.StringWidth` ignores them, which is the second reason we
chose that library.

The escape codes are emitted only when output is a real terminal and `NO_COLOR`
is unset, so redirecting to a file produces clean text.

## 8. Testing a terminal layout without a terminal

Because `renderCalendar` is a pure function, the tests build `Activity` values
directly instead of creating Git repositories. They run in milliseconds and can
check things that are awkward to check by eye.

The most valuable test renders subjects containing emoji, Japanese text, and
accented Latin characters at five different widths, then asserts that every line
of the grid has the same display width. That single assertion catches the whole
class of alignment bugs, including ones a Latin-only test would never reveal.

The others confirm Monday-first placement, six-week months, `+N more` accounting,
fields disappearing as cells narrow, the highlight appearing only when asked, and
control characters never reaching the output.

## Intentional limits of this increment

The grid is printed once; there is no navigation, selection, or month paging.
Weeks always start on Monday — the configurable Sunday start is still future
work. Only the first repository path is shown inside a cell, though `--day`
lists them all. All authors and merge commits are still included, and the
performance characteristics of step 3 are unchanged: full histories are read
into memory before filtering.
