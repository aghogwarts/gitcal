package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

func runActivity(ctx context.Context, args []string, out, errOut io.Writer) int {
	options := flag.NewFlagSet("activity", flag.ContinueOnError)
	options.SetOutput(errOut)
	monthText := options.String("month", time.Now().Format("2006-01"), "month to show (YYYY-MM), in local time")
	group := options.String("group", "", "only repositories in this group")
	options.Usage = func() {
		fmt.Fprintln(options.Output(), "Usage: gitcal activity [--month YYYY-MM] [--group NAME] [folder]")
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
	chosen, ok := resolveSelection(options.Arg(0), *group, os.Stdin, out, errOut)
	if !ok {
		return 1
	}
	result, err := collectActivity(ctx, chosen.roots, month, chosen.filter)
	if err != nil {
		fmt.Fprintf(errOut, "Error: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "Activity for %s — local author dates, all authors and merges\n", result.Month.Format("January 2006"))
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
	fmt.Fprintf(out, "\n%d unique commits on %d days; read %d of %d selected repositories (%d discovered).\n",
		total, len(result.Days), result.ReadRepositories, result.SelectedRepositories, result.DiscoveredRepositories)
	for _, warning := range result.Warnings {
		fmt.Fprintf(errOut, "Warning: %v\n", warning)
	}
	if len(result.Warnings) > 0 {
		fmt.Fprintln(errOut, "Activity incomplete: some paths or repositories could not be read.")
		return 1
	}
	return 0
}
