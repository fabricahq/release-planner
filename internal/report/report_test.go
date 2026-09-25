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
	return Status{Server: "https://github.com", Repository: "o/r", RunURL: "https://github.com/o/r/actions/runs/100", Merged: merged, Plan: p, Jobs: j}
}

func contains(t *testing.T, body string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("comment lacks %q:\n%s", w, body)
		}
	}
}

func lacks(t *testing.T, body string, unwanted ...string) {
	t.Helper()
	for _, w := range unwanted {
		if strings.Contains(body, w) {
			t.Errorf("comment has %q:\n%s", w, body)
		}
	}
}

func TestRendersAReleasePullRequest(t *testing.T) {
	s := status(release(), false, results("validate", "success", "release-checks", "success", "release-assets", "success", "attest", "success", "publish", "skipped", "downstream", "skipped"))
	s.Assets = []Asset{{Name: "tool_linux_amd64.tar.gz", Size: 3 << 20, Digest: "sha256:abc"}, {Name: "SHA256SUMS", Size: 120, Digest: "sha256:def"}}
	body := Render(s)
	contains(t, body, Marker+"\n## Release v1.2.0\n", "Merging publishes v1.2.0.\n",
		"| Release commit | [`0123456`](https://github.com/o/r/commit/"+commit+") |",
		"| Previous release | [v1.1.0](https://github.com/o/r/releases/tag/v1.1.0) |",
		"- ✅ Release checks\n- ✅ Build the release assets\n- ✅ Attest the release assets\n",
		"`_releases/v1.2.0.md` breaks these rules.", `- line 3: no-empty-heading: "## Fixes" has no content`,
		"| `tool_linux_amd64.tar.gz` | 3.0 MiB | `abc` |", "| `SHA256SUMS` | 120 B | `def` |")
	lacks(t, body, "Publish", "❌", "fork")

	s.Jobs["validate"].Outputs["build"] = "false"
	contains(t, Render(s), "comes from a fork")

	first := release()
	first.Previous, first.Prerelease = "", true
	contains(t, Render(status(first, false, results("validate", "success"))), "| Version | `v1.2.0` (prerelease) |", "None: this is the first release")
}

func TestRendersFailures(t *testing.T) {
	body := Render(status(release(), false, results("validate", "success", "release-checks", "failure", "publish", "skipped")))
	contains(t, body, "❌ **The release-checks job failed.** See [the workflow run](https://github.com/o/r/actions/runs/100).", "Fix the cause and push to this branch", "- ❌ Release checks")
	lacks(t, body, "Merging publishes", "Re-run")

	s := status(release(), true, results("validate", "success", "publish", "failure"))
	s.MergedBy = "mona"
	contains(t, Render(s), "❌ **The publish job failed.**", "@mona Once the cause is fixed, use **Re-run failed jobs** on that run.")

	// Validation failed before it wrote a plan.
	body = Render(status(nil, true, results("validate", "failure", "publish", "skipped")))
	contains(t, body, Marker+"\n## Release\n", "❌ **The validate job failed.**")

	contains(t, Render(status(release(), false, results("validate", "success", "attest", "cancelled"))), "❌ **The attest job was cancelled.**")
}

func TestRendersPublicationReuseAndDownstream(t *testing.T) {
	p := release()
	p.Reused, p.BuildRun = true, 77
	s := status(p, true, results("validate", "success", "release-checks", "skipped", "release-assets", "skipped", "attest", "skipped", "publish", "success", "downstream", "failure"))
	s.Downstream = []Target{{"o/tap", "update.yml", "success"}, {"o/bucket", "update.yml", "failure"}, {"o/other", "update.yml", ""}}
	body := Render(s)
	contains(t, body, "✅ Published [v1.2.0](https://github.com/o/r/releases/tag/v1.2.0).",
		"- ♻️ Release checks: reused from [the pull request's run](https://github.com/o/r/actions/runs/77)",
		"- ♻️ Attest the release assets: reused",
		"- ✅ [o/tap `update.yml`](https://github.com/o/tap/actions/workflows/update.yml)",
		"- ❌ [o/bucket `update.yml`](https://github.com/o/bucket/actions/workflows/update.yml)",
		"- ❔ [o/other `update.yml`](https://github.com/o/other/actions/workflows/update.yml)",
		"❌ **The downstream job failed.**")
	lacks(t, body, "Merging publishes")

	// Downstream jobs that never ran, such as for a prerelease, list nothing.
	s.Jobs["downstream"] = Job{Result: "skipped"}
	lacks(t, Render(s), "### Downstream")
}

