package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func historyRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "project with spaces")
	git(t, "init", "--quiet", "--initial-branch=main", repo)
	return repo
}

func historyCommit(t *testing.T, repo, subject, authorDate, committerDate string) {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "-c", "commit.gpgsign=false",
		"-c", "core.hooksPath="+t.TempDir(), "commit", "--quiet", "--allow-empty",
		"--allow-empty-message", "-m", subject)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(value), "GIT_") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_AUTHOR_NAME=Ansh Goyal", "GIT_AUTHOR_EMAIL=ansh@example.invalid",
		"GIT_COMMITTER_NAME=Test Committer", "GIT_COMMITTER_EMAIL=committer@example.invalid",
		"GIT_AUTHOR_DATE="+authorDate, "GIT_COMMITTER_DATE="+committerDate)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create commit: %v\n%s", err, out)
	}
}

func TestHistoryFieldsLimitAndBranchScope(t *testing.T) {
	repo := historyRepo(t)
	for i := 0; i < 23; i++ {
		subject := fmt.Sprintf("Change %02d | café 🚀", i)
		date := fmt.Sprintf("2026-09-01T10:%02d:00+05:30", i)
		historyCommit(t, repo, subject, date, "2026-09-02T08:00:00Z")
	}
	git(t, "-C", repo, "checkout", "--quiet", "-b", "unmerged")
	historyCommit(t, repo, "Must not appear on main", "2026-09-03T10:00:00Z", "2026-09-03T10:00:00Z")
	git(t, "-C", repo, "checkout", "--quiet", "main")
	git(t, "-C", repo, "config", "color.ui", "always")
	git(t, "-C", repo, "config", "format.pretty", "oneline")
	git(t, "-C", repo, "config", "log.showSignature", "true")
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "wrong-repo"))
	commits, err := readHistory(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 20 {
		t.Fatalf("got %d commits; want 20", len(commits))
	}
	if commits[0].Subject != "Change 22 | café 🚀" || commits[19].Subject != "Change 03 | café 🚀" {
		t.Fatalf("unexpected history boundaries: %+v / %+v", commits[0], commits[19])
	}
	first := commits[0]
	if first.AuthorName != "Ansh Goyal" || first.AuthorEmail != "ansh@example.invalid" || len(first.Hash) != 40 {
		t.Fatalf("incorrect commit fields: %+v", first)
	}
	if first.AuthoredAt.Format(time.RFC3339) != "2026-09-01T10:22:00+05:30" {
		t.Fatalf("author timestamp/offset lost: %v", first.AuthoredAt)
	}
}

func TestHistoryEmptyBranchAndWorktree(t *testing.T) {
	repo := historyRepo(t)
	commits, err := readHistory(context.Background(), repo)
	if err != nil || len(commits) != 0 {
		t.Fatalf("new repository: %+v, %v", commits, err)
	}
	historyCommit(t, repo, "First", "2026-09-01T10:00:00Z", "2026-09-01T10:00:00Z")
	linked := filepath.Join(t.TempDir(), "linked worktree")
	git(t, "-C", repo, "worktree", "add", "--quiet", "--detach", linked)
	commits, err = readHistory(context.Background(), linked)
	if err != nil || len(commits) != 1 || commits[0].Subject != "First" {
		t.Fatalf("detached worktree: %+v, %v", commits, err)
	}
	git(t, "-C", repo, "checkout", "--quiet", "--orphan", "new-start")
	commits, err = readHistory(context.Background(), repo)
	if err != nil || len(commits) != 0 {
		t.Fatalf("unborn branch with other branches present: %+v, %v", commits, err)
	}
}

func TestHistoryIncludesMergeAndOtherAuthors(t *testing.T) {
	repo := historyRepo(t)
	historyCommit(t, repo, "Base", "2026-09-01T10:00:00Z", "2026-09-01T10:00:00Z")
	git(t, "-C", repo, "checkout", "--quiet", "-b", "feature")
	historyCommit(t, repo, "Feature", "2026-09-02T10:00:00Z", "2026-09-02T10:00:00Z")
	git(t, "-C", repo, "checkout", "--quiet", "main")
	git(t, "-C", repo, "-c", "user.name=Other Author", "-c", "user.email=other@example.invalid",
		"-c", "commit.gpgsign=false", "-c", "core.hooksPath="+t.TempDir(),
		"merge", "--quiet", "--no-ff", "-m", "Merge feature", "feature")
	commits, err := readHistory(context.Background(), repo)
	if err != nil || len(commits) != 3 {
		t.Fatalf("merge history: %+v, %v", commits, err)
	}
	if commits[0].Subject != "Merge feature" || commits[0].AuthorEmail != "other@example.invalid" {
		t.Fatalf("merge or other author excluded: %+v", commits[0])
	}
}

