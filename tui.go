package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// The plan's three levels, plus the repository picker that edits groups and
// exclusions without leaving the interface.
type viewMode int

const (
	viewGrid viewMode = iota
	viewDay
	viewCommit
	viewRepos
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

// reposLoadedMsg carries every discovered repository, not only the ones the
// active filter selects, so an excluded repository stays reachable to edit.
type reposLoadedMsg struct {
	repositories []string
	err          error
}

// Cached results belong to a calendar month and its active filters. A different
// group or author view can contain different commits for the very same month.
type calendarCacheKey struct {
	year  int
	month time.Month
	zone  *time.Location
	group string
	mine  bool
}

const calendarCacheLimit = 6

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
	// Keep a few completed views in memory for quick back-and-forth navigation.
	cache map[calendarCacheKey]Activity
	order []calendarCacheKey

	width, height int
	help          bool
	styles        styles

	// activeGroup is the filter actually in effect for this session. It starts
	// at chosen.group but Tab can move it on, without touching --group itself.
	activeGroup string

	// The repository picker (viewRepos) edits chosen.config directly and saves
	// on every change, so a crash mid-edit cannot lose more than one keystroke.
	repos            []string
	reposIndex       int
	reposLoading     bool
	reposErr         error
	reposSaveErr     error
	reposChanged     bool
	reposNeedsReload bool
	editingGroup     bool
	groupInput       string
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
		activeGroup:   chosen.group,
	}
}

// groupCycle lists every stop Tab visits, in order: everything, then each
// group in use, then the repositories nobody has assigned yet. It is
// recomputed on each press, so a group created moments ago in the picker
// already has a place in the cycle.
func (m calendarModel) groupCycle() []string {
	cycle := append([]string{""}, m.chosen.config.groupNames()...)
	return append(cycle, ungrouped)
}

// cycleGroup moves the session's active filter on by one stop and reloads,
// the same way changing month does. --group itself is untouched, so the next
// run starts back where it was launched, not wherever this session ended.
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

func startOfMonth(month time.Time) time.Time {
	return time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, month.Location())
}

func daysIn(month time.Time) int {
	return startOfMonth(month).AddDate(0, 1, -1).Day()
}

func (m calendarModel) Init() tea.Cmd {
	return func() tea.Msg { return reloadMsg{} }
}

func (m calendarModel) cacheKey() calendarCacheKey {
	return calendarCacheKey{m.month.Year(), m.month.Month(), m.month.Location(), m.activeGroup, m.chosen.authors.mineOnly}
}

func (m *calendarModel) forgetCachedActivity() {
	m.cache, m.order = nil, nil
}

// rememberActivity evicts the oldest completed view when the cache is full.
func (m *calendarModel) rememberActivity(activity Activity) {
	key := m.cacheKey()
	if m.cache == nil {
		m.cache = make(map[calendarCacheKey]Activity)
	}
	if _, exists := m.cache[key]; !exists {
		if len(m.order) == calendarCacheLimit {
			delete(m.cache, m.order[0])
			m.order = m.order[1:]
		}
		m.order = append(m.order, key)
	}
	m.cache[key] = activity
}

// beginLoad cancels a previous scan, then either restores a completed view or
// starts a fresh read. The request number rejects late results in both cases.
func (m *calendarModel) beginLoad() tea.Cmd {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.request++
	m.loadErr = nil
	if cached, ok := m.cache[m.cacheKey()]; ok {
		m.activity = cached
		m.loading = false
		m.commit = 0
		return nil
	}
	ctx, cancel := context.WithCancel(m.root)
	m.cancel = cancel
	m.loading = true

	request, chosen, month := m.request, m.chosen, m.month
	return func() tea.Msg {
		activity, err := collectActivity(ctx, chosen.roots, month, chosen.filter, chosen.authors)
		return activityLoadedMsg{request: request, activity: activity, err: err}
	}
}