func TestRendersNotesEdits(t *testing.T) {
	p := &plan.Plan{Edits: []plan.Edit{{Tag: "v1.0.0", File: "_releases/v1.0.0.md", HandEdited: true}, {Tag: "v1.1.0", File: "_releases/v1.1.0.md"}}}
	body := Render(status(p, false, results("validate", "success", "publish", "skipped")))
	contains(t, body, "## Release notes edits\n",
		"- Merging replaces the notes of [v1.0.0](https://github.com/o/r/releases/tag/v1.0.0) with `_releases/v1.0.0.md`.\n  - ⚠️ The v1.0.0 notes on GitHub differ",
		"- Merging replaces the notes of [v1.1.0](https://github.com/o/r/releases/tag/v1.1.0) with `_releases/v1.1.0.md`.\n")
	lacks(t, body, "Release commit", "Merging publishes")

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

// fakeComments serves a pull request's comments and records writes.
type fakeComments struct {
	mu       sync.Mutex
	comments []publish.Comment
	writes   []string
	denied   bool
}

func (f *fakeComments) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	var sent struct{ Body string }
	_ = json.Unmarshal(body, &sent)
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/issues/7/comments":
		_ = json.NewEncoder(w).Encode(f.comments)
	case f.denied:
		http.Error(w, `{"message":"Resource not accessible by integration"}`, http.StatusForbidden)
	case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/issues/7/comments":
		f.writes = append(f.writes, "create "+sent.Body)
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/repos/o/r/issues/comments/"):
		f.writes = append(f.writes, "update "+strings.TrimPrefix(r.URL.Path, "/repos/o/r/issues/comments/")+" "+sent.Body)
	default:
		http.Error(w, "unexpected", http.StatusTeapot)
	}
}

func TestUpsertEditsOneCommentInPlace(t *testing.T) {
	f := &fakeComments{comments: []publish.Comment{{ID: 1, Body: "LGTM"}}}
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	gh := &publish.GitHub{BaseURL: server.URL, Token: "token", Repository: "o/r", HTTP: server.Client()}
	ctx := context.Background()

	if err := Upsert(ctx, gh, 7, "", true); err != nil || len(f.writes) != 0 {
		t.Fatal(err, f.writes)
	}
	if err := Upsert(ctx, gh, 7, Marker+"\nfirst", false); err != nil || len(f.writes) != 0 {
		t.Fatal("created a comment it wasn't asked to", err, f.writes)
	}
	if err := Upsert(ctx, gh, 7, Marker+"\nfirst", true); err != nil || strings.Join(f.writes, "|") != "create "+Marker+"\nfirst" {
		t.Fatal(err, f.writes)
	}
	f.comments = append(f.comments, publish.Comment{ID: 2, Body: Marker + "\nfirst"})
	f.writes = nil
	if err := Upsert(ctx, gh, 7, Marker+"\nfirst", true); err != nil || len(f.writes) != 0 {
		t.Fatal("rewrote an identical comment", err, f.writes)
	}
	if err := Upsert(ctx, gh, 7, Marker+"\nsecond", false); err != nil || strings.Join(f.writes, "|") != "update 2 "+Marker+"\nsecond" {
		t.Fatal(err, f.writes)
	}
	f.writes = nil
	if err := Upsert(ctx, gh, 7, "", false); err != nil || len(f.writes) != 1 || !strings.Contains(f.writes[0], "no longer requests a release") {
		t.Fatal(err, f.writes)
	}
	f.denied = true
	if err := Upsert(ctx, gh, 7, Marker+"\nthird", true); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatal(err)
	}
}
