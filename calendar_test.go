package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

var calendarZone = time.FixedZone("test local", 19800)

func calendarEntry(t *testing.T, stamp, repository, subject string) ActivityEntry {
	t.Helper()
	authored, err := time.ParseInLocation("2006-01-02T15:04", stamp, calendarZone)
	if err != nil {
		t.Fatal(err)
	}
	return ActivityEntry{
		Commit: Commit{
			Hash:        "0123456789abcdef0123456789abcdef01234567",
			AuthorName:  "Demo Author",
			AuthorEmail: "demo@example.invalid",
			AuthoredAt:  authored,
			Subject:     subject,
		},
		Repositories: []string{"/projects/" + repository},
	}
}

func calendarMonth(year int, month time.Month) time.Time {
	return time.Date(year, month, 1, 0, 0, 0, 0, calendarZone)
}

// gridRows keeps only the box-drawn lines, leaving out the title and summary.
func gridRows(rendered string) []string {
	var rows []string
	for _, line := range strings.Split(rendered, "\n") {
		if strings.ContainsAny(line, "┌┬┐├┼┤└┴┘│") {
			rows = append(rows, line)
		}
	}
	return rows
}

func rowCells(t *testing.T, row string) []string {
	t.Helper()
	parts := strings.Split(row, "│")
	if len(parts) != calendarColumns+2 {
		t.Fatalf("row is not seven cells: %q", row)
	}
	return parts[1 : len(parts)-1]
}

// Misjudging one cell's width shifts every column to its right, so alignment
// is checked against text that byte and rune counts both measure wrongly.
func TestCalendarKeepsEveryRowTheSameDisplayWidth(t *testing.T) {
	activity := Activity{
		Month:                  calendarMonth(2026, time.September),
		DiscoveredRepositories: 2,
		ReadRepositories:       2,
		Days: []ActivityDay{
			{Date: "2026-09-02", Entries: []ActivityEntry{
				calendarEntry(t, "2026-09-02T09:15", "api", "✨ 日本語のコミットメッセージ"),
				calendarEntry(t, "2026-09-02T11:40", "website", "Café façade fix"),
			}},
			{Date: "2026-09-18", Entries: []ActivityEntry{
				calendarEntry(t, "2026-09-18T08:00", "api", "🐛🐛 fix"),
			}},
		},
	}
	for _, width := range []int{40, 80, 100, 137, 250} {
		rendered := renderCalendar(activity, calendarOptions{
			Width: width, Today: "2026-09-18", Highlight: true, EntriesPerDay: 3,
		})
		rows := gridRows(rendered)
		if len(rows) == 0 {
			t.Fatalf("width %d produced no grid", width)
		}
		want := ansi.StringWidth(rows[0])
		for i, row := range rows {
			if got := ansi.StringWidth(row); got != want {
				t.Fatalf("width %d, row %d is %d columns; want %d: %q", width, i, got, want, row)
			}
		}
		if expected := calendarColumns*cellWidth(width) + calendarColumns + 1; want != expected {
			t.Fatalf("width %d rendered %d columns; want %d", width, want, expected)
		}
	}
}

func TestCalendarStartsWeeksOnMondayAndPlacesDates(t *testing.T) {
	// 1 September 2026 is a Tuesday, so Monday's first cell stays empty.
	activity := Activity{Month: calendarMonth(2026, time.September)}
	rows := gridRows(renderCalendar(activity, calendarOptions{Width: 100, EntriesPerDay: 3}))

	weekdays := rowCells(t, rows[1])
	if strings.TrimSpace(weekdays[0]) != "Mon" || strings.TrimSpace(weekdays[6]) != "Sun" {
		t.Fatalf("weekday header: %q", weekdays)
	}
	firstWeek := rowCells(t, rows[3])
	if strings.TrimSpace(firstWeek[0]) != "" || strings.TrimSpace(firstWeek[1]) != "1" {
		t.Fatalf("1 September should sit under Tuesday: %q", firstWeek)
	}
	if last := strings.TrimSpace(firstWeek[6]); last != "6" {
		t.Fatalf("first week should end on 6 September, got %q", last)
	}
}

func TestCalendarSpansSixWeeksWhenTheMonthNeedsThem(t *testing.T) {
	// March 2026 has 31 days and begins on a Sunday: the longest possible month.
	rows := gridRows(renderCalendar(Activity{Month: calendarMonth(2026, time.March)},
		calendarOptions{Width: 100, EntriesPerDay: 3}))
	separators := 0
	for _, row := range rows {
		if strings.HasPrefix(row, "├") {
			separators++
		}
	}
	// Each week is preceded by a separator; the first doubles as the header rule.
	if separators != 6 {
		t.Fatalf("expected six week rows, found %d separators", separators)
	}
	var lastDay string
	for _, row := range rows {
		if strings.HasPrefix(row, "│") {
			if cells := strings.Split(row, "│"); len(cells) == calendarColumns+2 {
				if value := strings.TrimSpace(cells[2]); value == "31" {
					lastDay = value
				}
			}
		}
	}
	if lastDay != "31" {
		t.Fatal("31 March should appear under Tuesday in the final week")
	}
}

