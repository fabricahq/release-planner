// Package report writes the release status comment on a release pull request: one comment,
// found by a hidden marker and edited in place, that follows the release from the pull
// request's checks through publication.
package report

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fabricahq/release-planner/internal/notes"
	"github.com/fabricahq/release-planner/internal/plan"
	"github.com/fabricahq/release-planner/internal/publish"
)

// Marker identifies the status comment among a pull request's comments.
const Marker = "<!-- release-planner:status -->"

// Job is one Release workflow job's result and outputs, as the workflow's needs context has them.
type Job struct {
	Result  string            `json:"result"`
	Outputs map[string]string `json:"outputs"`
}

// Asset is a built release file.
type Asset struct {
	Name   string
	Size   int64
	Digest string // sha256:<hex>
}

// Target is one downstream workflow the downstream job ran, as its results output lists it.
type Target struct {
	Repository string `json:"repository"`
	Workflow   string `json:"workflow"`
	Error      string `json:"error,omitempty"`
}

// Status is everything the comment describes.
type Status struct {
	// Server is the GitHub URL, such as https://github.com, and Repository the owner/name.
	Server, Repository string
	RunURL             string
	// Merged is true after the pull request merged, in the run that publishes.
	Merged bool
	// Plan is the validated plan, or nil when validation failed before writing one.
	Plan     *plan.Plan
	Jobs     map[string]Job
	Assets   []Asset
	MergedBy string
}

// jobs are the Release workflow's jobs, in the order they run.
var jobs = []struct{ id, label string }{
	{"validate", "Validate the request"},
	{"release-checks", "Release checks"},
	{"release-assets", "Build the release assets"},
	{"attest", "Attest the release assets"},
	{"publish", "Publish"},
	{"downstream", "Run downstream workflows"},
}

var icons = map[string]string{"success": "✅", "failure": "❌", "cancelled": "⛔"}

