# Step 7 walkthrough — telling gitcal which commits are yours

Every step so far has treated every commit the same: whoever wrote it, it
counted. Step 7 adds the first question the calendar cannot answer from Git
metadata alone: *is this mine?*

Git already stores an author email on every commit. The problem is not
missing data — it is that the same person's data is inconsistent. A work
laptop commits as `you@work.example`; a personal machine commits as
`you@personal.example`; sometimes a client's mandated `.gitconfig` overrides
both. "Mine" is not one email address, it is a small set of them, decided by
you, not derivable from any single repository's history.

## 1. A second filter, built the same way as the first

Step 6a already solved a structurally identical problem: "does this
repository count?" became a `repositoryFilter`, built once from configuration
and asked a yes/no question by every renderer. Author filtering asks a
different question — "is this commit mine?" — but the shape of the answer is
the same, so it reuses the shape exactly:

```go
type authorFilter struct {
	identities map[string]bool
	mineOnly   bool
}

func (c config) authors(mineOnly bool) authorFilter {
	built := authorFilter{identities: make(map[string]bool, len(c.Identities)), mineOnly: mineOnly}
	for _, email := range c.Identities {
		built.identities[strings.ToLower(strings.TrimSpace(email))] = true
	}
	return built
}

func (f authorFilter) isMine(email string) bool {
	return f.identities[strings.ToLower(strings.TrimSpace(email))]
}

func (f authorFilter) includes(email string) bool {
	if !f.mineOnly {
		return true
	}
	return f.isMine(email)
}
```

`isMine` and `includes` look almost redundant next to each other, and that is
deliberate rather than an oversight. `includes` is what `collectActivity`
calls to decide whether a commit stays in the result at all — it is the
gate. `isMine` is what a renderer calls to decide whether to print `(you)`
next to a commit it is showing regardless — it is a label, not a gate. Folding
them into one method would make a display-only lookup accidentally start
filtering, or a filtering decision start rendering. Keeping the zero value
usable — `authorFilter{}` has `mineOnly: false`, so `includes` always returns
true — meant every existing test that built a plain `repositoryFilter{}`
before this step could pass an `authorFilter{}` alongside it and keep working
exactly as before.

## 2. Computing "mine" before deciding whether it matters

The obvious place to filter is where the repository filter already filters:
build the entry, check whether to keep it, move on. But author filtering has
one more requirement the repository filter never had — the commit detail view
wants to say "(you)" even in the *everyone* view, not only when mine-only is
active. So the field has to be computed unconditionally, and only *then* does
the filter decide whether the entry survives:

```go
for _, entry := range byHash {
	// Mine is set regardless of filtering, so a display can point out your
	// own commits even while showing everyone's.
	entry.Mine = authors.isMine(entry.Commit.AuthorEmail)
	if !authors.includes(entry.Commit.AuthorEmail) {
		continue
	}
	sort.Strings(entry.Repositories)
	entry.Groups = groupsOf(entry.Repositories, filter)
	...
}
```

Getting this order backwards — checking `includes` first and only setting
`Mine` on the entries that pass — would still work for the mine-only view,
because every surviving entry there is trivially mine. It would just make
`Mine` silently always `false` in the everyone view, since that branch would
never run for entries that continue past. The bug would be invisible until
someone tried to use the field for exactly the case it exists for.

## 3. Same email, different case, every time

Git does not normalise author emails. `Author@Work.example` and
`author@work.example` are, to Git, unrelated strings that happen to look
similar. A person typing their own address into `identities add` is not going
to remember which capitalisation their `.gitconfig` used eight months ago on
a machine they no longer own.

```go
func (c *config) addIdentity(email string) bool {
	key := strings.ToLower(strings.TrimSpace(email))
	for _, existing := range c.Identities {
		if strings.ToLower(existing) == key {
			return false
		}
	}
	c.Identities = append(c.Identities, strings.TrimSpace(email))
	return true
}
```

