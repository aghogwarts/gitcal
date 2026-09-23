package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// The three levels the plan calls for: the month, one date, one commit.
type viewMode int

const (
	viewGrid viewMode = iota
	viewDay
	viewCommit
)

// reloadMsg asks the model to start a scan. Init cannot change the model, so
// the first load arrives as a message like every later one.
type reloadMsg struct{}

// activityLoadedMsg carries a finished scan. Its request number lets the model
// ignore results for a month the user has already navigated away from.
type activityLoadedMsg struct {
	request  int
	activity Activity
	err      error
}

type calendarModel struct {
	root          context.Context
	chosen        selection
	entriesPerDay int

	month    time.Time
	selected time.Time
	mode     viewMode
	commit   int

	activity Activity
	loading  bool
	loadErr  error
	request  int
	cancel   context.CancelFunc

	width, height int
	help          bool
	styles        styles
}

func newCalendarModel(ctx context.Context, chosen selection, month time.Time, entriesPerDay, width int) calendarModel {
	return calendarModel{
		root:          ctx,
		chosen:        chosen,
		entriesPerDay: entriesPerDay,
		month:         month,
		selected:      startOfMonth(month),
		width:         width,
		height:        24,
		styles:        newStyles(true),
	}
}

func startOfMonth(month time.Time) time.Time {
	return time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, month.Location())
}

func daysIn(month time.Time) int {
	return startOfMonth(month).AddDate(0, 1, -1).Day()
}

func (m calendarModel) Init() tea.Cmd {
	return func() tea.Msg { return reloadMsg{} }
}

// beginLoad cancels any scan still running for a previous month, so holding a
// month key does not leave several full history reads competing for the disk.
func (m *calendarModel) beginLoad() tea.Cmd {
	if m.cancel != nil {
		m.cancel()
	}
	ctx, cancel := context.WithCancel(m.root)
	m.cancel = cancel
	m.request++
	m.loading = true
	m.loadErr = nil

	request, chosen, month := m.request, m.chosen, m.month
	return func() tea.Msg {
		activity, err := collectActivity(ctx, chosen.roots, month, chosen.filter)
		return activityLoadedMsg{request: request, activity: activity, err: err}
	}
}

func (m calendarModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = message.Width, message.Height
		return m, nil

	case reloadMsg:
		return m, (&m).beginLoad()

	case activityLoadedMsg:
		// A superseded month's result would contradict the header, so drop it.
		if message.request != m.request {
			return m, nil
		}
		m.loading = false
		m.activity, m.loadErr = message.activity, message.err
		m.commit = 0
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(message)
	}
	return m, nil
}

func (m calendarModel) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c", "q":
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit
	case "?":
		m.help = !m.help
		return m, nil
	case "r":
		return m, (&m).beginLoad()
	}

	if m.mode == viewGrid {
		return m.handleGridKey(key)
	}
	return m.handleListKey(key)
}

func (m calendarModel) handleGridKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "left", "h":
		return m.moveSelection(-1), nil
	case "right", "l":
		return m.moveSelection(1), nil
	case "up", "k":
		return m.moveSelection(-7), nil
	case "down", "j":
		return m.moveSelection(7), nil
	case "[", "pgup":
		return m.changeMonth(-1)
	case "]", "pgdown":
		return m.changeMonth(1)
	case "t":
		return m.jumpToToday()
	case "enter":
		if len(m.selectedEntries()) > 0 {
			m.mode, m.commit = viewDay, 0
		}
		return m, nil
	}
	return m, nil
}

func (m calendarModel) handleListKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	entries := m.selectedEntries()
	switch key.String() {
	case "esc", "backspace", "left", "h":
		if m.mode == viewCommit {
			m.mode = viewDay
		} else {
			m.mode = viewGrid
		}
		return m, nil
	case "enter", "right", "l":
		if m.mode == viewDay && len(entries) > 0 {
			m.mode = viewCommit
		}
		return m, nil
	case "up", "k":
		if m.commit > 0 {
			m.commit--
		}
		return m, nil
	case "down", "j":
		if m.commit < len(entries)-1 {
			m.commit++
		}
		return m, nil
	}
	return m, nil
}

// moveSelection stops at the month's edges: changing month is always a
// deliberate keypress, never a side effect of moving one day.
func (m calendarModel) moveSelection(days int) calendarModel {
	day := m.selected.Day() + days
	if day < 1 || day > daysIn(m.month) {
		return m
	}
	m.selected = m.selected.AddDate(0, 0, days)
	return m
}

