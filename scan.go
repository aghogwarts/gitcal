package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// scanResult separates useful results from recoverable failures.
type scanResult struct {
	Repositories []string
	Warnings     []error
}

func scan(ctx context.Context, root string) (scanResult, error) {
	var result scanResult
	if err := ctx.Err(); err != nil {
		return result, err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return result, fmt.Errorf("resolve scan folder: %w", err)
	}
	// Resolve the chosen root, but do not follow links encountered below it.
	absRoot, err = filepath.EvalSymlinks(absRoot)
	if err != nil {
		return result, fmt.Errorf("open scan folder: %w", err)
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return result, fmt.Errorf("open scan folder: %w", err)
	}
	if !info.IsDir() || info.Name() == ".git" {
		return result, fmt.Errorf("choose a working folder, not a file or .git directory: %s", absRoot)
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return result, fmt.Errorf("Git is required; install Git and make it available on PATH: %w", err)
	}

	err = filepath.WalkDir(absRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			if path == absRoot {
				return walkErr
			}
			result.Warnings = append(result.Warnings, walkErr)
			return nil
		}
		if entry.Name() != ".git" {
			return nil
		}
		// A .git directory marks an ordinary checkout; a .git file can
		// point to metadata for a worktree or submodule. Let Git validate it.
		repo := filepath.Dir(path)
		if err := validateRepository(ctx, gitPath, repo, path); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			result.Warnings = append(result.Warnings, fmt.Errorf("%s: %w", repo, err))
		} else {
			result.Repositories = append(result.Repositories, repo)
		}
		if entry.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return result, fmt.Errorf("scan: %w", err)
	}
	sort.Strings(result.Repositories)
	return result, nil
}

func validateRepository(ctx context.Context, gitPath, repo, marker string) error {
	// Separate arguments preserve spaces and avoid invoking a shell.
	cmd := exec.CommandContext(ctx, gitPath, "--git-dir", marker, "--work-tree", repo, "rev-parse", "--git-dir")
	cmd.Dir = repo
	// A calling shell's Git overrides must not redirect this repository check.
	cmd.Env = []string{}
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(variable), "GIT_") {
			cmd.Env = append(cmd.Env, variable)
		}
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Git could not validate repository: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}
