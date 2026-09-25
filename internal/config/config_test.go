package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFillsDefaults(t *testing.T) {
	c, err := Parse([]byte("schema-version: 1\nversion: v0.2.0\nvalidate:\n  run: make test\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	if c.FirstVersion != "v0.1.0" || c.NotesDir != "_releases" || c.Branch != "main" || c.Validate.Run != "make test" || c.ReleaseNotesStyle.Set() || c.Style != "" {
		t.Fatalf("%+v", c)
	}
}

func TestValidateIsOptional(t *testing.T) {
	c, err := Parse([]byte("schema-version: 1\nversion: v0.2.0\n"), "")
	if err != nil || c.Validate.Enabled() {
		t.Fatal(c, err)
	}
	c, err = Parse([]byte("schema-version: 1\nversion: v0.2.0\nvalidate:\n  workflow: ci.yml\n"), "")
	if err != nil || !c.Validate.Enabled() || c.Validate.Workflow != "ci.yml" {
		t.Fatal(c, err)
	}
}

func TestParseReadsEverySetting(t *testing.T) {
	c, err := Parse([]byte(`schema-version: 1
version: 0123456789abcdef0123456789abcdef01234567
first-version: v1.0.0
notes-dir: docs/releases
branch: trunk
release-notes-style:
  file: docs/release-notes-style.md
  mode: replace
validate:
  go: '1.27.x'
  node: '22'
  run: |
    go install example.com/tool@v1
    tool check
`), "\n- Use Keep a Changelog headings.\n\n")
	if err != nil {
		t.Fatal(err)
	}
	if c.Validate.Run != "go install example.com/tool@v1\ntool check" || c.Validate.Node != "22" || c.Style != "- Use Keep a Changelog headings." {
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

func TestParseRejectsInvalidSettings(t *testing.T) {
	for name, tc := range map[string]struct{ yaml, want string }{
		"missing version":   {"schema-version: 1\nvalidate:\n  run: x\n", "version:"},
		"missing schema":    {"version: v0.2.0\nvalidate:\n  run: x\n", "reads schema-version 1, not 0"},
		"future schema":     {"schema-version: 2\nversion: v0.2.0\nvalidate:\n  run: x\n", "reads schema-version 1, not 2"},
		"branch pin":        {"schema-version: 1\nversion: main\nvalidate:\n  run: x\n", "version:"},
		"short sha":         {"schema-version: 1\nversion: 0123456\nvalidate:\n  run: x\n", "version:"},
		"bad first":         {"schema-version: 1\nversion: v0.2.0\nfirst-version: 1.0\nvalidate:\n  run: x\n", "first-version"},
		"escaping dir":      {"schema-version: 1\nversion: v0.2.0\nnotes-dir: ../x\nvalidate:\n  run: x\n", "notes-dir"},
		"absolute dir":      {"schema-version: 1\nversion: v0.2.0\nnotes-dir: /x\nvalidate:\n  run: x\n", "notes-dir"},
		"pattern dir":       {"schema-version: 1\nversion: v0.2.0\nnotes-dir: rel*\n", "path filters treat as patterns"},
		"config dir":        {"schema-version: 1\nversion: v0.2.0\nnotes-dir: .release-planner/notes\nvalidate:\n  run: x\n", "notes-dir"},
		"run and workflow":  {"schema-version: 1\nversion: v0.2.0\nvalidate:\n  run: x\n  workflow: ci.yml\n", "run or workflow, not both"},
		"workflow tools":    {"schema-version: 1\nversion: v0.2.0\nvalidate:\n  go: '1.27.x'\n  workflow: ci.yml\n", "set up toolchains in ci.yml"},
		"workflow path":     {"schema-version: 1\nversion: v0.2.0\nvalidate:\n  workflow: ../ci.yml\n", "validate.workflow"},
		"tools, no run":     {"schema-version: 1\nversion: v0.2.0\nvalidate:\n  go: '1.27.x'\n", "no run script"},
		"injected tool":     {"schema-version: 1\nversion: v0.2.0\nvalidate:\n  go: \"1.2'\\n  x: y\"\n  run: x\n", "validate.go"},
		"unknown key":       {"schema-version: 1\nversion: v0.2.0\nnotes:\n  sections: []\nvalidate:\n  run: x\n", "field notes not found"},
		"unknown style key": {"schema-version: 1\nversion: v0.2.0\nrelease-notes-style:\n  path: s.md\n  mode: append\n", "field path not found"},
		"unknown validate":  {"schema-version: 1\nversion: v0.2.0\nvalidate:\n  ruby: '3'\n  run: x\n", "field ruby not found"},
		"style shorthand":   {"schema-version: 1\nversion: v0.2.0\nrelease-notes-style: replace\n", "needs a file and a mode"},
		"bad style mode":    {"schema-version: 1\nversion: v0.2.0\nrelease-notes-style:\n  file: s.md\n  mode: overwrite\n", "use append or replace, not \"overwrite\""},
		"no style mode":     {"schema-version: 1\nversion: v0.2.0\nrelease-notes-style:\n  file: s.md\n", "use append or replace, not \"\""},
		"no style file":     {"schema-version: 1\nversion: v0.2.0\nrelease-notes-style:\n  mode: append\n", "name your style file"},
		"style outside":     {"schema-version: 1\nversion: v0.2.0\nrelease-notes-style:\n  file: ../s.md\n  mode: append\n", "inside the repository"},
		"style not md":      {"schema-version: 1\nversion: v0.2.0\nrelease-notes-style:\n  file: style.txt\n  mode: append\n", "a .md file"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(tc.yaml), ""); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}
