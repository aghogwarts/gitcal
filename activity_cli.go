package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

func runActivity(ctx context.Context, args []string, out, errOut io.Writer) int {
	options := flag.NewFlagSet("activity", flag.ContinueOnError)
	options.SetOutput(errOut)
	monthText := options.String("month", time.Now().Format("2006-01"), "month to show (YYYY-MM), in local time")
	group := options.String("group", "", "only repositories in this group")
	mine := options.Bool("mine", false, "only commits authored by one of your configured identities")
	showTimings := options.Bool("timings", false, "print discovery and history timings to stderr")
	options.Usage = func() {
		fmt.Fprintln(options.Output(), "Usage: gitcal activity [--month YYYY-MM] [--group NAME] [--mine] [--timings] [folder]")
		fmt.Fprintln(options.Output(), "Place options before the folder; omit the folder to use your configured folders.")
		options.PrintDefaults()
	}
	if err := options.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if options.NArg() > 1 {
		options.Usage()
		return 2
	}
	month, err := time.ParseInLocation("2006-01", *monthText, time.Local)
	if err != nil || month.Year() < 1 {
		fmt.Fprintln(errOut, "Error: month must be YYYY-MM with year 0001–9999 and month 01–12 (for example, 2026-09).")
		return 2
	}
	chosen, ok := resolveSelection(options.Arg(0), *group, *mine, os.Stdin, out, errOut)
	if !ok {
		return 1
	}
	var timings *activityTimings
	if *showTimings {
		timings = new(activityTimings)
	}
	result, err := collectActivityWithTimings(ctx, chosen.roots, month, chosen.filter, chosen.authors, timings)
	if timings != nil {
		printActivityTimings(errOut, *timings)
	}
	if err != nil {
		fmt.Fprintf(errOut, "Error: %v\n", err)
		return 1
	}

	var tags []string
	if *mine {
		tags = append(tags, "mine only")
	}
	if *group != "" {
		tags = append(tags, *group+" only")
	}
	scope := "all authors and merges"
	if len(tags) > 0 {
		scope = strings.Join(tags, " · ")
	}
	fmt.Fprintf(out, "Activity for %s — local author dates, %s\n", result.Month.Format("January 2006"), scope)
	total := 0
	for _, day := range result.Days {
		fmt.Fprintf(out, "\n%s (%d commits)\n", day.Date, len(day.Entries))
		for _, entry := range day.Entries {
			commit := entry.Commit
			fmt.Fprintf(out, "  %s  %.12s  %s\n    %s <%s>\n",
				commit.AuthoredAt.In(month.Location()).Format("15:04 -07:00"),
				commit.Hash, displayText(commit.Subject),
				displayText(commit.AuthorName), displayText(commit.AuthorEmail))
			for _, repo := range entry.Repositories {
				fmt.Fprintf(out, "    repo: %s\n", displayText(repo))
			}
			total++
		}
	}
	if total == 0 {
		fmt.Fprintln(out, "No matching commits in the repositories successfully read.")
	}
	scanScope := fmt.Sprintf("read %d of %d selected repositories (%d discovered)",
		result.ReadRepositories, result.SelectedRepositories, result.DiscoveredRepositories)
	if len(tags) > 0 {
		scanScope = strings.Join(tags, " · ") + " · " + scanScope
	}
	fmt.Fprintf(out, "\n%d unique commits on %d days; %s.\n", total, len(result.Days), scanScope)
	for _, warning := range result.Warnings {
		fmt.Fprintf(errOut, "Warning: %v\n", warning)
	}
	if len(result.Warnings) > 0 {
		fmt.Fprintln(errOut, "Activity incomplete: some paths or repositories could not be read.")
		return 1
	}
	return 0
}

func printActivityTimings(out io.Writer, timings activityTimings) {
	other := timings.Total - timings.Discovery - timings.History
	if other < 0 {
		other = 0
	}
	fmt.Fprintf(out, "Timings (activity collection):\n  Discovery: %s\n  Git history: %s\n  Other: %s\n  Total: %s\n",
		timings.Discovery, timings.History, other, timings.Total)
	if timings.SlowestRepository != "" {
		fmt.Fprintf(out, "  Slowest repository read: %s (%s)\n",
			displayText(timings.SlowestRepository), timings.SlowestRead)
	}
}
