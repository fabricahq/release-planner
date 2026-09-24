// Package draft creates the raw material for a release's notes: everything that can be
// worked out deterministically, so the agent spends its effort on judgment. It checks the
// version, lists every pull request with its author, names first-time contributors, and
// writes the closing link. It makes no choices about grouping or wording; the agent
// organizes and writes the notes as the repository's style and policy say.
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
	"strings"

	"github.com/fabricahq/release-planner/internal/config"
	"github.com/fabricahq/release-planner/internal/contributors"
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

// Write creates <notes-dir>/<version>.md for a release cut at head, and returns its path and
// warnings about anything it couldn't look up. With gh, it credits pull request authors by
// GitHub handle and names new contributors; with nil, it writes the notes without them.
func Write(ctx context.Context, repo gitrepo.Repo, c config.Config, ownerName, version, head string, gh contributors.GitHub) (string, []string, error) {
	if !repository.MatchString(ownerName) {
		return "", nil, fmt.Errorf("repository must be owner/name, not %q", ownerName)
	}
	v, ok := semver.Parse(version)
	if !ok {
		return "", nil, fmt.Errorf("%q is not a version such as v1.2.0", version)
	}
	inv, err := plan.Take(ctx, repo, plan.Options{NotesDir: c.NotesDir, FirstVersion: c.FirstVersion}, head)
	if err != nil {
		return "", nil, err
	}
	if len(inv.PendingRequests) > 0 {
		return "", nil, fmt.Errorf("resolve untagged release request %s first", inv.PendingRequests[0])
	}
	tags, err := repo.Tags(ctx)
	if err != nil {
		return "", nil, err
	}
	for _, t := range tags {
		if other, ok := semver.Parse(t); ok && semver.Compare(v, other) <= 0 {
			return "", nil, fmt.Errorf("%s must be newer than existing tag %s", version, t)
		}
	}
	if inv.Previous == "" && version != c.FirstVersion {
		return "", nil, fmt.Errorf("the first release must be %s", c.FirstVersion)
	}
	if gh != nil {
		contributors.Add(ctx, gh, &inv)
	}

	name := path.Join(c.NotesDir, version+".md")
	file := filepath.Join(repo.Dir, filepath.FromSlash(name))
	if _, err := os.Stat(file); err == nil {
		return "", nil, fmt.Errorf("%s already exists; edit it instead", name)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", nil, err
	}

	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return "", nil, err
	}
	return name, inv.Warnings, os.WriteFile(file, []byte(Render(inv, ownerName, version)), 0o644)
}

// Render returns a draft's notes for version from the inventory. It writes a TODO opening
// for the agent to replace, then every pull request and direct commit in merge order with its
// author's handle when known, the new contributors, and the closing link. Pull request numbers
// are left for GitHub to link; direct commits and the closing link get explicit URLs.
func Render(inv plan.Inventory, ownerName, version string) string {
	var b strings.Builder
	b.WriteString(Opening + "\n")
	b.WriteString("\n## Pull Requests\n\n")
	listed := 0
	for _, commit := range inv.Commits {
		by := ""
		if commit.AuthorHandle != "" {
			by = " by @" + commit.AuthorHandle
		}
		switch {
		case commit.PullRequest != 0 && commit.OnBranch:
			fmt.Fprintf(&b, "- %s%s in #%d\n", commit.Title, by, commit.PullRequest)
		case commit.OnBranch && !strings.HasPrefix(commit.Subject, "Merge "):
			fmt.Fprintf(&b, "- %s in https://github.com/%s/commit/%s\n", commit.Title, ownerName, commit.SHA[:7])
		default:
			continue
		}
		listed++
	}
	if listed == 0 {
		b.WriteString("- No changes on the release branch since the previous release.\n")
	}
	if len(inv.NewContributors) > 0 {
		b.WriteString("\n## New Contributors\n\n")
		for _, c := range inv.NewContributors {
			fmt.Fprintf(&b, "- @%s made their first contribution in #%d\n", c.Handle, c.PullRequest)
		}
	}
	base := "https://github.com/" + ownerName
	if inv.Previous != "" {
		fmt.Fprintf(&b, "\n**Full Changelog**: %s/compare/%s...%s\n", base, inv.Previous, version)
	} else {
		fmt.Fprintf(&b, "\nThis is the first release. Browse the source at [%s](%s/tree/%s).\n", version, base, version)
	}
	return b.String()
}
