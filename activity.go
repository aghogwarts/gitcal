package main

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// One commit can occur in several checkouts. Keep every source path while
// counting the commit once in the combined activity view.
type ActivityEntry struct {
	Commit       Commit
	Repositories []string
	Groups       []string // Groups of the source repositories, sorted and unique.
}

type ActivityDay struct {
	Date    string // YYYY-MM-DD in the activity month's timezone.
	Entries []ActivityEntry
}

type Activity struct {
	Month                  time.Time
	DiscoveredRepositories int
	SelectedRepositories   int
	ReadRepositories       int
	Days                   []ActivityDay
	Warnings               []error
}

func collectActivity(ctx context.Context, roots []string, month time.Time, filter repositoryFilter) (Activity, error) {
	start := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, month.Location())
	end := start.AddDate(0, 1, 0)
	result := Activity{Month: start}
	scanned, err := scanAll(ctx, roots)
	if err != nil {
		return result, err
	}
	result.DiscoveredRepositories = len(scanned.Repositories)
	result.Warnings = append(result.Warnings, scanned.Warnings...)

	// Filtering before reading means an excluded repository costs nothing.
	selected := make([]string, 0, len(scanned.Repositories))
	for _, repo := range scanned.Repositories {
		if filter.includes(repo) {
			selected = append(selected, repo)
		}
	}
	result.SelectedRepositories = len(selected)

	byHash := make(map[string]*ActivityEntry)
	for _, repo := range selected {
		commits, err := readCommits(ctx, repo, 0)
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Errorf("%s: %w", repo, err))
			continue
		}
		result.ReadRepositories++
		for _, commit := range commits {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			// Compare instants against local month boundaries, never date strings
			// in the commit's original timezone or a 20-commit sample.
			if commit.AuthoredAt.Before(start) || !commit.AuthoredAt.Before(end) {
				continue
			}
			if existing, ok := byHash[commit.Hash]; ok {
				existing.Repositories = append(existing.Repositories, repo)
			} else {
				byHash[commit.Hash] = &ActivityEntry{Commit: commit, Repositories: []string{repo}}
			}
		}
	}

	byDay := make(map[string][]ActivityEntry)
	for _, entry := range byHash {
		sort.Strings(entry.Repositories)
		entry.Groups = groupsOf(entry.Repositories, filter)
		date := entry.Commit.AuthoredAt.In(start.Location()).Format("2006-01-02")
		byDay[date] = append(byDay[date], *entry)
	}
	for date, entries := range byDay {
		sort.Slice(entries, func(i, j int) bool {
			left, right := entries[i].Commit, entries[j].Commit
			if left.AuthoredAt.Equal(right.AuthoredAt) {
				return left.Hash < right.Hash
			}
			return left.AuthoredAt.Before(right.AuthoredAt)
		})
		result.Days = append(result.Days, ActivityDay{Date: date, Entries: entries})
	}
	sort.Slice(result.Days, func(i, j int) bool { return result.Days[i].Date < result.Days[j].Date })
	return result, nil
}

// A deduplicated commit can come from clones sitting in different groups, so
// its groups are a set rather than a single value.
func groupsOf(repositories []string, filter repositoryFilter) []string {
	seen := map[string]bool{}
	var groups []string
	for _, repository := range repositories {
		if group := filter.groupOf(repository); !seen[group] {
			seen[group] = true
			groups = append(groups, group)
		}
	}
	sort.Strings(groups)
	return groups
}
