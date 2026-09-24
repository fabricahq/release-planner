// Package draft creates a release notes file for the agent to finish.
package draft

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/fabricahq/release-planner/internal/config"
	"github.com/fabricahq/release-planner/internal/gitrepo"
	"github.com/fabricahq/release-planner/internal/plan"
	"github.com/fabricahq/release-planner/internal/semver"
)

// Opening is the first line of every draft; plan rejects notes that still contain it.
const Opening = plan.DraftOpening

var repository = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// Repository finds owner/name from the origin remote, for the links in the notes.
func Repository(ctx context.Context, repo gitrepo.Repo) (string, error) {
	url, err := repo.Run(ctx, "remote", "get-url", "origin")
	if err != nil {
		return "", fmt.Errorf("find the GitHub repository from the origin remote, or pass --repository owner/name: %v", err)
	}
	url = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(url), "/"), ".git")
	parts := strings.FieldsFunc(url, func(r rune) bool { return r == '/' || r == ':' })
	if len(parts) < 2 || !repository.MatchString(parts[len(parts)-2]+"/"+parts[len(parts)-1]) {
		return "", fmt.Errorf("could not read owner/name from origin %q; pass --repository owner/name", url)
	}
	return parts[len(parts)-2] + "/" + parts[len(parts)-1], nil
}

// Write creates <notes-dir>/<version>.md for a release cut at head and returns its path.
func Write(ctx context.Context, repo gitrepo.Repo, c config.Config, ownerName, version, head string) (string, error) {
	if !repository.MatchString(ownerName) {
		return "", fmt.Errorf("repository must be owner/name, not %q", ownerName)
	}
	v, ok := semver.Parse(version)
	if !ok {
		return "", fmt.Errorf("%q is not a version such as v1.2.0", version)
	}
	inv, err := plan.Take(ctx, repo, plan.Options{NotesDir: c.NotesDir, FirstVersion: c.FirstVersion}, head)
	if err != nil {
		return "", err
	}
	if len(inv.PendingRequests) > 0 {
		return "", fmt.Errorf("resolve untagged release request %s first", inv.PendingRequests[0])
	}
	tags, err := repo.Tags(ctx)
	if err != nil {
		return "", err
	}
	for _, t := range tags {
		if other, ok := semver.Parse(t); ok && semver.Compare(v, other) <= 0 {
			return "", fmt.Errorf("%s must be newer than existing tag %s", version, t)
		}
	}
	if inv.Previous == "" && version != c.FirstVersion {
		return "", fmt.Errorf("the first release must be %s", c.FirstVersion)
	}

	name := path.Join(c.NotesDir, version+".md")
	file := filepath.Join(repo.Dir, filepath.FromSlash(name))
	if _, err := os.Stat(file); err == nil {
		return "", fmt.Errorf("%s already exists; edit it instead", name)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return "", err
	}
	return name, os.WriteFile(file, []byte(Render(inv, ownerName, version)), 0o644)
}

// Render returns a draft's notes for version from the inventory since the previous release,
// with links into the GitHub repository ownerName. The agent adds headings as the release
// notes style says; the draft holds only the fixed parts: a Pull Requests section grouped by
// conventional-commit type, and the closing link.
func Render(inv plan.Inventory, ownerName, version string) string {
	base := "https://github.com/" + ownerName
	entries := map[string][]string{}
	for _, commit := range inv.Commits {
		var entry string
		switch {
		case commit.PullRequest != 0 && commit.OnBranch:
			entry = fmt.Sprintf("- %s in #%d\n", commit.Title, commit.PullRequest)
		case commit.OnBranch && !strings.HasPrefix(commit.Subject, "Merge "):
			entry = fmt.Sprintf("- %s in %s/commit/%s\n", commit.Title, base, commit.SHA[:7])
		default:
			continue
		}
		group := groupOf(commit.Title)
		entries[group] = append(entries[group], entry)
	}

	var b strings.Builder
	b.WriteString(Opening + "\n")
	b.WriteString("\n## Pull Requests\n")
	listed := false
	for _, group := range groups {
		if len(entries[group.heading]) == 0 {
			continue
		}
		listed = true
		fmt.Fprintf(&b, "\n### %s\n\n", group.heading)
		for _, entry := range entries[group.heading] {
			b.WriteString(entry)
		}
	}
	if !listed {
		b.WriteString("\n- No changes on the release branch since the previous release.\n")
	}
	if inv.Previous != "" {
		fmt.Fprintf(&b, "\n**Full Changelog**: %s/compare/%s...%s\n", base, inv.Previous, version)
	} else {
		fmt.Fprintf(&b, "\nThis is the first release. Browse the source at [%s](%s/tree/%s).\n", version, base, version)
	}
	return b.String()
}

// groups are the Pull Requests subsections, in order, with the conventional-commit types
// each collects. Other Changes holds titles without a recognized type, for the agent to sort.
var groups = []struct {
	heading string
	types   []string
}{
	{"✨ Features", []string{"feat"}},
	{"🐛 Bug Fixes", []string{"fix"}},
	{"📖 Documentation", []string{"docs"}},
	{"🤖 CI", []string{"ci"}},
	{"🧹 Chores", []string{"chore", "refactor", "test", "build", "perf", "style", "revert", "deps"}},
	{OtherChanges, nil},
}

// OtherChanges is the group for titles without a conventional-commit type.
const OtherChanges = "Other Changes"

var conventional = regexp.MustCompile(`^([a-z]+)(\([^)]*\))?!?:\s`)

// groupOf returns the Pull Requests subsection for a pull request or commit title.
func groupOf(title string) string {
	if m := conventional.FindStringSubmatch(strings.ToLower(title)); m != nil {
		for _, group := range groups {
			if slices.Contains(group.types, m[1]) {
				return group.heading
			}
		}
	}
	return OtherChanges
}
