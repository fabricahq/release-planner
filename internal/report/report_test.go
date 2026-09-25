package report

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/fabricahq/release-planner/internal/notes"
	"github.com/fabricahq/release-planner/internal/plan"
	"github.com/fabricahq/release-planner/internal/publish"
)

const commit = "0123456789abcdef0123456789abcdef01234567"

func release() *plan.Plan {
	return &plan.Plan{Tag: "v1.2.0", Version: "1.2.0", Commit: commit, Previous: "v1.1.0", File: "_releases/v1.2.0.md", BuildRun: 100}
}

func results(r ...string) map[string]Job {
	m := map[string]Job{}
	for i := 0; i+1 < len(r); i += 2 {
		m[r[i]] = Job{Result: r[i+1], Outputs: map[string]string{"build": "true"}}
	}
	return m
}

func status(p *plan.Plan, merged bool, j map[string]Job) Status {
	return Status{Server: "https://github.com", Repository: "o/r", RunURL: "https://github.com/o/r/actions/runs/100", RunID: "100", RunAttempt: "1",
		Branch: "main", HeadRef: "release-v1.2.0", Merged: merged, Plan: p, Jobs: j}
}

// runJobs lists a run's jobs, each linked at https://github.com/o/r/actions/runs/<run>/job/<n>.
func runJobs(run string, names ...string) []publish.RunJob {
	var jobs []publish.RunJob
	for i, name := range names {
		jobs = append(jobs, publish.RunJob{Name: name, URL: fmt.Sprintf("https://github.com/o/r/actions/runs/%s/job/%d", run, i+1)})
	}
	return jobs
}

func contains(t *testing.T, body string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("lacks %q:\n%s", w, body)
		}
	}
}

func lacks(t *testing.T, body string, unwanted ...string) {
	t.Helper()
	for _, w := range unwanted {
		if strings.Contains(body, w) {
			t.Errorf("has %q:\n%s", w, body)
		}
	}
}