// Render writes the comment, or returns "" when there's nothing to report: the pull
// request requests no release and edits no notes, and nothing failed.
func Render(s Status) string {
	failed := ""
	for _, j := range jobs {
		if r := s.Jobs[j.id].Result; failed == "" && (r == "failure" || r == "cancelled") {
			failed = j.id
		}
	}
	p := s.Plan
	if failed == "" && (p == nil || p.Empty()) {
		return ""
	}
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	link := func(tag string) string {
		return fmt.Sprintf("[%s](%s/%s/releases/tag/%s)", tag, s.Server, s.Repository, tag)
	}
	line("%s", Marker)
	switch {
	case p != nil && p.Tag != "":
		line("## Release %s\n", p.Tag)
	case p != nil:
		line("## Release notes edits\n")
	default:
		line("## Release\n")
	}

	published := s.Jobs["publish"].Result == "success"
	if p != nil && p.Tag != "" {
		switch {
		case s.Merged && published:
			line("✅ Published %s.\n", link(p.Tag))
		case !s.Merged && failed == "":
			line("Merging publishes %s.", p.Tag)
			if s.Jobs["validate"].Outputs["build"] != "true" {
				line("Release checks and assets run after the merge, because this pull request comes from a fork.")
			}
			line("")
		}
	}
	if failed != "" {
		line("❌ **The %s job %s.** See [the workflow run](%s).", failed, map[string]string{"failure": "failed", "cancelled": "was cancelled"}[s.Jobs[failed].Result], s.RunURL)
		if s.Merged {
			retry := "Once the cause is fixed, use **Re-run failed jobs** on that run. It uses the same release commit and files, and changes nothing that already succeeded."
			if s.MergedBy != "" {
				retry = "@" + s.MergedBy + " " + retry
			}
			line("%s\n", retry)
		} else {
			line("Fix the cause and push to this branch, and the checks run again. Nothing is published until this pull request merges.\n")
		}
	}

	if p != nil && p.Tag != "" {
		line("| | |\n| --- | --- |")
		version := "`" + p.Tag + "`"
		if p.Prerelease {
			version += " (prerelease)"
		}
		line("| Version | %s |", version)
		line("| Release commit | [`%s`](%s/%s/commit/%s) |", short(p.Commit), s.Server, s.Repository, p.Commit)
		if p.Previous != "" {
			line("| Previous release | %s |\n", link(p.Previous))
		} else {
			line("| Previous release | None: this is the first release |\n")
		}
	}

	var ran []string
	for _, j := range jobs {
		switch job := s.Jobs[j.id]; {
		case job.Result == "skipped" && p != nil && p.Reused && (j.id == "release-checks" || j.id == "release-assets" || j.id == "attest"):
			ran = append(ran, fmt.Sprintf("- ♻️ %s: reused from [the pull request's run](%s/%s/actions/runs/%d)", j.label, s.Server, s.Repository, p.BuildRun))
		case icons[job.Result] != "":
			ran = append(ran, fmt.Sprintf("- %s %s", icons[job.Result], j.label))
		}
	}
	if len(ran) > 0 {
		line("### Jobs\n\n%s\n", strings.Join(ran, "\n"))
	}

	if p != nil {
		findings(&b, p.File, p.Findings)
		if len(p.Edits) > 0 {
			line("### Notes edits\n")
			for _, e := range p.Edits {
				switch {
				case s.Merged && published:
					line("- ✅ Updated the notes of %s from `%s`.", link(e.Tag), e.File)
				case s.Merged:
					line("- The notes of %s aren't updated yet.", link(e.Tag))
				default:
					line("- Merging replaces the notes of %s with `%s`.", link(e.Tag), e.File)
					if e.HandEdited {
						line("  - ⚠️ The %s notes on GitHub differ from `%s` on the release branch: someone edited them on GitHub. Merging replaces them with this file.", e.Tag, e.File)
					}
				}
			}
			line("")
			for _, e := range p.Edits {
				findings(&b, e.File, e.Findings)
			}
		}
		if len(p.Warnings) > 0 {
			line("### Settings\n")
			for _, w := range p.Warnings {
				line("- ⚠️ %s", w)
			}
			line("")
		}
	}

	if len(s.Assets) > 0 {
		line("### Assets\n\n| File | Size | SHA-256 |\n| --- | ---: | --- |")
		for _, a := range s.Assets {
			line("| `%s` | %s | `%s` |", a.Name, size(a.Size), strings.TrimPrefix(a.Digest, "sha256:"))
		}
		line("")
	}

	if d := s.Jobs["downstream"]; d.Result == "success" || d.Result == "failure" {
		var targets []Target
		_ = json.Unmarshal([]byte(d.Outputs["results"]), &targets)
		if len(targets) > 0 {
			line("### Downstream\n")
			for _, t := range targets {
				icon := "✅"
				if t.Error != "" {
					icon = "❌"
				}
				line("- %s [%s `%s`](%s/%s/actions/workflows/%s)", icon, t.Repository, t.Workflow, s.Server, t.Repository, t.Workflow)
			}
			line("")
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func findings(b *strings.Builder, file string, found []notes.Finding) {
	if len(found) == 0 {
		return
	}
	fmt.Fprintf(b, "### Release notes rules\n\n`%s` breaks these rules. They don't block merging; fix them if they're mistakes.\n\n", file)
	for _, f := range found {
		fmt.Fprintf(b, "- %s\n", f.String())
	}
	b.WriteString("\n")
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func size(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// Upsert edits the pull request's status comment to body, or creates it if create is true.
// An empty body leaves a missing comment missing and says an existing one is out of date.
func Upsert(ctx context.Context, gh *publish.GitHub, number int, body string, create bool) error {
	comments, err := gh.Comments(ctx, number)
	if err != nil {
		return err
	}
	for _, c := range comments {
		if strings.Contains(c.Body, Marker) {
			if body == "" {
				body = Marker + "\n## Release\n\nThis pull request no longer requests a release or edits release notes.\n"
			}
			if c.Body == body {
				return nil
			}
			return gh.UpdateComment(ctx, c.ID, body)
		}
	}
	if body == "" || !create {
		return nil
	}
	return gh.CreateComment(ctx, number, body)
}
