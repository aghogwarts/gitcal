package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain points every test at a configuration that does not exist, so no
// test can read or overwrite the one belonging to whoever runs them. Tests
// that need to write override the variable with their own temporary path.
func TestMain(m *testing.M) {
	directory, err := os.MkdirTemp("", "gitcal-tests")
	if err != nil {
		panic(err)
	}
	os.Setenv(configPathVariable, filepath.Join(directory, "absent.toml"))
	code := m.Run()
	os.RemoveAll(directory)
	os.Exit(code)
}

func tempConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv(configPathVariable, path)
	return path
}

func TestConfigSurvivesASaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")

	empty, err := loadConfig(path)
	if err != nil {
		t.Fatalf("missing configuration should not be an error: %v", err)
	}
	if len(empty.Roots) != 0 || len(empty.Groups) != 0 {
		t.Fatalf("missing configuration was not empty: %+v", empty)
	}

	original := config{
		Roots:    []string{`C:\Users\someone\projects`, "/home/someone/src"},
		Excluded: []string{`C:\Users\someone\projects\vendored`},
		Groups: map[string]string{
			`C:\Users\someone\projects\api`: "work",
			"/home/someone/src/blog":        "personal",
		},
	}
	if err := saveConfig(path, original); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Backslashes are the reason for choosing TOML literal strings; a path that
	// came back with them doubled or eaten would break every later lookup.
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if strings.Contains(string(written), `\\`) {
		t.Fatalf("Windows paths were escaped on the way out:\n%s", written)
	}
	if !strings.HasPrefix(string(written), "# gitcal configuration.") {
		t.Fatalf("the explanatory header is missing:\n%s", written)
	}

	reloaded, err := loadConfig(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	for _, root := range original.Roots {
		if !containsPath(reloaded.Roots, root) {
			t.Fatalf("root %q did not survive the round trip: %+v", root, reloaded.Roots)
		}
	}
	for repository, group := range original.Groups {
		if reloaded.Groups[repository] != group {
			t.Fatalf("group for %q became %q", repository, reloaded.Groups[repository])
		}
	}

	if err := os.WriteFile(path, []byte("roots = [unterminated"), 0o644); err != nil {
		t.Fatalf("write invalid: %v", err)
	}
	if _, err := loadConfig(path); err == nil || !strings.Contains(err.Error(), "not valid TOML") {
		t.Fatalf("invalid TOML should name the problem, got %v", err)
	}
}