func equal(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// The layout the maintainer approved, for a release pull request with assets before the merge.
func TestRendersTheReleasePullRequestLayout(t *testing.T) {
	const run = "https://github.com/fabricahq/release-planner-sandbox/actions/runs/36167909638"
	s := Status{Server: "https://github.com", Repository: "fabricahq/release-planner-sandbox", RunURL: run, RunID: "36167909638", RunAttempt: "1",
		Branch: "main", HeadRef: "claude/release-v0.1.0",
		Plan: &plan.Plan{Tag: "v0.1.0", Version: "0.1.0", Commit: "2854d3febaf7c27b76d8a81cd1a279bb782a8464", File: "_releases/v0.1.0.md", BuildRun: 36167909638},
		Jobs: results("validate", "success", "release-assets", "success", "attest", "success", "publish", "skipped"),
		RunJobs: []publish.RunJob{
			{Name: "validate", URL: run + "/job/108180056110"},
			{Name: "release-assets / build", URL: run + "/job/108180239187"},
			{Name: "attest", URL: run + "/job/108180464217"},
			{Name: "report", URL: run + "/job/108180600000"},
		},
		BuildsAssets: true,
		Assets:       []Asset{{"SHA256SUMS", 390}, {"greet_0.1.0_darwin_amd64.tar.gz", 736973}, {"greet_0.1.0_linux_amd64.tar.gz", 2 << 20}},
		Archive:      run + "/artifacts/10878168024",
	}
	body := splice("Why v0.1.0? It's the first release, and the policy starts at v0.1.0.", Render(s))
	equal(t, body, `<!-- release-planner:summary:start -->
**[✏️ Edit the v0.1.0 release notes](https://github.com/fabricahq/release-planner-sandbox/edit/claude/release-v0.1.0/_releases/v0.1.0.md)**

**When you merge this PR:**
- The release commit, `+"`2854d3f`"+`, is tagged `+"`v0.1.0`"+`.
- The v0.1.0 GitHub release is published with these release notes and the files listed below.
- This description updates with a link to the release, and you're @mentioned if anything fails.

### Release status

| Version | Release commit | Previous release |
| --- | --- | --- |
| `+"`v0.1.0`"+` | [`+"`2854d3f`"+`](https://github.com/fabricahq/release-planner-sandbox/commit/2854d3febaf7c27b76d8a81cd1a279bb782a8464) | None |
<!-- release-planner:summary:end -->

Why v0.1.0? It's the first release, and the policy starts at v0.1.0.

<!-- release-planner:status:start -->
#### Jobs

| | Job | |
| :-: | --- | --- |
| ✅ | Check the version and release notes | [Details](https://github.com/fabricahq/release-planner-sandbox/actions/runs/36167909638/job/108180056110) |
| ✅ | Build the release assets | [Details](https://github.com/fabricahq/release-planner-sandbox/actions/runs/36167909638/job/108180239187) |
| ✅ | Attest the release assets | [Details](https://github.com/fabricahq/release-planner-sandbox/actions/runs/36167909638/job/108180464217) |
| ⏸️ | Publish | Runs when you merge |

#### Assets

[Download all (zip)](https://github.com/fabricahq/release-planner-sandbox/actions/runs/36167909638/artifacts/10878168024): a workflow artifact, for signed-in users who can read this repository, until it expires.

| File | Size |
| --- | ---: |
| `+"`SHA256SUMS`"+` | 390 B |
| `+"`greet_0.1.0_darwin_amd64.tar.gz`"+` | 719.7 KiB |
| `+"`greet_0.1.0_linux_amd64.tar.gz`"+` | 2.0 MiB |
<!-- release-planner:status:end -->`)

	// Without the artifact's ID, the zip link is left out; without the jobs, each links the run.
	s.Archive, s.RunJobs = "", nil
	body = Render(s).Status
	lacks(t, body, "Download all")
	contains(t, body, "| ✅ | Build the release assets | [Details]("+run+") |")

	// Assets that aren't built yet aren't "listed below".
	s.Assets = nil
	contains(t, Render(s).Summary, "- The v0.1.0 GitHub release is published with these release notes and the release assets.\n")
	s.BuildsAssets = false
	contains(t, Render(s).Summary, "- The v0.1.0 GitHub release is published with these release notes.\n")
}

func TestRendersWhatMergingDoes(t *testing.T) {
	s := status(release(), false, results("validate", "success", "release-checks", "success", "publish", "skipped"))
	s.Downstream = []Target{{Repository: "o/tap", Workflow: "update.yml"}}
	summary := Render(s).Summary
	contains(t, summary, "**[✏️ Edit the v1.2.0 release notes](https://github.com/o/r/edit/release-v1.2.0/_releases/v1.2.0.md)**\n\n",
		"- The v1.2.0 GitHub release is published with these release notes.\n- Then o/tap `update.yml` runs.\n- This description updates",
		"| `v1.2.0` | [`0123456`](https://github.com/o/r/commit/"+commit+") | [v1.1.0](https://github.com/o/r/releases/tag/v1.1.0) |\n")

	s.Downstream = append(s.Downstream, Target{Repository: "o/bucket", Workflow: "update.yml"})
	contains(t, Render(s).Summary, "- Then these workflows run: o/tap `update.yml`, o/bucket `update.yml`.\n")

	// Prereleases start no downstream workflows.
	s.Plan.Tag, s.Plan.Prerelease = "v1.2.0-rc.1", true
	summary = Render(s).Summary
	contains(t, summary, "| `v1.2.0-rc.1` (prerelease) |")
	lacks(t, summary, "Then")

	// A fork's branch is edited in the fork, and its checks wait for the merge.
	s = status(release(), false, results("validate", "success", "publish", "skipped"))
	s.HeadRepository, s.Jobs["validate"].Outputs["build"] = "someone/r", "false"
	blocks := Render(s)
	contains(t, blocks.Summary, "(https://github.com/someone/r/edit/release-v1.2.0/_releases/v1.2.0.md)")
	if !strings.HasSuffix(blocks.Status, "| ⏸️ | Publish | Runs when you merge |\n\nRelease checks and assets run after the merge, because this pull request comes from a fork.\n") {
		t.Error(blocks.Status)
	}

	// Settings warnings close the status.
	s.Plan.Warnings = []string{"The release environment has no deployment branch rule."}
	contains(t, Render(s).Status, "| Runs when you merge |\n\n⚠️ The release environment has no deployment branch rule.\n\nRelease checks and assets run after the merge")
}

func TestRendersAPublishedRelease(t *testing.T) {
	p := release()
	p.Reused, p.BuildRun, p.Merged, p.PullRequest = true, 77, "dddddddddddddddddddddddddddddddddddddddd", 7
	s := status(p, true, results("validate", "success", "release-checks", "skipped", "release-assets", "skipped", "attest", "skipped", "publish", "success", "downstream", "success"))
	s.RunJobs = runJobs("200", "validate", "publish", "downstream (o/tap:update.yml)", "downstream (o/bucket:update.yml)", "report")
	s.BuildJobs = runJobs("77", "validate", "release-checks / test", "release-checks / lint", "release-assets / build", "attest", "report")
	s.Downstream = []Target{{"o/tap", "update.yml", "success"}, {"o/bucket", "update.yml", "success"}}
	s.BuildsAssets, s.Assets = true, []Asset{{"tool_linux_amd64.tar.gz", 3 << 20}, {"SHA256SUMS", 120}}
	s.Archive = "https://github.com/o/r/actions/runs/77/artifacts/9"
	blocks := Render(s)
	equal(t, blocks.Summary, `**[✏️ Edit the v1.2.0 release notes](https://github.com/o/r/edit/main/_releases/v1.2.0.md)**

✅ Published [v1.2.0](https://github.com/o/r/releases/tag/v1.2.0) from `+"`0123456`"+`.

### Release status

| Version | Release commit | Previous release |
| --- | --- | --- |
| `+"`v1.2.0`"+` | [`+"`0123456`"+`](https://github.com/o/r/commit/`+commit+`) | [v1.1.0](https://github.com/o/r/releases/tag/v1.1.0) |
`)
	equal(t, blocks.Status, `#### Jobs

| | Job | |
| :-: | --- | --- |
| ✅ | Check the version and release notes | [Details](https://github.com/o/r/actions/runs/200/job/1) |
| ♻️ | Run the release checks | [Reused from the pull request](https://github.com/o/r/actions/runs/77/job/2) |
| ♻️ | Build the release assets | [Reused from the pull request](https://github.com/o/r/actions/runs/77/job/4) |
| ♻️ | Attest the release assets | [Reused from the pull request](https://github.com/o/r/actions/runs/77/job/5) |
| ✅ | Publish | [Details](https://github.com/o/r/actions/runs/200/job/2) |
| ✅ | Run o/tap `+"`update.yml`"+` | [Details](https://github.com/o/r/actions/runs/200/job/3) |
| ✅ | Run o/bucket `+"`update.yml`"+` | [Details](https://github.com/o/r/actions/runs/200/job/4) |

#### Assets

| File | Size |
| --- | ---: |
| [`+"`tool_linux_amd64.tar.gz`"+`](https://github.com/o/r/releases/download/v1.2.0/tool_linux_amd64.tar.gz) | 3.0 MiB |
| [`+"`SHA256SUMS`"+`](https://github.com/o/r/releases/download/v1.2.0/SHA256SUMS) | 120 B |
`)

	// Without the pull request's jobs, a reused job links its run.
	s.BuildJobs = nil
	contains(t, Render(s).Status, "| ♻️ | Build the release assets | [Reused from the pull request](https://github.com/o/r/actions/runs/77) |")

	// Downstream jobs that never ran, such as for a prerelease, list nothing.
	s.Jobs["downstream"] = Job{Result: "skipped"}
	lacks(t, Render(s).Status, "Run o/")
}

func TestRendersNotesEdits(t *testing.T) {
	p := &plan.Plan{Edits: []plan.Edit{{Tag: "v1.0.0", File: "_releases/v1.0.0.md", HandEdited: true}, {Tag: "v1.1.0", File: "_releases/v1.1.0.md"}}}
	s := status(p, false, results("validate", "success", "publish", "skipped"))
	s.HeadRef, s.RunJobs = "fix-notes", runJobs("100", "validate", "report")
	blocks := Render(s)
	equal(t, blocks.Summary, `**[✏️ Edit the v1.0.0 release notes](https://github.com/o/r/edit/fix-notes/_releases/v1.0.0.md)**

**[✏️ Edit the v1.1.0 release notes](https://github.com/o/r/edit/fix-notes/_releases/v1.1.0.md)**

**When you merge this PR:**
- The v1.0.0 release notes on GitHub are replaced with `+"`_releases/v1.0.0.md`"+`. Its tag and files don't change.
- ⚠️ Someone edited the v1.0.0 notes on GitHub since they merged, so they differ from `+"`_releases/v1.0.0.md`"+` on the release branch. Merging replaces those edits.
- The v1.1.0 release notes on GitHub are replaced with `+"`_releases/v1.1.0.md`"+`. Its tag and files don't change.
- This description updates with a link to the release, and you're @mentioned if anything fails.

### Release status

| Version | Release notes file |
| --- | --- |
| [v1.0.0](https://github.com/o/r/releases/tag/v1.0.0) | `+"`_releases/v1.0.0.md`"+` |
| [v1.1.0](https://github.com/o/r/releases/tag/v1.1.0) | `+"`_releases/v1.1.0.md`"+` |
`)
	equal(t, blocks.Status, `#### Jobs

| | Job | |
| :-: | --- | --- |
| ✅ | Check the version and release notes | [Details](https://github.com/o/r/actions/runs/100/job/1) |
| ⏸️ | Publish | Runs when you merge |
`)

	s.Merged, s.Jobs["publish"] = true, Job{Result: "success"}
	s.RunJobs = runJobs("200", "validate", "publish", "report")
	blocks = Render(s)
	equal(t, blocks.Summary, `**[✏️ Edit the v1.0.0 release notes](https://github.com/o/r/edit/main/_releases/v1.0.0.md)**

**[✏️ Edit the v1.1.0 release notes](https://github.com/o/r/edit/main/_releases/v1.1.0.md)**

✅ Updated the notes of [v1.0.0](https://github.com/o/r/releases/tag/v1.0.0).

✅ Updated the notes of [v1.1.0](https://github.com/o/r/releases/tag/v1.1.0).

### Release status

| Version | Release notes file |
| --- | --- |
| [v1.0.0](https://github.com/o/r/releases/tag/v1.0.0) | `+"`_releases/v1.0.0.md`"+` |
| [v1.1.0](https://github.com/o/r/releases/tag/v1.1.0) | `+"`_releases/v1.1.0.md`"+` |
`)
	equal(t, blocks.Status, `#### Jobs

| | Job | |
| :-: | --- | --- |
| ✅ | Check the version and release notes | [Details](https://github.com/o/r/actions/runs/200/job/1) |
| ✅ | Publish | [Details](https://github.com/o/r/actions/runs/200/job/2) |
`)

	// A release and a notes edit in one pull request.
	both := release()
	both.Edits = p.Edits[1:]
	s = status(both, false, results("validate", "success", "publish", "skipped"))
	contains(t, Render(s).Summary, "(https://github.com/o/r/edit/release-v1.2.0/_releases/v1.2.0.md)**\n\n**[✏️ Edit the v1.1.0 release notes](https://github.com/o/r/edit/release-v1.2.0/_releases/v1.1.0.md)**\n\n",
		"- The v1.2.0 GitHub release is published with these release notes.\n- The v1.1.0 release notes on GitHub are replaced with `_releases/v1.1.0.md`. Its tag and files don't change.\n- This description updates",
		"| [v1.1.0](https://github.com/o/r/releases/tag/v1.1.0) |\n\n| Version | Release notes file |\n| --- | --- |\n| [v1.1.0](https://github.com/o/r/releases/tag/v1.1.0) | `_releases/v1.1.0.md` |\n")
	s.Merged, s.Jobs["publish"] = true, Job{Result: "success"}
	contains(t, Render(s).Summary, "✅ Published [v1.2.0](https://github.com/o/r/releases/tag/v1.2.0) from `0123456`.\n\n✅ Updated the notes of [v1.1.0](https://github.com/o/r/releases/tag/v1.1.0).\n\n### Release status")
}

func TestRendersFailures(t *testing.T) {
	// After the merge, the outcome replaces what merging does.
	s := status(release(), true, results("validate", "success", "release-checks", "success", "publish", "failure"))
	s.MergedBy, s.RunJobs = "mona", runJobs("100", "validate", "release-checks", "publish", "report")
	blocks := Render(s)
	equal(t, blocks.Summary, `**[✏️ Edit the v1.2.0 release notes](https://github.com/o/r/edit/main/_releases/v1.2.0.md)**

❌ **The publish job failed.** See [the workflow run](https://github.com/o/r/actions/runs/100). Once the cause is fixed, use **Re-run failed jobs** on that run. It uses the same release commit and files, and changes nothing that already succeeded.

### Release status

| Version | Release commit | Previous release |
| --- | --- | --- |
| `+"`v1.2.0`"+` | [`+"`0123456`"+`](https://github.com/o/r/commit/`+commit+`) | [v1.1.0](https://github.com/o/r/releases/tag/v1.1.0) |
`)
	equal(t, blocks.Status, `#### Jobs

| | Job | |
| :-: | --- | --- |
| ✅ | Check the version and release notes | [Details](https://github.com/o/r/actions/runs/100/job/1) |
| ✅ | Run the release checks | [Details](https://github.com/o/r/actions/runs/100/job/2) |
| ❌ | Publish | [Details](https://github.com/o/r/actions/runs/100/job/3) |
`)

	// Merged but not yet published: the assets are still only the zip.
	s.Assets, s.Archive = []Asset{{"SHA256SUMS", 120}}, "https://github.com/o/r/actions/runs/100/artifacts/9"
	contains(t, Render(s).Status, "[Download all (zip)]", "| `SHA256SUMS` | 120 B |")
	lacks(t, Render(s).Status, "releases/download")

	// A downstream failure follows the published release.
	s = status(release(), true, results("validate", "success", "publish", "success", "downstream", "failure"))
	s.Downstream = []Target{{"o/tap", "update.yml", "success"}, {"o/bucket", "update.yml", "failure"}, {"o/other", "update.yml", ""}}
	blocks = Render(s)
	contains(t, blocks.Summary, "✅ Published [v1.2.0](https://github.com/o/r/releases/tag/v1.2.0) from `0123456`.\n\n❌ **The downstream job failed.**")
	contains(t, blocks.Status, "| ✅ | Publish | [Details](https://github.com/o/r/actions/runs/100) |\n| ✅ | Run o/tap `update.yml` |",
		"| ❌ | Run o/bucket `update.yml` |", "| ❔ | Run o/other `update.yml` |")

	// Before the merge, a failure says how to fix it, and merging still waits.
	blocks = Render(status(release(), false, results("validate", "success", "release-checks", "failure", "publish", "skipped")))
	contains(t, blocks.Summary, "**\n\n❌ **The release-checks job failed.** See [the workflow run](https://github.com/o/r/actions/runs/100). Fix the cause and push to this branch, and the checks run again. Nothing is published until this pull request merges.\n\n**When you merge this PR:**\n")
	contains(t, blocks.Status, "| ❌ | Run the release checks |", "| ⏸️ | Publish | Runs when you merge |")
	lacks(t, blocks.Summary, "Re-run")

	// Validation failed before it wrote a plan.
	blocks = Render(status(nil, true, results("validate", "failure", "publish", "skipped")))
	equal(t, blocks.Summary, "❌ **The validate job failed.** See [the workflow run](https://github.com/o/r/actions/runs/100). Once the cause is fixed, use **Re-run failed jobs** on that run. It uses the same release commit and files, and changes nothing that already succeeded.\n")
	equal(t, blocks.Status, "#### Jobs\n\n| | Job | |\n| :-: | --- | --- |\n| ❌ | Check the version and release notes | [Details](https://github.com/o/r/actions/runs/100) |\n")

	// A pull request that requests nothing says nothing about merging.
	blocks = Render(status(&plan.Plan{}, false, results("validate", "failure", "publish", "skipped")))
	equal(t, blocks.Summary, "❌ **The validate job failed.** See [the workflow run](https://github.com/o/r/actions/runs/100). Fix the cause and push to this branch, and the checks run again. Nothing is published until this pull request merges.\n")
	lacks(t, blocks.Status, "Publish")

	blocks = Render(status(release(), false, results("validate", "success", "release-assets", "success", "attest", "cancelled")))
	contains(t, blocks.Summary, "❌ **The attest job was cancelled.**")
	contains(t, blocks.Status, "| ⚪ | Attest the release assets |")
}

func TestRendersRuleWarnings(t *testing.T) {
	p := release()
	p.Findings = []notes.Finding{{Rule: "no-empty-heading", Line: 3, Message: `"## Fixes" has no content`}}
	p.Edits = []plan.Edit{{Tag: "v1.1.0", File: "_releases/v1.1.0.md", Findings: []notes.Finding{{Rule: "closing-link", Line: 9, Message: "the last line doesn't link the full changelog"}}}}
	s := status(p, false, results("validate", "success", "publish", "skipped"))
	equal(t, Render(s).Status, `#### Jobs

| | Job | |
| :-: | --- | --- |
| ⚠️ | Check the version and release notes (2 warnings) | [Details](https://github.com/o/r/actions/runs/100) |
| ⏸️ | Publish | Runs when you merge |

#### Release notes warnings

`+"`_releases/v1.2.0.md`"+` breaks these rules. They don't block merging; fix them if they're mistakes.

- line 3: no-empty-heading: "## Fixes" has no content

`+"`_releases/v1.1.0.md`"+` breaks these rules. They don't block merging; fix them if they're mistakes.

- line 9: closing-link: the last line doesn't link the full changelog
`)

	p.Edits = nil
	contains(t, Render(s).Status, "| ⚠️ | Check the version and release notes (1 warning) |")
}

func TestJobURLMatchesTheJobOrItsCalledWorkflow(t *testing.T) {
	jobs := runJobs("1", "validate-notes", "release-assets-lint", "release-assets / build", "release-assets / sign", "validate")
	for name, want := range map[string]string{
		"validate":       "https://github.com/o/r/actions/runs/1/job/5",
		"release-assets": "https://github.com/o/r/actions/runs/1/job/3",
		"attest":         "fallback",
	} {
		if got := jobURL(jobs, name, "fallback"); got != want {
			t.Errorf("%s: %s", name, got)
		}
	}
}

func TestRendersNothingWithoutARequest(t *testing.T) {
	if blocks := Render(status(&plan.Plan{}, false, results("validate", "success", "publish", "skipped"))); blocks != (Blocks{}) {
		t.Fatal(blocks)
	}
	if blocks := Render(status(nil, false, results("validate", "success"))); blocks != (Blocks{}) {
		t.Fatal(blocks)
	}
}

// fakePullRequest serves one pull request's description and comments, and records writes.
type fakePullRequest struct {
	mu       sync.Mutex
	body     *string
	comments []publish.Comment
	writes   []string
	denied   bool
}

func (f *fakePullRequest) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, _ := io.ReadAll(r.Body)
	var sent struct{ Body string }
	_ = json.Unmarshal(data, &sent)
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/pulls/7":
		_ = json.NewEncoder(w).Encode(map[string]any{"body": f.body, "merged": true})
	case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/issues/7/comments":
		_ = json.NewEncoder(w).Encode(f.comments)
	case f.denied:
		http.Error(w, `{"message":"Resource not accessible by integration"}`, http.StatusForbidden)
	case r.Method == http.MethodPatch && r.URL.Path == "/repos/o/r/pulls/7":
		f.body = &sent.Body
		f.writes = append(f.writes, "edit")
	case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/issues/7/comments":
		f.comments = append(f.comments, publish.Comment{ID: int64(len(f.comments) + 1), Body: sent.Body})
		f.writes = append(f.writes, "comment "+sent.Body)
	default:
		http.Error(w, "unexpected", http.StatusTeapot)
	}
}

