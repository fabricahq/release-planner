package generate

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/fabricahq/release-planner/internal/config"
)

func cfg(t *testing.T, version string) config.Config {
	t.Helper()
	c, err := config.Parse([]byte("schema-version: 1\nversion: "+version+"\nfirst-version: v1.0.0\nrelease-checks:\n  go: '1.27.x'\n  run: |\n    go install example.com/tool@v1\n    tool check\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func read(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func put(t *testing.T, root, name, body string) {
	t.Helper()
	file := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// snapshot captures every file under root, to prove an operation changed nothing.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		files[rel] = read(t, root, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func equalSnapshots(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func actions(changes []Change) map[string]string {
	m := map[string]string{}
	for _, c := range changes {
		m[c.Path] = c.Action
	}
	return m
}

func problemFor(t *testing.T, err error, path, want string) {
	t.Helper()
	var problems Problems
	if !errors.As(err, &problems) {
		t.Fatalf("got %v, want problems", err)
	}
	for _, p := range problems {
		if p.Path == path && strings.Contains(p.Reason, want) {
			return
		}
	}
	t.Fatalf("no problem for %s containing %q in:\n%v", path, want, err)
}

func TestInstallIsIdempotent(t *testing.T) {
	root := t.TempDir()
	c := cfg(t, "v0.2.0")
	changes, err := Install(root, c, false)
	if err != nil {
		t.Fatal(err)
	}
	got := actions(changes)
	for _, p := range []string{WorkflowPath, AgentsSkillPath, ClaudeSkillPath, config.Policy} {
		if got[p] != "created" {
			t.Errorf("%s: %s", p, got[p])
		}
	}
	if got[AgentsPath] != "added" {
		t.Errorf("AGENTS.md: %s", got[AgentsPath])
	}
	if err := Check(root, c); err != nil {
		t.Fatalf("check after install: %v", err)
	}

	before := snapshot(t, root)
	changes, err = Install(root, c, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range changes {
		if ch.Action != "unchanged" {
			t.Errorf("second install changed %s: %s", ch.Path, ch.Action)
		}
	}
	if !equalSnapshots(before, snapshot(t, root)) {
		t.Fatal("second install changed the disk")
	}
}

// A notes directory with YAML-significant characters still produces a workflow that parses
// back to the same path filters.
func TestWorkflowQuotesNotesDirectory(t *testing.T) {
	c, err := config.Parse([]byte("schema-version: 1\nversion: v0.2.0\nrelease-notes-dir: \"team's releases\"\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := Install(root, c, false); err != nil {
		t.Fatal(err)
	}
	var wf struct {
		On struct {
			Push        struct{ Branches, Paths []string }
			PullRequest struct{ Paths []string } `yaml:"pull_request"`
		}
	}
	if err := yaml.Unmarshal([]byte(read(t, root, WorkflowPath)), &wf); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(wf.On.Push.Paths, []string{"team's releases/v*.md"}) || !slices.Equal(wf.On.Push.Branches, []string{"main"}) || wf.On.PullRequest.Paths[0] != "team's releases/**" {
		t.Fatalf("%+v", wf.On)
	}
}

func TestWorkflowRendersTheConfiguredSettings(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root, cfg(t, "v0.2.0"), false); err != nil {
		t.Fatal(err)
	}
	wf := read(t, root, WorkflowPath)
	for _, want := range []string{
		"# release-planner:generated v0.2.0 sha256:",
		"RELEASE_PLANNER_VERSION: v0.2.0\n",
		"gh attestation verify SHA256SUMS --repo fabricahq/release-planner\n",
		"run: release-planner check\n",
		"paths: ['_releases/v*.md']\n",
		"go-version: '1.27.x'",
		"          go install example.com/tool@v1\n          tool check\n",
		"environment: release",
		"actions: read # to check the environments' settings and find the pull request's run\n",
		`release-planner validate --ci --base "$BASE" --head "$HEAD" --head-repository "$HEAD_REPOSITORY"`,
		`release-planner validate --ci --merged "$MERGED"`,
		"  release-checks:\n    needs: validate\n    if: needs.validate.outputs.build == 'true'\n",
		"needs: [validate, release-checks]\n",
		"${{ needs.validate.outputs.commit }}",
		"merged-commit:",
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("workflow lacks %q", want)
		}
	}
	if strings.Contains(wf, "setup-node") || strings.Contains(wf, "setup-python") {
		t.Error("workflow sets up tools the config did not ask for")
	}
	skill := read(t, root, AgentsSkillPath)
	if !strings.HasPrefix(skill, "---\nname: release\n") || !strings.Contains(skill, "release-planner/v0.2.0/install.sh | sh -s -- --version v0.2.0") {
		t.Errorf("skill:\n%s", skill)
	}
	if skill != read(t, root, ClaudeSkillPath) {
		t.Error("the two skill copies differ")
	}
}

func TestSectionKeepsSurroundingText(t *testing.T) {
	root := t.TempDir()
	put(t, root, AgentsPath, "# Guide\n\nRun make test.\n\n## Releases\nWe release on Fridays.\n")
	if _, err := Install(root, cfg(t, "v0.1.0"), false); err != nil {
		t.Fatal(err)
	}
	agents := read(t, root, AgentsPath)
	if !strings.HasPrefix(agents, "# Guide\n\nRun make test.\n\n## Releases\nWe release on Fridays.\n\n<!-- release-planner:begin v0.1.0 ") {
		t.Fatalf("appended section:\n%s", agents)
	}

	// Text after the section, added later, survives an upgrade untouched.
	put(t, root, AgentsPath, agents+"\n## Style\nPrefer small PRs.\n")
	changes, err := Install(root, cfg(t, "v0.2.0"), false)
	if err != nil {
		t.Fatal(err)
	}
	if actions(changes)[AgentsPath] != "updated" {
		t.Fatal(changes)
	}
	agents = read(t, root, AgentsPath)
	if !strings.HasPrefix(agents, "# Guide\n\nRun make test.\n\n## Releases\nWe release on Fridays.\n\n<!-- release-planner:begin v0.2.0 ") ||
		!strings.HasSuffix(agents, "<!-- release-planner:end -->\n\n## Style\nPrefer small PRs.\n") {
		t.Fatalf("upgraded section:\n%s", agents)
	}
}

func TestRefusesHandEdits(t *testing.T) {
	root := t.TempDir()
	c := cfg(t, "v0.2.0")
	if _, err := Install(root, c, false); err != nil {
		t.Fatal(err)
	}
	put(t, root, AgentsPath, strings.Replace(read(t, root, AgentsPath), "Never tag", "Ask @josh first. Never tag", 1))
	put(t, root, WorkflowPath, read(t, root, WorkflowPath)+"# tweak\n")
	before := snapshot(t, root)

	_, err := Install(root, c, false)
	problemFor(t, err, AgentsPath, "edited")
	problemFor(t, err, WorkflowPath, "edited")
	if !equalSnapshots(before, snapshot(t, root)) {
		t.Fatal("a refused install changed the disk")
	}
	problemFor(t, Check(root, c), AgentsPath, "edited")

	changes, err := Install(root, c, true)
	if err != nil {
		t.Fatal(err)
	}
	if a := actions(changes); a[AgentsPath] != "replaced" || a[WorkflowPath] != "replaced" {
		t.Fatal(changes)
	}
	if err := Check(root, c); err != nil {
		t.Fatalf("check after --force: %v", err)
	}
}

func TestRefusesForeignWorkflow(t *testing.T) {
	root := t.TempDir()
	put(t, root, WorkflowPath, "name: Mine\n")
	_, err := Install(root, cfg(t, "v0.2.0"), false)
	problemFor(t, err, WorkflowPath, "not written by release-planner")
	if read(t, root, WorkflowPath) != "name: Mine\n" {
		t.Fatal("foreign workflow was changed")
	}
}

func TestRejectsMalformedMarkers(t *testing.T) {
	for name, body := range map[string]string{
		"begin only": "# Guide\n<!-- release-planner:begin v0.1.0 sha256:0123456789abcdef -->\n## Releases\n",
		"end first":  "<!-- release-planner:end -->\n<!-- release-planner:begin v0.1.0 sha256:0123456789abcdef -->\n",
		"two begins": "<!-- release-planner:begin v0.1.0 sha256:0123456789abcdef -->\n<!-- release-planner:begin v0.1.0 sha256:0123456789abcdef -->\n<!-- release-planner:end -->\n",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			put(t, root, AgentsPath, body)
			_, err := Install(root, cfg(t, "v0.2.0"), true)
			problemFor(t, err, AgentsPath, "release-planner")
			if read(t, root, AgentsPath) != body {
				t.Fatal("malformed file was changed")
			}
		})
	}
}

