package draft

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fabricahq/release-planner/internal/config"
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
	if _, err := Write(context.Background(), repo, c, "fabricahq/example", "v0.1.0", "HEAD"); err == nil || !strings.Contains(err.Error(), "first release must be v1.0.0") {
		t.Fatal(err)
	}
	name, err := Write(context.Background(), repo, c, "fabricahq/example", "v1.0.0", "HEAD")
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
	if _, err := Write(context.Background(), repo, c, "fabricahq/example", "v1.0.0", "HEAD"); err == nil || !strings.Contains(err.Error(), "already exists") {
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

	if _, err := Write(context.Background(), repo, c, "fabricahq/example", "v1.0.0", "HEAD"); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatal(err)
	}
	name, err := Write(context.Background(), repo, c, "fabricahq/example", "v1.1.0", "HEAD")
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
