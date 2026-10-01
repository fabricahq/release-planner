package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFillsDefaults(t *testing.T) {
	c, err := Parse([]byte("schema-version: 1\nversion: v0.2.0\nrelease-checks:\n  run: make test\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	if c.FirstVersion != "v0.1.0" || c.NotesDir != "_releases" || c.Branch != "main" || c.ReleaseChecks.Run != "make test" || c.ReleaseNotesStyle.Set() || c.Style != "" {
		t.Fatalf("%+v", c)
	}
}

func TestNotesDirInReadsOnlyTheNotesDirectory(t *testing.T) {
	for data, want := range map[string]string{"": "_releases", "release-notes-dir: docs/releases/\nunknown: key\n": "docs/releases", "[": "_releases"} {
		if got := NotesDirIn([]byte(data)); got != want {
			t.Errorf("NotesDirIn(%q) = %q, want %q", data, got, want)
		}
	}
}

func TestReleaseChecksAreOptional(t *testing.T) {
	c, err := Parse([]byte("schema-version: 1\nversion: v0.2.0\n"), "")
	if err != nil || c.ReleaseChecks.Enabled() {
		t.Fatal(c, err)
	}
	c, err = Parse([]byte("schema-version: 1\nversion: v0.2.0\nrelease-checks:\n  workflow: ci.yml\n"), "")
	if err != nil || !c.ReleaseChecks.Enabled() || c.ReleaseChecks.Workflow != "ci.yml" {
		t.Fatal(c, err)
	}
}

func TestParseReadsEverySetting(t *testing.T) {
	c, err := Parse([]byte(`schema-version: 1
version: 0123456789abcdef0123456789abcdef01234567
first-version: v1.0.0
release-notes-dir: docs/releases
release-branch: trunk
release-notes-rules:
  no-long-heading: off
  list-every-change: off
release-notes-style:
  file: docs/release-notes-style.md
  mode: replace
release-checks:
  go: '1.27.x'
  node: '22'
  run: |
    go install example.com/tool@v1
    tool check
`), "\n- Use Keep a Changelog headings.\n\n")
	if err != nil {
		t.Fatal(err)
	}
	if c.ReleaseChecks.Run != "go install example.com/tool@v1\ntool check" || c.ReleaseChecks.Node != "22" || c.Style != "- Use Keep a Changelog headings." ||
		strings.Join(c.RulesOff(), ",") != "list-every-change,no-long-heading" {
		t.Fatalf("%+v", c)
	}
}

func TestLoadReadsOnlyTheNamedStyleFile(t *testing.T) {
	root := t.TempDir()
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "run release-planner init") {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, Dir), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A style file in the suggested place is ignored until the config names it.
	write(StyleFile, "- Our style.\n")
	write(File, "schema-version: 1\nversion: v0.2.0\n")
	if c, err := Load(root); err != nil || c.Style != "" {
		t.Fatal(c, err)
	}
	write(File, "schema-version: 1\nversion: v0.2.0\nrelease-notes-style:\n  file: docs/style.md\n  mode: replace\n")
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "docs/style.md does not exist") {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	write("docs/style.md", "\n")
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "docs/style.md is empty") {
		t.Fatal(err)
	}
	write("docs/style.md", "- Our style.\n")
	c, err := Load(root)
	if err != nil || c.Style != "- Our style." || c.ReleaseNotesStyle.Mode != StyleReplace {
		t.Fatal(c, err)
	}
}

