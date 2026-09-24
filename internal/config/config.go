// Package config reads release-planner.yml, the one file a repository writes to adopt Release Planner.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/fabricahq/release-planner/internal/semver"
	"go.yaml.in/yaml/v3"
)

// File is the config's path relative to the repository root.
const File = "release-planner.yml"

// Config is the repository's release configuration.
type Config struct {
	// Version pins Release Planner itself: a release tag or a full commit SHA. The generated
	// workflow, the CLI that CI runs, and the agent guide all come from this one version.
	Version string `yaml:"version"`
	// FirstVersion is the version the repository's first release must use.
	FirstVersion string `yaml:"first-version"`
	// NotesDir holds the v<semver>.md release requests.
	NotesDir string `yaml:"notes-dir"`
	// Branch is the branch whose notes changes publish releases.
	Branch   string   `yaml:"branch"`
	Validate Validate `yaml:"validate"`
	Notes    Notes    `yaml:"notes"`
}

// Notes configures the release-note headings. Omit it to use DefaultSections.
type Notes struct {
	Sections []Section `yaml:"sections"`
}

// Section is one release-note heading and what belongs under it.
type Section struct {
	Heading string `yaml:"heading"`
	Include string `yaml:"include"`
	// SummarizeFirst asks for these changes to also appear in the opening sentences.
	SummarizeFirst bool `yaml:"summarize-first"`
}

// DefaultSections are Release Planner's standard categories, in order.
var DefaultSections = []Section{
	{Heading: "✨ New Features", Include: "What users can do that they couldn't before."},
	{Heading: "⬆️ Improvements", Include: "Better behavior, performance, or usability of something that already existed."},
	{Heading: "🐛 Squashed Bugs", Include: "The trigger, the previous wrong behavior, and the corrected result."},
	{Heading: "⛓️‍💥 Breaking Changes", Include: "Who is affected, the old and new behavior, and exact migration steps.", SummarizeFirst: true},
}

// Validate describes the repository's checks, run on the exact commit being released.
type Validate struct {
	Go     string `yaml:"go"`
	Node   string `yaml:"node"`
	Python string `yaml:"python"`
	Run    string `yaml:"run"`
}

var (
	commitSHA   = regexp.MustCompile(`^[0-9a-f]{40}$`)
	toolVersion = regexp.MustCompile(`^[0-9A-Za-z.*+-]+$`)
	branchName  = regexp.MustCompile(`^[0-9A-Za-z._/-]+$`)
)

// Load reads and validates the config at the repository root.
func Load(root string) (Config, error) {
	data, err := os.ReadFile(filepath.Join(root, File))
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("%s not found; create it to adopt Release Planner (see the README)", File)
	}
	if err != nil {
		return Config{}, err
	}
	return Parse(data)
}

// Parse decodes a config, rejecting unknown keys so typos fail loudly, and applies defaults.
func Parse(data []byte) (Config, error) {
	var c Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("%s: %w", File, err)
	}
	if c.FirstVersion == "" {
		c.FirstVersion = "v0.1.0"
	}
	if c.NotesDir == "" {
		c.NotesDir = "releases"
	}
	if c.Branch == "" {
		c.Branch = "main"
	}
	c.Validate.Run = strings.TrimSpace(c.Validate.Run)
	if len(c.Notes.Sections) == 0 {
		c.Notes.Sections = slices.Clone(DefaultSections)
	}
	return c, c.check()
}

func (c Config) check() error {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if _, ok := semver.Parse(c.Version); !ok && !commitSHA.MatchString(c.Version) {
		add("version: pin Release Planner to a release tag such as v0.2.0 or a full 40-character commit SHA, not %q", c.Version)
	}
	if _, ok := semver.Parse(c.FirstVersion); !ok {
		add("first-version: %q is not a version such as v1.0.0", c.FirstVersion)
	}
	if clean := path.Clean(c.NotesDir); clean != c.NotesDir || path.IsAbs(clean) || clean == "." || strings.HasPrefix(clean, "..") {
		add("notes-dir: use a relative directory inside the repository, such as releases")
	}
	if !branchName.MatchString(c.Branch) {
		add("branch: %q is not a valid branch name", c.Branch)
	}
	for name, v := range map[string]string{"go": c.Validate.Go, "node": c.Validate.Node, "python": c.Validate.Python} {
		if v != "" && !toolVersion.MatchString(v) {
			add("validate.%s: %q is not a version such as 1.27.x", name, v)
		}
	}
	for i, s := range c.Notes.Sections {
		heading := strings.TrimSpace(s.Heading)
		if heading == "" || strings.ContainsAny(heading, "\n\r") || strings.HasPrefix(heading, "#") {
			add("notes.sections[%d].heading: give one line of heading text without leading #", i)
		}
		if strings.TrimSpace(s.Include) == "" {
			add("notes.sections[%d].include: say what belongs under %q", i, heading)
		}
		for _, reserved := range []string{"What's Changed", "New Contributors"} {
			if strings.EqualFold(heading, reserved) {
				add("notes.sections[%d].heading: %q is part of the fixed footer", i, reserved)
			}
		}
	}
	if c.Validate.Run == "" {
		add("validate.run: give the command that checks a release, such as make test")
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s:\n  %s", File, strings.Join(problems, "\n  "))
	}
	return nil
}