Comparison is case-folded; storage is not. The distinction matters for the
same reason step 6a kept `pathKey` for comparing paths but stored the path as
typed: a config file full of lowercased addresses would look like it had been
mangled, even though nothing was lost. `pathKey` itself was deliberately not
reused here — it resolves symlinks and applies Windows-only case folding,
both meaningless for an email address, so `authorFilter` gets its own,
smaller normalisation instead of stretching a filesystem-shaped tool over a
mail-shaped problem.

## 4. Refusing to show you an empty, silent calendar

`--group nonexistent` still shows *something*: a grid with zero commits and a
tag saying which group was requested, so the emptiness is self-explanatory.
`--mine` with no identities configured would have exactly the same
mechanical result — a filter that matches nothing — but with no tag able to
explain it, because there is nothing to name. An empty calendar and a
misconfigured one look identical from the inside.

So `resolveSelection` checks for this specific case before anything else runs:

```go
if mine && len(saved.Identities) == 0 {
	fmt.Fprintln(errOut, "Error: no identities configured. Add yours with:")
	fmt.Fprintln(errOut, "  gitcal identities add you@example.com")
	return selection{}, false
}
```

This mirrors the check step 6a already added for an entirely empty `roots`
list — refuse early, name the exact command that fixes it, rather than
producing a technically correct but silently useless result. The interactive
picker's `m` key repeats the same check for the same reason:

```go
func (m calendarModel) toggleMine() (tea.Model, tea.Cmd) {
	if !m.chosen.authors.mineOnly && len(m.chosen.config.Identities) == 0 {
		m.loadErr = errors.New("no identities configured; run: gitcal identities add you@example.com")
		return m, nil
	}
	m.chosen.authors.mineOnly = !m.chosen.authors.mineOnly
	m.mode, m.activity, m.loadErr = viewGrid, Activity{Month: m.month}, nil
	return m, (&m).beginLoad()
}
```

Only turning mine-only *on* needs the check. Turning it back off always works
without one, because "show me everyone" can never be a configuration mistake
— every calendar this program can show already defaults to it.

## 5. Reusing Tab's exact shape for a different toggle

Step 6b added `Tab` to cycle which group is shown, session-only, without
touching the `--group` flag it was launched with. `m` needed the identical
guarantee for `--mine`, so it reuses the reasoning without reusing the code —
a toggle only needs two states, where `Tab`'s cycle needed a list, so a
simpler function was the right size:

```go
m.chosen.authors.mineOnly = !m.chosen.authors.mineOnly
m.mode, m.activity = viewGrid, Activity{Month: m.month}
return m, (&m).beginLoad()
```

The `chosen.mine` field recorded from the `--mine` flag at launch is never
written to by `toggleMine` — only `chosen.authors.mineOnly`, a working copy,
changes. Quit and rerun without `--mine`, and the session starts from
"everyone" again, exactly as `Tab` already guarantees for groups. Getting this
wrong once already cost a mistaken README sentence in step 6b — writing
"restarting is required to change groups" when the code actually reloaded
live — so this time the distinction between *what was launched with* and
*what the session is currently showing* was named as two separate fields
before either changed, not after a wrong claim needed fixing.

## 6. Threading a new question through an old function

`collectActivity` grew a fourth parameter:

```go
func collectActivity(ctx context.Context, roots []string, month time.Time, filter repositoryFilter, authors authorFilter) (Activity, error)
```

Every caller — `activity_cli.go`, `calendar_cli.go`, `tui.go`, and every test
that calls it directly — needed the new argument. This is the least
interesting-looking part of the change and the easiest to get wrong by
missing a call site, which is exactly why grepping for every occurrence of
the function name before considering the change finished mattered more than
writing the new parameter itself. Two functions growing a parameter in the
same step (`resolveSelection` also gained `mine bool`) doubled the places a
missed call site could hide a compile error until the one test that exercised
that exact path ran.

## 7. What is left

Matching is by email only. A commit authored under a name you recognise but
an email you have not added yet will not count as yours, and there is no
"also match by name" fallback — deliberately, since two people can plausibly
share a display name but never legitimately share an email address, so email
is the only field precise enough to trust.

There is also no interactive picker for identities, unlike repositories in
step 6b. Adding one would look almost identical to the group-name text field
already built — type an email, `Enter` to confirm — but it was left for
later rather than built speculatively before anyone asked for it.