func serve(t *testing.T, f *fakePullRequest) *publish.GitHub {
	t.Helper()
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	return &publish.GitHub{BaseURL: server.URL, Token: "token", Repository: "o/r", HTTP: server.Client()}
}

func TestSplice(t *testing.T) {
	blocks := Blocks{Summary: "sum\n", Status: "stat\n"}
	summary, section := SummaryStart+"\nsum\n"+SummaryEnd, StatusStart+"\nstat\n"+StatusEnd
	why := "Why v1.2.0? Minor: adds the --shout flag.  \r\n\r\n  Leaves out #9, which only changed tests.\r\n"
	for name, c := range map[string]struct{ body, want string }{
		"empty":        {"", summary + "\n\n" + section},
		"no newline":   {"Why", summary + "\n\nWhy\n\n" + section},
		"newline":      {"Why\n", summary + "\n\nWhy\n\n" + section},
		"blank line":   {"Why\r\n\r\n", summary + "\n\nWhy\r\n\r\n" + section},
		"replaced":     {SummaryStart + "\nold\n" + SummaryEnd + "\r\n\r\n" + why + "\r\n" + StatusStart + "\nold\n" + StatusEnd, summary + "\r\n\r\n" + why + "\r\n" + section},
		"text around":  {"Above " + SummaryStart + "old" + SummaryEnd + why + StatusStart + "old" + StatusEnd + "\r\n  below\n", "Above " + summary + why + section + "\r\n  below\n"},
		"old format":   {"**[✏️ Edit the release notes](x)**\r\n\r\n" + why + "\n" + StatusStart + "\n### Release status\n\nold\n\n" + StatusEnd, summary + "\n\n**[✏️ Edit the release notes](x)**\r\n\r\n" + why + "\n" + section},
		"summary only": {SummaryStart + "\nold\n" + SummaryEnd + "\n\n" + why, summary + "\n\n" + why + "\n" + section},
		// A block whose end marker was deleted runs to the next block, or to the end.
		"summary end lost":  {SummaryStart + "\nold\n" + why + StatusStart + "old" + StatusEnd, summary + section},
		"status end lost":   {SummaryStart + SummaryEnd + why + StatusStart + "\nold", summary + why + section},
		"both ends lost":    {SummaryStart + "\nold\n" + why + StatusStart + "\nold", summary + section},
		"first of two ends": {SummaryStart + SummaryEnd + why + StatusStart + "\nold\n" + StatusEnd + "\nb " + StatusEnd, summary + why + section + "\nb " + StatusEnd},
	} {
		t.Run(name, func(t *testing.T) {
			if got := splice(c.body, blocks); got != c.want {
				t.Fatalf("got %q\nwant %q", got, c.want)
			}
		})
	}
	// Splicing again changes nothing.
	body := splice(why, blocks)
	if again := splice(body, blocks); again != body || !strings.Contains(body, "\n\n"+why+"\n"+StatusStart) {
		t.Fatalf("%q", again)
	}
}

