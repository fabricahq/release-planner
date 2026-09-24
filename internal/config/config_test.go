package config

import (
	"strings"
	"testing"
)

func TestDefaults(t *testing.T) {
	c, err := Parse([]byte("version: v0.2.0\nvalidate:\n  run: make test\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.FirstVersion != "v0.1.0" || c.NotesDir != "releases" || c.Branch != "main" || c.Validate.Run != "make test" {
		t.Fatalf("%+v", c)
	}
}

func TestFullConfig(t *testing.T) {
	c, err := Parse([]byte(`version: 0123456789abcdef0123456789abcdef01234567
first-version: v1.0.0
notes-dir: docs/releases
branch: trunk
validate:
  go: '1.27.x'
  node: '22'
  run: |
    go install example.com/tool@v1
    tool check
`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Validate.Run != "go install example.com/tool@v1\ntool check" || c.Validate.Node != "22" {
		t.Fatalf("%+v", c)
	}
}

func TestNoteSections(t *testing.T) {
	c, _ := Parse([]byte("version: v0.2.0\nvalidate:\n  run: x\n"))
	if len(c.Notes.Sections) != 4 || c.Notes.Sections[0].Heading != "✨ New Features" || !c.Notes.Sections[3].SummarizeFirst {
		t.Fatalf("defaults: %+v", c.Notes.Sections)
	}
	c, err := Parse([]byte(`version: v0.2.0
validate:
  run: x
notes:
  sections:
    - heading: Fixes
      include: Bugs we fixed.
    - heading: 🔒 Security
      include: Vulnerabilities and their CVE IDs.
      summarize-first: true
`))
	if err != nil || len(c.Notes.Sections) != 2 || c.Notes.Sections[1].Heading != "🔒 Security" {
		t.Fatalf("custom: %+v %v", c.Notes.Sections, err)
	}
}

func TestRejections(t *testing.T) {
	for name, tc := range map[string]struct{ yaml, want string }{
		"missing version":  {"validate:\n  run: x\n", "version:"},
		"branch pin":       {"version: main\nvalidate:\n  run: x\n", "version:"},
		"short sha":        {"version: 0123456\nvalidate:\n  run: x\n", "version:"},
		"bad first":        {"version: v0.2.0\nfirst-version: 1.0\nvalidate:\n  run: x\n", "first-version"},
		"escaping dir":     {"version: v0.2.0\nnotes-dir: ../x\nvalidate:\n  run: x\n", "notes-dir"},
		"absolute dir":     {"version: v0.2.0\nnotes-dir: /x\nvalidate:\n  run: x\n", "notes-dir"},
		"missing run":      {"version: v0.2.0\n", "validate.run"},
		"injected tool":    {"version: v0.2.0\nvalidate:\n  go: \"1.2'\\n  x: y\"\n  run: x\n", "validate.go"},
		"unknown key":      {"version: v0.2.0\nskills: [claude]\nvalidate:\n  run: x\n", "field skills not found"},
		"unknown validate": {"version: v0.2.0\nvalidate:\n  ruby: '3'\n  run: x\n", "field ruby not found"},
		"hashed heading":   {"version: v0.2.0\nvalidate:\n  run: x\nnotes:\n  sections:\n    - heading: '## Fixes'\n      include: y\n", "without leading #"},
		"no include":       {"version: v0.2.0\nvalidate:\n  run: x\nnotes:\n  sections:\n    - heading: Fixes\n", "say what belongs"},
		"footer heading":   {"version: v0.2.0\nvalidate:\n  run: x\nnotes:\n  sections:\n    - heading: What's Changed\n      include: y\n", "fixed footer"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(tc.yaml)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}