func TestPathsMatchDespiteSeparatorsAndCase(t *testing.T) {
	root := t.TempDir()
	repository := filepath.Join(root, "Api")
	if err := os.Mkdir(repository, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	var saved config
	saved.setGroup(repository, "work")

	// A trailing separator and a redundant path element mean the same folder.
	awkward := filepath.Join(repository, "nested", "..") + string(filepath.Separator)
	if got := saved.filter("").groupOf(awkward); got != "work" {
		t.Fatalf("group of %q was %q; want work", awkward, got)
	}
	if !saved.filter("work").includes(repository) {
		t.Fatalf("the work filter excluded the only work repository")
	}

	// Reassigning must replace the group rather than leave two entries behind.
	saved.setGroup(awkward, "personal")
	if len(saved.Groups) != 1 {
		t.Fatalf("reassignment left %d entries: %+v", len(saved.Groups), saved.Groups)
	}
	if got := saved.filter("").groupOf(repository); got != "personal" {
		t.Fatalf("group after reassignment was %q; want personal", got)
	}

	saved.setGroup(repository, "")
	if got := saved.filter("").groupOf(repository); got != ungrouped {
		t.Fatalf("cleared group was %q; want %s", got, ungrouped)
	}

	// Adding the same folder twice, spelled differently, must not duplicate it.
	if !saved.addRoot(root) {
		t.Fatalf("the first root was not added")
	}
	if saved.addRoot(filepath.Join(root, ".")) {
		t.Fatalf("the same root was added twice")
	}
	if !saved.removeRoot(root) || len(saved.Roots) != 0 {
		t.Fatalf("root was not removed: %+v", saved.Roots)
	}
}

func TestFilterSelectsByGroupAndExclusion(t *testing.T) {
	saved := config{Groups: map[string]string{
		filepath.FromSlash("/repos/api"):  "work",
		filepath.FromSlash("/repos/blog"): "personal",
	}}
	saved.exclude(filepath.FromSlash("/repos/vendored"))

	cases := []struct {
		group      string
		repository string
		want       bool
	}{
		{"", filepath.FromSlash("/repos/api"), true},
		{"", filepath.FromSlash("/repos/unknown"), true},
		{"", filepath.FromSlash("/repos/vendored"), false},
		{"work", filepath.FromSlash("/repos/api"), true},
		{"work", filepath.FromSlash("/repos/blog"), false},
		{"WORK", filepath.FromSlash("/repos/api"), true},
		{"personal", filepath.FromSlash("/repos/blog"), true},
		// An unassigned repository belongs to exactly one bucket, so asking for
		// it is possible and asking for another group must not return it.
		{ungrouped, filepath.FromSlash("/repos/unknown"), true},
		{ungrouped, filepath.FromSlash("/repos/api"), false},
		// Exclusion beats a matching group.
		{"work", filepath.FromSlash("/repos/vendored"), false},
	}
	for _, c := range cases {
		if got := saved.filter(c.group).includes(c.repository); got != c.want {
			t.Fatalf("filter %q on %s was %v; want %v", c.group, c.repository, got, c.want)
		}
	}

	if !saved.include(filepath.FromSlash("/repos/vendored")) {
		t.Fatalf("un-excluding a repository reported no change")
	}
	if !saved.filter("").includes(filepath.FromSlash("/repos/vendored")) {
		t.Fatalf("the repository is still excluded after include")
	}
	if got := saved.groupNames(); len(got) != 2 || got[0] != "personal" || got[1] != "work" {
		t.Fatalf("group names were %v; want [personal work]", got)
	}
}

func TestRootsAndReposCommandsEditTheSavedFile(t *testing.T) {
	path := tempConfig(t)
	root := t.TempDir()
	repository := filepath.Join(root, "api")
	git(t, "init", "--quiet", "--initial-branch=main", repository)

	steps := []struct {
		args []string
		code int
	}{
		{[]string{"roots", "add", root}, 0},
		{[]string{"roots", "add", root}, 0}, // Adding twice is harmless.
		{[]string{"roots", "add", filepath.Join(root, "absent")}, 1},
		{[]string{"roots", "remove", filepath.Join(root, "absent")}, 1},
		{[]string{"roots"}, 0},
		{[]string{"repos", "set", repository, "work"}, 0},
		{[]string{"repos", "exclude", repository}, 0},
		{[]string{"repos", "wat"}, 2},
		{[]string{"repos", "set", repository}, 2},
	}
	for _, step := range steps {
		var out, errOut bytes.Buffer
		if code := run(context.Background(), step.args, &out, &errOut); code != step.code {
			t.Fatalf("%v: exit %d want %d; stdout %s; stderr %s", step.args, code, step.code, &out, &errOut)
		}
	}

	saved, err := loadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(saved.Roots) != 1 || pathKey(saved.Roots[0]) != pathKey(root) {
		t.Fatalf("roots were %v; want just %s", saved.Roots, root)
	}
	if saved.filter("").groupOf(repository) != "work" {
		t.Fatalf("group was not saved: %+v", saved.Groups)
	}
	if saved.filter("").includes(repository) {
		t.Fatalf("exclusion was not saved: %+v", saved.Excluded)
	}

	var listed bytes.Buffer
	if code := run(context.Background(), []string{"repos"}, &listed, &listed); code != 0 {
		t.Fatalf("repos list: exit %d; %s", code, &listed)
	}
	if !strings.Contains(listed.String(), "excluded") {
		t.Fatalf("repos list did not report the excluded repository:\n%s", &listed)
	}
}

func TestAnExplicitFolderOverridesTheConfiguredRoots(t *testing.T) {
	tempConfig(t)
	configured, override := t.TempDir(), t.TempDir()

	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"roots", "add", configured}, &out, &errOut); code != 0 {
		t.Fatalf("roots add: exit %d; %s", code, &errOut)
	}

	chosen, ok := resolveSelection("", "", strings.NewReader(""), &out, &errOut)
	if !ok || len(chosen.roots) != 1 || pathKey(chosen.roots[0]) != pathKey(configured) {
		t.Fatalf("without a folder argument the configured root should win, got %v", chosen.roots)
	}

	chosen, ok = resolveSelection(override, "", strings.NewReader(""), &out, &errOut)
	if !ok || len(chosen.roots) != 1 || pathKey(chosen.roots[0]) != pathKey(override) {
		t.Fatalf("the folder argument should override the configuration, got %v", chosen.roots)
	}
	// Overriding the folder must not silently apply saved exclusions either.
	if !chosen.filter.includes(filepath.Join(override, "anything")) {
		t.Fatalf("an explicit folder should include everything under it")
	}
}

