# Step 6b walkthrough — editing settings without leaving the calendar

Step 6a gave gitcal a memory: `repos set`, `repos exclude`, and a file that
persists between runs. But it lives outside the calendar. Deciding a repository
belongs in `personal` while looking at the grid means dropping out, running a
command, and coming back. Step 6b closes that gap: press `g`, and the same
edits happen inline.

The interesting part of this step is not the feature — assign a group, exclude
a repository — but the mechanics of building a small text editor inside a
program that has no text editor widget anywhere in it yet.

## 1. A fourth view, not a new program

Steps 4 and 5 established three view levels: the month grid, one date's list,
one commit's detail. Adding a fourth — the repository picker — is one line in
the existing enum:

```go
type viewMode int

const (
	viewGrid viewMode = iota
	viewDay
	viewCommit
	viewRepos
)
```

Everything that already exists for switching between grid, day, and commit —
`Update` dispatching on `key.String()`, `View` dispatching on `m.mode`, `?`
still opening help from anywhere — keeps working unmodified. The picker only
had to plug into the same two switch statements, not invent a second program
running alongside the first.

## 2. Listing more than the filter would show

The picker's first job is deciding what to list. The obvious answer —
"whatever the current `--group` selects" — is wrong. If you launched with
`--group personal`, an excluded repository, and every `work` repository, are
invisible to the active filter. But those are exactly the repositories you
might want the picker for: un-excluding one, or moving one into `personal`.

So the picker calls `scanAll` directly, bypassing the filter entirely:

```go
func (m *calendarModel) beginReposScan() tea.Cmd {
	if m.cancel != nil {
		m.cancel()
	}
	ctx, cancel := context.WithCancel(m.root)
	m.cancel = cancel
	m.reposLoading = true
	m.reposErr = nil

	roots := m.chosen.roots
	return func() tea.Msg {
		result, err := scanAll(ctx, roots)
		return reposLoadedMsg{repositories: result.Repositories, err: err}
	}
}
```

This is discovery only — walking directories and validating `.git` markers —
not the expensive full-history read that `collectActivity` does. It runs fast
enough that opening the picker feels immediate, but it still runs as a
`tea.Cmd` rather than inline, because step 5 already established the rule that
does the exhaustive telling: **long work must run off the event loop**, and the
model cannot draw anything while `Update` is still running. "Fast" and "off the
loop" are not contradictory; the command pattern costs nothing when the work
finishes quickly.

Reusing `m.cancel` — the same field `beginLoad` uses for the month scan — means
whichever background task started most recently is the one a `q` or a new scan
will cancel. Only one of "reading a month's history" or "listing repositories"
is ever meaningfully running at once, so one field can track either.

## 3. Reading, not guessing, the field to edit

Pressing Enter on a repository opens a text field. The field should not start
empty if the repository already has a group — retyping a name you are only
adjusting would be needless friction. So opening it reads the existing value
back out of the filter that already computes it:

```go
func (m calendarModel) currentGroup() string {
	if m.reposIndex >= len(m.repos) {
		return ""
	}
	if group := m.chosen.filter.groupOf(m.repos[m.reposIndex]); group != ungrouped {
		return group
	}
	return ""
}
```

`groupOf` was written in step 6a for the CLI's `repos list`; the picker did not
need a new lookup, only a new caller. This is a small case of a principle worth
naming: when step 6a built `repositoryFilter` as a thing the renderers query
rather than a pile of `if` statements scattered through them, it was
implicitly preparing for whatever queried it next — not because that was
foreseen, but because a well-shaped abstraction tends to have more than one
use.

## 4. Building a text field with no text field widget

Bubble Tea ships no input widget in the core package — that lives in a
separate `bubbles` library, which this project has not added as a dependency.
Building one by hand turned out to be a dozen lines, because a single-line
field only needs to answer four questions: what to do with a character, with
backspace, with confirmation, and with cancellation.

```go
func (m calendarModel) handleGroupInputKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyEsc:
		m.editingGroup = false
		return m, nil
	case tea.KeyEnter:
		m.editingGroup = false
		(&m).setGroup(strings.TrimSpace(m.groupInput))
		return m, nil
	case tea.KeyBackspace:
		if runes := []rune(m.groupInput); len(runes) > 0 {
			m.groupInput = string(runes[:len(runes)-1])
		}
		return m, nil
	case tea.KeySpace:
		m.groupInput += " "
		return m, nil
	case tea.KeyRunes:
		m.groupInput += string(key.Runes)
		return m, nil
	}
	return m, nil
}
```

### Why `key.Type`, not `key.String()`

