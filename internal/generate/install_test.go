package generate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fabricahq/release-planner/internal/config"
)

func cfg(t *testing.T, version string) config.Config {
	t.Helper()
	c, err := config.Parse([]byte("schema-version: 1\nversion: "+version+"\nfirst-version: v1.0.0\nvalidate:\n  go: '1.27.x'\n  run: |\n    go install example.com/tool@v1\n    tool check\n"), "")
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

func TestWorkflowContent(t *testing.T) {
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
		"paths: ['releases/v*.md']",
		"go-version: '1.27.x'",
		"          go install example.com/tool@v1\n          tool check\n",
		"environment: release",
		"${{ needs.plan.outputs.commit }}",
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
	changed.Validate.Run = "make test"
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

func TestValidateForms(t *testing.T) {
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
	if strings.Contains(wf, "validate:") || !strings.Contains(wf, "needs: [plan]\n") {
		t.Errorf("no checks configured, but the workflow validates:\n%s", wf)
	}

	root = t.TempDir()
	withCI := parse("validate:\n  workflow: ci.yml\n")
	_, err := Install(root, withCI, false)
	problemFor(t, err, ".github/workflows/ci.yml", "missing")

	put(t, root, ".github/workflows/ci.yml", "on:\n  pull_request:\n")
	_, err = Install(root, withCI, false)
	problemFor(t, err, ".github/workflows/ci.yml", "cannot be called")

	put(t, root, ".github/workflows/ci.yml", "on:\n  workflow_call:\n")
	_, err = Install(root, withCI, false)
	problemFor(t, err, ".github/workflows/ci.yml", "no ref input")

	put(t, root, ".github/workflows/ci.yml", "on:\n  pull_request:\n  workflow_call:\n    inputs:\n      ref:\n        type: string\n")
	if _, err := Install(root, withCI, false); err != nil {
		t.Fatal(err)
	}
	wf = read(t, root, WorkflowPath)
	for _, want := range []string{"uses: ./.github/workflows/ci.yml\n", "ref: ${{ needs.plan.outputs.commit }}\n", "secrets: inherit\n", "needs: [plan, validate]\n"} {
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
