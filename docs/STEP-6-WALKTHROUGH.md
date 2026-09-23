# Step 6a walkthrough — remembering things between runs

Steps 1 to 5 built a program that forgets everything the moment it exits. Every
run needed a folder path, and every repository under that folder counted equally.
This step gives the program a memory: a file it writes, reads back, and reasons
about.

That sounds simple, and the file handling genuinely is. The interesting problems
are the ones underneath: deciding *where* the file goes, deciding when two paths
that look different are the same folder, and keeping the new behaviour from
breaking every example that already worked.

## 1. Why the configuration does not live next to the program

The obvious place for `config.toml` is beside the executable. It is also the
wrong place, for three reasons.

A program installed into `C:\Program Files` or `/usr/local/bin` usually cannot
write to its own folder — those locations are deliberately read-only for normal
users. Settings written beside the binary also disappear the moment the binary is
replaced by an update. And if two people share a machine, they share one binary
but should not share one set of repositories.

Every operating system already has an answer, and Go exposes it as one function:

```go
directory, err := os.UserConfigDir()
```

| System  | What it returns                         |
| ------- | --------------------------------------- |
| Windows | `%AppData%`, e.g. `C:\Users\you\AppData\Roaming` |
| macOS   | `~/Library/Application Support`         |
| Linux   | `$XDG_CONFIG_HOME`, defaulting to `~/.config` |

We append our own folder so the file cannot collide with anything else:

```go
return filepath.Join(directory, "gitcal", "config.toml"), nil
```

One environment variable overrides all of it:

```go
const configPathVariable = "GITCAL_CONFIG"

func defaultConfigPath() (string, error) {
	if override := os.Getenv(configPathVariable); override != "" {
		return override, nil
	}
	...
}
```

This was added for the tests, and that is worth dwelling on. Without it, running
`go test` would read — and the command tests would *overwrite* — the real
configuration of whoever ran them. A test that damages the developer's setup is
a bad test, and a test whose result depends on that setup is a useless one.

The escape hatch turned out to be useful on its own. Trying out a different set
of folders is now one variable away, and the README documents it as a feature
rather than a test artifact. Designing for testability often produces this: the
seam you add to isolate a test is a seam users want too.

## 2. Why TOML, and why single quotes matter

The three realistic choices were JSON, YAML, and TOML.

JSON is in the standard library, but it has no comments, so a file cannot explain
itself, and it escapes backslashes. A Windows path in JSON looks like this:

```json
"roots": ["C:\\Users\\you\\source\\repos"]
```

For a file we are explicitly inviting people to edit by hand, on a platform whose
paths are full of backslashes, that is a poor first impression.

YAML allows comments but is a famously large specification, where indentation is
significant and `no` is a boolean.

TOML has comments, an unambiguous specification, and — decisively here — *literal
strings*:

```toml
roots = ['C:\Users\you\source\repos']
```

Single quotes mean "no escape processing at all". The path reads exactly as it
appears in Explorer. This is not a cosmetic detail; it is the difference between
a file people will edit and a file they will avoid.

A test enforces it, because a future library change could silently switch to
double quotes:

```go
if strings.Contains(string(written), `\\`) {
	t.Fatalf("Windows paths were escaped on the way out:\n%s", written)
}
```

### Choosing the library

Two candidates: `github.com/pelletier/go-toml/v2` and `github.com/BurntSushi/toml`.
After step 5's dependency conflict, the first thing to check was not features but
requirements. Fetching each module's `.mod` file directly from the proxy:

```
github.com/pelletier/go-toml/v2 v2.4.3  →  go 1.21.0
github.com/BurntSushi/toml      v1.6.0  →  go 1.18
```

Both declare *no dependencies of their own* and both sit well below our 1.26
floor, so neither could repeat step 5's problem of one library dragging another
into an incompatible version. That made the choice low-stakes; `go-toml/v2` won on
being the more actively maintained of the two.

The habit is the lesson. A `.mod` file is two lines and answers "what will this
drag in?" before `go get` has a chance to reshape the whole dependency graph.

## 3. Writing a file that cannot be half-written

The naive save is one line, and it has a real failure mode:

```go
os.WriteFile(path, body, 0o644)  // Truncates first, then writes.
```

