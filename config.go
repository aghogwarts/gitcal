package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const ungrouped = "ungrouped"

// config is what gitcal remembers between runs. The commands write it; editing
// it by hand stays possible, which is why the format is readable.
type config struct {
	Roots      []string          `toml:"roots"`
	Excluded   []string          `toml:"excluded"`
	Groups     map[string]string `toml:"groups"`
	Identities []string          `toml:"identities"` // Author emails that count as yours.
}

const configHeader = `# gitcal configuration.
#
# Written by the gitcal commands; safe to edit by hand.
# Single-quoted strings are literal, so Windows paths need no escaping.

`

// configPathVariable lets one machine keep separate configurations, and lets
// the tests run without reading or writing the real one.
const configPathVariable = "GITCAL_CONFIG"

func defaultConfigPath() (string, error) {
	if override := os.Getenv(configPathVariable); override != "" {
		return override, nil
	}
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate configuration folder: %w", err)
	}
	return filepath.Join(directory, "gitcal", "config.toml"), nil
}

// loadConfig treats a missing file as an empty configuration: a first run is
// normal, not a failure.
func loadConfig(path string) (config, error) {
	var loaded config
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return loaded, nil
	}
	if err != nil {
		return loaded, fmt.Errorf("read configuration: %w", err)
	}
	if err := toml.Unmarshal(content, &loaded); err != nil {
		return loaded, fmt.Errorf("%s is not valid TOML: %w", path, err)
	}
	return loaded, nil
}

// saveConfig writes through a temporary file, so an interrupted write cannot
// leave a half-written configuration behind.
func saveConfig(path string, saved config) error {
	sort.Strings(saved.Roots)
	sort.Strings(saved.Excluded)
	sort.Strings(saved.Identities)
	if len(saved.Groups) == 0 {
		saved.Groups = nil
	}
	body, err := toml.Marshal(saved)
	if err != nil {
		return fmt.Errorf("encode configuration: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create configuration folder: %w", err)
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append([]byte(configHeader), body...), 0o644); err != nil {
		return fmt.Errorf("write configuration: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		os.Remove(temporary)
		return fmt.Errorf("replace configuration: %w", err)
	}
	return nil
}

// pathKey canonicalises a path for comparison only. Windows paths differ by
// case without differing in meaning, and a symlinked root resolves to the same
// repository the scanner reports.
func pathKey(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	} else if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

func (c *config) addRoot(folder string) bool {
	key := pathKey(folder)
	for _, existing := range c.Roots {
		if pathKey(existing) == key {
			return false
		}
	}
	absolute, err := filepath.Abs(folder)
	if err != nil {
		absolute = folder
	}
	c.Roots = append(c.Roots, filepath.Clean(absolute))
	return true
}

func (c *config) removeRoot(folder string) bool {
	key := pathKey(folder)
	for index, existing := range c.Roots {
		if pathKey(existing) == key {
			c.Roots = append(c.Roots[:index], c.Roots[index+1:]...)
			return true
		}
	}
	return false
}

func (c *config) setGroup(repository, group string) {
	if c.Groups == nil {
		c.Groups = map[string]string{}
	}
	// Reassigning must not leave the repository listed under its old path form.
	c.clearGroup(repository)
	if group != "" && group != ungrouped {
		c.Groups[filepath.Clean(repository)] = group
	}
}

func (c *config) clearGroup(repository string) {
	key := pathKey(repository)
	for stored := range c.Groups {
		if pathKey(stored) == key {
			delete(c.Groups, stored)
		}
	}
}

func (c *config) exclude(repository string) bool {
	key := pathKey(repository)
	for _, existing := range c.Excluded {
		if pathKey(existing) == key {
			return false
		}
	}
	c.Excluded = append(c.Excluded, filepath.Clean(repository))
	return true
}

func (c *config) include(repository string) bool {
	key := pathKey(repository)
	for index, existing := range c.Excluded {
		if pathKey(existing) == key {
			c.Excluded = append(c.Excluded[:index], c.Excluded[index+1:]...)
			return true
		}
	}
	return false
}

// addIdentity and removeIdentity compare emails case-insensitively, the way
// mail systems do, rather than by the OS-dependent rules pathKey applies to
// folders. Git itself never lowercases an address, so what is stored keeps
// whatever case was typed.
func (c *config) addIdentity(email string) bool {
	key := strings.ToLower(strings.TrimSpace(email))
	for _, existing := range c.Identities {
		if strings.ToLower(existing) == key {
			return false
		}
	}
	c.Identities = append(c.Identities, strings.TrimSpace(email))
	return true
}

func (c *config) removeIdentity(email string) bool {
	key := strings.ToLower(strings.TrimSpace(email))
	for index, existing := range c.Identities {
		if strings.ToLower(existing) == key {
			c.Identities = append(c.Identities[:index], c.Identities[index+1:]...)
			return true
		}
	}
	return false
}

// authorFilter answers "is this commit mine?", the counterpart to
// repositoryFilter answering "does this repository count?". Building the
// lookup once means every renderer asks the same question the same way.
type authorFilter struct {
	identities map[string]bool
	mineOnly   bool
}

func (c config) authors(mineOnly bool) authorFilter {
	built := authorFilter{identities: make(map[string]bool, len(c.Identities)), mineOnly: mineOnly}
	for _, email := range c.Identities {
		built.identities[strings.ToLower(strings.TrimSpace(email))] = true
	}
	return built
}

func (f authorFilter) isMine(email string) bool {
	return f.identities[strings.ToLower(strings.TrimSpace(email))]
}

// includes matches repositoryFilter.includes: the zero value, and one built
// with mineOnly false, both include every commit.
func (f authorFilter) includes(email string) bool {
	if !f.mineOnly {
		return true
	}
	return f.isMine(email)
}

// groupNames lists every group in use, so commands can report the real choices
// rather than only the suggested work and personal.
func (c config) groupNames() []string {
	seen := map[string]bool{}
	var names []string
	for _, group := range c.Groups {
		if !seen[group] {
			seen[group] = true
			names = append(names, group)
		}
	}
	sort.Strings(names)
	return names
}

// repositoryFilter answers "does this repository count?" without the renderers
// or the scanner needing to know about configuration at all.
type repositoryFilter struct {
	excluded map[string]bool
	groups   map[string]string
	only     string
}

// filter builds a lookup keyed by canonical path. The zero filter includes
// everything, which is what an explicit folder argument wants.
func (c config) filter(group string) repositoryFilter {
	built := repositoryFilter{
		excluded: make(map[string]bool, len(c.Excluded)),
		groups:   make(map[string]string, len(c.Groups)),
		only:     group,
	}
	for _, repository := range c.Excluded {
		built.excluded[pathKey(repository)] = true
	}
	for repository, name := range c.Groups {
		built.groups[pathKey(repository)] = name
	}
	return built
}

func (f repositoryFilter) groupOf(repository string) string {
	if group, ok := f.groups[pathKey(repository)]; ok {
		return group
	}
	return ungrouped
}

func (f repositoryFilter) includes(repository string) bool {
	key := pathKey(repository)
	if f.excluded[key] {
		return false
	}
	if f.only == "" {
		return true
	}
	group, ok := f.groups[key]
	if !ok {
		group = ungrouped
	}
	return strings.EqualFold(group, f.only)
}
