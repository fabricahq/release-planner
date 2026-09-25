package contributors

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/fabricahq/release-planner/internal/plan"
)

// fakeGitHub answers contributor lookups from maps; a missing entry is a failed lookup.
type fakeGitHub struct {
	authors map[int]string
	before  map[string]bool
}

func (f fakeGitHub) PullRequestAuthor(_ context.Context, number int) (string, error) {
	if handle, ok := f.authors[number]; ok {
		return handle, nil
	}
	return "", errors.New("not found")
}

func (f fakeGitHub) ContributedBefore(_ context.Context, handle, ref string) (bool, error) {
	if ref != "v1.0.0" {
		return false, errors.New("unexpected ref " + ref)
	}
	if before, ok := f.before[handle]; ok {
		return before, nil
	}
	return false, errors.New("rate limited")
}

// A lookup that fails leaves the entry without a handle and adds a warning, rather than
// failing the inventory.
func TestFailedLookupsBecomeWarnings(t *testing.T) {
	inv := plan.Inventory{Previous: "v1.0.0", PullRequests: []int{7, 8}, Commits: []plan.Commit{
		{SHA: "5ba22db0000000000000000000000000000000000", Title: "feat: add b", PullRequest: 7, OnBranch: true},
		{SHA: "6c33d1e0000000000000000000000000000000000", Title: "fix: repair c", PullRequest: 8, OnBranch: true},
	}}
	Add(context.Background(), fakeGitHub{authors: map[int]string{7: "octocat"}}, &inv)
	if inv.Commits[0].AuthorHandle != "octocat" || inv.Commits[1].AuthorHandle != "" || len(inv.Warnings) != 2 {
		t.Fatalf("%+v %v", inv.Commits, inv.Warnings)
	}
}

// A new contributor is credited for their first pull request merged into the release branch,
// in merge order: not a lower-numbered one merged later, and not one merged inside another
// branch, which the notes don't list.
func TestNewContributorsAreCreditedInMergeOrder(t *testing.T) {
	inv := plan.Inventory{PullRequests: []int{9, 10, 11, 20}, Commits: []plan.Commit{
		{SHA: "9999999000000000000000000000000000000000", Title: "fix typo", PullRequest: 9, OnBranch: false},
		{SHA: "1010101000000000000000000000000000000000", Title: "Add docs", PullRequest: 10, OnBranch: true},
		{SHA: "2020202000000000000000000000000000000000", Title: "Add feature", PullRequest: 20, OnBranch: true},
		{SHA: "1111111000000000000000000000000000000000", Title: "Fix feature", PullRequest: 11, OnBranch: true},
	}}
	gh := fakeGitHub{authors: map[int]string{9: "grace", 10: "hubot", 11: "ada", 20: "ada"}}
	Add(context.Background(), gh, &inv)
	want := []plan.NewContributor{{Handle: "hubot", PullRequest: 10}, {Handle: "ada", PullRequest: 20}}
	if !slices.Equal(inv.NewContributors, want) {
		t.Fatalf("got %+v, want %+v", inv.NewContributors, want)
	}
}

// In a first release, every author's first pull request here is their first contribution,
// with no lookup of earlier history.
func TestFirstReleaseWelcomesEveryAuthor(t *testing.T) {
	inv := plan.Inventory{PullRequests: []int{1, 2, 3}, Commits: []plan.Commit{
		{SHA: "1111111000000000000000000000000000000000", Title: "Start", PullRequest: 1, OnBranch: true},
		{SHA: "2222222000000000000000000000000000000000", Title: "More", PullRequest: 2, OnBranch: true},
		{SHA: "3333333000000000000000000000000000000000", Title: "Again", PullRequest: 3, OnBranch: true},
	}}
	Add(context.Background(), fakeGitHub{authors: map[int]string{1: "octocat", 2: "hubot", 3: "octocat"}}, &inv)
	if len(inv.NewContributors) != 2 || inv.NewContributors[0] != (plan.NewContributor{Handle: "octocat", PullRequest: 1}) || inv.NewContributors[1] != (plan.NewContributor{Handle: "hubot", PullRequest: 2}) || len(inv.Warnings) != 0 {
		t.Fatalf("%+v %v", inv.NewContributors, inv.Warnings)
	}
}
