package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
)

const fallbackWidth = 80

func runCalendar(ctx context.Context, args []string, out, errOut io.Writer) int {
	options := flag.NewFlagSet("calendar", flag.ContinueOnError)
	options.SetOutput(errOut)
	monthText := options.String("month", time.Now().Format("2006-01"), "month to show (YYYY-MM), in local time")
	dayText := options.String("day", "", "list every commit for one date (YYYY-MM-DD) instead of the grid")
	width := options.Int("width", 0, "grid width in columns; 0 detects the terminal")
	entriesPerDay := options.Int("per-day", defaultEntriesPerDay, `commits shown in each date cell before "+N more"`)
	interactive := options.Bool("interactive", false, "browse the calendar with the keyboard even when output is redirected")
	static := options.Bool("static", false, "print the grid once and exit, even on a terminal")
	options.Usage = func() {
		fmt.Fprintln(options.Output(), "Usage: gitcal calendar [--month YYYY-MM] [--day YYYY-MM-DD] [--per-day N] [--width N] <folder>")
		fmt.Fprintln(options.Output(), "Place options before the folder; omit --month for the current month.")
		fmt.Fprintln(options.Output(), "On a terminal the calendar is interactive; redirected output is printed once.")
		options.PrintDefaults()
	}
	if err := options.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if options.NArg() != 1 {
		options.Usage()
		return 2
	}

	monthGiven := false
	options.Visit(func(f *flag.Flag) {
		if f.Name == "month" {
			monthGiven = true
		}
	})
	if *dayText != "" && monthGiven {
		fmt.Fprintln(errOut, "Error: use --day or --month, not both; --day already identifies its month.")
		return 2
	}
	if *entriesPerDay < 1 {
		fmt.Fprintln(errOut, "Error: --per-day must be 1 or more.")
		return 2
	}
	if *interactive && *static {
		fmt.Fprintln(errOut, "Error: use --interactive or --static, not both.")
		return 2
	}

	// A date selects its own month, so both paths read the same activity data.
	layout, value := "2006-01", *monthText
	if *dayText != "" {
		layout, value = "2006-01-02", *dayText
	}
	selected, err := time.ParseInLocation(layout, value, time.Local)
	if err != nil || selected.Year() < 1 {
		if *dayText != "" {
			fmt.Fprintln(errOut, "Error: --day must be YYYY-MM-DD (for example, 2026-09-18).")
		} else {
			fmt.Fprintln(errOut, "Error: --month must be YYYY-MM with year 0001–9999 and month 01–12 (for example, 2026-09).")
		}
		return 2
	}

	// The interactive view reads each month itself, so it starts before any scan.
	if *dayText == "" && !*static && (*interactive || canBeInteractive(out)) {
		return runInteractiveCalendar(ctx, options.Arg(0), selected, *entriesPerDay,
			resolveWidth(out, *width), errOut)
	}

	activity, err := collectActivity(ctx, options.Arg(0), selected)
	if err != nil {
		fmt.Fprintf(errOut, "Error: %v\n", err)
		return 1
	}

	if *dayText != "" {
		fmt.Fprint(out, renderDay(activity, selected.Format("2006-01-02")))
	} else {
		fmt.Fprint(out, renderCalendar(activity, calendarOptions{
			Width:         resolveWidth(out, *width),
			Today:         time.Now().In(time.Local).Format("2006-01-02"),
			EntriesPerDay: *entriesPerDay,
			Styles:        newStyles(supportsHighlight(out)),
		}))
	}

	for _, warning := range activity.Warnings {
		fmt.Fprintf(errOut, "Warning: %v\n", warning)
	}
	if len(activity.Warnings) > 0 {
		fmt.Fprintln(errOut, "Calendar incomplete: some paths or repositories could not be read.")
		return 1
	}
	return 0
}

// resolveWidth asks the terminal for its size, falling back to a width that
// fits a standard console when output is redirected to a file or a pipe.
func resolveWidth(out io.Writer, override int) int {
	if override > 0 {
		return override
	}
	if file, ok := out.(*os.File); ok {
		if columns, _, err := term.GetSize(int(file.Fd())); err == nil && columns > 0 {
			return columns
		}
	}
	return fallbackWidth
}

// canBeInteractive requires a terminal on both ends: keyboard control needs a
// real stdin, and redirected output must stay plain text for scripts.
func canBeInteractive(out io.Writer) bool {
	file, ok := out.(*os.File)
	return ok && term.IsTerminal(int(file.Fd())) && term.IsTerminal(int(os.Stdin.Fd()))
}

func runInteractiveCalendar(ctx context.Context, folder string, month time.Time, entriesPerDay, width int, errOut io.Writer) int {
	model := newCalendarModel(ctx, folder, month, entriesPerDay, width)
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(ctx))
	finished, err := program.Run()
	if err != nil && !errors.Is(err, tea.ErrProgramKilled) && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(errOut, "Error: %v\n", err)
		return 1
	}
	if final, ok := finished.(calendarModel); ok && final.loadErr != nil {
		fmt.Fprintf(errOut, "Error: %v\n", final.loadErr)
		return 1
	}
	return 0
}

func supportsHighlight(out io.Writer) bool {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
		return false
	}
	file, ok := out.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}
