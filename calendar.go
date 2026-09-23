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
	EntriesPerDay int
	Group         string // Named below the grid so a filtered month cannot look empty.
	Footer        string // Replaces the default hint below the grid.
	Styles        styles // Disabled styles render plain text for pipes and NO_COLOR.
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

	style := options.Styles
	bar := style.border.render("│")

	var output strings.Builder
	fmt.Fprintf(&output, "%s\n\n", style.heading.render(first.Format("January 2006")+
		" — local author dates, all authors and merges"))

	output.WriteString(horizontalRule(style, cell, "┌", "┬", "┐"))
	output.WriteString(bar)
	for column, name := range weekdayNames(text) {
		header := style.weekday
		if isWeekend(column) {
			header = style.weekendName
		}
		output.WriteString(cellContents(header.render(name), cell))
		output.WriteString(bar)
	}
	output.WriteString("\n")

	for start := 1 - offset; start <= daysInMonth; start += 7 {
		output.WriteString(horizontalRule(style, cell, "├", "┼", "┤"))

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
			columns[column] = dayLines(date, byDate[date.Format("2006-01-02")], options, text, location, isWeekend(column))
			if len(columns[column]) > height {
				height = len(columns[column])
			}
		}

		for row := 0; row < height; row++ {
			output.WriteString(bar)
			for _, lines := range columns {
				line := ""
				if row < len(lines) {
					line = lines[row]
				}
				output.WriteString(cellContents(line, cell))
				output.WriteString(bar)
			}
			output.WriteString("\n")
		}
	}
	output.WriteString(horizontalRule(style, cell, "└", "┴", "┘"))

	scope := fmt.Sprintf("read %d of %d selected repositories (%d discovered)",
		activity.ReadRepositories, activity.SelectedRepositories, activity.DiscoveredRepositories)
	if options.Group != "" {
		scope = style.label.render(options.Group+" only") + " · " + scope
	}
	fmt.Fprintf(&output, "\n%d unique commits on %d days; %s.\n", total, len(activity.Days), scope)
	footer := options.Footer
	if footer == "" {
		footer = style.status.render(fmt.Sprintf(
			"Showing up to %d commits per date. Use --day YYYY-MM-DD for one date's full list.",
			options.EntriesPerDay))
	}
	fmt.Fprintln(&output, footer)
	return output.String()
}

// dayLines builds one cell's text: the date number, then as many entries as
// fit the caller's limit, then a count of whatever had to be left out.
func dayLines(date time.Time, entries []ActivityEntry, options calendarOptions, text int, location *time.Location, weekend bool) []string {
	style := options.Styles
	key := date.Format("2006-01-02")
	number := " " + strconv.Itoa(date.Day()) + " "

	// Today and the selection are badges, and one date can be both. Otherwise
	// the weekend tint and whether the date has any commits decide the colour.
	switch {
	case key == options.Today && key == options.Selected:
		number = style.todaySelected.render(number)
	case key == options.Today:
		number = style.today.render(number)
	case key == options.Selected:
		number = style.selected.render(number)
	case len(entries) > 0 && weekend:
		number = style.weekendDate.render(number)
	case len(entries) > 0:
		number = style.date.render(number)
	case weekend:
		number = style.quietWeekend.render(number)
	default:
		number = style.quietDate.render(number)
	}
	lines := []string{number}

	shown := entries
	if len(shown) > options.EntriesPerDay {
		shown = shown[:options.EntriesPerDay]
	}
	for _, entry := range shown {
		lines = append(lines, entryLine(entry, style, text, location))
	}
	if hidden := len(entries) - len(shown); hidden > 0 {
		lines = append(lines, style.more.render(fmt.Sprintf("+%d more", hidden)))
	}
	return lines
}

// entryLine adds fields only while they still leave room for the subject.
func entryLine(entry ActivityEntry, style styles, text int, location *time.Location) string {
	line := displayText(entry.Commit.Subject)
	if text >= textWidthForRepository && len(entry.Repositories) > 0 {
		name := displayText(filepath.Base(entry.Repositories[0]))
		line = style.repository(name).render(name) + ": " + line
	}
	if text >= textWidthForTime {
		line = style.time.render(entry.Commit.AuthoredAt.In(location).Format("15:04")) + " " + line
	}
	return line
}