`os.WriteFile` empties the file before writing the new contents. If the process is
killed in between — Ctrl+C at an unlucky moment, a power cut, a full disk — the
file on disk is empty or truncated mid-value. The user's settings are gone, and
the next run reports a parse error.

The fix is to write somewhere else and then swap:

```go
temporary := path + ".tmp"
if err := os.WriteFile(temporary, append([]byte(configHeader), body...), 0o644); err != nil {
	return fmt.Errorf("write configuration: %w", err)
}
if err := os.Rename(temporary, path); err != nil {
	os.Remove(temporary)
	return fmt.Errorf("replace configuration: %w", err)
}
```

A rename within one filesystem is *atomic*: at every instant, `config.toml` is
either entirely the old contents or entirely the new ones, never a mixture.
Interrupting the write now leaves a stray `.tmp` file, which is harmless.

Note the `os.Remove` on the failure path. Without it, a failed rename leaves the
temporary file behind permanently.

### The missing file is not an error

```go
content, err := os.ReadFile(path)
if errors.Is(err, fs.ErrNotExist) {
	return loaded, nil
}
```

A first run has no configuration, and that is completely normal. Returning an
error would force every caller to distinguish "you have not configured anything
yet" from "your disk is broken". Returning the zero `config` instead lets the
rest of the program treat "no settings" as just another set of settings.

`errors.Is` rather than `err == fs.ErrNotExist` matters, because the error is
wrapped in a `*fs.PathError`. `errors.Is` unwraps until it finds a match.

## 4. When are two paths the same folder?

This is the part of the step with real depth.

The user types `d:\work_dsi\gitcal`. The scanner reports `D:\work_dsi\gitcal`.
On Windows those are the same folder, but `==` says they differ, and a map keyed
by the raw string would lose the group assignment entirely.

Four separate problems hide in this:

1. **Case.** Windows and macOS filesystems are usually case-insensitive; Linux is
   not.
2. **Redundant elements.** `projects\api\nested\..` and `projects\api` are the
   same place.
3. **Trailing separators.** `projects\api\` and `projects\api`.
4. **Symlinks.** A root reached through a link resolves to the path the scanner
   actually reports.

One function handles all four:

```go
func pathKey(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	} else if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}
```

The `else if` is the careful bit. `EvalSymlinks` fails when the path does not
exist — a configured folder that has since been deleted, or a repository in a
test that was never created. Falling back to `filepath.Abs` means a
not-yet-existing path still gets a usable key rather than being silently
compared as a bare relative string.

`filepath.Clean` removes the `..` elements and the trailing separator. The
`runtime.GOOS` check lowercases only where case genuinely does not distinguish
two folders; doing it everywhere would merge two distinct repositories on Linux.

### Canonical for comparison, original for display

`pathKey` is used for *comparison only*. What gets stored and shown is the path
the user typed, cleaned but not lowercased:

```go
c.Groups[filepath.Clean(repository)] = group
```

A configuration file full of lowercased paths would look wrong to the person
reading it. The canonical form is an implementation detail of matching, not the
data itself.

### Reassignment has to clear the old spelling

Storing the original spelling creates a subtle bug. Assign a group using
`D:\Projects\Api`, then reassign using `d:\projects\api`, and a naive map write
adds a *second* entry. Both match the same folder, and which one wins depends on
Go's randomised map iteration order.

```go
func (c *config) setGroup(repository, group string) {
	// Reassigning must not leave the repository listed under its old path form.
	c.clearGroup(repository)
	if group != "" && group != ungrouped {
		c.Groups[filepath.Clean(repository)] = group
	}
}
```

`clearGroup` walks the map comparing canonical keys and deletes every match.
Deleting during `range` is explicitly safe in Go — an entry removed before it is
reached will not be produced.

The test pins the behaviour down:

```go
saved.setGroup(awkward, "personal")
if len(saved.Groups) != 1 {
	t.Fatalf("reassignment left %d entries: %+v", len(saved.Groups), saved.Groups)
}
```

## 5. A filter, not a pile of conditionals

The renderers and the scanner should not know that a configuration file exists.
Their question is narrower: *does this repository count?*

```go
type repositoryFilter struct {
	excluded map[string]bool
	groups   map[string]string
	only     string
}
```

The `config` builds one, converting its slices into maps keyed canonically:

```go
func (c config) filter(group string) repositoryFilter
```

That conversion is why filtering is fast. Checking a slice for each of a hundred
repositories is a hundred linear scans; a map lookup is constant time, and the
maps are built once.

The **zero value includes everything**, which is doing real work:

```go
func (f repositoryFilter) includes(repository string) bool {
	key := pathKey(repository)
	if f.excluded[key] {
		return false
	}
	if f.only == "" {
		return true
	}
	...
}
```

A `nil` map returns the zero value for any lookup rather than panicking, so
`f.excluded[key]` on an empty filter is simply `false`. That means
`repositoryFilter{}` needs no constructor and no nil checks, and the existing
tests could be updated by passing `repositoryFilter{}` and nothing else.

Ordering matters: exclusion is checked before the group. An excluded repository
stays excluded even if its group matches, because exclusion is the stronger
statement.

Group comparison uses `strings.EqualFold` so `--group Work` and `--group work`
behave the same, matching the case-insensitive spirit of the rest of the tool.

## 6. Filter early

One line decides whether exclusion is a display filter or a performance feature:

```go
// Filtering before reading means an excluded repository costs nothing.
selected := make([]string, 0, len(scanned.Repositories))
for _, repo := range scanned.Repositories {
	if filter.includes(repo) {
		selected = append(selected, repo)
	}
}
```

This sits *between* discovery and history reading. Discovery is cheap: walking
directories looking for `.git`. Reading history is expensive: one Git process per
repository, parsing every reachable commit.

Excluding a large vendored clone therefore makes the scan genuinely faster, not
just tidier. If the filter ran during rendering instead, the program would do all
the expensive work and throw the results away.

`Activity` grew a field to keep this visible:

```go
DiscoveredRepositories int
SelectedRepositories   int
ReadRepositories       int
```

Three numbers, because three different things can surprise you: how many exist,
how many you chose, and how many could actually be read.

## 7. Making a quiet month distinguishable from a filtered one

A filtered calendar can look identical to a month where you did nothing. The
summary line therefore names the filter:

```go
scope := fmt.Sprintf("read %d of %d selected repositories (%d discovered)",
	activity.ReadRepositories, activity.SelectedRepositories, activity.DiscoveredRepositories)
