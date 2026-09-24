// Package generate renders the files Release Planner writes into a repository and keeps them current.
package generate

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"strings"
	"text/template"

	"github.com/fabricahq/release-planner/internal/config"
	"github.com/fabricahq/release-planner/internal/semver"
)

// Module is the command every generated file runs, at the version the config pins.
const Module = "github.com/fabricahq/release-planner/cmd/release-planner"

// Actions pins every third-party action the workflow uses to a reviewed commit.
var Actions = struct {
	Checkout, SetupGo, SetupNode, SetupPython, UploadArtifact, DownloadArtifact string
}{
	Checkout:         "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1",
	SetupGo:          "actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0",
	SetupNode:        "actions/setup-node@820762786026740c76f36085b0efc47a31fe5020 # v7.0.0",
	SetupPython:      "actions/setup-python@5fda3b95a4ea91299a34e894583c3862153e4b97 # v7.0.0",
	UploadArtifact:   "actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7.0.1",
	DownloadArtifact: "actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c # v8.0.1",
}

//go:embed templates
var templates embed.FS

var parsed = template.Must(template.New("").Option("missingkey=error").ParseFS(templates, "templates/*.tmpl"))

type data struct {
	config.Config
	Module      string
	Actions     any
	Marker      string
	ValidateRun string
	PolicyPath  string
	StyleText   string
	// IsRelease is true when the config pins a release tag, which ships binaries;
	// a commit pin is built from source with Go.
	IsRelease bool
	// Install is the command that installs the pinned version on a developer's machine.
	Install string
	// StartsBeforeOne is true when the first release is 0.x, so the policy needs pre-1.0 rules.
	StartsBeforeOne bool
}

func render(name string, c config.Config, marker string) string {
	var b bytes.Buffer
	// Indent the validation script as a YAML block scalar under `run: |`.
	var run strings.Builder
	for i, line := range strings.Split(c.Validate.Run, "\n") {
		if i > 0 {
			run.WriteByte('\n')
		}
		if line = strings.TrimRight(line, " \t"); line != "" {
			run.WriteString("          " + line)
		}
	}
	d := data{Config: c, Module: Module, Actions: Actions, Marker: marker, ValidateRun: run.String(),
		PolicyPath: config.Policy, StyleText: style(c), IsRelease: IsRelease(c.Version), Install: InstallCommand(c.Version),
		StartsBeforeOne: semver.MustParse(c.FirstVersion).Major == 0}
	if err := parsed.ExecuteTemplate(&b, name, d); err != nil {
		// Templates are embedded and the config is validated, so this is a programming error.
		panic(err)
	}
	return b.String()
}

// IsRelease reports whether a pinned version is a release tag, which has published binaries.
func IsRelease(version string) bool {
	_, ok := semver.Parse(version)
	return ok
}

// InstallCommand installs a pinned version: the release binary, or a build from source for a commit.
func InstallCommand(version string) string {
	if IsRelease(version) {
		return "curl -fsSL https://raw.githubusercontent.com/fabricahq/release-planner/" + version + "/install.sh | sh -s -- --version " + version
	}
	return "go install " + Module + "@" + version
}

// DefaultStyle is Release Planner's release notes style, which a repository can append to or replace.
func DefaultStyle() string {
	var b bytes.Buffer
	if err := parsed.ExecuteTemplate(&b, "style.md.tmpl", nil); err != nil {
		panic(err)
	}
	return b.String()
}

// style combines the default release notes style with the repository's style file.
func style(c config.Config) string {
	switch {
	case c.Style == "":
		return strings.TrimSpace(DefaultStyle())
	case c.ReleaseNotesStyle.Mode == config.StyleReplace:
		return c.Style
	}
	return strings.TrimSpace(DefaultStyle()) + "\n\nThis repository adds:\n\n" + c.Style
}

// Guide renders the agent release procedure for this config.
func Guide(c config.Config) string { return render("guide.md.tmpl", c, "") }

// InitConfig renders the starter config that release-planner init writes.
func InitConfig(version, firstVersion string) string {
	var b bytes.Buffer
	if err := parsed.ExecuteTemplate(&b, "config.yml.tmpl", map[string]string{"Version": version, "FirstVersion": firstVersion}); err != nil {
		panic(err)
	}
	return b.String()
}

// Policy renders the starter release policy a repository fills in.
func Policy(c config.Config) string { return render("policy.md.tmpl", c, "") }

func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}