func (m calendarModel) changeMonth(offset int) (tea.Model, tea.Cmd) {
	m.month = startOfMonth(m.month).AddDate(0, offset, 0)
	day := m.selected.Day()
	if limit := daysIn(m.month); day > limit {
		day = limit
	}
	m.selected = time.Date(m.month.Year(), m.month.Month(), day, 0, 0, 0, 0, m.month.Location())
	m.mode, m.activity = viewGrid, Activity{Month: m.month}
	return m, (&m).beginLoad()
}

func (m calendarModel) jumpToToday() (tea.Model, tea.Cmd) {
	today := time.Now().In(m.month.Location())
	m.selected = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, m.month.Location())
	if today.Year() == m.month.Year() && today.Month() == m.month.Month() {
		return m, nil
	}
	m.month = startOfMonth(m.selected)
	m.mode, m.activity = viewGrid, Activity{Month: m.month}
	return m, (&m).beginLoad()
}

func (m calendarModel) selectedEntries() []ActivityEntry {
	return entriesForDate(m.activity, m.selected.Format("2006-01-02"))
}

func (m calendarModel) View() string {
	if m.help {
		return m.helpView()
	}
	switch m.mode {
	case viewDay:
		return renderDayList(m.activity, m.selected.Format("2006-01-02"), m.commit, m.width, m.height-4, m.styles) +
			"\n" + m.status("enter detail · esc back · ↑↓ move · q quit")
	case viewCommit:
		entries := m.selectedEntries()
		if m.commit >= len(entries) {
			return m.status("no commit selected · esc back")
		}
		return renderCommitDetail(entries[m.commit], m.month.Location(), m.styles) +
			"\n" + m.status("esc back · q quit")
	}

	grid := Activity{Month: m.month}
	if !m.loading && m.loadErr == nil {
		grid = m.activity
	}
	return renderCalendar(grid, calendarOptions{
		Width:         m.width,
		Today:         time.Now().In(m.month.Location()).Format("2006-01-02"),
		Selected:      m.selected.Format("2006-01-02"),
		EntriesPerDay: m.entriesPerDay,
		Group:         m.chosen.group,
		Styles:        m.styles,
		Footer:        m.status("←↑↓→ date · [ ] month · enter open · t today · r refresh · ? help · q quit"),
	})
}

// status keeps loading and failure visible in the same place as the key hints,
// which matters because every month change triggers a fresh scan.
func (m calendarModel) status(keys string) string {
	switch {
	case m.loading:
		return m.styles.loading.render(fmt.Sprintf("Reading %s… (q cancels)", m.month.Format("January 2006")))
	case m.loadErr != nil:
		return m.styles.failure.render("Error: "+displayText(m.loadErr.Error())) +
			m.styles.status.render(" · r retries · q quits")
	case len(m.activity.Warnings) > 0:
		return m.styles.failure.render(fmt.Sprintf("%d repositories could not be read", len(m.activity.Warnings))) +
			m.styles.status.render(" · "+keys)
	default:
		return m.styles.status.render(strings.TrimSpace(
			m.selected.Format("Monday, 2 January 2006") + " · " + keys))
	}
}

var helpKeys = [][2]string{
	{"← → ↑ ↓", "Move between dates (stops at the month's edge)"},
	{"h l k j", "The same, without the arrow keys"},
	{"[ ]", "Previous / next month, also PgUp and PgDn"},
	{"Enter", "Open the selected date, then the selected commit"},
	{"Esc", "Back up one level"},
	{"t", "Jump to today"},
	{"r", "Re-read the current month"},
	{"?", "Close this help"},
	{"q, Ctrl+C", "Quit"},
}

func (m calendarModel) helpView() string {
	var output strings.Builder
	output.WriteString(m.styles.heading.render("gitcal calendar — keys") + "\n\n")
	for _, binding := range helpKeys {
		fmt.Fprintf(&output, "  %s  %s\n",
			m.styles.key.render(fmt.Sprintf("%-11s", binding[0])), binding[1])
	}
	output.WriteString("\n" + m.styles.heading.render("Colours") + "\n\n")
	output.WriteString("  " + m.styles.today.render(" 9 ") + "  today" +
		"    " + m.styles.selected.render(" 9 ") + "  selected date\n")
	output.WriteString("  " + m.styles.weekendDate.render(" 9 ") + "  weekend" +
		"  " + m.styles.quietDate.render(" 9 ") + "  no commits\n")
	output.WriteString("\n  Each repository keeps its own colour, chosen from its name:\n  ")
	for _, name := range []string{"api", "website", "tooling", "docs"} {
		output.WriteString(m.styles.repository(name).render(name) + "  ")
	}
	output.WriteString("\n\n" + m.styles.status.render(
		"Each month change re-reads every repository's history, so a large\n"+
			"projects folder takes a moment. Caching is a later step.") + "\n")
	return output.String()
}