Every other handler in this program — `handleGridKey`, `handleListKey`,
`handleReposKey` itself — switches on `key.String()`, because "q" and "enter"
read naturally as strings and there was never a reason to want the letter Q
typed as data. A text field inverts that: it wants *every* letter as data,
including the ones that mean something everywhere else in the program.

`key.String()` collapses that distinction away — a "q" keypress and a "Q"
keypress both are simply the string that would be typed. `key.Type`, checked
before reaching for the string, keeps the categories apart: `tea.KeyRunes` is
"an ordinary character arrived, and here it is in `key.Runes`," entirely
separate from named keys like `tea.KeyEsc` or `tea.KeyEnter`. That distinction
is exactly what a text field needs and a key-shortcut handler does not.

### Truncating text safely

`m.groupInput[:len(m.groupInput)-1]` would corrupt a name containing anything
outside ASCII, because Go strings are byte slices and a multi-byte UTF-8
character sliced in the middle becomes invalid text. Group names are free-form,
so this was worth getting right the first time rather than after a bug report:

```go
if runes := []rune(m.groupInput); len(runes) > 0 {
	m.groupInput = string(runes[:len(runes)-1])
}
```

Converting to `[]rune` first means one backspace removes one *character*,
however many bytes it takes to encode — the same reasoning `calendar.go`
already applies with `utf8.RuneCountInString` when measuring display width in
step 4, applied here to editing instead of measuring.

## 5. Making the rest of the program forget its own shortcuts

Building the field was the easy half. The hard half was making sure nothing
*else* the program already does — quitting on `q`, opening help on `?` —
fires while someone is in the middle of typing a name like "quick-fixes".

The fix sits at the very top of `handleKey`, before any of the shortcuts are
even considered:

```go
func (m calendarModel) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While a group name is being typed, every character belongs to the text
	// field. Only Ctrl+C still ends the program; even q must be typeable.
	if m.mode == viewRepos && m.editingGroup {
		if key.Type == tea.KeyCtrlC {
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}
		return m.handleGroupInputKey(key)
	}

	switch key.String() {
	case "ctrl+c", "q":
		...
```

The condition is checked once, at the entrance to key handling, rather than
threading an `if !m.editingGroup` through every existing `case` in the
program's shortcut switch. Adding a mode that swallows global shortcuts is a
one-line guard at the top, not a change scattered across every place a
shortcut is checked — which also means the *next* view this program grows,
whatever it turns out to be, will not need to remember this rule unless it
also needs a text field.

Ctrl+C staying live even while typing is deliberate. Every other program on
the user's machine treats Ctrl+C as an unconditional stop signal; carving out
an exception here — "unless you're typing" — would be the one surprising
inconsistency in an otherwise predictable set of keys.

The test that pins this down types the letter that would otherwise quit the
program:

```go
typed := press(t, editing, keyRune("q"))
if typed.groupInput != "q" || !typed.editingGroup {
	t.Fatalf("q was not treated as text: input %q, editing %v", typed.groupInput, typed.editingGroup)
}
```

## 6. Saving on every keystroke's result, not on exit

`repos set` from the command line does one write and exits. The picker could
buffer every edit and write once when you leave with `Esc` — fewer disk writes,
and it would feel tidier. It does the opposite: every confirmed edit saves
immediately.

```go
func (m *calendarModel) saveChosen() {
	m.chosen.filter = m.chosen.config.filter(m.chosen.group)
	m.reposChanged = true
	m.reposSaveErr = saveConfig(m.chosen.path, m.chosen.config)
}
```

The reasoning is the same one that shaped `saveConfig`'s write-then-rename in
step 6a: a setting you have already told the program should not depend on
you also telling it to leave cleanly. If the process were killed — the window
closed, the machine slept badly, a crash three edits later — buffering would
lose every edit made since the picker opened. Saving immediately means the
worst a crash can cost is the one edit in flight, and that edit has not been
confirmed yet anyway.

This does mean the picker calls `saveConfig` once per edit rather than once
per session. `saveConfig` is a few hundred bytes of TOML through
`os.WriteFile` and `os.Rename` — fast enough that assigning several
repositories in a row does not feel like it is waiting on disk at all.

## 7. Only reloading the grid when there was something to reload

Filtering happens once, when `saveChosen` rebuilds `m.chosen.filter`. But the
*grid* underneath the picker is still showing whatever it read before you
opened `g` — it does not know an exclusion changed until it is told to read
again. Telling it unconditionally on every `Esc`, though, would rescan even
when you opened the picker, looked, and left without touching anything.

```go
case "esc", "backspace", "left", "h":
	m.mode = viewGrid
	if m.reposChanged {
		// A group or exclusion changed, so the grid it is about to show
		// again would otherwise contradict what was just edited.
		m.reposChanged = false
		return m, (&m).beginLoad()
	}
	return m, nil
```

