package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

const (
	calendarColumns = 7

	// A cell holds one space of padding on each side, so its text area is two
	// columns narrower than the cell itself.
	minCellWidth = 10
	maxCellWidth = 28

	// Text areas at least this wide gain an extra field per entry.
	textWidthForRepository = 12
	textWidthForTime       = 20

	defaultEntriesPerDay = 3
)

// calendarOptions carries every terminal-dependent decision, so rendering
// stays a pure function of the activity data.
type calendarOptions struct {
	Width         int    // Total columns available for the grid.
	Today         string // YYYY-MM-DD, used only to highlight that cell.
	Selected      string // YYYY-MM-DD, set only by the interactive view.
	Highlight     bool   // Emit ANSI codes; false for pipes and NO_COLOR.
	EntriesPerDay int
	Footer        string // Replaces the default hint below the grid.
}

// cellWidth divides the terminal between seven columns and the eight vertical
// rules separating them, then clamps the result so narrow terminals stay
// readable and wide ones do not stretch a month across the screen.
func cellWidth(total int) int {
	width := (total - (calendarColumns + 1)) / calendarColumns
	if width < minCellWidth {
		return minCellWidth
	}
	if width > maxCellWidth {
		return maxCellWidth
	}
	return width
}

func renderCalendar(activity Activity, options calendarOptions) string {
	if options.EntriesPerDay < 1 {
		options.EntriesPerDay = defaultEntriesPerDay
	}
	cell := cellWidth(options.Width)
	text := cell - 2

	byDate := make(map[string][]ActivityEntry, len(activity.Days))
	total := 0
	for _, day := range activity.Days {
		byDate[day.Date] = day.Entries
		total += len(day.Entries)
	}

	month := activity.Month
	location := month.Location()
	first := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, location)
	// Go numbers Sunday as 0; shift so Monday starts the week at column 0.
	offset := (int(first.Weekday()) + 6) % 7
	daysInMonth := first.AddDate(0, 1, -1).Day()

	var output strings.Builder
	fmt.Fprintf(&output, "%s — local author dates, all authors and merges\n\n", first.Format("January 2006"))

	output.WriteString(horizontalRule(cell, "┌", "┬", "┐"))
	output.WriteString("│")
	for _, name := range weekdayNames(text) {
		output.WriteString(cellContents(name, cell))
		output.WriteString("│")
	}
	output.WriteString("\n")

	for start := 1 - offset; start <= daysInMonth; start += 7 {
		output.WriteString(horizontalRule(cell, "├", "┼", "┤"))

		// Every cell in a week shares the tallest cell's height, which keeps
		// the vertical rules aligned down the whole grid.
		columns := make([][]string, calendarColumns)
		height := 1
		for column := range columns {
			number := start + column
			if number < 1 || number > daysInMonth {
				continue
			}
			date := time.Date(month.Year(), month.Month(), number, 0, 0, 0, 0, location)
			columns[column] = dayLines(date, byDate[date.Format("2006-01-02")], options, text, location)
			if len(columns[column]) > height {
				height = len(columns[column])
			}
		}

		for row := 0; row < height; row++ {
			output.WriteString("│")
			for _, lines := range columns {
				line := ""
				if row < len(lines) {
					line = lines[row]
				}
				output.WriteString(cellContents(line, cell))
				output.WriteString("│")
			}
			output.WriteString("\n")
		}
	}
	output.WriteString(horizontalRule(cell, "└", "┴", "┘"))

	fmt.Fprintf(&output, "\n%d unique commits on %d days; read %d of %d discovered repositories.\n",
		total, len(activity.Days), activity.ReadRepositories, activity.DiscoveredRepositories)
	footer := options.Footer
	if footer == "" {
		footer = fmt.Sprintf("Showing up to %d commits per date. Use --day YYYY-MM-DD for one date's full list.",
			options.EntriesPerDay)
	}
	fmt.Fprintln(&output, footer)
	return output.String()
}

// dayLines builds one cell's text: the date number, then as many entries as
// fit the caller's limit, then a count of whatever had to be left out.
func dayLines(date time.Time, entries []ActivityEntry, options calendarOptions, text int, location *time.Location) []string {
	number := strconv.Itoa(date.Day())
	if options.Highlight {
		// Today and the selection need distinct styles, and one date can be both.
		key := date.Format("2006-01-02")
		switch {
		case key == options.Today && key == options.Selected:
			number = "\x1b[1;4;7m " + number + " \x1b[0m"
		case key == options.Today:
			number = "\x1b[7m " + number + " \x1b[0m"
		case key == options.Selected:
			number = "\x1b[1;4m " + number + " \x1b[0m"
		}
	}
	lines := []string{number}

	shown := entries
	if len(shown) > options.EntriesPerDay {
		shown = shown[:options.EntriesPerDay]
	}
	for _, entry := range shown {
		lines = append(lines, entryLine(entry, text, location))
	}
	if hidden := len(entries) - len(shown); hidden > 0 {
		lines = append(lines, fmt.Sprintf("+%d more", hidden))
	}
	return lines
}

