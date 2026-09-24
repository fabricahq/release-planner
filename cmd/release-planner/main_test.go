package main

import (
	"bytes"
	"context"
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
	config := "schema-version: 1\nversion: v0.1.0\nfirst-version: v1.0.0\nvalidate:\n  run: make test\n"
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
	if code, out, _ := cli(t, "guide", "--default-style"); code != 0 || !strings.HasPrefix(out, "- Open with one or two sentences") {
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

// A release pull request whose base branch moved on after it branched still plans the
// notes it adds, rather than failing because the base tip isn't in its history.
func TestPlanPullRequestAfterBaseMoved(t *testing.T) {
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
	write("releases/v1.0.0.md", "The first release.\n")
	git("add", "-A")
	git("commit", "-q", "-m", "Release v1.0.0")
	head := git("rev-parse", "HEAD")
	git("checkout", "-q", "main")
	write("README.md", "Moved on\n")
	git("add", "-A")
	git("commit", "-q", "-m", "Unrelated change")
	base := git("rev-parse", "HEAD")

	if code, out, errOut := cli(t, "plan", "--dir", dir, "--ci", "--event", "pull_request", "--base", base, "--head", head); code != 0 || !strings.Contains(out, `"tag": "v1.0.0"`) {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
	// Outside a pull request, the range must still be exact.
	if code, _, errOut := cli(t, "plan", "--dir", dir, "--base", base, "--head", head); code == 0 || !strings.Contains(errOut, "ancestor") {
		t.Fatalf("%d %s", code, errOut)
	}
}

// A release request warns about a release environment that anyone's branch could publish
// from, but still plans the release.
func TestPlanWarnsAboutAnUnprotectedReleaseEnvironment(t *testing.T) {
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
	write("releases/v1.0.0.md", "The first release.\n")
	git("add", "-A")
	git("commit", "-q", "-m", "Release v1.0.0")

	out := filepath.Join(t.TempDir(), "plan.json")
	code, stdout, errOut := cli(t, "plan", "--dir", dir, "--ci", "--event", "push", "--base", base, "--head", "HEAD", "--out", out)
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
