package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/fabricahq/release-planner/internal/gitrepo"
	"github.com/fabricahq/release-planner/internal/publish"
	"github.com/fabricahq/release-planner/internal/report"
	"github.com/fabricahq/release-planner/internal/semver"
)

func cmdReport(ctx context.Context, args []string, out io.Writer) error {
	fs, _ := flags("report", "report --needs <json> --branch <name> (--pull-request <number> | --merged <sha>) [--plan <file>] [--assets <dir>] [--downstream <owner/name:workflow.yml>...]")
	needs := fs.String("needs", "", "the Release workflow's needs context, as JSON")
	branch := fs.String("branch", "", "release branch")
	number := fs.String("pull-request", "", "the release pull request, in its own run")
	merged := fs.String("merged", "", "after the merge: the commit the release pull request merged as")
	planFile := fs.String("plan", "", "release plan written by release-planner validate, if validate wrote one")
	assetsDir := fs.String("assets", "", "directory of the built release assets, if any")
	repository := fs.String("repository", os.Getenv("GITHUB_REPOSITORY"), "GitHub repository, as owner/name")
	var downstream targets
	fs.Var(&downstream, "downstream", "a downstream workflow, as owner/name:workflow.yml; repeat for each")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *needs == "" || *branch == "" || !gitrepo.ValidRepository(*repository) || (*number == "") == (*merged == "") {
		fs.Usage()
		return fmt.Errorf("--needs, --branch, --repository (or GITHUB_REPOSITORY), and one of --pull-request or --merged are required")
	}
	server := os.Getenv("GITHUB_SERVER_URL")
	if server == "" {
		server = "https://github.com"
	}
	s := report.Status{Server: server, Repository: *repository, Merged: *merged != "",
		RunURL: fmt.Sprintf("%s/%s/actions/runs/%s", server, *repository, os.Getenv("GITHUB_RUN_ID"))}
	if err := json.Unmarshal([]byte(*needs), &s.Jobs); err != nil {
		return fmt.Errorf("--needs: %v", err)
	}
	if *planFile != "" {
		p, err := readPlan(*planFile)
		switch {
		case err == nil:
			s.Plan, s.MergedBy = &p, p.MergedBy
		case !errors.Is(err, os.ErrNotExist):
			return err
		}
	}
	if *assetsDir != "" {
		// The assets are missing when they weren't built, which the jobs report.
		if files, err := publish.ReadAssets(*assetsDir); err == nil {
			for _, f := range files {
				s.Assets = append(s.Assets, report.Asset{Name: f.Name, Size: int64(len(f.Data)), Digest: f.Digest})
			}
		}
	}

	gh := api(os.Getenv("GITHUB_TOKEN"), *repository)
	s.Downstream = downstream
	switch s.Jobs["downstream"].Result {
	case "success":
		for i := range s.Downstream {
			s.Downstream[i].Result = "success"
		}
	case "failure", "cancelled":
		// The needs context has one result for the whole matrix, so read each target's job.
		run, _ := strconv.ParseInt(os.Getenv("GITHUB_RUN_ID"), 10, 64)
		conclusions, err := gh.JobConclusions(ctx, run)
		if err != nil {
			fmt.Fprintf(out, "::warning title=Release status::Couldn't read the downstream jobs' results: %s\n", escapeData(err.Error()))
		}
		for i, t := range s.Downstream {
			s.Downstream[i].Result = conclusions[t.Job()]
		}
	}
	pr, _ := strconv.Atoi(*number)
	if s.Merged && s.Plan != nil {
		pr = s.Plan.PullRequest
	}
	if s.Merged && pr == 0 {
		// Validation failed before it named the pull request.
		merge, err := gh.MergedPullRequest(ctx, *merged, *branch)
		if err != nil || merge == nil {
			fmt.Fprintf(out, "::warning title=Release status::Found no pull request merged as %s to report on (%v)\n", *merged, err)
			return nil
		}
		pr, s.MergedBy = merge.Number, merge.MergedBy
	}
	body := report.Render(s)
	if body != "" {
		if err := appendEnvFile("GITHUB_STEP_SUMMARY", strings.Replace(body, report.Marker+"\n", "", 1)); err != nil {
			return err
		}
	}
	// A new comment waits for a plan in the pull request, so an ordinary change whose settings
	// fail validation isn't taken for a release. After the merge, every failure is reported.
	create := s.Merged || s.Plan != nil
	if err := report.Upsert(ctx, gh, pr, body, create); err != nil {
		// A fork's pull request gets a read-only token; the step summary still has the report.
		fmt.Fprintf(out, "::warning title=Release status::Couldn't update the release status comment on #%d: %s\n", pr, escapeData(err.Error()))
		return nil
	}
	fmt.Fprintf(out, "Updated the release status on #%d.\n", pr)
	return nil
}

// targets collects repeated --target or --downstream flags.
type targets []report.Target

func (t *targets) String() string { return "" }

func (t *targets) Set(v string) error {
	repository, workflow, ok := strings.Cut(v, ":")
	if !ok || !gitrepo.ValidRepository(repository) || workflow == "" {
		return fmt.Errorf("use owner/name:workflow.yml, not %q", v)
	}
	*t = append(*t, report.Target{Repository: repository, Workflow: workflow})
	return nil
}

func cmdDownstream(ctx context.Context, args []string, out io.Writer) error {
	fs, _ := flags("downstream", "downstream --tag <tag> --target <owner/name:workflow.yml>...")
	tag := fs.String("tag", "", "the new release's tag")
	var list targets
	fs.Var(&list, "target", "a workflow to run, as owner/name:workflow.yml; repeat for each")
	if err := fs.Parse(args); err != nil {
		return err
	}
	version, ok := semver.Parse(*tag)
	if !ok || len(list) == 0 {
		fs.Usage()
		return fmt.Errorf("--tag must be a version tag, and at least one --target is required")
	}
	if version.IsPrerelease() {
		fmt.Fprintf(out, "%s is a prerelease, so no downstream workflows run.\n", *tag)
		return nil
	}
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return fmt.Errorf("set GITHUB_TOKEN to a token that can run workflows in the downstream repositories")
	}
	inputs := map[string]string{"tag": *tag, "version": strings.TrimPrefix(*tag, "v")}
	failed := 0
	for _, t := range list {
		if err := api(token, t.Repository).DispatchWorkflow(ctx, t.Workflow, inputs); err != nil {
			failed++
			fmt.Fprintf(out, "::error title=Downstream::%s\n", escapeData(err.Error()))
			continue
		}
		fmt.Fprintf(out, "Ran %s in %s for %s.\n", t.Workflow, t.Repository, *tag)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d downstream workflows didn't start; %s is published either way", failed, len(list), *tag)
	}
	return nil
}
