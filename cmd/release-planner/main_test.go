package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func cli(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestUsageListsCommandsAndRejectsUnknownOnes(t *testing.T) {
	if code, out, _ := cli(t); code != 0 || !strings.Contains(out, "inventory") {
		t.Fatalf("%d %s", code, out)
	}
	if code, _, errOut := cli(t, "tag"); code != 2 || !strings.Contains(errOut, `unknown command "tag"`) {
		t.Fatalf("%d %s", code, errOut)
	}
}

func TestInstallThenCheckAndGuideSucceed(t *testing.T) {
	dir := t.TempDir()
	config := "schema-version: 1\nversion: v0.1.0\nfirst-version: v1.0.0\nrelease-checks:\n  run: make test\n"
	if err := os.MkdirAll(filepath.Join(dir, ".release-planner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".release-planner/config.yml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := cli(t, "install", "--dir", dir); code != 0 || !strings.Contains(out, "created   .github/workflows/release-planner.yml") {
		t.Fatalf("install: %d %s %s", code, out, errOut)
	}
	if code, out, _ := cli(t, "install", "--dir", dir); code != 0 || !strings.Contains(out, "Nothing to do.") {
		t.Fatalf("second install: %d %s", code, out)
	}
	if code, out, _ := cli(t, "check", "--dir", dir); code != 0 || !strings.Contains(out, "match") {
		t.Fatalf("check: %d %s", code, out)
	}
	workflow := filepath.Join(dir, ".github/workflows/release-planner.yml")
	data, _ := os.ReadFile(workflow)
	if err := os.WriteFile(workflow, append(data, "# edit\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := cli(t, "check", "--dir", dir); code != 1 || !strings.Contains(errOut, ".github/workflows/release-planner.yml: edited") {
		t.Fatalf("check after edit: %d %s", code, errOut)
	}
	if code, out, _ := cli(t, "guide", "--dir", dir); code != 0 || !strings.Contains(out, "# Prepare a release (Release Planner v0.1.0)") {
		t.Fatalf("guide: %d %s", code, out)
	}
	if code, out, _ := cli(t, "guide", "--default-style"); code != 0 || !strings.HasPrefix(out, "- Organize changes under these headings") {
		t.Fatalf("default style: %d %s", code, out)
	}
}

func TestInitCreatesAnInstallableConfigOnce(t *testing.T) {
	dir := t.TempDir()
	code, out, errOut := cli(t, "init", "--dir", dir, "--version", "v0.1.0", "--first-version", "v1.0.0")
	if code != 0 || !strings.Contains(out, "created   .release-planner/config.yml\ncreated   .release-planner/policy.md\n") {
		t.Fatalf("init: %d %s %s", code, out, errOut)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".release-planner/config.yml"))
	for _, want := range []string{"schema-version: 1\n", "version: v0.1.0\n", "first-version: v1.0.0\n", "#   file: .release-planner/release-notes-style.md\n#   mode: append\n"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("config lacks %q:\n%s", want, data)
		}
	}
	// The starter config is complete enough to install from.
	if code, _, errOut := cli(t, "install", "--dir", dir); code != 0 {
		t.Fatalf("install after init: %s", errOut)
	}
	if code, _, errOut := cli(t, "init", "--dir", dir, "--version", "v0.1.0"); code != 1 || !strings.Contains(errOut, "already exists") {
		t.Fatalf("second init: %d %s", code, errOut)
	}
}

func TestMissingConfigPointsToInit(t *testing.T) {
	if code, _, errOut := cli(t, "install", "--dir", t.TempDir()); code != 1 || !strings.Contains(errOut, ".release-planner/config.yml not found; run release-planner init") {
		t.Fatalf("%d %s", code, errOut)
	}
}

// A release pull request whose base branch moved on after it branched still validates the
// notes it adds, rather than failing because the base tip isn't in its history.
func TestValidatePullRequestAfterBaseMoved(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	write(".release-planner/config.yml", "schema-version: 1\nversion: v0.1.0\nfirst-version: v1.0.0\n")
	git("add", "-A")
	git("commit", "-q", "-m", "Adopt Release Planner")
	git("checkout", "-q", "-b", "release")
	write("_releases/v1.0.0.md", "The first release.\n")
	git("add", "-A")
	git("commit", "-q", "-m", "Release v1.0.0")
	head := git("rev-parse", "HEAD")
	git("checkout", "-q", "main")
	write("README.md", "Moved on\n")
	git("add", "-A")
	git("commit", "-q", "-m", "Unrelated change")
	base := git("rev-parse", "HEAD")

	if code, out, errOut := cli(t, "validate", "--dir", dir, "--ci", "--event", "pull_request", "--base", base, "--head", head); code != 0 || !strings.Contains(out, `"tag": "v1.0.0"`) {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
	// Outside a pull request, the range must still be exact.
	if code, _, errOut := cli(t, "validate", "--dir", dir, "--base", base, "--head", head); code == 0 || !strings.Contains(errOut, "ancestor") {
		t.Fatalf("%d %s", code, errOut)
	}
}

// inventory credits pull request authors by GitHub handle, and a lookup that fails becomes
// a warning rather than a failed inventory.
func TestInventoryAddsAuthorHandles(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/repos/fabricahq/example/pulls/7":
			_, _ = w.Write([]byte(`{"user":{"login":"octocat"}}`))
		case "/repos/fabricahq/example/commits":
			// octocat has no commits in the previous release.
			if r.URL.Query().Get("sha") != "v1.0.0" || r.URL.Query().Get("author") != "octocat" {
				http.Error(w, "unexpected query", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(api.Close)
	t.Setenv("GITHUB_API_URL", api.URL)
	t.Setenv("GITHUB_TOKEN", "token")

	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Mona Lisa", "GIT_AUTHOR_EMAIL=m@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("remote", "add", "origin", "https://github.com/fabricahq/example.git")
	if err := os.MkdirAll(filepath.Join(dir, ".release-planner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".release-planner/config.yml"), []byte("schema-version: 1\nversion: v0.1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "Initial commit")
	git("tag", "v1.0.0")
	git("commit", "-q", "--allow-empty", "-m", "feat: add login (#7)")
	git("commit", "-q", "--allow-empty", "-m", "fix: typo (#8)")

	code, out, errOut := cli(t, "inventory", "--dir", dir)
	if code != 0 {
		t.Fatalf("%d %s", code, errOut)
	}
	var inv struct {
		Commits []struct {
			PullRequest  int    `json:"pullRequest"`
			Author       string `json:"author"`
			AuthorHandle string `json:"authorHandle"`
			Entry        string `json:"entry"`
		} `json:"commits"`
		Closing         string `json:"closing"`
		NewContributors []struct {
			Handle      string `json:"handle"`
			PullRequest int    `json:"pullRequest"`
			Entry       string `json:"entry"`
		} `json:"newContributors"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(out), &inv); err != nil {
		t.Fatal(err)
	}
	if len(inv.Commits) != 2 || inv.Commits[0].AuthorHandle != "octocat" || inv.Commits[0].Author != "Mona Lisa" || inv.Commits[1].AuthorHandle != "" {
		t.Fatalf("%+v", inv.Commits)
	}
	if inv.Commits[0].Entry != "- feat: add login by @octocat in #7" || inv.Commits[1].Entry != "- fix: typo in #8" ||
		inv.Closing != "**Full Changelog**: https://github.com/fabricahq/example/compare/v1.0.0...<version>" {
		t.Fatalf("entries %+v %q", inv.Commits, inv.Closing)
	}
	if len(inv.NewContributors) != 1 || inv.NewContributors[0].Handle != "octocat" || inv.NewContributors[0].PullRequest != 7 ||
		inv.NewContributors[0].Entry != "- @octocat made their first contribution in #7" {
		t.Fatalf("new contributors %+v", inv.NewContributors)
	}
	if len(inv.Warnings) != 1 || !strings.Contains(inv.Warnings[0], "no author for pull request #8") {
		t.Fatalf("warnings %v", inv.Warnings)
	}

	// Offline, nothing is looked up, and entries go without handles.
	if _, out, _ := cli(t, "inventory", "--dir", dir, "--offline", "--repository", "octo/other"); strings.Contains(out, "octocat") || !strings.Contains(out, `"entry": "- feat: add login in #7"`) || !strings.Contains(out, "octo/other/compare/v1.0.0...<version>") {
		t.Fatalf("offline: %s", out)
	}
}

// A release request warns about a release environment that anyone's branch could publish
// from, but still plans the release.
func TestValidateWarnsAboutAnUnprotectedReleaseEnvironment(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/fabricahq/example/environments/release" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"deployment_branch_policy":null,"protection_rules":[]}`))
	}))
	t.Cleanup(api.Close)
	summary := filepath.Join(t.TempDir(), "summary")
	t.Setenv("GITHUB_API_URL", api.URL)
	t.Setenv("GITHUB_TOKEN", "token")
	t.Setenv("GITHUB_REPOSITORY", "fabricahq/example")
	t.Setenv("GITHUB_STEP_SUMMARY", summary)
	t.Setenv("GITHUB_OUTPUT", filepath.Join(t.TempDir(), "output"))

	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	write(".release-planner/config.yml", "schema-version: 1\nversion: v0.1.0\nfirst-version: v1.0.0\n")
	git("add", "-A")
	git("commit", "-q", "-m", "Adopt Release Planner")
	base := git("rev-parse", "HEAD")
	write("_releases/v1.0.0.md", "The first release.\n")
	git("add", "-A")
	git("commit", "-q", "-m", "Release v1.0.0")

	out := filepath.Join(t.TempDir(), "plan.json")
	code, stdout, errOut := cli(t, "validate", "--dir", dir, "--ci", "--event", "push", "--base", base, "--head", "HEAD", "--out", out)
	if code != 0 {
		t.Fatalf("%d %s", code, errOut)
	}
	if !strings.Contains(stdout, "::warning title=Release environment::The release environment has no deployment branch rule") {
		t.Fatalf("no annotation: %q", stdout)
	}
	if data, _ := os.ReadFile(summary); !strings.Contains(string(data), "no deployment branch rule") {
		t.Fatalf("summary: %s", data)
	}
}

// Locally, notes that break a rule fail validate with every finding, so the agent fixes them.
// In the workflow, they're warnings, and the release still goes ahead.
func TestValidateFailsLocallyAndWarnsInCI(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	write(".release-planner/config.yml", "schema-version: 1\nversion: v0.1.0\nfirst-version: v1.0.0\n")
	git("add", "-A")
	git("commit", "-q", "-m", "Adopt Release Planner (#1)")
	base := git("rev-parse", "HEAD")
	good := "## ✨ New Features\n\nEverything. (#1)\n\n## Pull Requests\n\n- Adopt Release Planner in #1\n\nThis is the first release. Browse the source at [v1.0.0](https://github.com/o/r/tree/v1.0.0).\n"
	write("_releases/v1.0.0.md", good)
	git("add", "-A")
	git("commit", "-q", "-m", "Release v1.0.0")
	if code, out, errOut := cli(t, "validate", "--dir", dir, "--base", base); code != 0 || !strings.Contains(out, `"tag": "v1.0.0"`) {
		t.Fatalf("%d %s %s", code, out, errOut)
	}

	write("_releases/v1.0.0.md", "## ✨ New Features\n\n"+good)
	git("commit", "-q", "-am", "Break a rule")
	code, out, errOut := cli(t, "validate", "--dir", dir, "--base", base)
	if code != 1 || out != "" || !strings.Contains(errOut, "_releases/v1.0.0.md breaks release notes rules:\n  line 1: no-empty-heading:") || !strings.Contains(errOut, "line 3: no-duplicate-heading:") {
		t.Fatalf("local: %d %q %s", code, out, errOut)
	}

	summary := filepath.Join(t.TempDir(), "summary")
	t.Setenv("GITHUB_REPOSITORY", "o/r")
	t.Setenv("GITHUB_STEP_SUMMARY", summary)
	t.Setenv("GITHUB_OUTPUT", filepath.Join(t.TempDir(), "output"))
	planFile := filepath.Join(t.TempDir(), "plan.json")
	code, out, errOut = cli(t, "validate", "--dir", dir, "--ci", "--event", "push", "--base", base, "--head", "HEAD", "--out", planFile)
	if code != 0 || !strings.Contains(out, "::warning file=_releases/v1.0.0.md,line=1,title=no-empty-heading::") || !strings.Contains(out, "Validated v1.0.0") {
		t.Fatalf("ci: %d %s %s", code, out, errOut)
	}
	if data, _ := os.ReadFile(summary); !strings.Contains(string(data), "- line 3: no-duplicate-heading:") {
		t.Fatalf("summary: %s", data)
	}
	if data, _ := os.ReadFile(planFile); !strings.Contains(string(data), `"tag": "v1.0.0"`) {
		t.Fatalf("plan: %s", data)
	}

	// Rules turned off are skipped.
	write(".release-planner/config.yml", "schema-version: 1\nversion: v0.1.0\nfirst-version: v1.0.0\nrelease-notes-rules:\n  no-empty-heading: off\n  no-duplicate-heading: off\n")
	if code, _, errOut := cli(t, "validate", "--dir", dir, "--base", base); code != 0 {
		t.Fatalf("rules off: %d %s", code, errOut)
	}
}

func TestValidateListsRules(t *testing.T) {
	code, out, _ := cli(t, "validate", "--rules")
	if code != 0 || !strings.Contains(out, "list-every-change") || !strings.Contains(out, "no-long-heading             ## and ### headings are at most 80 characters.") {
		t.Fatalf("%d %s", code, out)
	}
}