func TestHistoryRejectsInvalidInputsAndBrokenObjects(t *testing.T) {
	root := t.TempDir()
	if _, err := readHistory(context.Background(), root); err == nil {
		t.Fatal("expected non-repository error")
	}
	if _, err := readHistory(context.Background(), filepath.Join(root, "missing")); err == nil {
		t.Fatal("expected missing-path error")
	}
	repo := historyRepo(t)
	child := filepath.Join(repo, "child")
	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := readHistory(context.Background(), child); err == nil {
		t.Fatal("must not silently use a parent repository")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readHistory(ctx, repo); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	// A ref pointing to a missing object must not masquerade as an empty branch.
	if err := os.WriteFile(filepath.Join(repo, ".git", "refs", "heads", "main"), []byte(strings.Repeat("a", 40)+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readHistory(context.Background(), repo); err == nil {
		t.Fatal("expected broken-object error")
	}
}

func TestParseHistoryRejectsMalformedOutput(t *testing.T) {
	for _, data := range []string{
		"unterminated", "one\x00two\x00",
		"hash\x00name\x00email\x00not-a-date\x00subject\x00",
	} {
		if _, err := parseHistory([]byte(data)); err == nil {
			t.Fatalf("expected parse error for %q", data)
		}
	}
	data := "hash\x00Name With Spaces\x00email\x002026-09-01T23:30:00-07:00\x00\x00"
	commits, err := parseHistory([]byte(data))
	if err != nil || len(commits) != 1 || commits[0].Subject != "" {
		t.Fatalf("empty subject was not preserved: %+v, %v", commits, err)
	}
}

func TestHistoryCommandAndLocalTime(t *testing.T) {
	repo := historyRepo(t)
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"history", repo}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "No commits yet") {
		t.Fatalf("empty command: exit %d; stdout %s; stderr %s", code, &out, &errOut)
	}
	historyCommit(t, repo, "Fix café | details\n\nBody should not appear", "2026-09-01T23:30:00-07:00", "2026-09-03T10:00:00Z")
	previousLocal := time.Local
	time.Local = time.FixedZone("test local", 5*3600+30*60)
	t.Cleanup(func() { time.Local = previousLocal })
	out.Reset()
	errOut.Reset()
	if code := run(context.Background(), []string{"history", repo}, &out, &errOut); code != 0 {
		t.Fatalf("history command: exit %d; %s", code, &errOut)
	}
	for _, want := range []string{"2026-09-02 12:00 +05:30", "Fix café | details", "Ansh Goyal <ansh@example.invalid>"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in %s", want, &out)
		}
	}
	if strings.Contains(out.String(), "Body should not appear") || errOut.Len() != 0 {
		t.Fatalf("unexpected command output: %s / %s", &out, &errOut)
	}
	if got := displayText("fix\x1b[31m\ttext\n"); strings.ContainsAny(got, "\x1b\t\n") {
		t.Fatalf("terminal controls survived: %q", got)
	}
	for _, args := range [][]string{{"history"}, {"history", repo, "extra"}} {
		if code := run(context.Background(), args, &out, &errOut); code != 2 {
			t.Fatalf("bad arguments: exit %d", code)
		}
	}
	if code := run(context.Background(), []string{"history", t.TempDir()}, &out, &errOut); code != 1 {
		t.Fatalf("invalid repository: exit %d", code)
	}
}

func TestHistoryDoesNotModifyRepository(t *testing.T) {
	repo := historyRepo(t)
	historyCommit(t, repo, "Original", "2026-09-01T10:00:00Z", "2026-09-01T10:00:00Z")
	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("keep me"), 0644); err != nil {
		t.Fatal(err)
	}
	snapshot := func() []byte {
		t.Helper()
		out, err := exec.Command("git", "-C", repo, "status", "--porcelain=v1", "--branch").Output()
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	before := snapshot()
	if _, err := readHistory(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	if after := snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("repository status changed: %s -> %s", before, after)
	}
}