// entryLine adds fields only while they still leave room for the subject.
func entryLine(entry ActivityEntry, text int, location *time.Location) string {
	line := displayText(entry.Commit.Subject)
	if text >= textWidthForRepository && len(entry.Repositories) > 0 {
		line = displayText(filepath.Base(entry.Repositories[0])) + ": " + line
	}
	if text >= textWidthForTime {
		line = entry.Commit.AuthoredAt.In(location).Format("15:04") + " " + line
	}
	return line
}

func weekdayNames(text int) []string {
	if text < 3 {
		return []string{"M", "T", "W", "T", "F", "S", "S"}
	}
	return []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
}

func horizontalRule(cell int, left, join, right string) string {
	segments := make([]string, calendarColumns)
	for i := range segments {
		segments[i] = strings.Repeat("─", cell)
	}
	return left + strings.Join(segments, join) + right + "\n"
}

// cellContents shortens and pads by display width, so East Asian characters,
// emoji, and the highlight's invisible escape codes all stay aligned.
func cellContents(line string, cell int) string {
	line = ansi.Truncate(line, cell-2, "…")
	gap := cell - 2 - ansi.StringWidth(line)
	if gap < 0 {
		gap = 0
	}
	return " " + line + strings.Repeat(" ", gap) + " "
}

func renderDay(activity Activity, date string) string {
	var output strings.Builder
	for _, day := range activity.Days {
		if day.Date != date {
			continue
		}
		fmt.Fprintf(&output, "%s — %d commits, local author dates\n\n", date, len(day.Entries))
		for _, entry := range day.Entries {
			commit := entry.Commit
			fmt.Fprintf(&output, "  %s  %.12s  %s\n    %s <%s>\n",
				commit.AuthoredAt.In(activity.Month.Location()).Format("15:04 -07:00"),
				commit.Hash, displayText(commit.Subject),
				displayText(commit.AuthorName), displayText(commit.AuthorEmail))
			for _, repository := range entry.Repositories {
				fmt.Fprintf(&output, "    repo: %s\n", displayText(repository))
			}
		}
		return output.String()
	}
	fmt.Fprintf(&output, "%s — no commits in the repositories successfully read.\n", date)
	return output.String()
}

func entriesForDate(activity Activity, date string) []ActivityEntry {
	for _, day := range activity.Days {
		if day.Date == date {
			return day.Entries
		}
	}
	return nil
}

// renderDayList is the interactive counterpart to renderDay: one line per
// commit, scrolled to keep the selection visible when a date is busy.
func renderDayList(activity Activity, date string, selected, width, height int) string {
	entries := entriesForDate(activity, date)
	var output strings.Builder
	fmt.Fprintf(&output, "%s — %d commits\n\n", date, len(entries))
	if len(entries) == 0 {
		output.WriteString("  No commits on this date.\n")
		return output.String()
	}

	visible := height
	if visible < 1 {
		visible = len(entries)
	}
	start := 0
	if len(entries) > visible {
		start = selected - visible/2
		if start < 0 {
			start = 0
		}
		if start > len(entries)-visible {
			start = len(entries) - visible
		}
	}
	end := start + visible
	if end > len(entries) {
		end = len(entries)
	}

	if start > 0 {
		fmt.Fprintf(&output, "  ↑ %d earlier\n", start)
	}
	for index := start; index < end; index++ {
		commit := entries[index].Commit
		marker := "  "
		if index == selected {
			marker = "▸ "
		}
		line := fmt.Sprintf("%s%s  %.12s  %s", marker,
			commit.AuthoredAt.In(activity.Month.Location()).Format("15:04"),
			commit.Hash, displayText(commit.Subject))
		if repositories := entries[index].Repositories; len(repositories) > 0 {
			line += "  · " + displayText(filepath.Base(repositories[0]))
		}
		output.WriteString(ansi.Truncate(line, width, "…") + "\n")
	}
	if end < len(entries) {
		fmt.Fprintf(&output, "  ↓ %d later\n", len(entries)-end)
	}
	return output.String()
}

func renderCommitDetail(entry ActivityEntry, location *time.Location) string {
	commit := entry.Commit
	var output strings.Builder
	fmt.Fprintf(&output, "%s\n\n", displayText(commit.Subject))
	fmt.Fprintf(&output, "  Commit  %s\n", commit.Hash)
	fmt.Fprintf(&output, "  Author  %s <%s>\n", displayText(commit.AuthorName), displayText(commit.AuthorEmail))
	fmt.Fprintf(&output, "  Date    %s\n", commit.AuthoredAt.In(location).Format("Monday, 2 January 2006, 15:04 -07:00"))
	for index, repository := range entry.Repositories {
		label := "  Repo    "
		if index > 0 {
			label = "          "
		}
		fmt.Fprintf(&output, "%s%s\n", label, displayText(repository))
	}
	output.WriteString("\nOnly the subject line is stored; full commit bodies are not read yet.\n")
	return output.String()
}
