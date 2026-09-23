package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const historyLimit = 20

// Commit holds data independently of how the terminal or future calendar shows it.
type Commit struct {
	Hash        string
	AuthorName  string
	AuthorEmail string
	AuthoredAt  time.Time
	Subject     string
}

func readHistory(ctx context.Context, folder string) ([]Commit, error) {
	return readCommits(ctx, folder, historyLimit)
}

// A zero limit reads the full reachable history. Month activity needs this:
// taking only 20 commits could omit matching commits earlier in the month.
func readCommits(ctx context.Context, folder string, limit int) ([]Commit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	repo, err := filepath.Abs(folder)
	if err != nil {
		return nil, fmt.Errorf("resolve repository folder: %w", err)
	}
	repo, err = filepath.EvalSymlinks(repo)
	if err != nil {
		return nil, fmt.Errorf("open repository folder: %w", err)
	}
	marker := filepath.Join(repo, ".git")
	if _, err := os.Stat(marker); err != nil {
		return nil, fmt.Errorf("choose a repository root containing .git: %w", err)
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("Git is required; install Git and make it available on PATH: %w", err)
	}
	if err := validateRepository(ctx, gitPath, repo, marker); err != nil {
		return nil, err
	}
	head, err := resolveHead(ctx, gitPath, repo, marker)
	if err != nil || head == "" {
		return nil, err
	}
	// NUL separates fields; -z also terminates each record with NUL. Ordinary
	// spaces, punctuation, and Unicode in author names and subjects stay intact.
	args := []string{"log", "--no-color",
		"--no-decorate", "--no-show-signature", "--no-notes", "--no-patch",
		"--encoding=UTF-8", "-z", "--format=%H%x00%an%x00%ae%x00%aI%x00%s"}
	if limit > 0 {
		args = append(args, "--max-count="+strconv.Itoa(limit))
	}
	args = append(args, head, "--")
	output, err := runGit(ctx, gitPath, repo, marker, args...)
	if err != nil {
		return nil, err
	}
	return parseHistory(output)
}

// An unborn branch is a valid branch with no first commit. Only that case is
// treated as empty; broken objects or other Git failures remain errors.
func resolveHead(ctx context.Context, gitPath, repo, marker string) (string, error) {
	output, headErr := runGit(ctx, gitPath, repo, marker, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if headErr == nil {
		return strings.TrimSpace(string(output)), nil
	}
	if !hasExitCode(headErr, 1) {
		return "", headErr
	}
	ref, err := runGit(ctx, gitPath, repo, marker, "symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		return "", headErr
	}
	_, err = runGit(ctx, gitPath, repo, marker, "show-ref", "--verify", "--quiet", strings.TrimSpace(string(ref)))
	if hasExitCode(err, 1) {
		return "", nil
	}
	return "", headErr
}

func hasExitCode(err error, code int) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == code
}

func parseHistory(data []byte) ([]Commit, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if data[len(data)-1] != 0 {
		return nil, fmt.Errorf("incomplete Git history: missing record terminator")
	}
	fields := bytes.Split(data[:len(data)-1], []byte{0})
	if len(fields)%5 != 0 {
		return nil, fmt.Errorf("invalid Git history: expected five fields per commit, got %d fields", len(fields))
	}
	commits := make([]Commit, 0, len(fields)/5)
	for i := 0; i < len(fields); i += 5 {
		authoredAt, err := time.Parse(time.RFC3339, string(fields[i+3]))
		if err != nil {
			return nil, fmt.Errorf("parse author timestamp for commit %s: %w", fields[i], err)
		}
		commits = append(commits, Commit{
			Hash: string(fields[i]), AuthorName: string(fields[i+1]),
			AuthorEmail: string(fields[i+2]), AuthoredAt: authoredAt,
			Subject: string(fields[i+4]),
		})
	}
	return commits, nil
}