func TestCalendarCountsCommitsItCannotShow(t *testing.T) {
	entries := make([]ActivityEntry, 0, 5)
	for i := 0; i < 5; i++ {
		entries = append(entries, calendarEntry(t, "2026-09-02T09:15", "api", "Routine change"))
	}
	activity := Activity{
		Month: calendarMonth(2026, time.September),
		Days:  []ActivityDay{{Date: "2026-09-02", Entries: entries}},
	}
	// Wide enough that the subjects are not shortened, so they can be counted.
	rendered := renderCalendar(activity, calendarOptions{Width: 250, EntriesPerDay: 2})
	if !strings.Contains(rendered, "+3 more") {
		t.Fatalf("missing overflow count:\n%s", rendered)
	}
	if strings.Count(rendered, "Routine change") != 2 {
		t.Fatalf("expected exactly two visible entries:\n%s", rendered)
	}
	if !strings.Contains(rendered, "5 unique commits on 1 days") {
		t.Fatalf("summary should count every commit, not the visible ones:\n%s", rendered)
	}
}

func TestCalendarDropsFieldsAsCellsNarrow(t *testing.T) {
	activity := Activity{
		Month: calendarMonth(2026, time.September),
		Days: []ActivityDay{{Date: "2026-09-02", Entries: []ActivityEntry{
			calendarEntry(t, "2026-09-02T09:15", "api", "Fix token refresh"),
		}}},
	}
	wide := renderCalendar(activity, calendarOptions{Width: 250, EntriesPerDay: 3})
	if !strings.Contains(wide, "09:15") || !strings.Contains(wide, "api:") {
		t.Fatalf("wide cells should carry time and repository:\n%s", wide)
	}
	medium := renderCalendar(activity, calendarOptions{Width: 130, EntriesPerDay: 3})
	if strings.Contains(medium, "09:15") || !strings.Contains(medium, "api:") {
		t.Fatalf("medium cells should drop the time but keep the repository:\n%s", medium)
	}
	narrow := renderCalendar(activity, calendarOptions{Width: 80, EntriesPerDay: 3})
	if strings.Contains(narrow, "api:") || !strings.Contains(narrow, "Fix") {
		t.Fatalf("narrow cells should keep only the subject:\n%s", narrow)
	}
}

func TestCalendarHighlightsTodayOnlyWhenAsked(t *testing.T) {
	activity := Activity{Month: calendarMonth(2026, time.September)}
	on := renderCalendar(activity, calendarOptions{Width: 100, Today: "2026-09-18", Highlight: true})
	if !strings.Contains(on, "\x1b[7m 18 \x1b[0m") {
		t.Fatal("today's date should be highlighted")
	}
	off := renderCalendar(activity, calendarOptions{Width: 100, Today: "2026-09-18"})
	if strings.Contains(off, "\x1b") {
		t.Fatal("no escape codes belong in unhighlighted output")
	}
	other := renderCalendar(activity, calendarOptions{Width: 100, Today: "2026-10-18", Highlight: true})
	if strings.Contains(other, "\x1b") {
		t.Fatal("a date outside the month must not highlight anything")
	}
}

func TestCellWidthClampsToReadableSizes(t *testing.T) {
	if got := cellWidth(20); got != minCellWidth {
		t.Fatalf("narrow terminal gave cell width %d; want %d", got, minCellWidth)
	}
	if got := cellWidth(1000); got != maxCellWidth {
		t.Fatalf("wide terminal gave cell width %d; want %d", got, maxCellWidth)
	}
	if got := cellWidth(78); got != 10 {
		t.Fatalf("an 80-column terminal gave cell width %d; want 10", got)
	}
}

func TestRenderDayListsEveryCommitAndReportsEmptyDates(t *testing.T) {
	entry := calendarEntry(t, "2026-09-02T09:15", "api", "Fix token refresh")
	entry.Repositories = append(entry.Repositories, "/clones/api")
	activity := Activity{
		Month: calendarMonth(2026, time.September),
		Days:  []ActivityDay{{Date: "2026-09-02", Entries: []ActivityEntry{entry}}},
	}
	listed := renderDay(activity, "2026-09-02")
	for _, want := range []string{"2026-09-02 — 1 commits", "09:15 +05:30", "0123456789ab",
		"Fix token refresh", "demo@example.invalid", "/projects/api", "/clones/api"} {
		if !strings.Contains(listed, want) {
			t.Fatalf("day view missing %q:\n%s", want, listed)
		}
	}
	if !strings.Contains(listed, "0123456789ab  Fix") {
		t.Fatalf("hash should be shortened to twelve characters:\n%s", listed)
	}
	empty := renderDay(activity, "2026-09-03")
	if !strings.Contains(empty, "no commits") {
		t.Fatalf("empty date should say so, got: %s", empty)
	}
}

func TestCalendarControlCharactersNeverReachTheGrid(t *testing.T) {
	activity := Activity{
		Month: calendarMonth(2026, time.September),
		Days: []ActivityDay{{Date: "2026-09-02", Entries: []ActivityEntry{
			calendarEntry(t, "2026-09-02T09:15", "api", "Subject\x07with\rcontrol"),
		}}},
	}
	rendered := renderCalendar(activity, calendarOptions{Width: 250, EntriesPerDay: 3})
	if strings.ContainsAny(rendered, "\x07\r") {
		t.Fatalf("control characters survived into the grid:\n%q", rendered)
	}
}
