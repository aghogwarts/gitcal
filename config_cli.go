package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/term"
)

// selection is what a command needs in order to know which repositories count:
// where to look, and which of the results to keep.
type selection struct {
	roots   []string
	filter  repositoryFilter
	authors authorFilter
	group   string
	mine    bool
	config  config
	path    string
}

// resolveSelection reconciles an explicit folder with the saved configuration.
// A folder given on the command line wins for that run and ignores grouping,
// which keeps the original one-off behaviour intact. Identities, unlike
// grouping, are not tied to any folder, so --mine applies the same way
// whether or not a folder was given.
func resolveSelection(folder, group string, mine bool, in io.Reader, out, errOut io.Writer) (selection, bool) {
	path, err := defaultConfigPath()
	if err != nil {
		fmt.Fprintf(errOut, "Error: %v\n", err)
		return selection{}, false
	}
	saved, err := loadConfig(path)
	if err != nil {
		fmt.Fprintf(errOut, "Error: %v\n", err)
		return selection{}, false
	}
	if mine && len(saved.Identities) == 0 {
		fmt.Fprintln(errOut, "Error: no identities configured. Add yours with:")
		fmt.Fprintln(errOut, "  gitcal identities add you@example.com")
		return selection{}, false
	}
	chosen := selection{config: saved, path: path, group: group, mine: mine}

	if folder != "" {
		chosen.roots = []string{folder}
		chosen.filter = repositoryFilter{only: group}
		if group != "" {
			// Grouping lives in the configuration, which this run bypassed.
			chosen.filter = saved.filter(group)
		}
		chosen.authors = saved.authors(mine)
		return chosen, true
	}

	if len(saved.Roots) == 0 {
		added, ok := promptForRoot(path, &saved, in, out, errOut)
		if !ok {
			return selection{}, false
		}
		saved = added
		chosen.config = saved
	}
	chosen.roots = saved.Roots
	chosen.filter = saved.filter(group)
	chosen.authors = saved.authors(mine)
	return chosen, true
}

// promptForRoot makes a first run friendly instead of failing with an error.
// Without a terminal to ask, it explains the command to run.
func promptForRoot(path string, saved *config, in io.Reader, out, errOut io.Writer) (config, bool) {
	if !interactiveInput(in) {
		fmt.Fprintln(errOut, "Error: no folders configured. Add one with:")
		fmt.Fprintln(errOut, "  gitcal roots add \"path/to/your/projects\"")
		fmt.Fprintln(errOut, "Or pass a folder directly for a single run.")
		return *saved, false
	}
	fmt.Fprintln(out, "gitcal has no folders to scan yet.")
	fmt.Fprintln(out, "Enter a folder containing your repositories (blank to cancel):")
	fmt.Fprint(out, "> ")

	reader := bufio.NewReader(in)
	line, _ := reader.ReadString('\n')
	folder := strings.Trim(strings.TrimSpace(line), `"`)
	if folder == "" {
		fmt.Fprintln(errOut, "Error: no folder given.")
		return *saved, false
	}
	info, err := os.Stat(folder)
	if err != nil || !info.IsDir() {
		fmt.Fprintf(errOut, "Error: %s is not a folder you can read.\n", folder)
		return *saved, false
	}
	saved.addRoot(folder)
	if err := saveConfig(path, *saved); err != nil {
		fmt.Fprintf(errOut, "Error: %v\n", err)
		return *saved, false
	}
	fmt.Fprintf(out, "Saved to %s. Change it later with: gitcal roots add|remove\n\n", path)
	return *saved, true
}

