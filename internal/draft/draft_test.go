package draft

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/fabricahq/release-planner/internal/config"
	"github.com/fabricahq/release-planner/internal/contributors"
	"github.com/fabricahq/release-planner/internal/gitrepo"
	"github.com/fabricahq/release-planner/internal/plan"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func commit(t *testing.T, dir, file string, message ...string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
	args := []string{"commit", "-q"}
	for _, m := range message {
		args = append(args, "-m", m)
	}
	git(t, dir, args...)
}

func setup(t *testing.T) (gitrepo.Repo, config.Config) {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "remote", "add", "origin", "git@github.com:fabricahq/example.git")
	commit(t, dir, "a", "Initial")
	c, err := config.Parse([]byte("schema-version: 1\nversion: v0.2.0\nfirst-version: v1.0.0\nvalidate:\n  run: make test\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	return gitrepo.Repo{Dir: dir}, c
}

func TestRepositoryReadsOwnerAndNameFromOrigin(t *testing.T) {
	repo, _ := setup(t)
	for url, want := range map[string]string{
		"git@github.com:fabricahq/example.git":                  "fabricahq/example",
		"https://github.com/fabricahq/public-rules":             "fabricahq/public-rules",
		"https://github.com/fabricahq/.code-rules-public.git":   "fabricahq/.code-rules-public",
		"http://proxy@127.0.0.1:8080/git/fabricahq/example.git": "fabricahq/example",
	} {
		git(t, repo.Dir, "remote", "set-url", "origin", url)
		if got, err := Repository(context.Background(), repo); err != nil || got != want {
			t.Errorf("%s: got %q %v", url, got, err)
		}
	}
}

func TestWriteDraftsAFirstRelease(t *testing.T) {
	repo, c := setup(t)
	commit(t, repo.Dir, "b", "Add b (#1)")
	if _, _, err := Write(context.Background(), repo, c, "fabricahq/example", "v0.1.0", "HEAD", nil); err == nil || !strings.Contains(err.Error(), "first release must be v1.0.0") {
		t.Fatal(err)
	}
	name, _, err := Write(context.Background(), repo, c, "fabricahq/example", "v1.0.0", "HEAD", nil)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(repo.Dir, name))
	notes := string(data)
	for _, want := range []string{
		Opening,
		Opening + "\n\n## Pull Requests\n\n",
		"- Initial in https://github.com/fabricahq/example/commit/",
		"- Add b in #1\n",
		"This is the first release. Browse the source at [v1.0.0](https://github.com/fabricahq/example/tree/v1.0.0).",
	} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes lack %q:\n%s", want, notes)
		}
	}
	if _, _, err := Write(context.Background(), repo, c, "fabricahq/example", "v1.0.0", "HEAD", nil); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatal(err)
	}
}

func TestWriteDraftsALaterRelease(t *testing.T) {
	repo, c := setup(t)
	git(t, repo.Dir, "tag", "v1.0.0")
	git(t, repo.Dir, "checkout", "-q", "-b", "feature")
	commit(t, repo.Dir, "b", "Work in progress")
	git(t, repo.Dir, "checkout", "-q", "main")
	git(t, repo.Dir, "merge", "-q", "--no-ff", "feature", "-m", "Merge pull request #7 from fabricahq/feature", "-m", "Add a Svelte group")

	if _, _, err := Write(context.Background(), repo, c, "fabricahq/example", "v1.0.0", "HEAD", nil); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatal(err)
	}
	name, _, err := Write(context.Background(), repo, c, "fabricahq/example", "v1.1.0", "HEAD", nil)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(repo.Dir, name))
	notes := string(data)
	want := Opening + "\n\n## Pull Requests\n\n- Add a Svelte group in #7\n\n**Full Changelog**: https://github.com/fabricahq/example/compare/v1.0.0...v1.1.0\n"
	if notes != want {
		t.Fatalf("got:\n%s\nwant:\n%s", notes, want)
	}
}

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

