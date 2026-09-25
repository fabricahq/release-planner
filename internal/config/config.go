// Package config reads .release-planner/, the folder a repository writes to adopt Release Planner.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/fabricahq/release-planner/internal/notes"
	"github.com/fabricahq/release-planner/internal/semver"
	"go.yaml.in/yaml/v3"
)

// Paths inside a repository, relative to its root. The repository owns every file in Dir.
const (
	Dir    = ".release-planner"
	File   = Dir + "/config.yml"
	Policy = Dir + "/policy.md"
	// StyleFile is the suggested place for a release notes style; the config names the file it uses.
	StyleFile = Dir + "/release-notes-style.md"
)

// How a repository's style file combines with Release Planner's default release notes style.
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
	NotesDir string `yaml:"release-notes-dir"`
	// Branch is the release branch: release pull requests merge into it, and releases publish from it.
	Branch        string        `yaml:"release-branch"`
	ReleaseChecks ReleaseChecks `yaml:"release-checks"`
	ReleaseAssets ReleaseAssets `yaml:"release-assets"`
	// Downstream lists workflows in other repositories to run after each new stable release.
	Downstream        []Downstream      `yaml:"downstream"`
	ReleaseNotesStyle ReleaseNotesStyle `yaml:"release-notes-style"`
	// ReleaseNotesRules sets release notes rules by ID. The only setting is RuleOff, which
	// turns a rule off.
	ReleaseNotesRules map[string]string `yaml:"release-notes-rules"`

	// Style is the content of ReleaseNotesStyle.File, or "" when the repository has none.
	Style string `yaml:"-"`
}

// ReleaseNotesStyle names the repository's own release notes style, if it has one,
// and says how it combines with the default style. Both fields are required together.
type ReleaseNotesStyle struct {
	// File is the markdown file holding the style, relative to the repository root.
	File string `yaml:"file"`
	// Mode is StyleAppend or StyleReplace.
	Mode string `yaml:"mode"`
}

// UnmarshalYAML rejects the one-line form, such as `release-notes-style: replace`, with a
// message that shows the file and mode it needs instead.
func (s *ReleaseNotesStyle) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: release-notes-style needs a file and a mode, such as:\n    release-notes-style:\n      file: %s\n      mode: append", node.Line, StyleFile)
	}
	// node.Decode doesn't inherit the decoder's strictness, so reject unknown keys here.
	for i := 0; i < len(node.Content); i += 2 {
		if key := node.Content[i]; key.Value != "file" && key.Value != "mode" {
			return fmt.Errorf("line %d: field %s not found in release-notes-style; use file and mode", key.Line, key.Value)
		}
	}
	type plain ReleaseNotesStyle
	return node.Decode((*plain)(s))
}

// Set reports whether the config names a release notes style.
func (s ReleaseNotesStyle) Set() bool { return s.File != "" || s.Mode != "" }

