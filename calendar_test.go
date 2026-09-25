package main

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

var calendarZone = time.FixedZone("test local", 19800)

// Tests run with their output piped, where lipgloss would correctly emit no
// colour at all. Forcing a profile keeps the styled cases meaningful.
func init() {
	renderer := lipgloss.NewRenderer(io.Discard)
	renderer.SetColorProfile(termenv.TrueColor)
	renderer.SetHasDarkBackground(true)
	styleRenderer = renderer
}

func colourful() styles { return newStyles(true) }

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
			Width: width, Today: "2026-09-18", Selected: "2026-09-02",
			EntriesPerDay: 3, Styles: colourful(),
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
	if !strings.Contains(rendered, "5 commits · 1 active day") {
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

// Disabled styles must produce output a script can read, and every state a
// date can be in has to look different from the others.
func TestDateStatesAreVisuallyDistinct(t *testing.T) {
	activity := Activity{
		Month: calendarMonth(2026, time.September),
		Days: []ActivityDay{{Date: "2026-09-08", Entries: []ActivityEntry{
			calendarEntry(t, "2026-09-08T09:15", "api", "Fix token refresh"),
		}}},
	}
	options := calendarOptions{Width: 120, Today: "2026-09-18", Selected: "2026-09-04",
		EntriesPerDay: 3, Styles: colourful()}

	plain := renderCalendar(activity, calendarOptions{Width: 120, Today: "2026-09-18",
		Selected: "2026-09-04", EntriesPerDay: 3})
	if strings.Contains(plain, "\x1b") {
		t.Fatalf("disabled styles must emit no escape codes:\n%q", plain)
	}

	rendered := renderCalendar(activity, options)
	states := map[string]string{
		"today":       cellFor(t, rendered, "18"),
		"selected":    cellFor(t, rendered, "4"),
		"with commit": cellFor(t, rendered, "8"),
		"weekend":     cellFor(t, rendered, "5"),
		"quiet":       cellFor(t, rendered, "9"),
	}
	for name, cell := range states {
		if !strings.Contains(cell, "\x1b") {
			t.Fatalf("%s date carries no styling: %q", name, cell)
		}
		for other, against := range states {
			if name != other && styleOf(cell) == styleOf(against) {
				t.Fatalf("%s and %s render identically: %q", name, other, styleOf(cell))
			}
		}
	}
}

func TestSelectedDateHasCompleteAccentOutline(t *testing.T) {
	activity := Activity{Month: calendarMonth(2026, time.September)}
	style := colourful()
	cell := cellWidth(120)
	rendered := renderCalendar(activity, calendarOptions{
		Width: 120, Selected: "2026-09-04", EntriesPerDay: 3, Styles: style,
	})
	rows := gridRows(rendered)

	// 4 September is in the first week. Its row is enclosed by the separator
	// above it and the separator above the following week.
	for _, row := range []string{rows[2], rows[4]} {
		plain := ansi.Strip(row)
		segments := strings.FieldsFunc(plain, func(r rune) bool {
			return strings.ContainsRune("├┼┤", r)
		})
		if len(segments) != calendarColumns || segments[4] != strings.Repeat("─", cell) {
			t.Fatalf("selected cell is missing a solid horizontal edge: %q", plain)
		}
	}

	if got := strings.Count(rows[3], style.selectedEdge.render("│")); got != 2 {
		t.Fatalf("selected cell should have two accent side edges, found %d: %q", got, rows[3])
	}
}

// cellFor returns the grid cell whose visible text is exactly the given date.
func cellFor(t *testing.T, rendered, day string) string {
	t.Helper()
	for _, row := range gridRows(rendered) {
		for _, cell := range strings.Split(row, "│") {
			if strings.TrimSpace(ansi.Strip(cell)) == day {
				return cell
			}
		}
	}
	t.Fatalf("no cell found for date %q", day)
	return ""
}

// styleOf keeps the escape codes and drops the text, so two dates can be
// compared on appearance alone.
func styleOf(cell string) string {
	var codes strings.Builder
	for _, part := range strings.Split(cell, "\x1b")[1:] {
		if end := strings.IndexByte(part, 'm'); end >= 0 {
			codes.WriteString(part[:end+1])
		}
	}
	return codes.String()
}

func TestRepositoryColoursAreStableAndVaried(t *testing.T) {
	style := colourful()
	if style.repository("api").render("api") != style.repository("api").render("api") {
		t.Fatal("a repository's colour must not change between renders")
	}
	if style.repository("api").render("x") == style.repository("website").render("x") {
		t.Fatal("different repositories should not share a colour by default")
	}
	if style.repository("API").render("x") != style.repository("api").render("x") {
		t.Fatal("colour should ignore case so the same repository matches itself")
	}
	if plain := newStyles(false); strings.Contains(plain.repository("api").render("api"), "\x1b") {
		t.Fatal("disabled styles must not colour repository names")
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
	rendered := renderCalendar(activity, calendarOptions{Width: 250, EntriesPerDay: 3, Styles: colourful()})
	if strings.ContainsAny(rendered, "\x07\r") {
		t.Fatalf("control characters survived into the grid:\n%q", rendered)
	}
}
