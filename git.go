package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Both buffered commands and the streaming history reader use the same Git
// environment, so repository configuration cannot redirect their output.
func gitCommand(ctx context.Context, gitPath, repo, marker string, args ...string) *exec.Cmd {
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
	return cmd
}

// runGit captures small Git results. History for the calendar uses a pipe.
func runGit(ctx context.Context, gitPath, repo, marker string, args ...string) ([]byte, error) {
	cmd := gitCommand(ctx, gitPath, repo, marker, args...)
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
