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
	c, err := config.Parse([]byte("version: v0.2.0\nfirst-version: v1.0.0\nvalidate:\n  run: make test\n"))
	if err != nil {
		t.Fatal(err)
	}
	return gitrepo.Repo{Dir: dir}, c
}

func TestRepository(t *testing.T) {
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

func TestFirstRelease(t *testing.T) {
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
		"\n## ✨ New Features\n\n## ⬆️ Improvements\n\n## 🐛 Squashed Bugs\n\n## ⛓️‍💥 Breaking Changes\n",
		"- Initial in https://github.com/fabricahq/example/commit/",
		"- Add b in https://github.com/fabricahq/example/pull/1\n",
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

func TestLaterRelease(t *testing.T) {
	repo, c := setup(t)
	git(t, repo.Dir, "tag", "v1.0.0")
	git(t, repo.Dir, "checkout", "-q", "-b", "feature")
	commit(t, repo.Dir, "b", "Work in progress")
	git(t, repo.Dir, "checkout", "-q", "main")
	git(t, repo.Dir, "merge", "-q", "--no-ff", "feature", "-m", "Merge pull request #7 from fabricahq/feature", "-m", "Add a Svelte group")
	c.Notes.Sections = []config.Section{{Heading: "Fixes", Include: "Bugs."}}

	if _, err := Write(context.Background(), repo, c, "fabricahq/example", "v1.0.0", "HEAD"); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatal(err)
	}
	name, err := Write(context.Background(), repo, c, "fabricahq/example", "v1.1.0", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(repo.Dir, name))
	notes := string(data)
	want := Opening + "\n\n## Fixes\n\n## What's Changed\n\n- Add a Svelte group in https://github.com/fabricahq/example/pull/7\n\n**Full Changelog**: https://github.com/fabricahq/example/compare/v1.0.0...v1.1.0\n"
	if notes != want {
		t.Fatalf("got:\n%s\nwant:\n%s", notes, want)
	}
}
