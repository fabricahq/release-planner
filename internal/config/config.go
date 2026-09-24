// Package config reads .release-planner/, the folder a repository writes to adopt Release Planner.
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
	"strings"

	"github.com/fabricahq/release-planner/internal/semver"
	"go.yaml.in/yaml/v3"
)

// Paths inside a repository, relative to its root. The repository owns every file in Dir.
const (
	Dir       = ".release-planner"
	File      = Dir + "/config.yml"
	Policy    = Dir + "/policy.md"
	StyleFile = Dir + "/release-notes-style.md"
)

// How StyleFile combines with Release Planner's default release notes style.
const (
	StyleAppend  = "append"
	StyleReplace = "replace"
)

// SchemaVersion is the config format this Release Planner reads.
const SchemaVersion = 1

// Config is the repository's release configuration.
type Config struct {
	// SchemaVersion is the config format, so a future format change fails loudly
	// instead of being misread.
	SchemaVersion int `yaml:"schema-version"`
	// Version pins Release Planner itself: a release tag or a full commit SHA. The generated
	// workflow, the CLI that CI runs, and the agent guide all come from this one version.
	Version string `yaml:"version"`
	// FirstVersion is the version the repository's first release must use.
	FirstVersion string `yaml:"first-version"`
	// NotesDir holds the v<semver>.md release notes, one file per release.
	NotesDir string `yaml:"notes-dir"`
	// Branch is the branch whose notes changes publish releases.
	Branch   string   `yaml:"branch"`
	Validate Validate `yaml:"validate"`
	// ReleaseNotesStyle says whether StyleFile appends to or replaces the default style.
	ReleaseNotesStyle string `yaml:"release-notes-style"`

	// Style is StyleFile's content, or "" when the repository has none.
	Style string `yaml:"-"`
}

// Validate describes optional release-only checks, run on the exact commit being released
// before it is tagged. Checks every pull request already runs belong in required PR checks
// instead. Set Run, a script, or Workflow, a reusable workflow; or neither.
type Validate struct {
	// Toolchains to set up before Run.
	Go     string `yaml:"go"`
	Node   string `yaml:"node"`
	Python string `yaml:"python"`
	// Run is a Bash script; any failing command stops the release.
	Run string `yaml:"run"`
	// Workflow names a workflow in .github/workflows that accepts workflow_call with a
	// `ref` input. It runs with the repository's secrets, but never the release-write token.
	Workflow string `yaml:"workflow"`
}

// Enabled reports whether the repository has release-only checks.
func (v Validate) Enabled() bool { return v.Run != "" || v.Workflow != "" }

var (
	commitSHA    = regexp.MustCompile(`^[0-9a-f]{40}$`)
	toolVersion  = regexp.MustCompile(`^[0-9A-Za-z.*+-]+$`)
	branchName   = regexp.MustCompile(`^[0-9A-Za-z._/-]+$`)
	workflowFile = regexp.MustCompile(`^[0-9A-Za-z._-]+\.ya?ml$`)
)

// Load reads and validates the config and the optional release notes style at the repository root.
func Load(root string) (Config, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(File)))
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("%s not found; run release-planner init to create it", File)
	}
	if err != nil {
		return Config{}, err
	}
	style, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(StyleFile)))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, err
	}
	return Parse(data, string(style))
}

// Parse decodes a config, rejecting unknown keys so typos fail loudly, and applies defaults.
// style is the content of the release notes style file, or "" if there is none.
func Parse(data []byte, style string) (Config, error) {
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
	if c.ReleaseNotesStyle == "" {
		c.ReleaseNotesStyle = StyleAppend
	}
	c.Validate.Run = strings.TrimSpace(c.Validate.Run)
	c.Style = strings.TrimSpace(style)
	return c, c.check()
}

func (c Config) check() error {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if c.SchemaVersion != SchemaVersion {
		add("schema-version: this Release Planner reads schema-version %d, not %d; set it, or pin the Release Planner version that reads this config", SchemaVersion, c.SchemaVersion)
	}
	if _, ok := semver.Parse(c.Version); !ok && !commitSHA.MatchString(c.Version) {
		add("version: pin Release Planner to a release tag such as v0.2.0 or a full 40-character commit SHA, not %q", c.Version)
	}
	if _, ok := semver.Parse(c.FirstVersion); !ok {
		add("first-version: %q is not a version such as v1.0.0", c.FirstVersion)
	}
	if clean := path.Clean(c.NotesDir); clean != c.NotesDir || path.IsAbs(clean) || clean == "." || strings.HasPrefix(clean, "..") || clean == Dir || strings.HasPrefix(clean, Dir+"/") {
		add("notes-dir: use a relative directory inside the repository and outside %s, such as releases", Dir)
	}
	if !branchName.MatchString(c.Branch) {
		add("branch: %q is not a valid branch name", c.Branch)
	}
	for name, v := range map[string]string{"go": c.Validate.Go, "node": c.Validate.Node, "python": c.Validate.Python} {
		if v != "" && !toolVersion.MatchString(v) {
			add("validate.%s: %q is not a version such as 1.27.x", name, v)
		}
	}
	v := c.Validate
	switch {
	case v.Run != "" && v.Workflow != "":
		add("validate: set run or workflow, not both")
	case v.Workflow != "" && (v.Go != "" || v.Node != "" || v.Python != ""):
		add("validate: go, node, and python apply to run; set up toolchains in %s instead", v.Workflow)
	case v.Workflow != "" && (!workflowFile.MatchString(v.Workflow) || v.Workflow == "release-planner.yml"):
		add("validate.workflow: name a workflow file in .github/workflows, such as ci.yml")
	case v.Run == "" && v.Workflow == "" && (v.Go != "" || v.Node != "" || v.Python != ""):
		add("validate: toolchains are set but there is no run script")
	}
	switch c.ReleaseNotesStyle {
	case StyleAppend:
	case StyleReplace:
		if c.Style == "" {
			add("release-notes-style: replace needs %s with your style; create it, or remove this setting to use the default style", StyleFile)
		}
	default:
		add("release-notes-style: use append or replace, not %q", c.ReleaseNotesStyle)
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s:\n  %s", File, strings.Join(problems, "\n  "))
	}
	return nil
}
