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
  gitcal help

Scan a folder and its subfolders for Git working repositories.
History shows up to 20 commits reachable from HEAD, including all authors
and merge commits. Author timestamps are displayed in your local timezone.
Quote paths containing spaces. Use Ctrl+C to cancel.
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
