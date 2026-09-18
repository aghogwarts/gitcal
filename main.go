package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
)

const usage = `Usage:
  gitcal scan <folder>
  gitcal help

Scan a folder and its subfolders for Git working repositories.
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
	if len(args) != 2 || args[0] != "scan" {
		fmt.Fprint(errOut, usage)
		return 2
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