// With GitHub, the draft credits authors by handle and names new contributors. Lookups that
// fail leave the entry without a handle and add a warning, rather than failing the draft.
func TestWriteCreditsAuthorsAndNewContributors(t *testing.T) {
	repo, c := setup(t)
	git(t, repo.Dir, "tag", "v1.0.0")
	commit(t, repo.Dir, "b", "feat: add b (#7)")
	commit(t, repo.Dir, "c", "fix: repair c (#8)")
	commit(t, repo.Dir, "d", "chore: bump d (#9)")
	gh := fakeGitHub{
		authors: map[int]string{7: "octocat", 8: "hubot", 9: "dependabot[bot]"},
		before:  map[string]bool{"octocat": false, "hubot": true},
	}
	name, warnings, err := Write(context.Background(), repo, c, "fabricahq/example", "v1.1.0", "HEAD", gh)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(repo.Dir, name))
	want := Opening + "\n\n## Pull Requests\n\n" +
		"- feat: add b by @octocat in #7\n" +
		"- fix: repair c by @hubot in #8\n" +
		"- chore: bump d by @dependabot[bot] in #9\n" +
		"\n## New Contributors\n\n- @octocat made their first contribution in #7\n" +
		"\n**Full Changelog**: https://github.com/fabricahq/example/compare/v1.0.0...v1.1.0\n"
	if string(data) != want || len(warnings) != 0 {
		t.Fatalf("got:\n%s\nwant:\n%s\nwarnings: %v", data, want, warnings)
	}

	// A failed author lookup drops only that handle.
	inv := plan.Inventory{Previous: "v1.0.0", PullRequests: []int{7, 8}, Commits: []plan.Commit{
		{SHA: "5ba22db0000000000000000000000000000000000", Title: "feat: add b", PullRequest: 7, OnBranch: true},
		{SHA: "6c33d1e0000000000000000000000000000000000", Title: "fix: repair c", PullRequest: 8, OnBranch: true},
	}}
	contributors.Add(context.Background(), fakeGitHub{authors: map[int]string{7: "octocat"}}, &inv)
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
	contributors.Add(context.Background(), gh, &inv)
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
	contributors.Add(context.Background(), fakeGitHub{authors: map[int]string{1: "octocat", 2: "hubot", 3: "octocat"}}, &inv)
	if len(inv.NewContributors) != 2 || inv.NewContributors[0] != (plan.NewContributor{Handle: "octocat", PullRequest: 1}) || inv.NewContributors[1] != (plan.NewContributor{Handle: "hubot", PullRequest: 2}) || len(inv.Warnings) != 0 {
		t.Fatalf("%+v %v", inv.NewContributors, inv.Warnings)
	}
}

// The draft lists every pull request and direct commit in merge order, titles as written,
// and leaves grouping and formatting to the agent.
func TestRenderListsBranchChangesInOrder(t *testing.T) {
	inv := plan.Inventory{Previous: "v1.0.0", Commits: []plan.Commit{
		{SHA: "1d7ee9b0000000000000000000000000000000000", Subject: "feat: add runes rules", Title: "feat: add runes rules"},
		{SHA: "5ba22db0000000000000000000000000000000000", Subject: "Merge pull request #7 from example/svelte", Title: "feat(svelte): add a Svelte group", PullRequest: 7, OnBranch: true},
		{SHA: "6c33d1e0000000000000000000000000000000000", Subject: "fix!: reject empty rule IDs (#9)", Title: "fix!: reject empty rule IDs", PullRequest: 9, OnBranch: true},
		{SHA: "9c01ab30000000000000000000000000000000000", Subject: "Fix a typo", Title: "Fix a typo", OnBranch: true},
		{SHA: "4e5f6a70000000000000000000000000000000000", Subject: "Merge branch 'main' into feature", Title: "Merge branch 'main' into feature", OnBranch: true},
	}}
	want := Opening + "\n\n## Pull Requests\n\n" +
		"- feat(svelte): add a Svelte group in #7\n" +
		"- fix!: reject empty rule IDs in #9\n" +
		"- Fix a typo in https://github.com/fabricahq/example/commit/9c01ab3\n" +
		"\n**Full Changelog**: https://github.com/fabricahq/example/compare/v1.0.0...v1.1.0\n"
	if got := Render(inv, "fabricahq/example", "v1.1.0"); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderFirstReleaseWithNoBranchChanges(t *testing.T) {
	got := Render(plan.Inventory{}, "fabricahq/example", "v1.0.0")
	for _, want := range []string{
		"- No changes on the release branch since the previous release.\n",
		"This is the first release. Browse the source at [v1.0.0](https://github.com/fabricahq/example/tree/v1.0.0).\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("draft lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Full Changelog") {
		t.Error("a first release has no comparison")
	}
}
