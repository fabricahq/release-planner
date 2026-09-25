package gitrepo

import (
	"context"
	"os/exec"
	"testing"
)

func TestGitHubRepositoryReadsOwnerAndNameFromOrigin(t *testing.T) {
	repo := Repo{Dir: t.TempDir()}
	if out, err := exec.Command("git", "-C", repo.Dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	if _, err := repo.GitHubRepository(context.Background()); err == nil {
		t.Fatal("no origin, but found a repository")
	}
	if _, err := repo.Run(context.Background(), "remote", "add", "origin", "git@github.com:fabricahq/example.git"); err != nil {
		t.Fatal(err)
	}
	for url, want := range map[string]string{
		"git@github.com:fabricahq/example.git":                  "fabricahq/example",
		"https://github.com/fabricahq/public-rules":             "fabricahq/public-rules",
		"https://github.com/fabricahq/.code-rules-public.git":   "fabricahq/.code-rules-public",
		"http://proxy@127.0.0.1:8080/git/fabricahq/example.git": "fabricahq/example",
	} {
		if _, err := repo.Run(context.Background(), "remote", "set-url", "origin", url); err != nil {
			t.Fatal(err)
		}
		if got, err := repo.GitHubRepository(context.Background()); err != nil || got != want {
			t.Errorf("%s: got %q %v", url, got, err)
		}
	}
}
