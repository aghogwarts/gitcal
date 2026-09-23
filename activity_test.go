package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func activityRepo(t *testing.T, root, name string) string {
	t.Helper()
	repo := filepath.Join(root, name)
	git(t, "init", "--quiet", "--initial-branch=main", repo)
	return repo
}

func TestActivityCombinesCompleteMonthAndDeduplicates(t *testing.T) {
	root := t.TempDir()
	first := activityRepo(t, root, filepath.Join("work", "project"))
	second := activityRepo(t, root, filepath.Join("personal", "project"))
	activityRepo(t, root, "empty")
	historyCommit(t, first, "Earlier matching commit", "2026-09-02T10:00:00+05:30", "2026-10-10T10:00:00Z")
	// Place the matching commit beyond the history command's 20-item sample.
	for i := 0; i < 22; i++ {
		historyCommit(t, first, fmt.Sprintf("Outside month %02d", i), "2026-10-15T10:00:00Z", "2026-11-01T10:00:00Z")
	}
	git(t, "-C", first, "checkout", "--quiet", "-b", "unmerged")
	historyCommit(t, first, "Unmerged must stay excluded", "2026-09-04T10:00:00Z", "2026-11-02T10:00:00Z")
	git(t, "-C", first, "checkout", "--quiet", "main")
	clone := filepath.Join(root, "another clone")
	git(t, "clone", "--quiet", "--no-hardlinks", first, clone)
	// Different author order from log order, and a timezone-induced day change.
	historyCommit(t, second, "Later day", "2026-09-03T15:00:00+05:30", "2026-11-03T10:00:00Z")
	historyCommit(t, second, "Earlier day after conversion", "2026-09-01T23:30:00-07:00", "2026-11-04T10:00:00Z")
	month := time.Date(2026, 9, 1, 0, 0, 0, 0, time.FixedZone("test local", 19800))
	result, err := collectActivity(context.Background(), []string{root}, month, repositoryFilter{}, authorFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if result.DiscoveredRepositories != 4 || result.ReadRepositories != 4 || len(result.Warnings) != 0 {
		t.Fatalf("repository accounting: %+v", result)
	}
	if len(result.Days) != 2 || result.Days[0].Date != "2026-09-02" || result.Days[1].Date != "2026-09-03" {
		t.Fatalf("date grouping: %+v", result.Days)
	}
	entries := result.Days[0].Entries
	if len(entries) != 2 || len(result.Days[1].Entries) != 1 || entries[0].Commit.Subject != "Earlier matching commit" || entries[1].Commit.Subject != "Earlier day after conversion" {
		t.Fatalf("filtering/deduplication/order: %+v", result.Days)
	}
	firstCanonical, err := filepath.EvalSymlinks(first)
	if err != nil {
		t.Fatal(err)
	}
	cloneCanonical, err := filepath.EvalSymlinks(clone)
	if err != nil {
		t.Fatal(err)
	}
	wantSources := []string{firstCanonical, cloneCanonical}
	sort.Strings(wantSources)
	if !reflect.DeepEqual(entries[0].Repositories, wantSources) {
		t.Fatalf("lost duplicate source paths: %v; want %v", entries[0].Repositories, wantSources)
	}
	preview, err := readHistory(context.Background(), first)
	if err != nil || len(preview) != 20 {
		t.Fatalf("history preview changed: %d commits, %v", len(preview), err)
	}
	for _, commit := range preview {
		if commit.Subject == "Earlier matching commit" {
			t.Fatal("fixture did not put the matching commit beyond the preview limit")
		}
	}
}

func TestActivityTimingsDoNotChangeTheAgenda(t *testing.T) {
	root := t.TempDir()
	repo := activityRepo(t, root, "project")
	historyCommit(t, repo, "Timed commit", "2026-09-03T10:00:00Z", "2026-09-03T10:00:00Z")
	var ordinaryOut, ordinaryErr bytes.Buffer
	if code := run(context.Background(), []string{"activity", "--month", "2026-09", root}, &ordinaryOut, &ordinaryErr); code != 0 || ordinaryErr.Len() != 0 {
		t.Fatalf("ordinary activity failed: exit %d, %s", code, &ordinaryErr)
	}
	var timedOut, timedErr bytes.Buffer
	if code := run(context.Background(), []string{"activity", "--timings", "--month", "2026-09", root}, &timedOut, &timedErr); code != 0 {
		t.Fatalf("timed activity failed: exit %d, %s", code, &timedErr)
	}
	if timedOut.String() != ordinaryOut.String() {
		t.Fatal("timing changed the agenda printed on stdout")
	}
	for _, phase := range []string{"Discovery:", "Git history:", "Other:", "Total:"} {
		if !strings.Contains(timedErr.String(), phase) {
			t.Fatalf("missing %q timing on stderr: %s", phase, &timedErr)
		}
	}
}

func TestActivityMonthBoundariesAndStableTies(t *testing.T) {
	root := t.TempDir()
	repo := activityRepo(t, root, "boundaries")
	// Local September is [Aug 31 18:30Z, Sep 30 18:30Z) at UTC+05:30.
	for _, fixture := range []struct{ subject, date string }{
		{"Before start", "2026-08-31T18:29:59Z"},
		{"At start A", "2026-08-31T18:30:00Z"},
		{"At start B", "2026-08-31T18:30:00Z"},
		{"Before end", "2026-09-30T18:29:59Z"},
		{"At end", "2026-09-30T18:30:00Z"},
	} {
		historyCommit(t, repo, fixture.subject, fixture.date, "2026-11-01T10:00:00Z")
	}
	month := time.Date(2026, 9, 1, 0, 0, 0, 0, time.FixedZone("test local", 19800))
	result, err := collectActivity(context.Background(), []string{root}, month, repositoryFilter{}, authorFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Days) != 2 || result.Days[0].Date != "2026-09-01" || result.Days[1].Date != "2026-09-30" {
		t.Fatalf("month boundaries: %+v", result.Days)
	}
	first, last := result.Days[0].Entries, result.Days[1].Entries
	if len(first) != 2 || first[0].Commit.Hash >= first[1].Commit.Hash || len(last) != 1 || last[0].Commit.Subject != "Before end" {
		t.Fatalf("boundary counts/tie ordering: %+v", result.Days)
	}
}

func TestActivityUsesCalendarMonthsAcrossLeapYearAndDST(t *testing.T) {
	// Go's time package reads this fixture's IANA timezone from the toolchain's
	// zoneinfo, including on Windows. No dependency on the host's local timezone.
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct{ month, date, wantDay string }{
		{"2024-02", "2024-03-01T04:59:59Z", "2024-02-29"},
		{"2026-03", "2026-04-01T03:59:59Z", "2026-03-31"},
	} {
		t.Run(fixture.month, func(t *testing.T) {
			root := t.TempDir()
			repo := activityRepo(t, root, "project")
			historyCommit(t, repo, "Last second", fixture.date, fixture.date)
			month, err := time.ParseInLocation("2006-01", fixture.month, location)
			if err != nil {
				t.Fatal(err)
			}
			result, err := collectActivity(context.Background(), []string{root}, month, repositoryFilter{}, authorFilter{})
			if err != nil || len(result.Days) != 1 || result.Days[0].Date != fixture.wantDay {
				t.Fatalf("calendar month calculation: %+v, %v", result.Days, err)
			}
		})
	}
}

func TestActivityPartialFailuresRemainVisible(t *testing.T) {
	root := t.TempDir()
	good := activityRepo(t, root, "good")
	historyCommit(t, good, "Keep this result", "2026-09-03T10:00:00Z", "2026-09-03T10:00:00Z")
	bad := activityRepo(t, root, "broken-object")
	if err := os.WriteFile(filepath.Join(bad, ".git", "refs", "heads", "main"), []byte(strings.Repeat("a", 40)+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "invalid-marker", ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	month := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	result, err := collectActivity(context.Background(), []string{root}, month, repositoryFilter{}, authorFilter{})
	if err != nil || result.DiscoveredRepositories != 2 || result.ReadRepositories != 1 || len(result.Warnings) != 2 || len(result.Days) != 1 {
		t.Fatalf("partial activity: %+v, %v", result, err)
	}
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"activity", "--month", "2026-09", root}, &out, &errOut)
	if code != 1 || !strings.Contains(out.String(), "Keep this result") || !strings.Contains(errOut.String(), "Activity incomplete") {
		t.Fatalf("partial CLI: exit %d, stdout %s, stderr %s", code, &out, &errOut)
	}
	out.Reset()
	errOut.Reset()
	code = run(context.Background(), []string{"activity", "--timings", "--month", "2026-09", root}, &out, &errOut)
	if code != 1 || !strings.Contains(out.String(), "Keep this result") || !strings.Contains(errOut.String(), "Timings (activity collection)") || !strings.Contains(errOut.String(), "Activity incomplete") {
		t.Fatalf("timed partial CLI: exit %d, stdout %s, stderr %s", code, &out, &errOut)
	}
}

func TestActivityEmptyMissingCancelledAndCLIArguments(t *testing.T) {
	root := t.TempDir()
	month := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	result, err := collectActivity(context.Background(), []string{root}, month, repositoryFilter{}, authorFilter{})
	if err != nil || len(result.Days) != 0 || result.DiscoveredRepositories != 0 {
		t.Fatalf("empty root: %+v, %v", result, err)
	}
	activityRepo(t, root, "empty-repository")
	result, err = collectActivity(context.Background(), []string{root}, month, repositoryFilter{}, authorFilter{})
	if err != nil || len(result.Days) != 0 || result.ReadRepositories != 1 {
		t.Fatalf("empty repository: %+v, %v", result, err)
	}
	if _, err := collectActivity(context.Background(), []string{filepath.Join(root, "missing")}, month, repositoryFilter{}, authorFilter{}); err == nil {
		t.Fatal("expected missing root error")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := collectActivity(ctx, []string{root}, month, repositoryFilter{}, authorFilter{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	for _, args := range [][]string{
		{"activity", "--month", "2026-13", root},
		{"activity", "--month", "2026-9", root}, {"activity", "--month", "0000-01", root},
		{"activity", "--month", "invalid", root}, {"activity", "--unknown", root},
		{"activity", root, "--month", "2026-09"}, {"activity", root, root},
	} {
		var out, errOut bytes.Buffer
		if code := run(context.Background(), args, &out, &errOut); code != 2 {
			t.Fatalf("args %v: exit %d; want 2", args, code)
		}
	}
	// Without a folder and without configured roots, the run explains itself
	// rather than treating it as a usage mistake.
	var noRoots bytes.Buffer
	if code := run(context.Background(), []string{"activity"}, &noRoots, &noRoots); code != 1 {
		t.Fatalf("unconfigured activity: exit %d; want 1", code)
	}
	if !strings.Contains(noRoots.String(), "gitcal roots add") {
		t.Fatalf("unconfigured activity did not suggest roots add: %s", &noRoots)
	}
	for _, args := range [][]string{{"activity", root}, {"activity", "--month", "2026-09", root}, {"activity", "--help"}} {
		var out, errOut bytes.Buffer
		if code := run(context.Background(), args, &out, &errOut); code != 0 {
			t.Fatalf("args %v: exit %d; stdout %s; stderr %s", args, code, &out, &errOut)
		}
	}
}