func interactiveInput(in io.Reader) bool {
	file, ok := in.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

func runRoots(args []string, out, errOut io.Writer) int {
	path, err := defaultConfigPath()
	if err != nil {
		fmt.Fprintf(errOut, "Error: %v\n", err)
		return 1
	}
	saved, err := loadConfig(path)
	if err != nil {
		fmt.Fprintf(errOut, "Error: %v\n", err)
		return 1
	}

	if len(args) == 0 || args[0] == "list" {
		if len(saved.Roots) == 0 {
			fmt.Fprintln(out, "No folders configured. Add one with: gitcal roots add <folder>")
			return 0
		}
		for _, root := range saved.Roots {
			fmt.Fprintln(out, root)
		}
		fmt.Fprintf(out, "\n%s, from %s\n", count(len(saved.Roots), "folder", "folders"), path)
		return 0
	}
	if len(args) != 2 || (args[0] != "add" && args[0] != "remove") {
		fmt.Fprintln(errOut, "Usage: gitcal roots [list | add <folder> | remove <folder>]")
		return 2
	}

	if args[0] == "add" {
		info, err := os.Stat(args[1])
		if err != nil || !info.IsDir() {
			fmt.Fprintf(errOut, "Error: %s is not a folder you can read.\n", args[1])
			return 1
		}
		if !saved.addRoot(args[1]) {
			fmt.Fprintln(out, "That folder is already configured.")
			return 0
		}
	} else if !saved.removeRoot(args[1]) {
		fmt.Fprintf(errOut, "Error: %s is not one of the configured folders.\n", args[1])
		return 1
	}

	if err := saveConfig(path, saved); err != nil {
		fmt.Fprintf(errOut, "Error: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "%s. %s configured.\n", map[string]string{
		"add": "Added", "remove": "Removed"}[args[0]], count(len(saved.Roots), "folder", "folders"))
	return 0
}

// count avoids the "1 folders" that plain formatting produces. The plural is
// spelled out because English will not derive "repositories" on its own.
func count(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// runIdentities mirrors runRoots: list/add/remove over one plain list, with
// no filesystem check, since an email address has nothing local to validate.
func runIdentities(args []string, out, errOut io.Writer) int {
	path, err := defaultConfigPath()
	if err != nil {
		fmt.Fprintf(errOut, "Error: %v\n", err)
		return 1
	}
	saved, err := loadConfig(path)
	if err != nil {
		fmt.Fprintf(errOut, "Error: %v\n", err)
		return 1
	}

	if len(args) == 0 || args[0] == "list" {
		if len(saved.Identities) == 0 {
			fmt.Fprintln(out, "No identities configured. Add yours with: gitcal identities add you@example.com")
			return 0
		}
		for _, email := range saved.Identities {
			fmt.Fprintln(out, email)
		}
		fmt.Fprintf(out, "\n%s, from %s\n", count(len(saved.Identities), "identity", "identities"), path)
		return 0
	}
	if len(args) != 2 || (args[0] != "add" && args[0] != "remove") {
		fmt.Fprintln(errOut, "Usage: gitcal identities [list | add <email> | remove <email>]")
		return 2
	}

	if args[0] == "add" {
		if strings.TrimSpace(args[1]) == "" {
			fmt.Fprintln(errOut, "Error: an identity needs an email address.")
			return 1
		}
		if !saved.addIdentity(args[1]) {
			fmt.Fprintln(out, "That identity is already configured.")
			return 0
		}
	} else if !saved.removeIdentity(args[1]) {
		fmt.Fprintf(errOut, "Error: %s is not one of the configured identities.\n", args[1])
		return 1
	}

	if err := saveConfig(path, saved); err != nil {
		fmt.Fprintf(errOut, "Error: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "%s. %s configured.\n", map[string]string{
		"add": "Added", "remove": "Removed"}[args[0]], count(len(saved.Identities), "identity", "identities"))
	return 0
}

func runRepos(ctx context.Context, args []string, out, errOut io.Writer) int {
	path, err := defaultConfigPath()
	if err != nil {
		fmt.Fprintf(errOut, "Error: %v\n", err)
		return 1
	}
	saved, err := loadConfig(path)
	if err != nil {
		fmt.Fprintf(errOut, "Error: %v\n", err)
		return 1
	}

	if len(args) == 0 || args[0] == "list" {
		return listRepositories(ctx, saved, out, errOut)
	}
	switch args[0] {
	case "set":
		if len(args) != 3 {
			fmt.Fprintln(errOut, `Usage: gitcal repos set <repository> <group>   (group "-" clears it)`)
			return 2
		}
		group := args[2]
		if group == "-" {
			group = ""
		}
		saved.setGroup(args[1], group)
	case "exclude":
		if len(args) != 2 {
			fmt.Fprintln(errOut, "Usage: gitcal repos exclude <repository>")
			return 2
		}
		if !saved.exclude(args[1]) {
			fmt.Fprintln(out, "That repository is already excluded.")
			return 0
		}
	case "include":
		if len(args) != 2 {
			fmt.Fprintln(errOut, "Usage: gitcal repos include <repository>")
			return 2
		}
		if !saved.include(args[1]) {
			fmt.Fprintf(errOut, "Error: %s is not excluded.\n", args[1])
			return 1
		}
	default:
		fmt.Fprintln(errOut, "Usage: gitcal repos [list | set <repository> <group> | exclude <repository> | include <repository>]")
		return 2
	}

	if err := saveConfig(path, saved); err != nil {
		fmt.Fprintf(errOut, "Error: %v\n", err)
		return 1
	}
	fmt.Fprintln(out, "Saved.")
	return 0
}

func listRepositories(ctx context.Context, saved config, out, errOut io.Writer) int {
	if len(saved.Roots) == 0 {
		fmt.Fprintln(out, "No folders configured. Add one with: gitcal roots add <folder>")
		return 0
	}
	scanned, err := scanAll(ctx, saved.Roots)
	if err != nil {
		fmt.Fprintf(errOut, "Error: %v\n", err)
		return 1
	}
	filter := saved.filter("")

	width := 0
	for _, repository := range scanned.Repositories {
		if name := filepath.Base(repository); len(name) > width {
			width = len(name)
		}
	}
	counts := map[string]int{}
	states := make([]string, 0, 4)
	for _, repository := range scanned.Repositories {
		state := filter.groupOf(repository)
		if !filter.includes(repository) {
			state = "excluded"
		}
		if counts[state] == 0 {
			states = append(states, state)
		}
		counts[state]++
		fmt.Fprintf(out, "%-*s  %-10s  %s\n", width, filepath.Base(repository), state, repository)
	}

	sort.Strings(states)
	summary := make([]string, 0, len(states))
	for _, state := range states {
		summary = append(summary, fmt.Sprintf("%d %s", counts[state], state))
	}
	fmt.Fprintf(out, "\n%s: %s\n", count(len(scanned.Repositories), "repository", "repositories"),
		strings.Join(summary, ", "))
	fmt.Fprintln(out, "Assign with: gitcal repos set <repository> <group>")

	for _, warning := range scanned.Warnings {
		fmt.Fprintf(errOut, "Warning: %v\n", warning)
	}
	if len(scanned.Warnings) > 0 {
		return 1
	}
	return 0
}
