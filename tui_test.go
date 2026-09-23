package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func newTestModel(t *testing.T, entries ...ActivityEntry) calendarModel {
	t.Helper()
	month := calendarMonth(2026, time.September)
	model := newCalendarModel(context.Background(), "projects", month, 3, 120)
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
	if !strings.Contains(view, "\x1b[1;4m 4 \x1b[0m") {
		t.Fatalf("the selected date should be underlined:\n%s", view)
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
	listed := renderDayList(activity, "2026-09-01", 39, 100, 10)
	if !strings.Contains(listed, "↑ 30 earlier") {
		t.Fatalf("scrolling to the end should report what is above:\n%s", listed)
	}
	if strings.Contains(listed, "later") {
		t.Fatalf("nothing follows the last commit:\n%s", listed)
	}
	if strings.Count(listed, "▸ ") != 1 {
		t.Fatalf("exactly one commit should be marked:\n%s", listed)
	}
	if empty := renderDayList(activity, "2026-09-02", 0, 100, 10); !strings.Contains(empty, "No commits") {
		t.Fatal("an empty date should say so")
	}
}