// Monday starts the week, so Saturday and Sunday are the last two columns.
func isWeekend(column int) bool {
	return column >= 5
}

func weekdayNames(text int) []string {
	if text < 3 {
		return []string{"M", "T", "W", "T", "F", "S", "S"}
	}
	return []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
}

func horizontalRule(style styles, cell int, left, join, right string) string {
	segments := make([]string, calendarColumns)
	for i := range segments {
		segments[i] = strings.Repeat("─", cell)
	}
	return style.border.render(left+strings.Join(segments, join)+right) + "\n"
}

// cellContents shortens and pads by display width, so East Asian characters,
// emoji, and the styles' invisible escape codes all stay aligned.
func cellContents(line string, cell int) string {
	line = ansi.Truncate(line, cell-2, "…")
	// Shortening can cut a style's closing sequence, which would let colour
	// bleed across the rest of the row.
	if strings.Contains(line, "\x1b") && !strings.HasSuffix(line, "\x1b[0m") {
		line += "\x1b[0m"
	}
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
func renderDayList(activity Activity, date string, selected, width, height int, style styles) string {
	entries := entriesForDate(activity, date)
	var output strings.Builder
	fmt.Fprintf(&output, "%s\n\n", style.heading.render(fmt.Sprintf("%s — %d commits", date, len(entries))))
	if len(entries) == 0 {
		output.WriteString(style.status.render("  No commits on this date.") + "\n")
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
		output.WriteString(style.status.render(fmt.Sprintf("  ↑ %d earlier", start)) + "\n")
	}
	for index := start; index < end; index++ {
		commit := entries[index].Commit
		marker := "  "
		if index == selected {
			marker = "▸ "
		}
		line := marker + style.time.render(
			commit.AuthoredAt.In(activity.Month.Location()).Format("15:04")) +
			"  " + style.label.render(fmt.Sprintf("%.12s", commit.Hash)) +
			"  " + displayText(commit.Subject)
		if repositories := entries[index].Repositories; len(repositories) > 0 {
			name := displayText(filepath.Base(repositories[0]))
			line += "  · " + style.repository(name).render(name)
		}
		line = ansi.Truncate(line, width, "…")
		if index == selected {
			line = style.row.render(line)
		}
		output.WriteString(line + "\n")
	}
	if end < len(entries) {
		output.WriteString(style.status.render(fmt.Sprintf("  ↓ %d later", len(entries)-end)) + "\n")
	}
	return output.String()
}

func renderCommitDetail(entry ActivityEntry, location *time.Location, style styles) string {
	commit := entry.Commit
	var output strings.Builder
	fmt.Fprintf(&output, "%s\n\n", style.heading.render(displayText(commit.Subject)))
	fmt.Fprintf(&output, "%s  %s\n", style.label.render("  Commit"), commit.Hash)
	fmt.Fprintf(&output, "%s  %s <%s>\n", style.label.render("  Author"),
		displayText(commit.AuthorName), displayText(commit.AuthorEmail))
	fmt.Fprintf(&output, "%s  %s\n", style.label.render("  Date  "),
		commit.AuthoredAt.In(location).Format("Monday, 2 January 2006, 15:04 -07:00"))
	for index, repository := range entry.Repositories {
		label := style.label.render("  Repo  ")
		if index > 0 {
			label = style.label.render("        ")
		}
		name := displayText(filepath.Base(repository))
		fmt.Fprintf(&output, "%s  %s\n", label,
			style.repository(name).render(displayText(repository)))
	}
	if len(entry.Groups) > 0 {
		fmt.Fprintf(&output, "%s  %s\n", style.label.render("  Group "),
			style.status.render(strings.Join(entry.Groups, ", ")))
	}
	output.WriteString("\n" + style.status.render(
		"Only the subject line is stored; full commit bodies are not read yet.") + "\n")
	return output.String()
}