if options.Group != "" {
	scope = style.label.render(options.Group+" only") + " · " + scope
}
```

Which produces:

```
7 unique commits on 2 days; personal only · read 1 of 1 selected repositories (9 discovered).
```

An interface that hides its own filters teaches people not to trust it. The
"9 discovered" is the crucial part: it says the other eight repositories exist
and were deliberately left out.

## 8. One bad folder should not lose the good ones

With a single root, an unreadable folder was obviously an error. With several,
that rule becomes hostile: one unplugged external drive would blank the whole
calendar.

```go
if succeeded == 0 {
	if lastErr != nil {
		return scanResult{}, lastErr
	}
	return combined, errors.New("no folders to scan; add one with: gitcal roots add <folder>")
}
```

A failed root becomes a warning as long as some other root worked; losing *every*
root is still an error. With one configured root this reduces exactly to the old
behaviour, so nothing regressed.

Overlapping roots need handling too. Configure both `D:\work` and
`D:\work\clients` and the inner repositories are found twice:

```go
for _, repository := range result.Repositories {
	if key := pathKey(repository); !seen[key] {
		seen[key] = true
		combined.Repositories = append(combined.Repositories, repository)
	}
}
```

The same `pathKey` again, doing a third distinct job.

## 9. Asking a question only when someone can hear it

A first run with nothing configured should not just fail. But prompting
unconditionally is worse than failing — a script in a build pipeline would hang
forever waiting for input nobody will type.

```go
func interactiveInput(in io.Reader) bool {
	file, ok := in.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}
```

Step 5 used `term.IsTerminal` to decide whether to start the keyboard interface.
The same check answers a different question here: not "can I draw?" but "can
anyone answer?". When the answer is no, the program says what to do instead:

```
Error: no folders configured. Add one with:
  gitcal roots add "path/to/your/projects"
Or pass a folder directly for a single run.
```

Notice the type assertion. `io.Reader` has no file descriptor; only an
`*os.File` does. A `strings.Reader` in a test fails the assertion and is
correctly treated as non-interactive — which is exactly what the test relies on:

```go
if _, ok := resolveSelection("", "", strings.NewReader("ignored\n"), &out, &errOut); ok {
	t.Fatalf("a non-terminal run should not have prompted")
}
```

### Prompting before the screen is taken over

In `calendar_cli.go` the order of two calls is load-bearing:

```go
// Resolving folders first means the first-run prompt is answered before the
// alternate screen takes the terminal over.
chosen, ok := resolveSelection(options.Arg(0), *group, os.Stdin, out, errOut)
if !ok {
	return 1
}

