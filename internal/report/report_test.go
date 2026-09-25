package report

import (
	"context"
	"encoding/json"
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
	return &plan.Plan{Tag: "v1.2.0", Version: "1.2.0", Commit: commit, Previous: "v1.1.0", File: "_releases/v1.2.0.md", BuildRun: 100,
		Findings: []notes.Finding{{Rule: "no-empty-heading", Line: 3, Message: `"## Fixes" has no content`}}}
}

func results(r ...string) map[string]Job {
	m := map[string]Job{}
	for i := 0; i+1 < len(r); i += 2 {
		m[r[i]] = Job{Result: r[i+1], Outputs: map[string]string{"build": "true"}}
	}
	return m
}

func status(p *plan.Plan, merged bool, j map[string]Job) Status {
	return Status{Server: "https://github.com", Repository: "o/r", RunURL: "https://github.com/o/r/actions/runs/100", RunID: "100", RunAttempt: "1", Merged: merged, Plan: p, Jobs: j}
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

func TestRendersAReleasePullRequest(t *testing.T) {
	s := status(release(), false, results("validate", "success", "release-checks", "success", "release-assets", "success", "attest", "success", "publish", "skipped", "downstream", "skipped"))
	s.Assets = []Asset{{Name: "tool_linux_amd64.tar.gz", Size: 3 << 20}, {Name: "SHA256SUMS", Size: 120}}
	s.Archive = "https://github.com/o/r/actions/runs/100/artifacts/9"
	body := Render(s)
	contains(t, body, "### Release status\n\n| Version | Release commit | Previous release |\n| --- | --- | --- |\n"+
		"| `v1.2.0` | [`0123456`](https://github.com/o/r/commit/"+commit+") | [v1.1.0](https://github.com/o/r/releases/tag/v1.1.0) |\n\n",
		"**Jobs:** ✅ Validate the request · ✅ Release checks · ✅ Build the release assets · ✅ Attest the release assets\n",
		"#### Release notes rules\n\n`_releases/v1.2.0.md` breaks these rules.", `- line 3: no-empty-heading: "## Fixes" has no content`,
		"#### Assets\n\n[Download all (zip)](https://github.com/o/r/actions/runs/100/artifacts/9): a workflow artifact",
		"| File | Size |\n| --- | ---: |\n| `tool_linux_amd64.tar.gz` | 3.0 MiB |\n| `SHA256SUMS` | 120 B |\n")
	lacks(t, body, "Publish", "❌", "fork", "Merging publishes", Start, End)
	if !strings.HasPrefix(body, "### Release status\n") || !strings.HasSuffix(body, "|\n") {
		t.Fatalf("%q", body)
	}

	// Without the artifact's ID, the zip link is left out.
	s.Archive = ""
	lacks(t, Render(s), "Download all")

	// Once published, each file links to its release download, and the zip link goes away.
	s.Archive, s.Merged = "https://github.com/o/r/actions/runs/100/artifacts/9", true
	s.Jobs["publish"] = Job{Result: "success"}
	body = Render(s)
	contains(t, body, "| [`tool_linux_amd64.tar.gz`](https://github.com/o/r/releases/download/v1.2.0/tool_linux_amd64.tar.gz) | 3.0 MiB |",
		"| [`SHA256SUMS`](https://github.com/o/r/releases/download/v1.2.0/SHA256SUMS) | 120 B |")
	lacks(t, body, "Download all")
	// Merged but not yet published: still only the zip.
	s.Jobs["publish"] = Job{Result: "failure"}
	body = Render(s)
	contains(t, body, "[Download all (zip)]", "| `SHA256SUMS` | 120 B |")
	lacks(t, body, "releases/download")
	s.Merged, s.Jobs["publish"] = false, Job{Result: "skipped"}

	s.Jobs["validate"].Outputs["build"] = "false"
	contains(t, Render(s), "comes from a fork")

	first := release()
	first.Previous, first.Prerelease = "", true
	contains(t, Render(status(first, false, results("validate", "success"))), "| `v1.2.0` (prerelease) | [`0123456`](https://github.com/o/r/commit/"+commit+") | None |")
}

func TestRendersFailures(t *testing.T) {
	body := Render(status(release(), false, results("validate", "success", "release-checks", "failure", "publish", "skipped")))
	contains(t, body, "❌ **The release-checks job failed.** See [the workflow run](https://github.com/o/r/actions/runs/100).", "Fix the cause and push to this branch", "**Jobs:** ✅ Validate the request · ❌ Release checks\n")
	lacks(t, body, "Merging publishes", "Re-run")

	s := status(release(), true, results("validate", "success", "publish", "failure"))
	s.MergedBy = "mona"
	body = Render(s)
	contains(t, body, "❌ **The publish job failed.** See [the workflow run](https://github.com/o/r/actions/runs/100).\nOnce the cause is fixed, use **Re-run failed jobs** on that run.")
	lacks(t, body, "@mona", "Published")

	// Validation failed before it wrote a plan.
	body = Render(status(nil, true, results("validate", "failure", "publish", "skipped")))
	contains(t, body, "### Release status\n\n❌ **The validate job failed.**")
	lacks(t, body, "| Version |")

	contains(t, Render(status(release(), false, results("validate", "success", "attest", "cancelled"))), "❌ **The attest job was cancelled.**")
}

func TestRendersPublicationReuseAndDownstream(t *testing.T) {
	p := release()
	p.Reused, p.BuildRun = true, 77
	s := status(p, true, results("validate", "success", "release-checks", "skipped", "release-assets", "skipped", "attest", "skipped", "publish", "success", "downstream", "failure"))
	s.Downstream = []Target{{"o/tap", "update.yml", "success"}, {"o/bucket", "update.yml", "failure"}, {"o/other", "update.yml", ""}}
	body := Render(s)
	contains(t, body, "✅ Published [v1.2.0](https://github.com/o/r/releases/tag/v1.2.0).",
		"**Jobs:** ✅ Validate the request · ♻️ Release checks ([reused](https://github.com/o/r/actions/runs/77)) · ♻️ Build the release assets ([reused](https://github.com/o/r/actions/runs/77)) · ♻️ Attest the release assets ([reused](https://github.com/o/r/actions/runs/77)) · ✅ Publish · ❌ Run downstream workflows\n",
		"#### Downstream\n",
		"- ✅ [o/tap `update.yml`](https://github.com/o/tap/actions/workflows/update.yml)",
		"- ❌ [o/bucket `update.yml`](https://github.com/o/bucket/actions/workflows/update.yml)",
		"- ❔ [o/other `update.yml`](https://github.com/o/other/actions/workflows/update.yml)",
		"❌ **The downstream job failed.**")
	lacks(t, body, "Merging publishes")

	// Downstream jobs that never ran, such as for a prerelease, list nothing.
	s.Jobs["downstream"] = Job{Result: "skipped"}
	lacks(t, Render(s), "Downstream")
}

func TestRendersNotesEdits(t *testing.T) {
	p := &plan.Plan{Edits: []plan.Edit{{Tag: "v1.0.0", File: "_releases/v1.0.0.md", HandEdited: true}, {Tag: "v1.1.0", File: "_releases/v1.1.0.md"}}}
	body := Render(status(p, false, results("validate", "success", "publish", "skipped")))
	contains(t, body, "### Release status\n\n**Jobs:** ✅ Validate the request\n\n#### Notes edits\n",
		"- Merging replaces the notes of [v1.0.0](https://github.com/o/r/releases/tag/v1.0.0) with `_releases/v1.0.0.md`.\n  - ⚠️ The v1.0.0 notes on GitHub differ",
		"- Merging replaces the notes of [v1.1.0](https://github.com/o/r/releases/tag/v1.1.0) with `_releases/v1.1.0.md`.\n")
	lacks(t, body, "Release commit", "Merging publishes", "Published")

	body = Render(status(p, true, results("validate", "success", "publish", "success")))
	contains(t, body, "- ✅ Updated the notes of [v1.0.0](https://github.com/o/r/releases/tag/v1.0.0) from `_releases/v1.0.0.md`.")
	lacks(t, body, "⚠️")
}

func TestRendersNothingWithoutARequest(t *testing.T) {
	if body := Render(status(&plan.Plan{}, false, results("validate", "success", "publish", "skipped"))); body != "" {
		t.Fatal(body)
	}
	if body := Render(status(nil, false, results("validate", "success"))); body != "" {
		t.Fatal(body)
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
	section := Start + "\nnew\n\n" + End
	for name, c := range map[string]struct{ body, want string }{
		"empty":             {"", section},
		"no newline":        {"Intro", "Intro\n\n" + section},
		"newline":           {"Intro\n", "Intro\n\n" + section},
		"blank line":        {"Intro\r\n\r\n", "Intro\r\n\r\n" + section},
		"replaced":          {"**Edit**\r\n\r\n" + Start + "\nold\n" + End, "**Edit**\r\n\r\n" + section},
		"text around kept":  {"a  \r\n" + Start + "old" + End + "\r\n  human text\n", "a  \r\n" + section + "\r\n  human text\n"},
		"end marker lost":   {"a\n" + Start + "\nold", "a\n" + section},
		"first of two ends": {"a\n" + Start + "\nold\n" + End + "\nb " + End, "a\n" + section + "\nb " + End},
	} {
		t.Run(name, func(t *testing.T) {
			got, found := splice(c.body, "new\n")
			if got != c.want || found != strings.Contains(c.body, Start) {
				t.Fatalf("got %q, %v\nwant %q", got, found, c.want)
			}
		})
	}
}

func TestUpdateEditsOnlyTheStatusSection(t *testing.T) {
	human := "**[✏️ Edit the release notes](https://github.com/o/r/edit/b/_releases/v1.2.0.md)**\r\n\r\nMerging publishes v1.2.0.  \r\n"
	f := &fakePullRequest{body: &human}
	gh := serve(t, f)
	ctx := context.Background()

	if changed, err := Update(ctx, gh, 7, "", true); changed || err != nil || len(f.writes) != 0 {
		t.Fatal(changed, err, f.writes)
	}
	if changed, err := Update(ctx, gh, 7, "### Release status\n", false); changed || err != nil || len(f.writes) != 0 {
		t.Fatal("added a section it wasn't asked to", err, f.writes)
	}
	if changed, err := Update(ctx, gh, 7, "### Release status\n\nfirst\n", true); !changed || err != nil {
		t.Fatal(changed, err)
	}
	if want := human + "\n" + Start + "\n### Release status\n\nfirst\n\n" + End; *f.body != want {
		t.Fatalf("%q", *f.body)
	}

	// The maintainer edits the description around the section.
	edited := "Intro  \r\n" + *f.body + "\r\nAfter\r\n"
	f.body = &edited
	if changed, err := Update(ctx, gh, 7, "### Release status\n\nfirst\n", false); changed || err != nil || len(f.writes) != 1 {
		t.Fatal("rewrote an unchanged section", changed, err, f.writes)
	}
	if changed, err := Update(ctx, gh, 7, "### Release status\n\nsecond\n", false); !changed || err != nil {
		t.Fatal(changed, err)
	}
	if want := "Intro  \r\n" + human + "\n" + Start + "\n### Release status\n\nsecond\n\n" + End + "\r\nAfter\r\n"; *f.body != want {
		t.Fatalf("%q", *f.body)
	}
	if changed, err := Update(ctx, gh, 7, "", false); !changed || err != nil || !strings.Contains(*f.body, "no longer requests a release") || !strings.HasSuffix(*f.body, End+"\r\nAfter\r\n") {
		t.Fatal(changed, err, *f.body)
	}

	f.body = nil
	if changed, err := Update(ctx, gh, 7, "### Release status\n", true); !changed || err != nil || *f.body != Start+"\n### Release status\n\n"+End {
		t.Fatal(changed, err, f.body)
	}

	f.body, f.denied = &human, true
	if _, err := Update(ctx, gh, 7, "### Release status\n", true); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatal(err)
	}
}

func TestFailureCommentMentionsWhoMerged(t *testing.T) {
	s := status(release(), true, results("validate", "success", "publish", "failure", "downstream", "skipped"))
	s.MergedBy = "mona"
	want := "<!-- release-planner:failure run=100 attempt=1 job=publish -->\n@mona The release's **publish** job failed in [this workflow run](https://github.com/o/r/actions/runs/100). The release status at the end of the description has the details and how to retry.\n"
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