// ReleaseChecks describes optional release-only checks, run on the exact commit being released
// before it is tagged. Checks every pull request already runs belong in required PR checks
// instead. Set Run, a script, or Workflow, a reusable workflow; or neither.
type ReleaseChecks struct {
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
func (v ReleaseChecks) Enabled() bool { return v.Run != "" || v.Workflow != "" }

// ReleaseAssets names the workflow that builds the files attached to each release, on the
// release pull request, before approval.
type ReleaseAssets struct {
	// Workflow names a workflow in .github/workflows that accepts workflow_call with string
	// inputs ref, tag, and version, checks out ref, and uploads the files as one artifact
	// named release-assets. It gets no secrets and can only read the repository.
	Workflow string `yaml:"workflow"`
}

// Downstream is a workflow in another repository, run with the new release's tag and
// version after each stable release, such as one that updates a Homebrew tap.
type Downstream struct {
	// Repository is the owner/name the workflow is in.
	Repository string `yaml:"repository"`
	// Workflow is the workflow's file name in that repository's .github/workflows. It needs a
	// workflow_dispatch trigger with string inputs tag and version.
	Workflow string `yaml:"workflow"`
}

// DownstreamOwner is the owner of every downstream repository, which one GitHub App
// installation token covers.
func (c Config) DownstreamOwner() string {
	if len(c.Downstream) == 0 {
		return ""
	}
	owner, _, _ := strings.Cut(c.Downstream[0].Repository, "/")
	return owner
}

// DownstreamRepositories lists the names of the downstream repositories, without the owner.
func (c Config) DownstreamRepositories() []string {
	var names []string
	for _, d := range c.Downstream {
		_, name, _ := strings.Cut(d.Repository, "/")
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}

var (
	commitSHA    = regexp.MustCompile(`^[0-9a-f]{40}$`)
	toolVersion  = regexp.MustCompile(`^[0-9A-Za-z.*+-]+$`)
	branchName   = regexp.MustCompile(`^[0-9A-Za-z._/-]+$`)
	workflowFile = regexp.MustCompile(`^[0-9A-Za-z._-]+\.ya?ml$`)
	repository   = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
)

// GeneratedWorkflow is the Release workflow's file name, which can't also be a workflow it calls.
const GeneratedWorkflow = "release-planner.yml"

// Load reads and validates the config and the optional release notes style at the repository root.
func Load(root string) (Config, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(File)))
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("%s not found; run release-planner init to create it", File)
	}
	if err != nil {
		return Config{}, err
	}
	c, err := decode(data)
	if err != nil {
		return Config{}, err
	}
	var style []byte
	if name := c.ReleaseNotesStyle.File; name != "" && validStylePath(name) {
		style, err = os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, fmt.Errorf("%s:\n  release-notes-style.file: %s does not exist; create it, or remove release-notes-style to use the default style", File, name)
		}
		if err != nil {
			return Config{}, err
		}
	}
	return Parse(data, string(style))
}

// DefaultNotesDir is where release notes live when the config doesn't set release-notes-dir.
// The leading underscore keeps it apart from the repository's own content.
const DefaultNotesDir = "_releases"

// NotesDirIn returns the release-notes-dir a config's content sets, or DefaultNotesDir when
// it sets none. It reads nothing else, so it also reads configs this version would reject.
func NotesDirIn(data []byte) string {
	var c struct {
		NotesDir string `yaml:"release-notes-dir"`
	}
	if yaml.Unmarshal(data, &c) != nil || c.NotesDir == "" {
		return DefaultNotesDir
	}
	return strings.Trim(c.NotesDir, "/")
}

// RuleOff is the release-notes-rules setting that turns a rule off.
const RuleOff = "off"

// RulesOff returns the IDs of the release notes rules the config turns off.
func (c *Config) RulesOff() []string {
	var off []string
	for id, setting := range c.ReleaseNotesRules {
		if setting == RuleOff {
			off = append(off, id)
		}
	}
	slices.Sort(off)
	return off
}

// Parse decodes a config, rejecting unknown keys so typos fail loudly, and applies defaults.
// style is the content of the file release-notes-style names, or "" if it names none.
func Parse(data []byte, style string) (Config, error) {
	c, err := decode(data)
	if err != nil {
		return Config{}, err
	}
	if c.FirstVersion == "" {
		c.FirstVersion = "v0.1.0"
	}
	if c.NotesDir == "" {
		c.NotesDir = DefaultNotesDir
	}
	if c.Branch == "" {
		c.Branch = "main"
	}
	c.ReleaseChecks.Run = strings.TrimSpace(c.ReleaseChecks.Run)
	c.Style = strings.TrimSpace(style)
	return c, c.check()
}

func decode(data []byte) (Config, error) {
	var c Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("%s: %v", File, err)
	}
	return c, nil
}

