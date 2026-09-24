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
	d := data{Config: c, Module: Module, Actions: Actions, Marker: marker, ValidateRun: run.String()}
	if err := parsed.ExecuteTemplate(&b, name, d); err != nil {
		// Templates are embedded and the config is validated, so this is a programming error.
		panic(err)
	}
	return b.String()
}

// Guide renders the agent release procedure for this config.
func Guide(c config.Config) string { return render("guide.md.tmpl", c, "") }

// Policy renders the starter release policy a repository fills in.
func Policy(c config.Config) string { return render("policy.md.tmpl", c, "") }

func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}
