package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func newTestModel(t *testing.T, entries ...ActivityEntry) calendarModel {
	t.Helper()
	month := calendarMonth(2026, time.September)
	model := newCalendarModel(context.Background(), selection{roots: []string{"projects"}}, month, 3, 120)
	model.request = 1
	if len(entries) > 0 {
		model.activity = Activity{
			Month:            month,
			ReadRepositories: 1,
			Days:             []ActivityDay{{Date: "2026-09-01", Entries: entries}},
		}
	} else {
		model.activity = Activity{Month: month}
	}
	return model
}

// newReposTestModel starts already inside the picker with a fixed repository
// list, so key-handling tests do not need a real scan to run first.
func newReposTestModel(t *testing.T, repositories ...string) calendarModel {
	t.Helper()
	path := tempConfig(t)
	month := calendarMonth(2026, time.September)
	chosen := selection{roots: []string{"projects"}, path: path}
	model := newCalendarModel(context.Background(), chosen, month, 3, 120)
	model.request = 1
	model.mode = viewRepos
	model.repos = repositories
	return model
}

func press(t *testing.T, model calendarModel, key tea.KeyMsg) calendarModel {
	t.Helper()
	next, _ := model.Update(key)
	updated, ok := next.(calendarModel)
	if !ok {
		t.Fatalf("Update returned %T, not a calendarModel", next)
	}
	return updated
}

func keyRune(value string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
}

// Moving must never leave the month: month changes are a deliberate keypress.
func TestSelectionStopsAtTheMonthEdges(t *testing.T) {
	model := newTestModel(t)
	for _, key := range []tea.KeyMsg{{Type: tea.KeyLeft}, {Type: tea.KeyUp}} {
		if got := press(t, model, key).selected.Day(); got != 1 {
			t.Fatalf("moving back from the 1st reached day %d", got)
		}
	}

	model.selected = time.Date(2026, time.September, 30, 0, 0, 0, 0, calendarZone)
	for _, key := range []tea.KeyMsg{{Type: tea.KeyRight}, {Type: tea.KeyDown}} {
		if got := press(t, model, key).selected.Day(); got != 30 {
			t.Fatalf("moving past the 30th reached day %d", got)
		}
	}

	if got := press(t, model, tea.KeyMsg{Type: tea.KeyDown}).selected.Day(); got != 30 {
		t.Fatal("a blocked move must not change the month either")
	}
	if got := press(t, model, keyRune("j")).selected.Month(); got != time.September {
		t.Fatalf("selection left September, reaching %s", got)
	}
}

func TestChangingMonthClampsTheDayAndReloads(t *testing.T) {
	model := newTestModel(t)
	model.month = time.Date(2026, time.January, 1, 0, 0, 0, 0, calendarZone)
	model.selected = time.Date(2026, time.January, 31, 0, 0, 0, 0, calendarZone)

	next, command := model.Update(keyRune("]"))
	updated := next.(calendarModel)
	if updated.month.Month() != time.February {
		t.Fatalf("month became %s", updated.month.Month())
	}
	// February 2026 has 28 days, so the 31st cannot survive the move.
	if updated.selected.Day() != 28 {
		t.Fatalf("selected day became %d; want 28", updated.selected.Day())
	}
	if !updated.loading || command == nil {
		t.Fatal("changing month should start a scan and show it is loading")
	}
	if len(updated.activity.Days) != 0 {
		t.Fatal("the previous month's commits must not remain on screen")
	}
}

// Holding a month key queues several scans; only the newest may be displayed.
func TestSupersededScanResultsAreDiscarded(t *testing.T) {
	model := newTestModel(t)
	model.request, model.loading = 2, true

	stale := Activity{Month: model.month, Days: []ActivityDay{{Date: "2026-09-05",
		Entries: []ActivityEntry{calendarEntry(t, "2026-09-05T09:00", "api", "Stale")}}}}
	next, _ := model.Update(activityLoadedMsg{request: 1, activity: stale})
	if updated := next.(calendarModel); len(updated.activity.Days) != 0 || !updated.loading {
		t.Fatal("an old month's result was applied")
	}

	next, _ = model.Update(activityLoadedMsg{request: 2, activity: stale})
	updated := next.(calendarModel)
	if len(updated.activity.Days) != 1 || updated.loading {
		t.Fatal("the current month's result was not applied")
	}
}