// validStylePath reports whether name is a markdown file inside the repository.
func validStylePath(name string) bool {
	clean := path.Clean(name)
	return clean == name && !path.IsAbs(clean) && !strings.HasPrefix(clean, "../") && strings.HasSuffix(clean, ".md")
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
		add("release-notes-dir: use a relative directory inside the repository and outside %s, such as %s", Dir, DefaultNotesDir)
	}
	if strings.ContainsAny(c.NotesDir, "*?[]!+\\") || strings.ContainsFunc(c.NotesDir, unicode.IsControl) {
		add("release-notes-dir: %q has characters that GitHub path filters treat as patterns; use letters, digits, and punctuation such as - _ . /", c.NotesDir)
	}
	if !branchName.MatchString(c.Branch) {
		add("release-branch: %q is not a valid branch name", c.Branch)
	}
	for name, v := range map[string]string{"go": c.ReleaseChecks.Go, "node": c.ReleaseChecks.Node, "python": c.ReleaseChecks.Python} {
		if v != "" && !toolVersion.MatchString(v) {
			add("release-checks.%s: %q is not a version such as 1.27.x", name, v)
		}
	}
	v := c.ReleaseChecks
	switch {
	case v.Run != "" && v.Workflow != "":
		add("release-checks: set run or workflow, not both")
	case v.Workflow != "" && (v.Go != "" || v.Node != "" || v.Python != ""):
		add("release-checks: go, node, and python apply to run; set up toolchains in %s instead", v.Workflow)
	case v.Workflow != "" && (!workflowFile.MatchString(v.Workflow) || v.Workflow == GeneratedWorkflow):
		add("release-checks.workflow: name a workflow file in .github/workflows other than %s, such as ci.yml", GeneratedWorkflow)
	case v.Run == "" && v.Workflow == "" && (v.Go != "" || v.Node != "" || v.Python != ""):
		add("release-checks: toolchains are set but there is no run script")
	}
	if w := c.ReleaseAssets.Workflow; w != "" && (!workflowFile.MatchString(w) || w == GeneratedWorkflow) {
		add("release-assets.workflow: name a workflow file in .github/workflows other than %s, such as build-release.yml", GeneratedWorkflow)
	}
	for i, d := range c.Downstream {
		switch {
		case !repository.MatchString(d.Repository):
			add("downstream[%d].repository: name the repository as owner/name, not %q", i, d.Repository)
		case !strings.EqualFold(c.DownstreamOwner(), strings.Split(d.Repository, "/")[0]):
			add("downstream[%d].repository: every downstream repository must belong to %s, so one GitHub App token covers them", i, c.DownstreamOwner())
		}
		if !workflowFile.MatchString(d.Workflow) {
			add("downstream[%d].workflow: name a workflow file in %s's .github/workflows, such as update-formula.yml", i, d.Repository)
		}
		for _, other := range c.Downstream[:i] {
			if other == d {
				add("downstream[%d]: %s in %s is listed twice", i, d.Workflow, d.Repository)
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(c.ReleaseNotesRules)) {
		switch {
		case !notes.Known(id):
			add("release-notes-rules: %q is not a release notes rule; run release-planner validate --rules to list them", id)
		case c.ReleaseNotesRules[id] != RuleOff:
			add("release-notes-rules.%s: use %s, not %q", id, RuleOff, c.ReleaseNotesRules[id])
		}
	}
	if s := c.ReleaseNotesStyle; s.Set() {
		switch {
		case s.File == "":
			add("release-notes-style.file: name your style file, such as %s", StyleFile)
		case !validStylePath(s.File):
			add("release-notes-style.file: use a relative path to a .md file inside the repository, such as %s", StyleFile)
		case c.Style == "":
			add("release-notes-style.file: %s is empty; write your style in it, or remove release-notes-style to use the default style", s.File)
		}
		if s.Mode != StyleAppend && s.Mode != StyleReplace {
			add("release-notes-style.mode: use append or replace, not %q", s.Mode)
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s:\n  %s", File, strings.Join(problems, "\n  "))
	}
	return nil
}
