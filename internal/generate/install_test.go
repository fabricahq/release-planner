package generate

import (
	"errors"
	"maps"
	"os"
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
permissions:
  contents: read
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          ref: ${{ inputs.ref }}
          persist-credentials: false
      - run: mkdir dist && echo "$VERSION" > dist/version.txt
        env:
          VERSION: ${{ inputs.version }}
      - uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7.0.1
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
	"plain":             "",
	"checks-script":     "release-checks:\n  go: '1.27.x'\n  run: go test ./...\n",
	"checks-workflow":   "release-checks:\n  workflow: release-checks.yml\n",
	"assets":            "release-assets:\n  workflow: build-release.yml\n",
	"downstream":        "downstream:\n  - repository: fabricahq/homebrew-tap\n    workflow: update-code-rules.yml\n",
	"all":               "release-checks:\n  workflow: release-checks.yml\nrelease-assets:\n  workflow: build-release.yml\ndownstream:\n  - repository: fabricahq/homebrew-tap\n    workflow: update-code-rules.yml\n  - repository: fabricahq/scoop-bucket\n    workflow: update.yml\n",
	"script-and-assets": "release-checks:\n  run: make smoke\nrelease-assets:\n  workflow: build-release.yml\n",
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
	Steps       []struct {
		Uses string            `yaml:"uses"`
		If   string            `yaml:"if"`
		With map[string]string `yaml:"with"`
		Run  string            `yaml:"run"`
	} `yaml:"steps"`
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
			downstream := strings.Contains(extra, "downstream")
			want := []string{"validate", "publish", "report"}
			if checks {
				want = append(want, "release-checks")
			}
			if assets {
				want = append(want, "release-assets", "attest", "attest-release")
			}
			if downstream {
				want = append(want, "downstream")
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
				if j.Permissions["id-token"] == "write" && id != "attest" && id != "attest-release" {
					t.Errorf("%s can mint OIDC tokens", id)
				}
			}
			publish := jobs["publish"]
			if publish.Environment != "release" || !strings.Contains(publish.If, "github.event_name != 'pull_request'") || !strings.Contains(publish.If, "!contains(needs.*.result, 'failure')") {
				t.Errorf("publish: %+v", publish)
			}
			wantNeeds := []any{"validate"}
			for _, id := range []string{"release-checks", "release-assets", "attest"} {
				if _, ok := jobs[id]; ok {
					wantNeeds = append(wantNeeds, id)
				}
			}
			if !slices.Equal(publish.Needs.([]any), wantNeeds) {
				t.Errorf("publish needs %v, want %v", publish.Needs, wantNeeds)
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
			if downstream {
				d := jobs["downstream"]
				if d.Environment != "downstream" || !strings.Contains(d.If, "!contains(needs.validate.outputs.version, '-')") || d.Permissions["contents"] != "read" ||
					!strings.HasPrefix(d.Steps[1].Uses, "actions/create-github-app-token@bcd2ba49218906704ab6c1aa796996da409d3eb1") ||
					d.Steps[1].With["owner"] != "fabricahq" || d.Steps[1].With["permission-actions"] != "write" {
					t.Errorf("downstream: %+v", d)
				}
				// Downstream workflows start only once the published files carry the release branch's attestation.
				if assets != (slices.Contains(d.Needs.([]any), "attest-release") && strings.Contains(d.If, "needs.attest-release.result == 'success'")) {
					t.Errorf("downstream needs %v if %q", d.Needs, d.If)
				}
				// One job per target, named for it, so a retry runs only the targets that failed.
				targets := []string{"fabricahq/homebrew-tap:update-code-rules.yml"}
				if name == "all" {
					targets = append(targets, "fabricahq/scoop-bucket:update.yml")
				}
				if d.Name != "downstream (${{ matrix.target }})" || d.Strategy.FailFast == nil || *d.Strategy.FailFast || !slices.Equal(d.Strategy.Matrix["target"], targets) {
					t.Errorf("downstream matrix: %q %+v", d.Name, d.Strategy)
				}
				if last := d.Steps[len(d.Steps)-1]; last.Run != `release-planner downstream --tag "$TAG" --target "$TARGET"` {
					t.Errorf("downstream runs %q", last.Run)
				}
				run := jobs["report"].Steps[len(jobs["report"].Steps)-1].Run
				for _, target := range targets {
					if !strings.Contains(run, "--downstream "+target) {
						t.Errorf("report lacks --downstream %s: %q", target, run)
					}
				}
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