func TestCheckFindsDrift(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root, cfg(t, "v0.2.0"), false); err != nil {
		t.Fatal(err)
	}
	problemFor(t, Check(root, cfg(t, "v0.3.0")), WorkflowPath, "generated by v0.2.0, but .release-planner/config.yml pins v0.3.0")

	changed := cfg(t, "v0.2.0")
	changed.ReleaseChecks.Run = "make test"
	problemFor(t, Check(root, changed), WorkflowPath, "differs from what .release-planner/config.yml generates")

	if err := os.Remove(filepath.Join(root, ClaudeSkillPath)); err != nil {
		t.Fatal(err)
	}
	problemFor(t, Check(root, cfg(t, "v0.2.0")), ClaudeSkillPath, "missing")
}

func TestPolicyIsSeededOnce(t *testing.T) {
	root := t.TempDir()
	c := cfg(t, "v0.2.0")
	if _, err := Install(root, c, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, root, config.Policy), "The first release is v1.0.0.") {
		t.Fatal("policy starter")
	}
	put(t, root, config.Policy, "Our policy.\n")
	if _, err := Install(root, c, false); err != nil {
		t.Fatal(err)
	}
	if read(t, root, config.Policy) != "Our policy.\n" {
		t.Fatal("install rewrote the policy")
	}
	if err := Check(root, c); err != nil {
		t.Fatalf("check judged the policy: %v", err)
	}
}