// toggleMine flips the session's author filter, the same way cycleGroup flips
// which group is shown. Switching to mine-only with no identities configured
// would just be an empty grid with no clue why, so that case is refused with
// an explanation instead.
func (m calendarModel) toggleMine() (tea.Model, tea.Cmd) {
	if !m.chosen.authors.mineOnly && len(m.chosen.config.Identities) == 0 {
		m.loadErr = errors.New("no identities configured; run: gitcal identities add you@example.com")
		return m, nil
	}
	m.chosen.authors.mineOnly = !m.chosen.authors.mineOnly
	m.mode, m.activity, m.loadErr = viewGrid, Activity{Month: m.month}, nil
	return m, (&m).beginLoad()
}

// beginReposScan lists every discovered repository under the configured
// folders. It shares the cancel-the-predecessor pattern with beginLoad, since
// only one background scan is ever useful at a time.
func (m *calendarModel) beginReposScan() tea.Cmd {
	if m.loading {
		// The calendar command may still deliver a canceled result. Invalidate
		// its request, and reload when the user returns to the grid.
		m.request++
		m.loading = false
		m.reposNeedsReload = true
	}
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

// toggleExclude flips whether the selected repository is scanned at all.
func (m *calendarModel) toggleExclude() {
	if m.reposIndex >= len(m.repos) {
		return
	}
	repository := m.repos[m.reposIndex]
	if m.chosen.config.isExcluded(repository) {
		m.chosen.config.include(repository)
	} else {
		m.chosen.config.exclude(repository)
	}
	m.saveChosen()
}

// setGroup assigns the selected repository's group. An empty name clears it,
// which matches what a text field reads when nothing has been typed into it.
func (m *calendarModel) setGroup(name string) {
	if m.reposIndex >= len(m.repos) {
		return
	}
	m.chosen.config.setGroup(m.repos[m.reposIndex], name)
	m.saveChosen()
}

// currentGroup reads back what setGroup would need to reproduce, so opening
// the text field starts from the repository's existing group rather than
// blank.
func (m calendarModel) currentGroup() string {
	if m.reposIndex >= len(m.repos) {
		return ""
	}
	if group := m.chosen.filter.groupOf(m.repos[m.reposIndex]); group != ungrouped {
		return group
	}
	return ""
}

// saveChosen writes the edited configuration immediately, rather than waiting
// for the picker to close, so a change survives even if the program is killed
// right afterwards.
func (m *calendarModel) saveChosen() {
	m.chosen.filter = m.chosen.config.filter(m.activeGroup)
	m.forgetCachedActivity()
	m.reposChanged = true
	m.reposSaveErr = saveConfig(m.chosen.path, m.chosen.config)
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
		if message.err == nil {
			m.rememberActivity(message.activity)
		}
		return m, nil

	case reposLoadedMsg:
		m.reposLoading = false
		m.repos, m.reposErr = message.repositories, message.err
		if m.reposIndex >= len(m.repos) {
			m.reposIndex = 0
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(message)
	}
	return m, nil
}

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
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit
	case "?":
		m.help = !m.help
		return m, nil
	case "r":
		m.forgetCachedActivity()
		if m.mode == viewRepos {
			m.reposNeedsReload = true
			return m, (&m).beginReposScan()
		}
		return m, (&m).beginLoad()
	case "g":
		if m.mode == viewGrid {
			m.mode, m.reposIndex = viewRepos, 0
			return m, (&m).beginReposScan()
		}
	case "tab":
		return m.cycleGroup()
	case "m":
		return m.toggleMine()
	}

	switch m.mode {
	case viewGrid:
		return m.handleGridKey(key)
	case viewRepos:
		return m.handleReposKey(key)
	}
	return m.handleListKey(key)
}