func TestUpdateEditsOnlyTheBlocks(t *testing.T) {
	why := "Why v1.2.0? Minor: adds the --shout flag.  \r\n"
	f := &fakePullRequest{body: &why}
	gh := serve(t, f)
	ctx := context.Background()
	first := Blocks{Summary: "### Release status\n\nfirst\n", Status: "#### Jobs\n"}

	if changed, err := Update(ctx, gh, 7, Blocks{}, true); changed || err != nil || len(f.writes) != 0 {
		t.Fatal(changed, err, f.writes)
	}
	if changed, err := Update(ctx, gh, 7, first, false); changed || err != nil || len(f.writes) != 0 {
		t.Fatal("added blocks it wasn't asked to", err, f.writes)
	}
	if changed, err := Update(ctx, gh, 7, first, true); !changed || err != nil {
		t.Fatal(changed, err)
	}
	if want := SummaryStart + "\n### Release status\n\nfirst\n" + SummaryEnd + "\n\n" + why + "\n" + StatusStart + "\n#### Jobs\n" + StatusEnd; *f.body != want {
		t.Fatalf("%q", *f.body)
	}

	// The maintainer edits the description around the blocks.
	edited := strings.Replace(*f.body, why, "Why v1.2.0? Because.\r\n\r\nAnd more.\r\n", 1) + "\r\nAfter\r\n"
	f.body = &edited
	if changed, err := Update(ctx, gh, 7, first, false); changed || err != nil || len(f.writes) != 1 {
		t.Fatal("rewrote unchanged blocks", changed, err, f.writes)
	}
	if changed, err := Update(ctx, gh, 7, Blocks{Summary: "second\n", Status: "#### Jobs\n\nsecond\n"}, false); !changed || err != nil {
		t.Fatal(changed, err)
	}
	if want := SummaryStart + "\nsecond\n" + SummaryEnd + "\n\nWhy v1.2.0? Because.\r\n\r\nAnd more.\r\n\n" + StatusStart + "\n#### Jobs\n\nsecond\n" + StatusEnd + "\r\nAfter\r\n"; *f.body != want {
		t.Fatalf("%q", *f.body)
	}
	if changed, err := Update(ctx, gh, 7, Blocks{}, false); !changed || err != nil ||
		!strings.HasPrefix(*f.body, SummaryStart+"\nThis pull request no longer requests a release or edits release notes.\n"+SummaryEnd+"\n\nWhy v1.2.0? Because.") ||
		!strings.HasSuffix(*f.body, StatusStart+"\n"+StatusEnd+"\r\nAfter\r\n") {
		t.Fatal(changed, err, *f.body)
	}

	// An old description with only the status block gets the summary too, even without create.
	old := why + "\n" + StatusStart + "\n### Release status\n\nold\n\n" + StatusEnd
	f.body = &old
	if changed, err := Update(ctx, gh, 7, first, false); !changed || err != nil || *f.body != SummaryStart+"\n### Release status\n\nfirst\n"+SummaryEnd+"\n\n"+why+"\n"+StatusStart+"\n#### Jobs\n"+StatusEnd {
		t.Fatalf("%v %v %q", changed, err, *f.body)
	}

	f.body = nil
	if changed, err := Update(ctx, gh, 7, first, true); !changed || err != nil || *f.body != SummaryStart+"\n### Release status\n\nfirst\n"+SummaryEnd+"\n\n"+StatusStart+"\n#### Jobs\n"+StatusEnd {
		t.Fatal(changed, err, *f.body)
	}

	f.body, f.denied = &why, true
	if _, err := Update(ctx, gh, 7, first, true); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatal(err)
	}
}