if *dayText == "" && !*static && (*interactive || canBeInteractive(out)) {
	return runInteractiveCalendar(ctx, chosen, selected, *entriesPerDay, ...)
}
```

Bubble Tea's alternate screen replaces the terminal contents entirely. A prompt
printed after it starts would be invisible, and the program would appear to hang.
Resolving first keeps the question on the ordinary screen where it can be read
and answered.

## 10. Not breaking what already worked

Steps 1 to 5 established that `gitcal calendar "some/folder"` works. Step 6 must
not change that, and the rule is one `if`:

```go
if folder != "" {
	chosen.roots = []string{folder}
	chosen.filter = repositoryFilter{only: group}
	if group != "" {
		// Grouping lives in the configuration, which this run bypassed.
		chosen.filter = saved.filter(group)
	}
	return chosen, true
}
```

An explicit folder wins and, by default, brings no saved exclusions with it.
"I asked for this folder" should mean this folder, all of it. But `--group` only
has meaning in terms of saved assignments, so asking for both loads them.

The flag change is smaller than it looks:

```go
if options.NArg() > 1 {   // was: != 1
```

The folder went from required to optional. Which broke a test asserting that
bare `activity` is a usage error — correctly, because it is no longer one. The
replacement checks the new contract instead:

```go
if !strings.Contains(noRoots.String(), "gitcal roots add") {
	t.Fatalf("unconfigured activity did not suggest roots add: %s", &noRoots)
}
```

A failing test after a deliberate behaviour change is the test doing its job.
The question to ask is always "is the old expectation still right?" — here it
was not, and the fix belonged in the test.

## 11. Keeping tests away from the real configuration

The command tests write settings. Left alone they would write *your* settings.

```go
func TestMain(m *testing.M) {
	directory, err := os.MkdirTemp("", "gitcal-tests")
	if err != nil {
		panic(err)
	}
	os.Setenv(configPathVariable, filepath.Join(directory, "absent.toml"))
	code := m.Run()
	os.RemoveAll(directory)
	os.Exit(code)
}
```

`TestMain` runs once for the whole package, before and after every test. The
default it establishes points at a file that *does not exist*, so any test not
thinking about configuration sees an empty one.

Tests that need to write override it per-test:

```go
func tempConfig(t *testing.T) string {
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv(configPathVariable, path)
	return path
}
```

`t.Setenv` restores the previous value automatically when the test ends, so tests
cannot leak state into each other. `t.TempDir` is removed automatically too.

Two layers, because they answer different questions: `TestMain` makes the safe
case the default, and `t.Setenv` handles the tests that genuinely need a file.

## 12. Two fixtures that failed, and why

Writing the tests produced two failures that were both my mistakes rather than
the code's.

An empty `.git` directory is not a repository:

```go
os.MkdirAll(filepath.Join(repository, ".git"), 0o755)  // Not enough.
```

The scanner validates candidates by running `git rev-parse --git-dir`, and Git
correctly refused. The fix was to make a real one, as the existing tests already
did:

```go
git(t, "init", "--quiet", "--initial-branch=main", filepath.Join(good, "repo"))
```

This is a good failure to have. The test accidentally confirmed that step 1's
validation works: a directory named `.git` is not enough to fool it.

The second was reusing a helper for the wrong type. `contains` compared strings
using `pathKey`, and I called it on commit subjects — running `EvalSymlinks` and
`ToLower` over "Paid work". It happened to pass, which is worse than failing.
Splitting it into `containsPath` and `containsText` made each comparison mean
what it says.

## 13. What is left

Step 6b adds repository selection inside the interactive calendar, so groups can
be assigned without leaving the interface.

Two known gaps stay open. Moving a repository on disk leaves its group behind,
pointing at a path that no longer exists — fixable later by pruning missing
paths, or by keying on something more durable than location. And a repository
belongs to exactly one group, so a repository that is both "work" and "open
source" cannot say so. That was a deliberate choice: one group per repository
makes `--group` unambiguous, and tags can be added later if the limit chafes.