func (m calendarModel) handleReposKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc", "backspace", "left", "h":
		m.mode = viewGrid
		if m.reposChanged || m.reposNeedsReload {
			// Edits, refreshes and interrupted loads need fresh calendar data.
			m.reposChanged, m.reposNeedsReload = false, false
			return m, (&m).beginLoad()
		}
		return m, nil
	case "up", "k":
		if m.reposIndex > 0 {
			m.reposIndex--
		}
		return m, nil
	case "down", "j":
		if m.reposIndex < len(m.repos)-1 {
			m.reposIndex++
		}
		return m, nil
	case "enter":
		if len(m.repos) == 0 {
			return m, nil
		}
		m.editingGroup = true
		m.groupInput = m.currentGroup()
		return m, nil
	case "e":
		if len(m.repos) == 0 {
			return m, nil
		}
		(&m).toggleExclude()
		return m, nil
	}
	return m, nil
}

// handleGroupInputKey builds the typed text one key at a time. tea.KeyType is
// used instead of key.String() so that a character with special meaning
// elsewhere, such as q or ?, is still just a character here.
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
	case viewRepos:
		return m.reposView()
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
		Group:         m.activeGroup,
		Mine:          m.chosen.authors.mineOnly,
		Styles:        m.styles,
		Footer:        m.status("←↑↓→ move · [ ] month · enter open · g repos · ? all keys · q quit"),
	})
}

// reposView lists every discovered repository with its current group or
// exclusion, so assigning one never requires leaving the calendar.
func (m calendarModel) reposView() string {
	var output strings.Builder
	output.WriteString(m.styles.heading.render("Repositories") + "\n\n")

	switch {
	case m.reposLoading:
		output.WriteString(m.styles.loading.render("Scanning your configured folders…") + "\n")
		return output.String()
	case m.reposErr != nil:
		output.WriteString(m.styles.failure.render("Error: "+displayText(m.reposErr.Error())) + "\n\n")
		output.WriteString(m.styles.status.render("r retries · esc back · q quit") + "\n")
		return output.String()
	case len(m.repos) == 0:
		output.WriteString(m.styles.status.render("No repositories found under your configured folders.") + "\n\n")
		output.WriteString(m.styles.status.render("esc back · q quit") + "\n")
		return output.String()
	}

	width := 0
	for _, repository := range m.repos {
		if name := filepath.Base(repository); len(name) > width {
			width = len(name)
		}
	}
	for index, repository := range m.repos {
		marker := "  "
		if index == m.reposIndex {
			marker = "▸ "
		}
		name := displayText(filepath.Base(repository))
		included := !m.chosen.config.isExcluded(repository)
		state := m.chosen.filter.groupOf(repository)
		stateStyle := m.styles.status
		switch {
		case !included:
			state, stateStyle = "excluded", m.styles.failure
		case state != ungrouped:
			stateStyle = m.styles.label
		}
		fmt.Fprintf(&output, "%s%s  %s  %s\n", marker,
			m.styles.repository(name).render(fmt.Sprintf("%-*s", width, name)),
			stateStyle.render(fmt.Sprintf("%-10s", state)),
			m.styles.status.render(displayText(repository)))
	}
	output.WriteString("\n")

	if m.editingGroup {
		fmt.Fprintf(&output, "%s%s▏\n", m.styles.label.render("  Group: "), m.groupInput)
		output.WriteString(m.styles.status.render(
			"type a name · enter save (blank clears the group) · esc cancel") + "\n")
		return output.String()
	}
	if m.reposSaveErr != nil {
		output.WriteString(m.styles.failure.render("Could not save: "+displayText(m.reposSaveErr.Error())) + "\n")
	}
	output.WriteString(m.styles.status.render(
		"↑↓ move · enter set group · e exclude/include · r rescan · esc back · q quit") + "\n")
	return output.String()
}

// status keeps loading and failure visible in the same place as the key hints.
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
	{"r", "Refresh cached activity, or rescan repositories in the picker"},
	{"g", "Open the repository picker: assign groups, exclude/include"},
	{"Tab", "Cycle the calendar between all, ungrouped, and each group in use"},
	{"m", "Toggle between everyone's commits and only your own"},
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
		"Dates use local author time. Merges and all authors are included unless filtered.") + "\n")
	output.WriteString("\n\n" + m.styles.status.render(
		"A new month reads every selected repository's history. The six most\n"+
			"recent completed views stay in memory; r fetches fresh data.") + "\n")
	return output.String()
}
