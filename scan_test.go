package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func git(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func TestScanRealRepositories(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "project with spaces")
	nested := filepath.Join(parent, "nested")
	linked := filepath.Join(root, "linked worktree")
	git(t, "init", "--quiet", parent)
	git(t, "-C", parent, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "--quiet", "--allow-empty", "-m", "Initial")
	git(t, "init", "--quiet", nested)
	git(t, "-C", parent, "worktree", "add", "--quiet", "--detach", linked)
	// Metadata must never be traversed, even if it contains a repository.
	git(t, "init", "--quiet", filepath.Join(parent, ".git", "hidden-repo"))
	// An empty directory named .git must not be mistaken for a repository.
	if err := os.MkdirAll(filepath.Join(root, "invalid", ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	// Simulate running from a shell with a repository override.
	t.Setenv("GIT_DIR", filepath.Join(root, "nonexistent"))
	result, err := scan(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{parent, nested, linked}
	for i := range want {
		want[i], err = filepath.EvalSymlinks(want[i])
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(want)
	if !reflect.DeepEqual(result.Repositories, want) {
		t.Fatalf("repositories = %v; want %v", result.Repositories, want)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("warnings = %v; want one invalid-repository warning", result.Warnings)
	}
}

func TestScanErrorsAndEmptyFolder(t *testing.T) {
	root := t.TempDir()
	result, err := scan(context.Background(), root)
	if err != nil || len(result.Repositories) != 0 || len(result.Warnings) != 0 {
		t.Fatalf("empty scan = %+v, %v", result, err)
	}
	if _, err := scan(context.Background(), filepath.Join(root, "missing")); err == nil {
		t.Fatal("expected error for missing root")
	}
	file := filepath.Join(root, "file.txt")
	if err := os.WriteFile(file, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := scan(context.Background(), file); err == nil {
		t.Fatal("expected error for file root")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := scan(ctx, root); err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestScanDoesNotFollowDirectoryLinks(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	git(t, "init", "--quiet", external)
	if err := os.Symlink(external, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	result, err := scan(context.Background(), root)
	if err != nil || len(result.Repositories) != 0 {
		t.Fatalf("scan followed directory symlink: %+v, %v", result, err)
	}
}

func TestCommandErrors(t *testing.T) {
	for _, args := range [][]string{{"scan"}, {"unknown"}, {"scan", "a", "b"}} {
		var out, errOut bytes.Buffer
		if code := run(context.Background(), args, &out, &errOut); code != 2 {
			t.Fatalf("args %v: exit = %d; want 2", args, code)
		}
	}
}