func TestParseReadsAssetsAndPostPublish(t *testing.T) {
	c, err := Parse([]byte("schema-version: 1\nversion: v0.2.0\nrelease-assets:\n  workflow: build-release.yml\npost-publish:\n  - workflow: deploy.yml\n    prereleases: true\n  - repository: fabricahq/homebrew-tap\n    workflow: update-code-rules.yml\n  - repository: fabricahq/scoop-bucket\n    workflow: update.yml\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	if c.ReleaseAssets.Workflow != "build-release.yml" || len(c.PostPublish) != 3 || c.DispatchOwner() != "fabricahq" ||
		c.PostPublish[0].Workflow != "deploy.yml" || !c.PostPublish[0].Prereleases || c.PostPublish[1].Repository != "fabricahq/homebrew-tap" || c.PostPublish[1].Prereleases {
		t.Fatalf("%+v", c)
	}
	if c, err := Parse([]byte("schema-version: 1\nversion: v0.2.0\npost-publish:\n  - workflow: deploy.yml\n"), ""); err != nil || c.DispatchOwner() != "" {
		t.Fatal(c, err)
	}
}

func TestParseRejectsInvalidSettings(t *testing.T) {
	for name, tc := range map[string]struct{ yaml, want string }{
		"missing version":   {"schema-version: 1\nrelease-checks:\n  run: x\n", "version:"},
		"missing schema":    {"version: v0.2.0\nrelease-checks:\n  run: x\n", "reads schema-version 1, not 0"},
		"future schema":     {"schema-version: 2\nversion: v0.2.0\nrelease-checks:\n  run: x\n", "reads schema-version 1, not 2"},
		"branch pin":        {"schema-version: 1\nversion: main\nrelease-checks:\n  run: x\n", "version:"},
		"short sha":         {"schema-version: 1\nversion: 0123456\nrelease-checks:\n  run: x\n", "version:"},
		"bad first":         {"schema-version: 1\nversion: v0.2.0\nfirst-version: 1.0\nrelease-checks:\n  run: x\n", "first-version"},
		"escaping dir":      {"schema-version: 1\nversion: v0.2.0\nrelease-notes-dir: ../x\nrelease-checks:\n  run: x\n", "release-notes-dir"},
		"absolute dir":      {"schema-version: 1\nversion: v0.2.0\nrelease-notes-dir: /x\nrelease-checks:\n  run: x\n", "release-notes-dir"},
		"pattern dir":       {"schema-version: 1\nversion: v0.2.0\nrelease-notes-dir: rel*\n", "path filters treat as patterns"},
		"config dir":        {"schema-version: 1\nversion: v0.2.0\nrelease-notes-dir: .release-planner/notes\nrelease-checks:\n  run: x\n", "release-notes-dir"},
		"run and workflow":  {"schema-version: 1\nversion: v0.2.0\nrelease-checks:\n  run: x\n  workflow: ci.yml\n", "run or workflow, not both"},
		"workflow tools":    {"schema-version: 1\nversion: v0.2.0\nrelease-checks:\n  go: '1.27.x'\n  workflow: ci.yml\n", "set up toolchains in ci.yml"},
		"workflow path":     {"schema-version: 1\nversion: v0.2.0\nrelease-checks:\n  workflow: ../ci.yml\n", "release-checks.workflow"},
		"tools, no run":     {"schema-version: 1\nversion: v0.2.0\nrelease-checks:\n  go: '1.27.x'\n", "no run script"},
		"injected tool":     {"schema-version: 1\nversion: v0.2.0\nrelease-checks:\n  go: \"1.2'\\n  x: y\"\n  run: x\n", "release-checks.go"},
		"unknown key":       {"schema-version: 1\nversion: v0.2.0\nnotes:\n  sections: []\nrelease-checks:\n  run: x\n", "field notes not found"},
		"unknown style key": {"schema-version: 1\nversion: v0.2.0\nrelease-notes-style:\n  path: s.md\n  mode: append\n", "field path not found"},
		"unknown check key": {"schema-version: 1\nversion: v0.2.0\nrelease-checks:\n  ruby: '3'\n  run: x\n", "field ruby not found"},
		"style shorthand":   {"schema-version: 1\nversion: v0.2.0\nrelease-notes-style: replace\n", "needs a file and a mode"},
		"bad style mode":    {"schema-version: 1\nversion: v0.2.0\nrelease-notes-style:\n  file: s.md\n  mode: overwrite\n", "use append or replace, not \"overwrite\""},
		"no style mode":     {"schema-version: 1\nversion: v0.2.0\nrelease-notes-style:\n  file: s.md\n", "use append or replace, not \"\""},
		"no style file":     {"schema-version: 1\nversion: v0.2.0\nrelease-notes-style:\n  mode: append\n", "name your style file"},
		"style outside":     {"schema-version: 1\nversion: v0.2.0\nrelease-notes-style:\n  file: ../s.md\n  mode: append\n", "inside the repository"},
		"old validate key":  {"schema-version: 1\nversion: v0.2.0\nvalidate:\n  run: x\n", "field validate not found"},
		"unknown rule":      {"schema-version: 1\nversion: v0.2.0\nrelease-notes-rules:\n  no-long-heading: off\n  no-todo-opening: off\n", `release-notes-rules: "no-todo-opening" is not a release notes rule; run release-planner validate --rules to list them`},
		"bad rule setting":  {"schema-version: 1\nversion: v0.2.0\nrelease-notes-rules:\n  no-long-heading: 100\n", `release-notes-rules.no-long-heading: use off, not "100"`},
		"rules as a list":   {"schema-version: 1\nversion: v0.2.0\nrelease-notes-rules: [no-long-heading]\n", "cannot unmarshal"},
		"style not md":      {"schema-version: 1\nversion: v0.2.0\nrelease-notes-style:\n  file: style.txt\n  mode: append\n", "a .md file"},
		"checks self":       {"schema-version: 1\nversion: v0.2.0\nrelease-checks:\n  workflow: release-planner.yml\n", "other than release-planner.yml"},
		"assets self":       {"schema-version: 1\nversion: v0.2.0\nrelease-assets:\n  workflow: release-planner.yml\n", "release-assets.workflow: name a workflow file in .github/workflows other than release-planner.yml"},
		"assets path":       {"schema-version: 1\nversion: v0.2.0\nrelease-assets:\n  workflow: .github/workflows/build.yml\n", "release-assets.workflow"},
		"assets key":        {"schema-version: 1\nversion: v0.2.0\nrelease-assets:\n  run: make\n", "field run not found"},
		"downstream": {"schema-version: 1\nversion: v0.2.0\ndownstream:\n  - repository: o/tap\n    workflow: a.yml\n",
			"downstream: Release Planner v0.5.0 runs these as post-publish workflows; move each entry under post-publish: as it is, and since GitHub can't rename an environment, set up a dispatch environment like the downstream one, with DISPATCH_APP_CLIENT_ID and DISPATCH_APP_PRIVATE_KEY in place of DOWNSTREAM_APP_CLIENT_ID and DOWNSTREAM_APP_PRIVATE_KEY: https://release-planner.fabricahq.com/customize/post-publish/#set-up-the-dispatch-environment"},
		"post-publish repo":       {"schema-version: 1\nversion: v0.2.0\npost-publish:\n  - repository: homebrew-tap\n    workflow: update.yml\n", `post-publish[0].repository: name the repository as owner/name, not "homebrew-tap"`},
		"post-publish owner":      {"schema-version: 1\nversion: v0.2.0\npost-publish:\n  - repository: o/tap\n    workflow: a.yml\n  - repository: p/tap\n    workflow: a.yml\n", "post-publish[1].repository: every repository a post-publish workflow is in must belong to o"},
		"post-publish file":       {"schema-version: 1\nversion: v0.2.0\npost-publish:\n  - repository: o/tap\n    workflow: update\n", "post-publish[0].workflow: name a workflow file in o/tap's .github/workflows"},
		"post-publish twice":      {"schema-version: 1\nversion: v0.2.0\npost-publish:\n  - repository: o/tap\n    workflow: a.yml\n  - repository: o/tap\n    workflow: a.yml\n", "post-publish[1]: o/tap:a.yml is listed twice"},
		"post-publish here twice": {"schema-version: 1\nversion: v0.2.0\npost-publish:\n  - workflow: deploy.yml\n  - workflow: deploy.yml\n", "post-publish[1]: deploy.yml is listed twice"},
		"post-publish self":       {"schema-version: 1\nversion: v0.2.0\npost-publish:\n  - workflow: release-planner.yml\n", "post-publish[0].workflow: name a workflow file in .github/workflows other than release-planner.yml"},
		"post-publish also pre": {"schema-version: 1\nversion: v0.2.0\npre-publish:\n  - workflow: migrate.yml\npost-publish:\n  - workflow: migrate.yml\n",
			"post-publish[0].workflow: migrate.yml already runs in the release as another kind of workflow; use a workflow of its own"},
		"post-publish key":        {"schema-version: 1\nversion: v0.2.0\npost-publish:\n  - repository: o/tap\n    workflow: a.yml\n    ref: main\n", "field ref not found"},
		"pre-publish prereleases": {"schema-version: 1\nversion: v0.2.0\npre-publish:\n  - workflow: migrate.yml\n    prereleases: true\n", "field prereleases not found"},
		"pre-publish self":        {"schema-version: 1\nversion: v0.2.0\npre-publish:\n  - workflow: release-planner.yml\n", "pre-publish[0].workflow: name a workflow file in .github/workflows other than release-planner.yml"},
		"pre-publish path":        {"schema-version: 1\nversion: v0.2.0\npre-publish:\n  - workflow: ../migrate-database.yml\n", "pre-publish[0].workflow"},
		"pre-publish is the build": {"schema-version: 1\nversion: v0.2.0\nrelease-assets:\n  workflow: build.yml\npre-publish:\n  - workflow: build.yml\n",
			"pre-publish[0].workflow: build.yml is also the release-assets workflow; use a workflow of its own"},
		"pre-publish is the checks": {"schema-version: 1\nversion: v0.2.0\nrelease-checks:\n  workflow: ci.yml\npre-publish:\n  - workflow: ci.yml\n",
			"pre-publish[0].workflow: ci.yml is also the release-checks workflow; use a workflow of its own"},
		"pre-publish twice": {"schema-version: 1\nversion: v0.2.0\npre-publish:\n  - workflow: migrate-database.yml\n  - workflow: migrate-database.yml\n", "pre-publish[1]: migrate-database.yml is listed twice"},
		"pre-publish elsewhere": {"schema-version: 1\nversion: v0.2.0\npre-publish:\n  - workflow: migrate.yml\n    repository: fabricahq/infra\n",
			"pre-publish[0].repository: running a workflow in another repository before publishing isn't supported yet; use a workflow in this repository"},
		"pre-publish environment key": {"schema-version: 1\nversion: v0.2.0\npre-publish:\n  - workflow: migrate-database.yml\n    environment: production\n", "field environment not found"},
		"pre-publish key":             {"schema-version: 1\nversion: v0.2.0\npre-publish:\n  - workflow: migrate-database.yml\n    secrets: inherit\n", "field secrets not found"},
		"pre-publish as one workflow": {"schema-version: 1\nversion: v0.2.0\npre-publish:\n  workflow: migrate-database.yml\n", "cannot unmarshal"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(tc.yaml), ""); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestParseReadsPrePublish(t *testing.T) {
	c, err := Parse([]byte("schema-version: 1\nversion: v0.2.0\npre-publish:\n  - workflow: migrate-database.yml\n  - workflow: warm-caches.yml\n"), "")
	if err != nil || len(c.PrePublish) != 2 || c.PrePublish[0].Workflow != "migrate-database.yml" || c.PrePublish[1].Workflow != "warm-caches.yml" || c.PrePublish[0].Environment != "" {
		t.Fatal(c, err)
	}
	if c, err := Parse([]byte("schema-version: 1\nversion: v0.2.0\n"), ""); err != nil || len(c.PrePublish) != 0 {
		t.Fatal(c, err)
	}
}