func TestMissingRootsAskOnlyWhenSomeoneCanAnswer(t *testing.T) {
	tempConfig(t)
	var out, errOut bytes.Buffer

	// strings.Reader is not a terminal, so the run must explain itself instead
	// of waiting forever for an answer that cannot arrive.
	if _, ok := resolveSelection("", "", strings.NewReader("ignored\n"), &out, &errOut); ok {
		t.Fatalf("a non-terminal run should not have prompted")
	}
	if !strings.Contains(errOut.String(), "gitcal roots add") {
		t.Fatalf("the failure did not say how to fix it: %s", &errOut)
	}
}

func TestOneUnreadableRootDoesNotHideTheOthers(t *testing.T) {
	good := t.TempDir()
	git(t, "init", "--quiet", "--initial-branch=main", filepath.Join(good, "repo"))
	missing := filepath.Join(t.TempDir(), "absent")

	result, err := scanAll(context.Background(), []string{missing, good})
	if err != nil {
		t.Fatalf("one bad root should not fail the scan: %v", err)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0].Error(), "absent") {
		t.Fatalf("expected one warning about %s, got %v", missing, result.Warnings)
	}
	if len(result.Repositories) != 1 {
		t.Fatalf("the readable root's repository was lost: %v", result.Repositories)
	}

	// Overlapping roots must not report the same repository twice.
	both, err := scanAll(context.Background(), []string{good, good})
	if err != nil {
		t.Fatalf("overlapping roots: %v", err)
	}
	if len(both.Repositories) != len(result.Repositories) {
		t.Fatalf("scanning the same root twice found %d repositories, want %d",
			len(both.Repositories), len(result.Repositories))
	}

	if _, err := scanAll(context.Background(), []string{missing}); err == nil {
		t.Fatalf("losing every root should still be an error")
	}
	if _, err := scanAll(context.Background(), nil); err == nil {
		t.Fatalf("scanning nothing should be an error")
	}
}

func TestGroupsDecideWhichCommitsAreCounted(t *testing.T) {
	root := t.TempDir()
	work := activityRepo(t, root, "employer")
	personal := activityRepo(t, root, "sideproject")
	ignored := activityRepo(t, root, "vendored")
	historyCommit(t, work, "Paid work", "2026-09-02T10:00:00Z", "2026-09-02T10:00:00Z")
	historyCommit(t, personal, "Evening work", "2026-09-02T20:00:00Z", "2026-09-02T20:00:00Z")
	historyCommit(t, ignored, "Someone else's commit", "2026-09-02T12:00:00Z", "2026-09-02T12:00:00Z")

	var saved config
	saved.setGroup(work, "work")
	saved.setGroup(personal, "personal")
	saved.exclude(ignored)
	month := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		group    string
		selected int
		subject  string
	}{
		{"", 2, "Paid work"},     // Excluded repositories never count.
		{"work", 1, "Paid work"}, //
		{"personal", 1, "Evening work"},
	}
	for _, c := range cases {
		result, err := collectActivity(context.Background(), []string{root}, month, saved.filter(c.group))
		if err != nil {
			t.Fatalf("group %q: %v", c.group, err)
		}
		if result.SelectedRepositories != c.selected {
			t.Fatalf("group %q selected %d repositories; want %d",
				c.group, result.SelectedRepositories, c.selected)
		}
		if result.DiscoveredRepositories != 3 {
			t.Fatalf("group %q discovered %d repositories; want 3 regardless of filtering",
				c.group, result.DiscoveredRepositories)
		}
		var subjects []string
		for _, day := range result.Days {
			for _, entry := range day.Entries {
				subjects = append(subjects, entry.Commit.Subject)
			}
		}
		if !containsText(subjects, c.subject) {
			t.Fatalf("group %q returned %v; expected it to contain %q", c.group, subjects, c.subject)
		}
		if containsText(subjects, "Someone else's commit") {
			t.Fatalf("group %q included an excluded repository's commit", c.group)
		}
	}

	// The group travels with the commit, so the detail view can name it.
	result, err := collectActivity(context.Background(), []string{root}, month, saved.filter(""))
	if err != nil {
		t.Fatal(err)
	}
	for _, day := range result.Days {
		for _, entry := range day.Entries {
			want := "work"
			if entry.Commit.Subject == "Evening work" {
				want = "personal"
			}
			if len(entry.Groups) != 1 || entry.Groups[0] != want {
				t.Fatalf("%q carried groups %v; want [%s]", entry.Commit.Subject, entry.Groups, want)
			}
		}
	}
}

func containsPath(values []string, want string) bool {
	for _, value := range values {
		if pathKey(value) == pathKey(want) {
			return true
		}
	}
	return false
}

func containsText(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
