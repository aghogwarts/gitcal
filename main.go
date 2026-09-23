package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"
	"unicode"
)

const usage = `Usage:
  gitcal scan <folder>
  gitcal history <repository-folder>
  gitcal activity [--month YYYY-MM] [--group NAME] [--mine] [folder]
  gitcal calendar [--month YYYY-MM] [--day YYYY-MM-DD] [--group NAME] [--mine] [folder]
  gitcal roots [list | add <folder> | remove <folder>]
  gitcal repos [list | set <repository> <group> | exclude <repository> | include <repository>]
  gitcal identities [list | add <email> | remove <email>]
  gitcal help

Scan a folder and its subfolders for Git working repositories.
History shows up to 20 commits reachable from HEAD, including all authors
and merge commits. Author timestamps are displayed in your local timezone.
Activity groups that month's commits across discovered repositories by local
author date, counting identical hashes once. Default: current month.
Calendar shows the same month as a grid of weeks starting on Monday, with a
few commits inside each date and "+N more" for the rest. On a terminal it is
interactive: arrow keys move between dates, [ and ] change month, Enter opens
a date and then a commit, and ? lists every key. Redirected output is printed
once instead. --day lists one date in full. Place options before the folder.
Roots remembers which folders to scan, so activity and calendar need no folder
argument; giving one anyway overrides the list for that run. Repos assigns each
repository a group such as work or personal, or excludes it, and --group then
limits the calendar to one group. Identities remembers your author email
addresses across machines and work/personal accounts, and --mine then limits
either command to commits authored by one of them. All are saved in a
configuration file you can also edit by hand.
These commands read full histories; large repositories can take time. All
authors and merges are included. Quote paths containing spaces.
Use Ctrl+C to cancel.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run keeps command handling separate from process exit and terminal output.
func run(ctx context.Context, args []string, out, errOut io.Writer) int {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h")) {
		fmt.Fprint(out, usage)
		return 0
	}
	if args[0] == "activity" {
		return runActivity(ctx, args[1:], out, errOut)
	}
	if args[0] == "calendar" {
		return runCalendar(ctx, args[1:], out, errOut)
	}
	if args[0] == "roots" {
		return runRoots(args[1:], out, errOut)
	}
	if args[0] == "repos" {
		return runRepos(ctx, args[1:], out, errOut)
	}
	if args[0] == "identities" {
		return runIdentities(args[1:], out, errOut)
	}
	if len(args) != 2 || (args[0] != "scan" && args[0] != "history") {
		fmt.Fprint(errOut, usage)
		return 2
	}
	if args[0] == "history" {
		return runHistory(ctx, args[1], out, errOut)
	}

	result, err := scan(ctx, args[1])
	if err != nil {
		fmt.Fprintf(errOut, "Error: %v\n", err)
		return 1
	}
	for _, repo := range result.Repositories {
		fmt.Fprintln(out, repo)
	}
	fmt.Fprintf(out, "\nFound %d repositories.\n", len(result.Repositories))
	for _, warning := range result.Warnings {
		fmt.Fprintf(errOut, "Warning: %v\n", warning)
	}
	if len(result.Warnings) > 0 {
		fmt.Fprintln(errOut, "Scan incomplete: some entries could not be checked.")
		return 1
	}
	return 0
}

func runHistory(ctx context.Context, repo string, out, errOut io.Writer) int {
	commits, err := readHistory(ctx, repo)
	if err != nil {
		fmt.Fprintf(errOut, "Error: %v\n", err)
		return 1
	}
	if len(commits) == 0 {
		fmt.Fprintln(out, "No commits yet on the current branch.")
		return 0
	}
	fmt.Fprintf(out, "Showing %d commits from HEAD (limit %d). Author times in local timezone.\n\n", len(commits), historyLimit)
	for _, commit := range commits {
		fmt.Fprintf(out, "%s  %.12s  %s\n  %s <%s>\n\n",
			commit.AuthoredAt.In(time.Local).Format("2006-01-02 15:04 -07:00"),
			commit.Hash, displayText(commit.Subject),
			displayText(commit.AuthorName), displayText(commit.AuthorEmail))
	}
	return 0
}

// Preserve stored text, but keep control characters out of terminal output.
func displayText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
}
