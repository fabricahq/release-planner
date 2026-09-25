// Package report writes the release status section at the end of a release pull request's
// description, which follows the release from the pull request's checks through publication,
// and, when a merged release fails, a comment that tells whoever merged it.
package report

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/fabricahq/release-planner/internal/notes"
	"github.com/fabricahq/release-planner/internal/plan"
	"github.com/fabricahq/release-planner/internal/publish"
)

// Start and End delimit the release status section at the end of a pull request's
// description. The section is rewritten on every report; the text around it is left alone.
const (
	Start = "<!-- release-planner:status:start -->"
	End   = "<!-- release-planner:status:end -->"
)

// Job is one Release workflow job's result and outputs, as the workflow's needs context has them.
type Job struct {
	Result  string            `json:"result"`
	Outputs map[string]string `json:"outputs"`
}

// Asset is a built release file.
type Asset struct {
	Name string
	Size int64
}

// Target is one downstream workflow, which its own downstream job runs.
type Target struct {
	Repository, Workflow string
	// Result is the conclusion of the target's job, or "" when it's unknown.
	Result string
}

// Job is the name of the target's job in the Release workflow's downstream matrix.
func (t Target) Job() string { return "downstream (" + t.Repository + ":" + t.Workflow + ")" }

// Status is everything the release status section describes.
type Status struct {
	// Server is the GitHub URL, such as https://github.com, and Repository the owner/name.
	Server, Repository string
	RunURL             string
	// RunID and RunAttempt identify the workflow run attempt that reports.
	RunID, RunAttempt string
	// Merged is true after the pull request merged, in the run that publishes.
	Merged bool
	// Plan is the validated plan, or nil when validation failed before writing one.
	Plan   *plan.Plan
	Jobs   map[string]Job
	Assets []Asset
	// Archive is the URL of the release-assets workflow artifact, a zip of the assets, or ""
	// when it's unknown.
	Archive  string
	MergedBy string
	// Downstream lists the downstream workflows, with their jobs' results.
	Downstream []Target
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

// Failed returns the first Release workflow job that failed or was cancelled, or "".
func (s Status) Failed() string {
	for _, j := range jobs {
		if r := s.Jobs[j.id].Result; r == "failure" || r == "cancelled" {
			return j.id
		}
	}
	return ""
}

// Render writes the release status section, without its markers, or returns "" when there's
// nothing to report: the pull request requests no release and edits no notes, and nothing failed.
func Render(s Status) string {
	failed := s.Failed()
	p := s.Plan
	if failed == "" && (p == nil || p.Empty()) {
		return ""
	}
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	link := func(tag string) string {
		return fmt.Sprintf("[%s](%s/%s/releases/tag/%s)", tag, s.Server, s.Repository, tag)
	}
	line("### Release status\n")

	published := s.Jobs["publish"].Result == "success"
	if p != nil && p.Tag != "" {
		version := "`" + p.Tag + "`"
		if p.Prerelease {
			version += " (prerelease)"
		}
		previous := "None"
		if p.Previous != "" {
			previous = link(p.Previous)
		}
		line("| Version | Release commit | Previous release |\n| --- | --- | --- |")
		line("| %s | [`%s`](%s/%s/commit/%s) | %s |\n", version, short(p.Commit), s.Server, s.Repository, p.Commit, previous)
		switch {
		case s.Merged && published:
			line("✅ Published %s.\n", link(p.Tag))
		case !s.Merged && failed == "" && s.Jobs["validate"].Outputs["build"] != "true":
			line("Release checks and assets run after the merge, because this pull request comes from a fork.\n")
		}
	}
	if failed != "" {
		line("❌ **The %s job %s.** See [the workflow run](%s).", failed, map[string]string{"failure": "failed", "cancelled": "was cancelled"}[s.Jobs[failed].Result], s.RunURL)
		if s.Merged {
			line("Once the cause is fixed, use **Re-run failed jobs** on that run. It uses the same release commit and files, and changes nothing that already succeeded.\n")
		} else {
			line("Fix the cause and push to this branch, and the checks run again. Nothing is published until this pull request merges.\n")
		}
	}

	var ran []string
	for _, j := range jobs {
		switch job := s.Jobs[j.id]; {
		case job.Result == "skipped" && p != nil && p.Reused && (j.id == "release-checks" || j.id == "release-assets" || j.id == "attest"):
			ran = append(ran, fmt.Sprintf("♻️ %s ([reused](%s/%s/actions/runs/%d))", j.label, s.Server, s.Repository, p.BuildRun))
		case icons[job.Result] != "":
			ran = append(ran, icons[job.Result]+" "+j.label)
		}
	}
	if len(ran) > 0 {
		line("**Jobs:** %s\n", strings.Join(ran, " · "))
	}

	if p != nil {
		findings(&b, p.File, p.Findings)
		if len(p.Edits) > 0 {
			line("#### Notes edits\n")
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
			line("#### Settings\n")
			for _, w := range p.Warnings {
				line("- ⚠️ %s", w)
			}
			line("")
		}
	}

	if len(s.Assets) > 0 {
		// Before publication, the assets are only a workflow artifact, which downloads as one zip.
		released := published && p != nil && p.Tag != ""
		line("#### Assets\n")
		if !released && s.Archive != "" {
			line("[Download all (zip)](%s): a workflow artifact, for signed-in users who can read this repository, until it expires.\n", s.Archive)
		}
		line("| File | Size |\n| --- | ---: |")
		for _, a := range s.Assets {
			name := "`" + a.Name + "`"
			if released {
				name = fmt.Sprintf("[%s](%s/%s/releases/download/%s/%s)", name, s.Server, s.Repository, p.Tag, url.PathEscape(a.Name))
			}
			line("| %s | %s |", name, size(a.Size))
		}
		line("")
	}

	if r := s.Jobs["downstream"].Result; len(s.Downstream) > 0 && icons[r] != "" {
		line("#### Downstream\n")
		for _, t := range s.Downstream {
			icon := icons[t.Result]
			if icon == "" {
				icon = "❔"
			}
			line("- %s [%s `%s`](%s/%s/actions/workflows/%s)", icon, t.Repository, t.Workflow, s.Server, t.Repository, t.Workflow)
		}
		line("")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func findings(b *strings.Builder, file string, found []notes.Finding) {
	if len(found) == 0 {
		return
	}
	fmt.Fprintf(b, "#### Release notes rules\n\n`%s` breaks these rules. They don't block merging; fix them if they're mistakes.\n\n", file)
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

// Update writes content into the release status section at the end of the pull request's
// description, and reports whether it changed the description. It adds the section only if
// create is true, and never changes the text outside the section. An empty content leaves a
// missing section missing and says an existing one is out of date.
func Update(ctx context.Context, gh *publish.GitHub, number int, content string, create bool) (bool, error) {
	// Read the description just before writing it, so a maintainer's edit is less likely to be lost.
	body, err := gh.PullRequestBody(ctx, number)
	if err != nil {
		return false, err
	}
	updated, found := splice(body, content)
	if !found && (content == "" || !create) {
		return false, nil
	}
	if found && content == "" {
		updated, _ = splice(body, "### Release status\n\nThis pull request no longer requests a release or edits release notes.\n")
	}
	if updated == body {
		return false, nil
	}
	return true, gh.UpdatePullRequestBody(ctx, number, updated)
}

// splice returns body with its status section's content replaced by content, or with the
// section appended after a blank line, and whether body had a section. A section whose end
// marker was deleted runs to the end of body.
func splice(body, content string) (string, bool) {
	section := Start + "\n" + content + "\n" + End
	i := strings.Index(body, Start)
	if i < 0 {
		switch {
		case body == "" || strings.HasSuffix(body, "\n\n") || strings.HasSuffix(body, "\r\n\r\n"):
		case strings.HasSuffix(body, "\n"):
			body += "\n"
		default:
			body += "\n\n"
		}
		return body + section, false
	}
	rest := body[i+len(Start):]
	j := strings.Index(rest, End)
	if j < 0 {
		return body[:i] + section, true
	}
	return body[:i] + section + rest[j+len(End):], true
}

// Failure writes the comment that tells whoever merged the pull request which job failed, or
// returns "" before the merge or when nothing failed. GitHub doesn't notify anyone mentioned
// in an edited description, so this comment is the notification.
func Failure(s Status) string {
	failed := s.Failed()
	if !s.Merged || failed == "" {
		return ""
	}
	mention := ""
	if s.MergedBy != "" {
		mention = "@" + s.MergedBy + " "
	}
	return fmt.Sprintf("%s\n%sThe release's **%s** job %s in [this workflow run](%s). The release status at the end of the description has the details and how to retry.\n",
		failureMarker(s, failed), mention, failed, map[string]string{"failure": "failed", "cancelled": "was cancelled"}[s.Jobs[failed].Result], s.RunURL)
}

// failureMarker identifies a failure comment by the run attempt and job it reports.
func failureMarker(s Status, job string) string {
	return fmt.Sprintf("%srun=%s attempt=%s job=%s -->", failurePrefix, s.RunID, s.RunAttempt, job)
}

const failurePrefix = "<!-- release-planner:failure "

// Notify posts the Failure comment on the pull request, unless there's nothing to report or
// the latest failure comment already reports the same run attempt and job, and reports
// whether it posted.
func Notify(ctx context.Context, gh *publish.GitHub, number int, s Status) (bool, error) {
	comment := Failure(s)
	if comment == "" {
		return false, nil
	}
	comments, err := gh.Comments(ctx, number)
	if err != nil {
		return false, err
	}
	for i := len(comments) - 1; i >= 0; i-- {
		if strings.Contains(comments[i].Body, failurePrefix) {
			if strings.Contains(comments[i].Body, failureMarker(s, s.Failed())) {
				return false, nil
			}
			break
		}
	}
	return true, gh.CreateComment(ctx, number, comment)
}
