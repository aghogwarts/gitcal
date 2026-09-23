package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

// scanResult separates useful results from recoverable failures.
type scanResult struct {
	Repositories []string
	Warnings     []error
}

// scanAll merges several roots into one result. A single unusable root is a
// warning while others still work; only losing every root is an error, which
// keeps the one-root case behaving exactly as it did before.
func scanAll(ctx context.Context, roots []string) (scanResult, error) {
	var combined scanResult
	var lastErr error
	succeeded := 0
	seen := map[string]bool{}
	for _, root := range roots {
		result, err := scan(ctx, root)
		if err != nil {
			if ctx.Err() != nil {
				return combined, err
			}
			lastErr = err
			combined.Warnings = append(combined.Warnings, err)
			continue
		}
		succeeded++
		combined.Warnings = append(combined.Warnings, result.Warnings...)
		// Overlapping roots would otherwise report a repository twice.
		for _, repository := range result.Repositories {
			if key := pathKey(repository); !seen[key] {
				seen[key] = true
				combined.Repositories = append(combined.Repositories, repository)
			}
		}
	}
	if succeeded == 0 {
		if lastErr != nil {
			return scanResult{}, lastErr
		}
		return combined, errors.New("no folders to scan; add one with: gitcal roots add <folder>")
	}
	sort.Strings(combined.Repositories)
	return combined, nil
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
	_, err := runGit(ctx, gitPath, repo, marker, "rev-parse", "--git-dir")
	if err != nil {
		return fmt.Errorf("Git could not validate repository: %w", err)
	}
	return nil
}
