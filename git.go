package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// runGit is shared by discovery and history. Git's stdout is data; stderr is
// diagnostic text and must never become part of the data we parse.
func runGit(ctx context.Context, gitPath, repo, marker string, args ...string) ([]byte, error) {
	commandArgs := []string{"--no-pager", "--git-dir", marker, "--work-tree", repo}
	commandArgs = append(commandArgs, args...)
	cmd := exec.CommandContext(ctx, gitPath, commandArgs...)
	cmd.Dir = repo
	cmd.Env = []string{}
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(variable), "GIT_") {
			cmd.Env = append(cmd.Env, variable)
		}
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("git %s: %w (%s)", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return output, nil
}