func TestFailureCommentMentionsWhoMerged(t *testing.T) {
	s := status(release(), true, results("validate", "success", "publish", "failure", "downstream", "skipped"))
	s.MergedBy = "mona"
	want := "<!-- release-planner:failure run=100 attempt=1 job=publish -->\n@mona The release's **publish** job failed in [this workflow run](https://github.com/o/r/actions/runs/100). The pull request description has the details and how to retry.\n"
	if got := Failure(s); got != want {
		t.Fatalf("%q", got)
	}
	s.MergedBy = ""
	contains(t, Failure(s), "-->\nThe release's **publish** job failed")

	s.Jobs["publish"] = Job{Result: "cancelled"}
	contains(t, Failure(s), "job=publish -->", "**publish** job was cancelled")

	for name, s := range map[string]Status{
		"success":      status(release(), true, results("validate", "success", "publish", "success")),
		"before merge": status(release(), false, results("validate", "success", "release-checks", "failure")),
	} {
		if got := Failure(s); got != "" {
			t.Errorf("%s: %q", name, got)
		}
	}
}

func TestNotifyCommentsOncePerRunAttemptAndJob(t *testing.T) {
	f := &fakePullRequest{comments: []publish.Comment{{ID: 1, Body: "LGTM"}}}
	gh := serve(t, f)
	ctx := context.Background()
	s := status(release(), true, results("validate", "success", "publish", "failure"))
	s.MergedBy = "mona"

	for range 2 {
		if _, err := Notify(ctx, gh, 7, s); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.writes) != 1 || !strings.HasPrefix(f.writes[0], "comment <!-- release-planner:failure run=100 attempt=1 job=publish -->\n@mona ") {
		t.Fatal(f.writes)
	}

	// A new attempt, or another job failing, is news; a repeat after other comments isn't.
	f.comments = append(f.comments, publish.Comment{ID: 9, Body: "Looking into it"})
	if posted, err := Notify(ctx, gh, 7, s); posted || err != nil {
		t.Fatal(posted, err)
	}
	s.RunAttempt = "2"
	if posted, err := Notify(ctx, gh, 7, s); !posted || err != nil || !strings.Contains(f.writes[1], "attempt=2 job=publish") {
		t.Fatal(posted, err, f.writes)
	}
	s.Jobs["publish"], s.Jobs["downstream"] = Job{Result: "success"}, Job{Result: "failure"}
	if posted, err := Notify(ctx, gh, 7, s); !posted || err != nil || !strings.Contains(f.writes[2], "attempt=2 job=downstream") {
		t.Fatal(posted, err, f.writes)
	}
	// Only the latest failure comment counts: attempt 2's publish failure was reported before
	// its downstream one, but the same failure again is news.
	s.Jobs["publish"] = Job{Result: "failure"}
	if posted, err := Notify(ctx, gh, 7, s); !posted || err != nil {
		t.Fatal(posted, err)
	}

	// Nothing failed, or the pull request is still open: no comment.
	f.writes = nil
	s.Jobs["publish"], s.Jobs["downstream"] = Job{Result: "success"}, Job{Result: "success"}
	if posted, err := Notify(ctx, gh, 7, s); posted || err != nil {
		t.Fatal(posted, err)
	}
	open := status(release(), false, results("validate", "success", "release-checks", "failure"))
	if posted, err := Notify(ctx, gh, 7, open); posted || err != nil || len(f.writes) != 0 {
		t.Fatal(posted, err, f.writes)
	}

	f.denied = true
	s.RunAttempt, s.Jobs["publish"] = "3", Job{Result: "failure"}
	if _, err := Notify(ctx, gh, 7, s); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatal(err)
	}
}