`m.reposChanged` is the whole mechanism: `saveChosen` sets it, and leaving
clears it after acting on it. A boolean that means "something happened since
we last checked" is a small, reusable shape — this program already used the
same idea for `m.loading` and `m.help`.

One consequence worth stating plainly, because it was easy to get backwards
while writing this: `m.chosen.filter` is rebuilt with `m.chosen.group` — the
group the calendar was *launched* with — not a new one. Assigning a repository
into the group you are currently viewing shows up the moment you leave the
picker, because it is the same filter, freshly computed. Switching to look at
a *different* group still means restarting with a different `--group` flag;
the picker edits assignments, it does not add a way to change which group's
view you are in.

## 8. What the tests had to prove

Nine tests were added, and each one earns its place by catching something the
implementation could plausibly have gotten wrong on the first attempt:

- Pressing `g` both switches the mode and starts a scan — either one without
  the other would leave the picker either invisible or empty.
- Delivering `reposLoadedMsg` clamps a now-out-of-range index, the same
  problem `activityLoadedMsg` handling in step 5 already had to solve for a
  shorter list of commits after a page shrinks.
- Typing a name, confirming it, and reading the saved file back — proving the
  round trip through `setGroup` and `saveConfig` together, not just that the
  in-memory struct changed.
- `q` while editing produces no quit command and becomes a character instead.
- `Esc` while editing discards the typed text and leaves the saved
  configuration untouched — the mirror image of the previous test.
- Excluding, then including again, ends up back where it started, and each
  step is visible on disk immediately.
- Leaving with `Esc` returns a reload command only when `reposChanged` is
  true, checked in both directions so neither an unwanted rescan nor a missed
  one could slip through unnoticed.
- Enter with an empty repository list does nothing, rather than opening a text
  field for a repository that does not exist at that index.
- The rendered view shows an assigned group and an exclusion in the same pass,
  since both states are decided by the same `switch` in `reposView`.

None of these needed a live terminal. Bubble Tea's model is a plain Go value —
`Update` takes a message and returns a new one — so every test in this file,
including the ones from step 5, drives the state machine directly and reads
the result, the same way `calendar_test.go` checks `renderCalendar`'s output
without a terminal underneath it either.

## 9. A gap the picker exposed: changing the view itself

The picker edits assignments, but it does not change what the calendar is
*showing*. Trying it revealed the gap directly: `--group personal` only takes
effect at launch, so seeing `work` instead meant quitting and retyping the
command. `Tab` closes that, cycling the session through every group without
restarting.

```go
func (m calendarModel) groupCycle() []string {
	cycle := append([]string{""}, m.chosen.config.groupNames()...)
	return append(cycle, ungrouped)
}

func (m calendarModel) cycleGroup() (tea.Model, tea.Cmd) {
	cycle := m.groupCycle()
	index := 0
	for i, name := range cycle {
		if strings.EqualFold(name, m.activeGroup) {
			index = i
			break
		}
	}
	m.activeGroup = cycle[(index+1)%len(cycle)]
	m.chosen.filter = m.chosen.config.filter(m.activeGroup)
	m.mode, m.activity = viewGrid, Activity{Month: m.month}
	return m, (&m).beginLoad()
}
```

Two things about this were worth getting right rather than obvious by
accident.

**The cycle is a new field, `activeGroup`, not a rewrite of `chosen.group`.**
`chosen.group` is what `--group` asked for; overwriting it while cycling would
make the *next* run start from wherever this session happened to leave off,
which is not what the flag means. `activeGroup` starts equal to it and moves
independently, so quitting and rerunning without `--group` always starts from
"all" again, exactly as before this feature existed.

**The list of groups is rebuilt on every press, not once at startup.** A
model computed at launch would go stale the moment the repository picker
creates a new group — pressing Tab would cycle through yesterday's groups
while today's sits unreachable until a restart. Reading
`m.chosen.config.groupNames()` fresh each time means a group assigned thirty
seconds ago in the picker already has a stop on the wheel, which is the same
"do not make the user restart for something they just did" reasoning that
motivated the picker in the first place.

`cycleGroup` otherwise reuses `changeMonth`'s exact shape — reset to the grid,
clear the stale activity, return a `beginLoad` command — because changing which
group is in view and changing which month is in view have the same
consequence: the grid on screen no longer matches what should be read, so it
has to be re-read before it can be trusted again.

## 10. What is left

The picker edits one repository at a time; selecting several and assigning
them together would still mean the command line. Renaming or moving a
repository still leaves its saved group pointing at a path that no longer
exists — flagged as a known gap in step 6a and unchanged here. And the picker
can only edit repositories the current configuration's roots can already find;
adding a folder still means leaving the calendar for `roots add`, at least
until a later step decides that is worth solving too.
