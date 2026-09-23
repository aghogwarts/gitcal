package main

import (
	"hash/fnv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// paint applies a style only when colour is enabled, so every renderer can call
// it unconditionally and redirected output stays plain text.
type paint struct {
	style   lipgloss.Style
	enabled bool
}

func (p paint) render(text string) string {
	if !p.enabled || text == "" {
		return text
	}
	return p.style.Render(text)
}

// Adaptive colours carry a value for each terminal background, and lipgloss
// picks between them, so the same palette stays legible on light and dark.
var (
	colourFaint     = lipgloss.AdaptiveColor{Light: "#B6B6B6", Dark: "#4D4D4D"}
	colourMuted     = lipgloss.AdaptiveColor{Light: "#7A7A7A", Dark: "#7D7D7D"}
	colourStrong    = lipgloss.AdaptiveColor{Light: "#24292F", Dark: "#E6EDF3"}
	colourWeekend   = lipgloss.AdaptiveColor{Light: "#8250DF", Dark: "#BC8CFF"}
	colourAccent    = lipgloss.AdaptiveColor{Light: "#0969DA", Dark: "#58A6FF"}
	colourOnAccent  = lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#0D1117"}
	colourSelection = lipgloss.AdaptiveColor{Light: "#D0D7DE", Dark: "#30363D"}
	colourWarning   = lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#D29922"}
	colourFailure   = lipgloss.AdaptiveColor{Light: "#CF222E", Dark: "#FF7B72"}
)

// Repository colours are assigned by name rather than by position, so a
// repository keeps the same colour as months change and as others appear.
var repositoryColours = []lipgloss.AdaptiveColor{
	{Light: "#1A7F37", Dark: "#3FB950"},
	{Light: "#0969DA", Dark: "#58A6FF"},
	{Light: "#8250DF", Dark: "#BC8CFF"},
	{Light: "#BC4C00", Dark: "#F0883E"},
	{Light: "#137775", Dark: "#39C5BB"},
	{Light: "#BF3989", Dark: "#F778BA"},
	{Light: "#A40E26", Dark: "#FF7B72"},
	{Light: "#7D4E00", Dark: "#D4A72C"},
}

type styles struct {
	enabled bool

	border        paint
	weekday       paint
	weekendName   paint
	date          paint
	quietDate     paint
	weekendDate   paint
	quietWeekend  paint
	today         paint
	selected      paint
	todaySelected paint
	time          paint
	more          paint
	status        paint
	loading       paint
	failure       paint
	heading       paint
	label         paint
	key           paint
	row           paint

	repositories []paint
}

// styleRenderer decides which escape codes the palette produces. By default it
// follows the terminal, emitting nothing when it cannot show colour; tests
// replace it so their results do not depend on how they were launched.
var styleRenderer = lipgloss.DefaultRenderer()

func newStyles(enabled bool) styles {
	of := func(build func(lipgloss.Style) lipgloss.Style) paint {
		return paint{style: build(styleRenderer.NewStyle()), enabled: enabled}
	}
	plain := styleRenderer.NewStyle()

	built := styles{
		enabled:      enabled,
		border:       of(func(s lipgloss.Style) lipgloss.Style { return s.Foreground(colourFaint) }),
		weekday:      of(func(s lipgloss.Style) lipgloss.Style { return s.Foreground(colourMuted).Bold(true) }),
		weekendName:  of(func(s lipgloss.Style) lipgloss.Style { return s.Foreground(colourWeekend).Bold(true) }),
		date:         of(func(s lipgloss.Style) lipgloss.Style { return s.Foreground(colourStrong).Bold(true) }),
		quietDate:    of(func(s lipgloss.Style) lipgloss.Style { return s.Foreground(colourFaint) }),
		weekendDate:  of(func(s lipgloss.Style) lipgloss.Style { return s.Foreground(colourWeekend).Bold(true) }),
		quietWeekend: of(func(s lipgloss.Style) lipgloss.Style { return s.Foreground(colourWeekend).Faint(true) }),
		today: of(func(s lipgloss.Style) lipgloss.Style {
			return s.Background(colourAccent).Foreground(colourOnAccent).Bold(true)
		}),
		selected: of(func(s lipgloss.Style) lipgloss.Style {
			return s.Background(colourSelection).Foreground(colourStrong).Bold(true)
		}),
		todaySelected: of(func(s lipgloss.Style) lipgloss.Style {
			return s.Background(colourAccent).Foreground(colourOnAccent).Bold(true).Underline(true)
		}),
		time:    of(func(s lipgloss.Style) lipgloss.Style { return s.Foreground(colourMuted) }),
		more:    of(func(s lipgloss.Style) lipgloss.Style { return s.Foreground(colourMuted).Italic(true) }),
		status:  of(func(s lipgloss.Style) lipgloss.Style { return s.Foreground(colourMuted) }),
		loading: of(func(s lipgloss.Style) lipgloss.Style { return s.Foreground(colourWarning).Bold(true) }),
		failure: of(func(s lipgloss.Style) lipgloss.Style { return s.Foreground(colourFailure).Bold(true) }),
		heading: of(func(s lipgloss.Style) lipgloss.Style { return s.Foreground(colourStrong).Bold(true) }),
		label:   of(func(s lipgloss.Style) lipgloss.Style { return s.Foreground(colourMuted) }),
		key:     of(func(s lipgloss.Style) lipgloss.Style { return s.Foreground(colourAccent).Bold(true) }),
		row: of(func(s lipgloss.Style) lipgloss.Style {
			return s.Background(colourSelection).Foreground(colourStrong)
		}),
	}
	for _, colour := range repositoryColours {
		built.repositories = append(built.repositories, paint{style: plain.Foreground(colour), enabled: enabled})
	}
	return built
}

// repository picks a colour from the repository's name, so the choice survives
// repositories appearing, disappearing, and being reordered.
func (s styles) repository(name string) paint {
	if len(s.repositories) == 0 {
		return paint{}
	}
	digest := fnv.New32a()
	digest.Write([]byte(strings.ToLower(name)))
	return s.repositories[int(digest.Sum32())%len(s.repositories)]
}