func TestEnterDescendsOnlyThroughDatesWithCommits(t *testing.T) {
	entry := calendarEntry(t, "2026-09-01T09:15", "api", "Fix token refresh")
	model := newTestModel(t, entry, calendarEntry(t, "2026-09-01T11:40", "web", "Add page"))

	opened := press(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if opened.mode != viewDay || opened.commit != 0 {
		t.Fatalf("Enter on a busy date gave mode %v, commit %d", opened.mode, opened.commit)
	}
	moved := press(t, opened, tea.KeyMsg{Type: tea.KeyDown})
	if moved.commit != 1 {
		t.Fatal("Down should move between commits inside the day view")
	}
	if again := press(t, moved, tea.KeyMsg{Type: tea.KeyDown}); again.commit != 1 {
		t.Fatal("commit selection should stop at the last commit")
	}
	detail := press(t, moved, tea.KeyMsg{Type: tea.KeyEnter})
	if detail.mode != viewCommit {
		t.Fatal("Enter in the day view should open the commit")
	}
	if back := press(t, detail, tea.KeyMsg{Type: tea.KeyEsc}); back.mode != viewDay {
		t.Fatal("Esc should return to the day view")
	}
	if back := press(t, press(t, detail, tea.KeyMsg{Type: tea.KeyEsc}), tea.KeyMsg{Type: tea.KeyEsc}); back.mode != viewGrid {
		t.Fatal("a second Esc should return to the grid")
	}

	empty := newTestModel(t)
	if opened := press(t, empty, tea.KeyMsg{Type: tea.KeyEnter}); opened.mode != viewGrid {
		t.Fatal("Enter on a date with no commits should do nothing")
	}
}

func TestViewDistinguishesTodayFromTheSelection(t *testing.T) {
	model := newTestModel(t)
	model.selected = time.Date(2026, time.September, 4, 0, 0, 0, 0, calendarZone)
	view := model.View()
	if styleOf(cellFor(t, view, "4")) == styleOf(cellFor(t, view, "5")) {
		t.Fatalf("the selected date should not look like an ordinary one:\n%s", view)
	}
	if strings.Contains(view, "Use --day") {
		t.Fatal("the interactive footer should replace the command-line hint")
	}
	if !strings.Contains(view, "q quit") {
		t.Fatal("the interactive footer should list key bindings")
	}
}

func TestLoadingAndErrorsStayVisible(t *testing.T) {
	model := newTestModel(t)
	model.loading = true
	if !strings.Contains(model.View(), "Reading September 2026…") {
		t.Fatal("a running scan should be reported in the footer")
	}

	model.loading = false
	model.loadErr = context.DeadlineExceeded
	if view := model.View(); !strings.Contains(view, "Error:") || !strings.Contains(view, "r retries") {
		t.Fatalf("a failed scan should be reported with a way out:\n%s", view)
	}
}

func TestHelpTogglesAndQuitCancels(t *testing.T) {
	model := press(t, newTestModel(t), keyRune("?"))
	if !strings.Contains(model.View(), "Jump to today") {
		t.Fatal("? should show the key reference")
	}
	if shown := press(t, model, keyRune("?")); shown.help {
		t.Fatal("? should close the key reference again")
	}

	_, command := newTestModel(t).Update(keyRune("q"))
	if command == nil {
		t.Fatal("q should quit")
	}
}

// Redirected output must stay scriptable, which is why detection checks the
// writer rather than assuming a terminal.
func TestRedirectedOutputIsNeverInteractive(t *testing.T) {
	if canBeInteractive(&bytes.Buffer{}) {
		t.Fatal("a buffer is not a terminal")
	}
}

func TestDayListScrollsToKeepTheSelectionVisible(t *testing.T) {
	entries := make([]ActivityEntry, 0, 40)
	for i := 0; i < 40; i++ {
		entries = append(entries, calendarEntry(t, "2026-09-01T09:15", "api", "Commit "+string(rune('A'+i%26))))
	}
	activity := Activity{
		Month: calendarMonth(2026, time.September),
		Days:  []ActivityDay{{Date: "2026-09-01", Entries: entries}},
	}
	listed := renderDayList(activity, "2026-09-01", 39, 100, 10, newStyles(false))
	if !strings.Contains(listed, "↑ 30 earlier") {
		t.Fatalf("scrolling to the end should report what is above:\n%s", listed)
	}
	if strings.Contains(listed, "later") {
		t.Fatalf("nothing follows the last commit:\n%s", listed)
	}
	if strings.Count(listed, "▸ ") != 1 {
		t.Fatalf("exactly one commit should be marked:\n%s", listed)
	}
	if empty := renderDayList(activity, "2026-09-02", 0, 100, 10, newStyles(false)); !strings.Contains(empty, "No commits") {
		t.Fatal("an empty date should say so")
	}
}

// g must both switch views and start a scan; without the scan the picker
// would show nothing until r was pressed by hand.
func TestPressingGOpensTheRepositoryPickerAndScans(t *testing.T) {
	next, command := newTestModel(t).Update(keyRune("g"))
	updated := next.(calendarModel)
	if updated.mode != viewRepos || !updated.reposLoading || command == nil {
		t.Fatalf("g gave mode %v, loading %v, command %v", updated.mode, updated.reposLoading, command)
	}
}

func TestReposLoadedMessagePopulatesTheList(t *testing.T) {
	model := newReposTestModel(t)
	model.reposLoading, model.reposIndex = true, 5

	next, _ := model.Update(reposLoadedMsg{repositories: []string{"/projects/api", "/projects/blog"}})
	updated := next.(calendarModel)
	if updated.reposLoading || len(updated.repos) != 2 {
		t.Fatalf("loading %v, repos %v", updated.reposLoading, updated.repos)
	}
	// An index left over from a longer, previous list must not go out of range.
	if updated.reposIndex != 0 {
		t.Fatalf("out-of-range index was not reset, got %d", updated.reposIndex)
	}
}

// The point of typing a name is that it works before any group exists
// anywhere, which cycling through known names could not do.
func TestTypingAGroupNameSavesItImmediately(t *testing.T) {
	repository := filepath.FromSlash("/projects/api")
	model := newReposTestModel(t, repository)

	editing := press(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if !editing.editingGroup {
		t.Fatal("enter should start editing the selected repository's group")
	}
	for _, r := range "work" {
		editing = press(t, editing, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	saved := press(t, editing, tea.KeyMsg{Type: tea.KeyEnter})
	if saved.editingGroup {
		t.Fatal("enter should close the text field")
	}
	if !saved.reposChanged {
		t.Fatal("saving a group should mark the picker as changed")
	}
	if got := saved.chosen.filter.groupOf(repository); got != "work" {
		t.Fatalf("group was %q; want work", got)
	}
	if saved.reposSaveErr != nil {
		t.Fatalf("save reported an error: %v", saved.reposSaveErr)
	}

	reloaded, err := loadConfig(saved.chosen.path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := reloaded.filter("").groupOf(repository); got != "work" {
		t.Fatalf("group on disk was %q; want work", got)
	}
}

// Every character while editing belongs to the text field, even the ones
// that quit or open help everywhere else in the program.
func TestReservedKeysAreOrdinaryCharactersWhileEditing(t *testing.T) {
	model := newReposTestModel(t, "/projects/api")
	editing := press(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	_, command := editing.Update(keyRune("q"))
	if command != nil {
		t.Fatal("q should not quit while a group name is being typed")
	}
	typed := press(t, editing, keyRune("q"))
	if typed.groupInput != "q" || !typed.editingGroup {
		t.Fatalf("q was not treated as text: input %q, editing %v", typed.groupInput, typed.editingGroup)
	}

	backspaced := press(t, press(t, typed, keyRune("!")), tea.KeyMsg{Type: tea.KeyBackspace})
	if backspaced.groupInput != "q" {
		t.Fatalf("backspace left %q; want q", backspaced.groupInput)
	}
}

func TestEscapeCancelsEditingWithoutSaving(t *testing.T) {
	repository := filepath.FromSlash("/projects/api")
	model := newReposTestModel(t, repository)
	editing := press(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	typed := press(t, editing, keyRune("x"))

	cancelled := press(t, typed, tea.KeyMsg{Type: tea.KeyEsc})
	if cancelled.editingGroup {
		t.Fatal("esc should close the text field")
	}
	if cancelled.reposChanged {
		t.Fatal("cancelling must not count as a change")
	}
	if got := cancelled.chosen.filter.groupOf(repository); got != ungrouped {
		t.Fatalf("group became %q without ever saving", got)
	}
}

func TestExcludeTogglesBackAndForthAndPersists(t *testing.T) {
	repository := filepath.FromSlash("/projects/vendored")
	model := newReposTestModel(t, repository)

	excluded := press(t, model, keyRune("e"))
	if excluded.chosen.filter.includes(repository) {
		t.Fatal("e should have excluded the selected repository")
	}
	reloaded, err := loadConfig(excluded.chosen.path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if reloaded.filter("").includes(repository) {
		t.Fatal("the exclusion was not saved")
	}

	included := press(t, excluded, keyRune("e"))
	if !included.chosen.filter.includes(repository) {
		t.Fatal("pressing e again should include it back")
	}
}

// Leaving the picker must reload the grid when something changed, and must
// not waste a scan when nothing did.
func TestLeavingTheRepositoryPickerReloadsOnlyIfSomethingChanged(t *testing.T) {
	unchanged := newReposTestModel(t, "/projects/api")
	_, command := unchanged.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if command != nil {
		t.Fatal("esc without any edits should not start a reload")
	}

	changed := press(t, newReposTestModel(t, "/projects/api"), keyRune("e"))
	if !changed.reposChanged {
		t.Fatal("excluding a repository should mark the picker as changed")
	}
	next, command := changed.Update(tea.KeyMsg{Type: tea.KeyEsc})
	updated := next.(calendarModel)
	if updated.mode != viewGrid || command == nil {
		t.Fatalf("esc after an edit gave mode %v, command %v", updated.mode, command)
	}
	if updated.reposChanged {
		t.Fatal("the changed flag should reset once the reload has been started")
	}
}

func TestEnterDoesNothingWithoutAnyRepositories(t *testing.T) {
	model := newReposTestModel(t)
	if opened := press(t, model, tea.KeyMsg{Type: tea.KeyEnter}); opened.editingGroup {
		t.Fatal("there is nothing to edit when no repository was found")
	}
}

// Tab must not need --group to have named anything: with no groups assigned
// yet, "all" and "ungrouped" are still two real, distinct stops.
func TestTabCyclesGroupsStartingFromAll(t *testing.T) {
	model := newTestModel(t)
	if model.activeGroup != "" {
		t.Fatalf("a session launched without --group started on %q", model.activeGroup)
	}

	next, command := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	updated := next.(calendarModel)
	if updated.activeGroup != ungrouped || command == nil {
		t.Fatalf("first Tab gave group %q, command %v; want %s and a reload", updated.activeGroup, command, ungrouped)
	}
	if !updated.loading {
		t.Fatal("cycling the group should reload, the same as changing month")
	}

	back, _ := updated.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := back.(calendarModel).activeGroup; got != "" {
		t.Fatalf("a second Tab with nothing else configured landed on %q; want all (empty)", got)
	}
}

// A group created moments earlier in the picker must already have a stop on
// the wheel; nobody should have to restart the program to reach it.
func TestTabVisitsEveryGroupInUseAndWrapsAround(t *testing.T) {
	model := newTestModel(t)
	model.chosen.config.setGroup("/repos/api", "work")
	model.chosen.config.setGroup("/repos/blog", "personal")
	model.chosen.filter = model.chosen.config.filter("")

	var seen []string
	current := model
	for i := 0; i < 4; i++ {
		next, _ := current.Update(tea.KeyMsg{Type: tea.KeyTab})
		current = next.(calendarModel)
		seen = append(seen, current.activeGroup)
	}
	want := []string{"personal", "work", ungrouped, ""}
	for i, group := range want {
		if seen[i] != group {
			t.Fatalf("stop %d was %q; want %q (full sequence: %v)", i, seen[i], group, seen)
		}
	}
}

// --group at startup must still be honoured on the very first frame, and a
// session that only cycles must leave it alone for the next run.
func TestCyclingGroupsNeverChangesTheLaunchFlag(t *testing.T) {
	month := calendarMonth(2026, time.September)
	chosen := selection{roots: []string{"projects"}, group: "work"}
	model := newCalendarModel(context.Background(), chosen, month, 3, 120)
	if model.activeGroup != "work" {
		t.Fatalf("launching with --group work started the session on %q", model.activeGroup)
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	updated := next.(calendarModel)
	if updated.activeGroup == "work" {
		t.Fatal("Tab should have moved the session away from its starting group")
	}
	if updated.chosen.group != "work" {
		t.Fatalf("cycling changed chosen.group to %q; --group must stay as launched", updated.chosen.group)
	}
}

func TestRepositoryPickerRendersTheCurrentStateOfEach(t *testing.T) {
	model := newReposTestModel(t, filepath.FromSlash("/projects/api"), filepath.FromSlash("/projects/vendored"))
	model.chosen.config.setGroup(filepath.FromSlash("/projects/api"), "work")
	model.chosen.config.exclude(filepath.FromSlash("/projects/vendored"))
	model.chosen.filter = model.chosen.config.filter("")

	view := model.View()
	if !strings.Contains(view, "work") {
		t.Fatalf("the assigned group is not shown:\n%s", view)
	}
	if !strings.Contains(view, "excluded") {
		t.Fatalf("the exclusion is not shown:\n%s", view)
	}
}
