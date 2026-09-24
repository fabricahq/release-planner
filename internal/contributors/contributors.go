// Package contributors adds GitHub identities to an inventory: each pull request author's
// handle, and which authors made their first contribution in the release. Git history
// records only author names, so these come from GitHub.
package contributors

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/fabricahq/release-planner/internal/plan"
)

// GitHub is what Add needs from the GitHub API.
type GitHub interface {
	// PullRequestAuthor returns the handle of a pull request's author.
	PullRequestAuthor(ctx context.Context, number int) (string, error)
	// ContributedBefore reports whether the handle authored any commit reachable from ref.
	ContributedBefore(ctx context.Context, handle, ref string) (bool, error)
}

// Add sets AuthorHandle on every pull request commit and lists the authors whose first
// merged pull request is in the inventory. A lookup that fails adds a warning instead of an
// error, so the inventory stays usable and the agent can fill the gap.
func Add(ctx context.Context, gh GitHub, inv *plan.Inventory) {
	warn := func(format string, args ...any) { inv.Warnings = append(inv.Warnings, fmt.Sprintf(format, args...)) }
	handles := map[int]string{}
	for _, number := range inv.PullRequests {
		handle, err := gh.PullRequestAuthor(ctx, number)
		if err != nil {
			warn("no author for pull request #%d: %v", number, err)
			continue
		}
		handles[number] = handle
	}
	for i := range inv.Commits {
		inv.Commits[i].AuthorHandle = handles[inv.Commits[i].PullRequest]
	}

	// An author is new when none of their commits is in the previous release. In a first
	// release, every author is new. Each is credited for their first pull request here.
	seen := map[string]bool{}
	for _, number := range inv.PullRequests {
		handle := handles[number]
		// Bots aren't contributors to welcome.
		if handle == "" || seen[handle] || strings.HasSuffix(handle, "[bot]") {
			continue
		}
		seen[handle] = true
		if inv.Previous != "" {
			before, err := gh.ContributedBefore(ctx, handle, inv.Previous)
			if err != nil {
				warn("couldn't tell whether @%s is a new contributor: %v", handle, err)
				continue
			}
			if before {
				continue
			}
		}
		inv.NewContributors = append(inv.NewContributors, plan.NewContributor{Handle: handle, PullRequest: number})
	}
	slices.SortFunc(inv.NewContributors, func(a, b plan.NewContributor) int { return a.PullRequest - b.PullRequest })
}