func TestUninstall(t *testing.T) {
	root := t.TempDir()
	put(t, root, AgentsPath, "# Guide\n\nRun make test.\n")
	c := cfg(t, "v0.2.0")
	if _, err := Install(root, c, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(root, false); err != nil {
		t.Fatal(err)
	}
	if got := read(t, root, AgentsPath); got != "# Guide\n\nRun make test.\n" {
		t.Fatalf("AGENTS.md after uninstall: %q", got)
	}
	for _, p := range []string{WorkflowPath, AgentsSkillPath, ClaudeSkillPath, ".agents", ".claude"} {
		if _, err := os.Stat(filepath.Join(root, p)); !os.IsNotExist(err) {
			t.Errorf("%s still exists", p)
		}
	}
	if _, err := os.Stat(filepath.Join(root, config.Policy)); err != nil {
		t.Error("uninstall removed the repository's policy")
	}
}

func TestReleaseChecksAcceptOnlyCallableWorkflows(t *testing.T) {
	parse := func(yaml string) config.Config {
		c, err := config.Parse([]byte("schema-version: 1\nversion: v0.2.0\n"+yaml), "")
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	root := t.TempDir()
	if _, err := Install(root, parse(""), false); err != nil {
		t.Fatal(err)
	}
	wf := read(t, root, WorkflowPath)
	if strings.Contains(wf, "release-checks:") || !strings.Contains(wf, "needs: [validate]\n") || strings.Contains(wf, "  attest:") {
		t.Errorf("no checks configured, but the workflow runs them:\n%s", wf)
	}

	root = t.TempDir()
	withCI := parse("release-checks:\n  workflow: ci.yml\n")
	_, err := Install(root, withCI, false)
	problemFor(t, err, ".github/workflows/ci.yml", "missing")

	put(t, root, ".github/workflows/ci.yml", "on:\n  pull_request:\n")
	_, err = Install(root, withCI, false)
	problemFor(t, err, ".github/workflows/ci.yml", "cannot be called")

	put(t, root, ".github/workflows/ci.yml", "on:\n  workflow_call:\n")
	_, err = Install(root, withCI, false)
	problemFor(t, err, ".github/workflows/ci.yml", "has no ref input")

	put(t, root, ".github/workflows/ci.yml", "on:\n  workflow_call:\n    inputs:\n      ref:\n        type: boolean\n")
	_, err = Install(root, withCI, false)
	problemFor(t, err, ".github/workflows/ci.yml", "without type: string")

	put(t, root, ".github/workflows/ci.yml", "on:\n  workflow_call:\n    inputs:\n      ref:\n        type: string\n      target:\n        type: string\n        required: true\n      mode:\n        type: string\n        required: true\n        default: fast\n")
	_, err = Install(root, withCI, false)
	problemFor(t, err, ".github/workflows/ci.yml", "can't supply (target)")

	put(t, root, ".github/workflows/ci.yml", "on:\n  pull_request:\n  workflow_call:\n    inputs:\n      ref:\n        type: string\n")
	if _, err := Install(root, withCI, false); err != nil {
		t.Fatal(err)
	}
	wf = read(t, root, WorkflowPath)
	for _, want := range []string{"uses: ./.github/workflows/ci.yml\n", "ref: ${{ needs.validate.outputs.commit }}\n", "secrets: inherit\n", "  release-checks:\n    needs: validate\n", "needs: [validate, release-checks]\n"} {
		if !strings.Contains(wf, want) {
			t.Errorf("workflow lacks %q", want)
		}
	}
	if err := Check(root, withCI); err != nil {
		t.Fatal(err)
	}
}

func TestCommitPinBuildsFromSource(t *testing.T) {
	root := t.TempDir()
	sha := "0123456789abcdef0123456789abcdef01234567"
	if _, err := Install(root, cfg(t, sha), false); err != nil {
		t.Fatal(err)
	}
	wf := read(t, root, WorkflowPath)
	if !strings.Contains(wf, `go install "github.com/fabricahq/release-planner/cmd/release-planner@$RELEASE_PLANNER_VERSION"`) || strings.Contains(wf, "gh release download") {
		t.Errorf("commit pin workflow:\n%s", wf)
	}
	if !strings.Contains(read(t, root, AgentsPath), "go install github.com/fabricahq/release-planner/cmd/release-planner@"+sha) {
		t.Error("AGENTS.md lacks the source install command")
	}
}

func TestReleaseAssetsNeedsTheReleaseInputs(t *testing.T) {
	c, err := config.Parse([]byte("schema-version: 1\nversion: v0.2.0\nrelease-assets:\n  workflow: build.yml\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	_, err = Install(root, c, false)
	problemFor(t, err, ".github/workflows/build.yml", "missing; release-assets.workflow")

	put(t, root, ".github/workflows/build.yml", "on:\n  workflow_call:\n    inputs:\n      ref:\n        type: string\n      tag:\n        type: string\n")
	_, err = Install(root, c, false)
	problemFor(t, err, ".github/workflows/build.yml", "has no version input, but the Release workflow passes it; add a workflow_call trigger with string inputs named ref, tag, and version")

	put(t, root, ".github/workflows/build.yml", "on:\n  workflow_call:\n    inputs:\n      ref:\n        type: string\n      tag:\n        type: number\n      version:\n        type: string\n")
	_, err = Install(root, c, false)
	problemFor(t, err, ".github/workflows/build.yml", "declares tag without type: string")

	// The Release workflow downloads the assets by the ID of the upload the workflow made.
	put(t, root, ".github/workflows/build.yml", strings.Replace(buildWorkflow, "    outputs:\n      artifact-id:\n        value: ${{ jobs.build.outputs.artifact-id }}\n", "", 1))
	_, err = Install(root, c, false)
	problemFor(t, err, ".github/workflows/build.yml", "has no artifact-id output")

	put(t, root, ".github/workflows/build.yml", buildWorkflow)
	if _, err := Install(root, c, false); err != nil {
		t.Fatal(err)
	}
	if err := Check(root, c); err != nil {
		t.Fatal(err)
	}
}

// buildWorkflow and checksWorkflow are minimal workflows the Release workflow can call.
const (
	buildWorkflow = `name: Build release
on:
  workflow_call:
    inputs:
      ref:
        type: string
        required: true
      tag:
        type: string
        required: true
      version:
        type: string
        required: true
    outputs:
      artifact-id:
        value: ${{ jobs.build.outputs.artifact-id }}
permissions:
  contents: read
jobs:
  build:
    runs-on: ubuntu-latest
    outputs:
      artifact-id: ${{ steps.upload.outputs.artifact-id }}
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          ref: ${{ inputs.ref }}
          persist-credentials: false
      - run: mkdir dist && echo "$VERSION" > dist/version.txt
        env:
          VERSION: ${{ inputs.version }}
      - id: upload
        uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7.0.1
        with:
          name: release-assets
          path: dist/
`
	checksWorkflow = `name: Release checks
on:
  workflow_call:
    inputs:
      ref:
        type: string
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          ref: ${{ inputs.ref }}
          persist-credentials: false
      - run: make release-checks
`
)

// combinations are the configurations the generated workflow must handle, by name.
var combinations = map[string]string{
	"plain":                 "",
	"checks-script":         "release-checks:\n  go: '1.27.x'\n  run: go test ./...\n",
	"checks-workflow":       "release-checks:\n  workflow: release-checks.yml\n",
	"assets":                "release-assets:\n  workflow: build-release.yml\n",
	"post-publish-dispatch": "post-publish:\n  - repository: fabricahq/homebrew-tap\n    workflow: update-code-rules.yml\n",
	"post-publish-here":     "post-publish:\n  - workflow: deploy.yml\n",
	"all": "release-checks:\n  workflow: release-checks.yml\nrelease-assets:\n  workflow: build-release.yml\npre-publish:\n  - workflow: migrate-database.yml\n" +
		"post-publish:\n  - workflow: deploy.yml\n    prereleases: true\n  - repository: fabricahq/homebrew-tap\n    workflow: update-code-rules.yml\n  - repository: fabricahq/scoop-bucket\n    workflow: update.yml\n",
	"script-and-assets": "release-checks:\n  run: make smoke\nrelease-assets:\n  workflow: build-release.yml\n",
	"pre-publish":       "pre-publish:\n  - workflow: migrate-database.yml\n",
	"two-pre-publish":   "pre-publish:\n  - workflow: migrate-database.yml\n  - workflow: warm-caches.yml\n",
	"pre-publish-and-assets": "release-checks:\n  run: make smoke\nrelease-assets:\n  workflow: build-release.yml\npre-publish:\n  - workflow: migrate-database.yml\n" +
		"post-publish:\n  - repository: fabricahq/homebrew-tap\n    workflow: update-code-rules.yml\n",
}

type job struct {
	Name     string `yaml:"name"`
	Needs    any    `yaml:"needs"`
	Strategy struct {
		FailFast *bool               `yaml:"fail-fast"`
		Matrix   map[string][]string `yaml:"matrix"`
	} `yaml:"strategy"`
	If          string            `yaml:"if"`
	Uses        string            `yaml:"uses"`
	Secrets     any               `yaml:"secrets"`
	Environment string            `yaml:"environment"`
	Permissions map[string]string `yaml:"permissions"`
	Concurrency map[string]any    `yaml:"concurrency"`
	Steps       []step            `yaml:"steps"`
	With        map[string]string `yaml:"with"`
	Outputs     map[string]string `yaml:"outputs"`
}

type step struct {
	ID   string            `yaml:"id"`
	Name string            `yaml:"name"`
	Uses string            `yaml:"uses"`
	If   string            `yaml:"if"`
	With map[string]string `yaml:"with"`
	Env  map[string]string `yaml:"env"`
	Run  string            `yaml:"run"`
}

// named returns the job's step with the name or ID, or fails the test.
func (j job) named(t *testing.T, name string) step {
	t.Helper()
	for _, s := range j.Steps {
		if s.Name == name || s.ID == name {
			return s
		}
	}
	t.Fatalf("no step %q in %+v", name, j.Steps)
	return step{}
}

// installCombination renders the workflow for a combination, with the workflows it calls.
func installCombination(t *testing.T, root, extra string) map[string]job {
	t.Helper()
	c, err := config.Parse([]byte("schema-version: 1\nversion: v0.4.0\n"+extra), "")
	if err != nil {
		t.Fatal(err)
	}
	put(t, root, ".github/workflows/build-release.yml", buildWorkflow)
	put(t, root, ".github/workflows/release-checks.yml", checksWorkflow)
	// A test may have written its own pre-publish workflow. The second one runs in another
	// environment, which a pre-publish workflow may.
	if _, err := os.Stat(filepath.Join(root, ".github/workflows/migrate-database.yml")); err != nil {
		put(t, root, ".github/workflows/migrate-database.yml", migrateWorkflow)
	}
	put(t, root, ".github/workflows/warm-caches.yml", strings.NewReplacer("Migrate the database", "Warm the caches", "production", "staging").Replace(migrateWorkflow))
	put(t, root, ".github/workflows/deploy.yml", strings.NewReplacer("Migrate the database", "Deploy", "make migrate", "make deploy").Replace(migrateWorkflow))
	if _, err := Install(root, c, false); err != nil {
		t.Fatal(err)
	}
	var wf struct {
		Jobs map[string]job `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(read(t, root, WorkflowPath)), &wf); err != nil {
		t.Fatal(err)
	}
	return wf.Jobs
}

func TestWorkflowCombinations(t *testing.T) {
	for name, extra := range combinations {
		t.Run(name, func(t *testing.T) {
			jobs := installCombination(t, t.TempDir(), extra)
			checks := strings.Contains(extra, "release-checks")
			assets := strings.Contains(extra, "release-assets")
			c, err := config.Parse([]byte("schema-version: 1\nversion: v0.4.0\n"+extra), "")
			if err != nil {
				t.Fatal(err)
			}
			prePublish := strings.Contains(extra, "pre-publish")
			// Each pre-publish workflow runs in a job of its own, numbered in the config's order.
			var pre []string
			for i, w := range []string{"migrate-database.yml", "warm-caches.yml"} {
				if strings.Contains(extra, "workflow: "+w) {
					pre = append(pre, fmt.Sprintf("pre-publish-%d", i+1))
				}
			}
			want := []string{"validate", "publish", "report"}
			want = append(want, pre...)
			if checks {
				want = append(want, "release-checks")
			}
			if assets {
				want = append(want, "release-assets", "attest", "attest-release")
			}
			// Each post-publish workflow runs in a job of its own too.
			for i := range c.PostPublish {
				want = append(want, fmt.Sprintf("post-publish-%d", i+1))
			}
			var got []string
			for id := range jobs {
				got = append(got, id)
			}
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Fatalf("jobs %v, want %v", got, want)
			}

			// Only publish can write to the repository, only after the merge, in the release environment.
			for id, j := range jobs {
				if j.Permissions["contents"] == "write" && id != "publish" {
					t.Errorf("%s can write contents", id)
				}
				if j.Permissions["id-token"] == "write" && id != "attest" && id != "attest-release" && !strings.HasPrefix(id, "pre-publish-") && !(strings.HasPrefix(id, "post-publish-") && j.Uses != "") {
					t.Errorf("%s can mint OIDC tokens", id)
				}
			}
			publish := jobs["publish"]
			if publish.Environment != "release" || !strings.Contains(publish.If, "github.event_name != 'pull_request'") || !strings.Contains(publish.If, "!contains(needs.*.result, 'failure')") {
				t.Errorf("publish: %+v", publish)
			}
			build := []any{"validate"}
			for _, id := range []string{"release-checks", "release-assets", "attest"} {
				if _, ok := jobs[id]; ok {
					build = append(build, id)
				}
			}
			wantNeeds := slices.Clone(build)
			for _, id := range pre {
				wantNeeds = append(wantNeeds, id)
			}
			if !slices.Equal(publish.Needs.([]any), wantNeeds) {
				t.Errorf("publish needs %v, want %v", publish.Needs, wantNeeds)
			}
			// A release publishes only after every pre-publish workflow succeeded, in this attempt
			// or one that Re-run failed jobs reuses; a skipped or cancelled one blocks it.
			guard := map[int]string{
				1: "(needs.validate.outputs.tag == '' || needs.pre-publish-1.result == 'success')",
				2: "(needs.validate.outputs.tag == '' || (needs.pre-publish-1.result == 'success' && needs.pre-publish-2.result == 'success'))",
			}[len(pre)]
			if prePublish != (guard != "" && strings.Contains(publish.If, guard)) {
				t.Errorf("publish runs if %q", publish.If)
			}
			// validate tells the report why a run stopped on purpose, even when it fails.
			for _, output := range []string{"withdrawn", "waiting-for", "waiting-for-file"} {
				if got := jobs["validate"].Outputs[output]; got != "${{ steps.validate.outputs."+output+" }}" {
					t.Errorf("validate outputs %s as %q", output, got)
				}
			}
			// publish tells the report what it did.
			if publish.Outputs["release"] != "${{ steps.publish.outputs.release }}" || publish.Outputs["notes"] != "${{ steps.publish.outputs.notes }}" ||
				publish.named(t, "publish").Name != "Publish the approved release" {
				t.Errorf("publish outputs %v", publish.Outputs)
			}
			// The report learns each hook job, with its workflow's own name.
			step := jobs["report"].Steps[len(jobs["report"].Steps)-1]
			if (prePublish || len(c.PostPublish) > 0) != strings.Contains(step.Run, ` --hooks "$HOOKS" `) {
				t.Errorf("report runs %q", step.Run)
			}
			var hooks []map[string]any
			if prePublish {
				if err := json.Unmarshal([]byte(step.Env["HOOKS"]), &hooks); err != nil || len(hooks) != len(pre)+len(c.PostPublish) ||
					!maps.Equal(hooks[0], map[string]any{"job": "pre-publish-1", "name": "pre-publish (migrate-database.yml)", "when": "pre-publish", "workflow": "migrate-database.yml", "title": "Migrate the database"}) {
					t.Errorf("HOOKS %v: %v", step.Env["HOOKS"], err)
				}
			}
			// The pre-publish workflows run in parallel, after the build.
			for i, id := range pre {
				hook, workflow := jobs[id], []string{"migrate-database.yml", "warm-caches.yml"}[i]
				if hook.Name != "pre-publish ("+workflow+")" || hook.Uses != "./.github/workflows/"+workflow || hook.Secrets != nil || hook.Environment != "" ||
					!maps.Equal(hook.Permissions, map[string]string{"contents": "read", "id-token": "write"}) ||
					!slices.Equal(hook.Needs.([]any), build) ||
					!maps.Equal(hook.With, map[string]string{"ref": "${{ needs.validate.outputs.commit }}", "tag": "${{ needs.validate.outputs.tag }}", "version": "${{ needs.validate.outputs.version }}"}) ||
					!maps.Equal(hook.Concurrency, map[string]any{"group": "release-pre-publish (" + workflow + ")", "cancel-in-progress": false, "queue": "max"}) {
					t.Errorf("%s: %+v", id, hook)
				}
				for _, want := range []string{"!cancelled()", "github.event_name != 'pull_request'", "needs.validate.outputs.publish == 'true'", "needs.validate.outputs.tag != ''", "!contains(needs.*.result, 'failure')", "!contains(needs.*.result, 'cancelled')"} {
					if !strings.Contains(hook.If, want) {
						t.Errorf("pre-publish runs if %q, without %q", hook.If, want)
					}
				}
			}
			// Publications wait their turn in order; without a queue, a third waiting run would
			// cancel the second.
			if !maps.Equal(publish.Concurrency, map[string]any{"group": "release-publication", "cancel-in-progress": false, "queue": "max"}) {
				t.Errorf("publish concurrency %v", publish.Concurrency)
			}
			// Publish reads the release branch's history to refuse a withdrawn request.
			if checkout := publish.named(t, "Check out the release branch's history"); !strings.HasPrefix(checkout.Uses, "actions/checkout@") || checkout.With["fetch-depth"] != "0" || checkout.With["persist-credentials"] != "false" {
				t.Errorf("publish starts with %+v", checkout)
			}
			// With a release App, publish writes with an App token that can also create a tag on a
			// commit that changes workflow files; reads keep the workflow's token.
			token := publish.named(t, "token")
			if !strings.HasPrefix(token.Uses, "actions/create-github-app-token@bcd2ba49218906704ab6c1aa796996da409d3eb1") || token.If != "steps.app.outputs.use == 'true'" ||
				token.With["client-id"] != "${{ vars.RELEASE_APP_CLIENT_ID }}" || token.With["private-key"] != "${{ secrets.RELEASE_APP_PRIVATE_KEY }}" ||
				token.With["permission-contents"] != "write" || token.With["permission-workflows"] != "write" || token.With["owner"] != "" || token.With["repositories"] != "" {
				t.Errorf("publish mints %+v", token)
			}
			if env := publish.Steps[len(publish.Steps)-1].Env; env["GITHUB_TOKEN"] != "${{ github.token }}" || env["RELEASE_TOKEN"] != "${{ steps.token.outputs.token }}" {
				t.Errorf("publish runs with %v", env)
			}
			run := publish.Steps[len(publish.Steps)-1].Run
			if assets != strings.Contains(run, "--assets \"$RUNNER_TEMP/release-assets\" --signer-workflow .github/workflows/release-planner.yml") || !strings.Contains(run, "--built-plan") {
				t.Errorf("publish runs %q", run)
			}
			if assets != (publish.Permissions["attestations"] == "read") {
				t.Errorf("publish permissions %v", publish.Permissions)
			}
			for _, step := range publish.Steps {
				if step.With["run-id"] != "" && (step.With["run-id"] != "${{ needs.validate.outputs.build-run }}" || step.If != "needs.validate.outputs.tag != ''") {
					t.Errorf("publish downloads from %+v", step)
				}
			}

			// Every download names the artifact the producing job uploaded, by its ID, so no
			// other job can swap it by uploading under the same name.
			for id, j := range jobs {
				for _, step := range j.Steps {
					if strings.HasPrefix(step.Uses, "actions/download-artifact@") && (step.With["name"] != "" || step.With["artifact-ids"] == "") {
						t.Errorf("%s downloads %+v, not by the producer's artifact ID", id, step.With)
					}
				}
			}
			for _, step := range jobs["publish"].Steps {
				if step.With["path"] == "${{ runner.temp }}/plan" && step.With["artifact-ids"] != "${{ needs.validate.outputs.plan-artifact }}" {
					t.Errorf("publish downloads the plan %q", step.With["artifact-ids"])
				}
			}
			if assets {
				attest := jobs["attest"]
				wantNeeds := []any{"validate", "release-assets"}
				if checks {
					// No repository code runs in a pull request's run after attest records the IDs.
					wantNeeds = append(wantNeeds, "release-checks")
				}
				if !slices.Equal(attest.Needs.([]any), wantNeeds) || attest.Steps[0].With["artifact-ids"] != "${{ needs.release-assets.outputs.artifact-id }}" {
					t.Errorf("attest: %+v", attest)
				}
				binding := attest.Steps[len(attest.Steps)-1]
				if !strings.HasPrefix(binding.Uses, "actions/upload-artifact@") || binding.With["name"] != "release-binding-${{ needs.validate.outputs.plan-artifact }}-${{ needs.release-assets.outputs.artifact-id }}" || binding.With["overwrite"] != "false" {
					t.Errorf("attest binds the artifacts with %+v", binding)
				}
			}

			// Checks and builds run only when validate says this run builds the release.
			for _, id := range []string{"release-checks", "release-assets"} {
				if j, ok := jobs[id]; ok && j.If != "needs.validate.outputs.build == 'true'" {
					t.Errorf("%s runs if %q", id, j.If)
				}
			}
			if assets {
				build := jobs["release-assets"]
				if build.Uses != "./.github/workflows/build-release.yml" || build.Secrets != nil || !maps.Equal(build.Permissions, map[string]string{"contents": "read"}) {
					t.Errorf("release-assets: %+v", build)
				}
				attest := jobs["attest"]
				if !maps.Equal(attest.Permissions, map[string]string{"contents": "read", "id-token": "write", "attestations": "write"}) ||
					!strings.HasPrefix(attest.Steps[1].Uses, "actions/attest-build-provenance@4d101475d8b20a2381f78447822ac1eab6504dd8") {
					t.Errorf("attest: %+v", attest)
				}
				// After publication, the published files are attested again from the release branch.
				again := jobs["attest-release"]
				if !maps.Equal(again.Permissions, attest.Permissions) || !slices.Equal(again.Needs.([]any), []any{"validate", "publish"}) ||
					again.If != "!cancelled() && needs.publish.result == 'success' && needs.validate.outputs.tag != ''" ||
					!strings.Contains(again.Steps[0].Run, `gh release download "$TAG"`) || again.Steps[1].Uses != attest.Steps[1].Uses ||
					again.Steps[1].With["subject-path"] != "${{ runner.temp }}/published/*" {
					t.Errorf("attest-release: %+v", again)
				}
			}
			// Post-publish workflows run once the release is published, and with assets, once the
			// published files carry the release branch's attestation. Each runs in its own job, so a
			// retry runs only the ones that failed, and only those that opt in run for prereleases.
			_ = json.Unmarshal([]byte(step.Env["HOOKS"]), &hooks)
			for i, h := range c.PostPublish {
				id := fmt.Sprintf("post-publish-%d", i+1)
				d, name := jobs[id], "post-publish ("+h.Workflow+")"
				if h.Repository != "" {
					name = "post-publish (" + h.Repository + ":" + h.Workflow + ")"
				}
				wantNeeds := []any{"validate", "publish"}
				if assets {
					wantNeeds = append(wantNeeds, "attest-release")
				}
				if d.Name != name || !slices.Equal(d.Needs.([]any), wantNeeds) || !strings.HasPrefix(d.If, "!cancelled() && needs.publish.result == 'success'") ||
					assets != strings.Contains(d.If, "needs.attest-release.result == 'success'") ||
					h.Prereleases == strings.Contains(d.If, "!contains(needs.validate.outputs.version, '-')") {
					t.Errorf("%s: %+v", id, d)
				}
				if hook := hooks[len(pre)+i]; hook["job"] != id || hook["name"] != name || hook["when"] != "post-publish" || (h.Prereleases != (hook["prereleases"] == true)) {
					t.Errorf("HOOKS has %v for %s", hook, id)
				}
				if h.Repository == "" {
					if d.Uses != "./.github/workflows/"+h.Workflow || d.Environment != "" || d.Secrets != nil ||
						!maps.Equal(d.Permissions, map[string]string{"contents": "read", "id-token": "write"}) ||
						!maps.Equal(d.With, map[string]string{"ref": "${{ needs.validate.outputs.commit }}", "tag": "${{ needs.validate.outputs.tag }}", "version": "${{ needs.validate.outputs.version }}"}) {
						t.Errorf("%s calls %+v", id, d)
					}
					continue
				}
				// A workflow in another repository starts with a GitHub App token limited to it.
				_, repo, _ := strings.Cut(h.Repository, "/")
				token := d.named(t, "token")
				if d.Environment != "dispatch" || !maps.Equal(d.Permissions, map[string]string{"contents": "read"}) ||
					!strings.HasPrefix(token.Uses, "actions/create-github-app-token@bcd2ba49218906704ab6c1aa796996da409d3eb1") ||
					token.With["client-id"] != "${{ vars.DISPATCH_APP_CLIENT_ID }}" || token.With["private-key"] != "${{ secrets.DISPATCH_APP_PRIVATE_KEY }}" ||
					token.With["owner"] != "fabricahq" || token.With["repositories"] != repo || token.With["permission-actions"] != "write" {
					t.Errorf("%s: %+v", id, d)
				}
				if last := d.Steps[len(d.Steps)-1]; last.Run != `release-planner dispatch --tag "$TAG" --target "$TARGET"` || last.Env["TARGET"] != h.Repository+":"+h.Workflow {
					t.Errorf("%s runs %q with %v", id, last.Run, last.Env)
				}
			}
			// A manual retry checks out the workflow's own commit, so check matches the Release
			// Planner version that runs even when the merged commit pins an older one.
			if ref := jobs["validate"].Steps[0].With["ref"]; ref != "${{ github.event.pull_request.head.sha || github.sha }}" {
				t.Errorf("validate checks out %q", ref)
			}
			// A job after publish runs even when the pull request's build was reused and its jobs
			// skipped, so its condition names a status function and GitHub adds no success().
			for id, j := range jobs {
				needs, _ := j.Needs.([]any)
				if slices.Contains(needs, any("publish")) && !strings.Contains(j.If, "!cancelled()") && !strings.Contains(j.If, "always()") {
					t.Errorf("%s runs if %q, which GitHub skips after a reused build", id, j.If)
				}
			}
			report := jobs["report"]
			if report.If != "always() && needs.validate.result != 'skipped'" || report.Permissions["pull-requests"] != "write" || len(report.Needs.([]any)) != len(jobs)-1 {
				t.Errorf("report: %+v", report)
			}
		})
	}
}

// The release App is optional, but half a setup fails instead of publishing without it.
func TestReleaseAppSettingsNeedBothOrNeither(t *testing.T) {
	check := installCombination(t, t.TempDir(), "")["publish"].named(t, "app")
	for name, tc := range map[string]struct {
		clientID, hasKey string
		ok               bool
		use              string
	}{
		"neither":   {"", "false", true, "use=false"},
		"both":      {"Iv1.abc", "true", true, "use=true"},
		"no key":    {"Iv1.abc", "false", false, ""},
		"no client": {"", "true", false, ""},
	} {
		t.Run(name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "output")
			cmd := exec.Command("bash", "-e", "-c", check.Run)
			cmd.Env = append(os.Environ(), "CLIENT_ID="+tc.clientID, "HAS_KEY="+tc.hasKey, "GITHUB_OUTPUT="+output)
			out, err := cmd.CombinedOutput()
			if (err == nil) != tc.ok {
				t.Fatalf("%v: %s", err, out)
			}
			got, _ := os.ReadFile(output)
			if strings.TrimSpace(string(got)) != tc.use {
				t.Fatalf("outputs %q", got)
			}
			if !tc.ok && !strings.Contains(string(out), "Set both RELEASE_APP_CLIENT_ID and RELEASE_APP_PRIVATE_KEY in the release environment, or neither") {
				t.Fatalf("%s", out)
			}
		})
	}
	if check.Env["CLIENT_ID"] != "${{ vars.RELEASE_APP_CLIENT_ID }}" || check.Env["HAS_KEY"] != "${{ secrets.RELEASE_APP_PRIVATE_KEY != '' }}" {
		t.Fatalf("%v", check.Env)
	}
}

// migrateWorkflow is a minimal pre-publish workflow whose jobs run in production.
const migrateWorkflow = `name: Migrate the database
on:
  workflow_call:
    inputs:
      ref:
        type: string
        required: true
      tag:
        type: string
        required: true
      version:
        type: string
        required: true
permissions:
  contents: read
  id-token: write
jobs:
  migrate:
    runs-on: ubuntu-latest
    timeout-minutes: 10
    environment: production
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          ref: ${{ inputs.ref }}
          persist-credentials: false
      - run: make migrate
  verify:
    needs: migrate
    runs-on: ubuntu-latest
    environment:
      name: production
      url: https://example.com
    steps:
      - run: make verify-schema
`

// A pre-publish workflow takes the release's inputs and gets no secrets from the Release
// workflow. Every job names the same GitHub environment literally, which holds its credentials;
// Release Planner reads the environment from there.
func TestPrePublishEnvironmentComesFromTheWorkflow(t *testing.T) {
	c, err := config.Parse([]byte("schema-version: 1\nversion: v0.2.0\npre-publish:\n  - workflow: migrate-database.yml\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	_, err = Install(root, c, false)
	problemFor(t, err, ".github/workflows/migrate-database.yml", "missing; pre-publish[0].workflow")

	put(t, root, ".github/workflows/migrate-database.yml", strings.Replace(migrateWorkflow, "      version:\n        type: string\n        required: true\n", "", 1))
	_, err = Install(root, c, false)
	problemFor(t, err, ".github/workflows/migrate-database.yml", "has no version input")

	const migrate = "    environment: production\n    steps:\n      - uses"
	for name, tc := range map[string]struct{ from, to, want string }{
		"a job without one": {migrate, "    steps:\n      - uses",
			"job migrate runs in no environment; every job must name the same GitHub environment, such as environment: production"},
		"two environments": {"      name: production\n", "      name: staging\n",
			"jobs run in different environments, production and staging; every job must name the same one"},
		"an expression": {migrate, "    environment: ${{ inputs.environment }}\n    steps:\n      - uses",
			"job migrate names its environment with an expression; name it literally, such as environment: production"},
		// Both jobs name it, so these rename it in both.
		"not a name": {"production", "prod uction",
			"\"prod uction\" isn't an environment name; use letters, digits, and . _ -"},
		"release": {"production", "Release",
			"Release is an environment Release Planner uses for its own credentials; use one of its own, such as production"},
		"dispatch": {"production", "Dispatch",
			"Dispatch is an environment Release Planner uses for its own credentials"},
		"calls a workflow": {"  verify:\n    needs: migrate\n    runs-on: ubuntu-latest\n    environment:\n      name: production\n      url: https://example.com\n    steps:\n      - run: make verify-schema\n",
			"  verify:\n    needs: migrate\n    uses: ./.github/workflows/verify.yml\n", "job verify calls another workflow"},
		"required secret": {"      version:\n        type: string\n        required: true\n", "      version:\n        type: string\n        required: true\n    secrets:\n      DATABASE_URL:\n        required: true\n",
			"requires secrets the Release workflow doesn't pass (DATABASE_URL); store them in the production environment instead"},
	} {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(migrateWorkflow, tc.from) {
				t.Fatalf("fixture lacks %q", tc.from)
			}
			put(t, root, ".github/workflows/migrate-database.yml", strings.ReplaceAll(migrateWorkflow, tc.from, tc.to))
			_, err := Install(root, c, false)
			problemFor(t, err, ".github/workflows/migrate-database.yml", tc.want)
		})
	}

	put(t, root, ".github/workflows/migrate-database.yml", migrateWorkflow)
	if _, err := Install(root, c, false); err != nil {
		t.Fatal(err)
	}
	if err := Check(root, c); err != nil {
		t.Fatal(err)
	}
	if wf := read(t, root, WorkflowPath); !strings.Contains(wf, "# Its jobs run in the GitHub environment production, which holds its credentials.") {
		t.Errorf("the generated workflow doesn't name the environment:\n%s", wf)
	}

	// Moving the workflow to another environment after install makes check fail until install
	// runs again, so the generated workflow never describes an environment the jobs don't use.
	put(t, root, ".github/workflows/migrate-database.yml", strings.ReplaceAll(migrateWorkflow, "production", "staging"))
	problemFor(t, Check(root, c), WorkflowPath, "differs from what .release-planner/config.yml generates; run release-planner install")
	if _, err := Install(root, c, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, root, WorkflowPath), "# Its jobs run in the GitHub environment staging, which holds its credentials.") {
		t.Error("install didn't pick up the new environment")
	}
}

// A pull request that changes a workflow the Release workflow calls runs check, which finds
// a called workflow that no longer fits.
func TestPullRequestsThatChangeCalledWorkflowsRunTheReleaseWorkflow(t *testing.T) {
	root := t.TempDir()
	put(t, root, ".github/workflows/migrate-database.yml", migrateWorkflow)
	installCombination(t, root, "release-checks:\n  workflow: release-checks.yml\nrelease-assets:\n  workflow: build-release.yml\npre-publish:\n  - workflow: migrate-database.yml\n")
	var wf struct {
		On struct {
			PullRequest struct{ Paths []string } `yaml:"pull_request"`
		}
	}
	if err := yaml.Unmarshal([]byte(read(t, root, WorkflowPath)), &wf); err != nil {
		t.Fatal(err)
	}
	want := []string{"_releases/**", ".release-planner/**", ".github/workflows/release-planner.yml", ".github/workflows/release-checks.yml", ".github/workflows/build-release.yml", ".github/workflows/migrate-database.yml"}
	if !slices.Equal(wf.On.PullRequest.Paths, want) {
		t.Fatalf("%q", wf.On.PullRequest.Paths)
	}
}

// The report shows the pre-publish workflow's own name, or its file name when it has none, or
// one that GitHub would evaluate as an expression in the Release workflow.
func TestReportNamesThePrePublishWorkflow(t *testing.T) {
	long := strings.Repeat("m", 101)
	for name, tc := range map[string]struct{ to, want string }{
		"named":          {"name: Migrate the database\n", "Migrate the database"},
		"unnamed":        {"", ""},
		"quoted":         {"name: 'Migrate: the database'\n", "Migrate: the database"},
		"an apostrophe":  {"name: \"Migrate Josh's database\"\n", "Migrate Josh's database"},
		"an expression":  {"name: Migrate ${{ github.ref }}\n", ""},
		"multi-line":     {"name: |\n  Migrate\n  the database\n", ""},
		"over-long":      {"name: " + long + "\n", ""},
		"100 characters": {"name: " + long[:100] + "\n", long[:100]},
		"a substitution": {"name: Migrate $(touch pwned)\n", "Migrate $(touch pwned)"},
		"a backtick":     {"name: Migrate `id`\n", "Migrate `id`"},
		"Markdown in it": {"name: Migrate ~~production~~ | *now*\n", "Migrate ~~production~~ | *now*"},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			put(t, root, ".github/workflows/migrate-database.yml", strings.Replace(migrateWorkflow, "name: Migrate the database\n", tc.to, 1))
			jobs := installCombination(t, root, combinations["pre-publish"])
			var hooks []struct{ Title string }
			if err := json.Unmarshal([]byte(jobs["report"].Steps[len(jobs["report"].Steps)-1].Env["HOOKS"]), &hooks); err != nil || len(hooks) != 1 || hooks[0].Title != tc.want {
				t.Fatalf("HOOKS has %+v, want the title %q: %v", hooks, tc.want, err)
			}
		})
	}
}

// The name reaches report as one argument, as written: the generated step passes it through an
// environment variable, so the shell never runs what it says.
func TestReportStepPassesThePrePublishNameAsWritten(t *testing.T) {
	for name, payload := range map[string]string{"substitution": "Migrate $(touch pwned)", "backticks": "Migrate `touch pwned`", "quotes": "Migrate Josh's \"database\"; touch pwned"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			put(t, root, ".github/workflows/migrate-database.yml", strings.Replace(migrateWorkflow, "name: Migrate the database\n", "name: '"+strings.ReplaceAll(payload, "'", "''")+"'\n", 1))
			step := installCombination(t, root, combinations["pre-publish"])["report"].Steps
			report := step[len(step)-1]
			if !strings.Contains(report.Env["HOOKS"], payload) && !strings.Contains(report.Env["HOOKS"], `\"database\"`) {
				t.Fatalf("HOOKS is %q", report.Env["HOOKS"])
			}
			// Run the step with a release-planner that records its arguments.
			bin, work := t.TempDir(), t.TempDir()
			args := filepath.Join(bin, "args")
			put(t, bin, "release-planner", "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done > "+args+"\n")
			if err := os.Chmod(filepath.Join(bin, "release-planner"), 0o755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", "-e", "-c", report.Run)
			cmd.Dir = work
			cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "HOOKS="+report.Env["HOOKS"],
				"NEEDS={}", "MERGED="+strings.Repeat("d", 40), "RUNNER_TEMP="+work)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			got, _ := os.ReadFile(args)
			if !strings.Contains(string(got), "\n--hooks\n"+report.Env["HOOKS"]+"\n") {
				t.Fatalf("report got:\n%s", got)
			}
			if _, err := os.Stat(filepath.Join(work, "pwned")); err == nil {
				t.Fatal("the shell ran the name")
			}
		})
	}
}

// A post-publish workflow in this repository is checked like a pre-publish one: its jobs name
// one environment, which isn't one of Release Planner's own, and it takes the release's inputs.
// One in another repository isn't read, since it isn't here.
func TestPostPublishEnvironmentComesFromTheWorkflow(t *testing.T) {
	c, err := config.Parse([]byte("schema-version: 1\nversion: v0.2.0\npost-publish:\n  - workflow: deploy.yml\n  - repository: fabricahq/homebrew-tap\n    workflow: update.yml\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	_, err = Install(root, c, false)
	problemFor(t, err, ".github/workflows/deploy.yml", "missing; post-publish[0].workflow")
	for workflow, want := range map[string]string{
		strings.Replace(migrateWorkflow, "    environment: production\n", "", 1):                              "job migrate runs in no environment",
		strings.ReplaceAll(migrateWorkflow, "production", "dispatch"):                                         "dispatch is an environment Release Planner uses for its own credentials",
		strings.Replace(migrateWorkflow, "      tag:\n        type: string\n        required: true\n", "", 1): "has no tag input",
	} {
		put(t, root, ".github/workflows/deploy.yml", workflow)
		_, err = Install(root, c, false)
		problemFor(t, err, ".github/workflows/deploy.yml", want)
	}
	put(t, root, ".github/workflows/deploy.yml", migrateWorkflow)
	if _, err := Install(root, c, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, root, WorkflowPath), "# Its jobs run in the GitHub environment production, which holds its credentials.") {
		t.Error("the generated workflow doesn't name the post-publish workflow's environment")
	}
}
