package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fabricahq/release-planner/internal/plan"
)

func writePlan(t *testing.T, p plan.Plan) string {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "release-plan.json")
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}

const merged = "dddddddddddddddddddddddddddddddddddddddd"

func TestReportWritesTheStatusInTheDescription(t *testing.T) {
	output := actionsFiles(t)
	description := "**[✏️ Edit the release notes](https://github.com/fabricahq/example/edit/release/_releases/v1.2.0.md)**\r\n\r\nMinor: adds the --shout flag."
	api := newAPI(t, map[string]any{"GET /pulls/7": map[string]any{"body": description}, "PATCH /pulls/7": map[string]any{}})
	file := writePlan(t, plan.Plan{Tag: "v1.2.0", Commit: strings.Repeat("a", 40), Previous: "v1.1.0"})
	needs := `{"validate":{"result":"success","outputs":{"tag":"v1.2.0","build":"true"}},"publish":{"result":"skipped","outputs":{}}}`
	code, out, errOut := cli(t, "report", "--needs", needs, "--branch", "main", "--pull-request", "7", "--plan", file, "--assets", filepath.Join(t.TempDir(), "none"))
	if code != 0 || !strings.Contains(out, "Updated the release status in the description of #7") {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
	var sent struct{ Body string }
	if err := json.Unmarshal([]byte(api.sent("PATCH /pulls/7")), &sent); err != nil ||
		!strings.HasPrefix(sent.Body, description+"\n\n<!-- release-planner:status:start -->\n### Release status\n\n| Version | Release commit | Previous release |") ||
		!strings.HasSuffix(sent.Body, "\n\n<!-- release-planner:status:end -->") {
		t.Fatal(sent.Body, err)
	}
	if summary := output("summary"); !strings.HasPrefix(summary, "### Release status\n") || strings.Contains(summary, "release-planner:status") {
		t.Fatal(summary)
	}
	if strings.Contains(strings.Join(api.requests, " "), "comments") {
		t.Fatal("commented on an open pull request", api.requests)
	}

	// Without a plan, a failure on a pull request isn't taken for a release.
	api = newAPI(t, map[string]any{"GET /pulls/7": map[string]any{"body": "Fix a typo"}})
	missing := filepath.Join(t.TempDir(), "release-plan.json")
	if code, _, errOut := cli(t, "report", "--needs", `{"validate":{"result":"failure"}}`, "--branch", "main", "--pull-request", "7", "--plan", missing); code != 0 || strings.Join(api.requests, " ") != "GET /pulls/7" {
		t.Fatalf("%d %s %v", code, errOut, api.requests)
	}
}

// Before publication, the assets link to the zip of the run that built them, when the
// Actions API names that artifact.
func TestReportLinksTheAssetsArtifact(t *testing.T) {
	actionsFiles(t)
	assets := t.TempDir()
	if err := os.WriteFile(filepath.Join(assets, "tool.tar.gz"), []byte("tool"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := writePlan(t, plan.Plan{Tag: "v1.2.0", Commit: strings.Repeat("a", 40), BuildRun: 77})
	needs := `{"validate":{"result":"success","outputs":{"build":"true"}},"publish":{"result":"skipped","outputs":{}}}`
	for name, c := range map[string]struct {
		artifacts any
		want      string
	}{
		"found":   {map[string]any{"artifacts": []any{map[string]any{"id": 9, "name": "release-assets", "expired": false}}}, "[Download all (zip)](https://github.com/fabricahq/example/actions/runs/77/artifacts/9)"},
		"expired": {map[string]any{"artifacts": []any{map[string]any{"id": 9, "name": "release-assets", "expired": true}}}, ""},
		"denied":  {status{403, map[string]string{"message": "no"}}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			api := newAPI(t, map[string]any{"GET /actions/runs/77/artifacts": c.artifacts, "GET /pulls/7": map[string]any{"body": ""}, "PATCH /pulls/7": map[string]any{}})
			if code, out, errOut := cli(t, "report", "--needs", needs, "--branch", "main", "--pull-request", "7", "--plan", file, "--assets", assets); code != 0 {
				t.Fatalf("%d %s %s", code, out, errOut)
			}
			body := api.sent("PATCH /pulls/7")
			if !strings.Contains(body, "| `tool.tar.gz` | 4 B |") || strings.Contains(body, "Download all") != (c.want != "") || !strings.Contains(body, c.want) {
				t.Fatal(body)
			}
		})
	}
}

// After the merge, a failure is reported in the merged pull request's description and in a
// comment to whoever merged it, even when validation failed before it named the pull request.
func TestReportMentionsWhoMergedAFailedRelease(t *testing.T) {
	actionsFiles(t)
	t.Setenv("GITHUB_RUN_ID", "100")
	t.Setenv("GITHUB_RUN_ATTEMPT", "2")
	old := "Intro\n\n<!-- release-planner:status:start -->\nold\n<!-- release-planner:status:end -->\n\nHuman footer"
	api := newAPI(t, map[string]any{
		"GET /commits/" + merged + "/pulls": []any{map[string]any{"number": 2, "merged_at": "2026-09-25T12:00:00Z", "merge_commit_sha": merged, "base": map[string]string{"ref": "main"}}},
		"GET /pulls/2":                      map[string]any{"head": map[string]any{"sha": strings.Repeat("c", 40)}, "merged_by": map[string]string{"login": "mona"}, "body": old},
		"PATCH /pulls/2":                    map[string]any{},
		"GET /issues/2/comments":            []any{map[string]any{"id": 5, "body": "<!-- release-planner:failure run=100 attempt=1 job=validate -->\n@mona earlier"}},
		"POST /issues/2/comments":           map[string]any{},
	})
	needs := `{"validate":{"result":"failure","outputs":{}},"publish":{"result":"skipped","outputs":{}}}`
	code, out, errOut := cli(t, "report", "--needs", needs, "--branch", "main", "--merged", merged, "--plan", filepath.Join(t.TempDir(), "missing.json"))
	if code != 0 || !strings.Contains(out, "Commented on #2 about the failed validate job.") {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
	var edit, comment struct{ Body string }
	_ = json.Unmarshal([]byte(api.sent("PATCH /pulls/2")), &edit)
	_ = json.Unmarshal([]byte(api.sent("POST /issues/2/comments")), &comment)
	if !strings.HasPrefix(edit.Body, "Intro\n\n<!-- release-planner:status:start -->\n### Release status\n\n❌ **The validate job failed.**") ||
		!strings.HasSuffix(edit.Body, "<!-- release-planner:status:end -->\n\nHuman footer") || strings.Contains(edit.Body, "@mona") {
		t.Fatal(edit.Body)
	}
	if !strings.HasPrefix(comment.Body, "<!-- release-planner:failure run=100 attempt=2 job=validate -->\n@mona The release's **validate** job failed in [this workflow run](https://github.com/fabricahq/example/actions/runs/100).") {
		t.Fatal(comment.Body)
	}
}

// A fork's pull request can't be edited; the report still succeeds.
func TestReportDegradesWithoutWriteAccess(t *testing.T) {
	actionsFiles(t)
	newAPI(t, map[string]any{"GET /pulls/7": map[string]any{"body": ""}, "PATCH /pulls/7": status{403, map[string]string{"message": "Resource not accessible by integration"}}})
	file := writePlan(t, plan.Plan{Tag: "v1.2.0", Commit: strings.Repeat("a", 40)})
	code, out, errOut := cli(t, "report", "--needs", `{"validate":{"result":"success","outputs":{}}}`, "--branch", "main", "--pull-request", "7", "--plan", file)
	if code != 0 || !strings.Contains(out, "::warning title=Release status::Couldn't update the release status in the description of #7") {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
}

func TestDownstreamRunsEachTargetWithTheRelease(t *testing.T) {
	output := actionsFiles(t)
	api := newAPI(t, map[string]any{
		"GET /repos/fabricahq/homebrew-tap":                                          map[string]string{"default_branch": "main"},
		"POST /repos/fabricahq/homebrew-tap/actions/workflows/update.yml/dispatches": status{204, nil},
	})
	code, out, errOut := cli(t, "downstream", "--tag", "v1.2.0", "--target", "fabricahq/homebrew-tap:update.yml", "--target", "fabricahq/scoop-bucket:update.yml")
	if code != 1 || !strings.Contains(out, "Ran update.yml in fabricahq/homebrew-tap for v1.2.0") || !strings.Contains(errOut, "1 of 2 downstream workflows didn't start; v1.2.0 is published either way") {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
	var sent struct {
		Ref    string
		Inputs map[string]string
	}
	if err := json.Unmarshal([]byte(api.sent("POST /repos/fabricahq/homebrew-tap/actions/workflows/update.yml/dispatches")), &sent); err != nil ||
		sent.Ref != "main" || sent.Inputs["tag"] != "v1.2.0" || sent.Inputs["version"] != "1.2.0" {
		t.Fatal(sent, err)
	}
	if !strings.Contains(out, "::error title=Downstream::") || !strings.Contains(out, "fabricahq/scoop-bucket") || output("output") != "" {
		t.Fatalf("%s %q", out, output("output"))
	}
}

// The downstream job is a matrix with one result in the needs context, so the report reads
// each target's job, at its latest attempt.
func TestReportListsEachDownstreamTarget(t *testing.T) {
	actionsFiles(t)
	t.Setenv("GITHUB_RUN_ID", "100")
	job := func(name, conclusion string, attempt int) map[string]any {
		return map[string]any{"name": name, "conclusion": conclusion, "run_attempt": attempt}
	}
	api := newAPI(t, map[string]any{
		"GET /actions/runs/100/jobs": map[string]any{"jobs": []any{
			job("publish", "success", 1),
			job("downstream (fabricahq/homebrew-tap:update.yml)", "success", 1),
			job("downstream (fabricahq/scoop-bucket:update.yml)", "failure", 1),
			job("downstream (fabricahq/winget:update.yml)", "failure", 1),
			job("downstream (fabricahq/winget:update.yml)", "success", 2),
		}},
		"GET /pulls/2":            map[string]any{"body": "Release v1.2.0"},
		"PATCH /pulls/2":          map[string]any{},
		"GET /issues/2/comments":  []any{},
		"POST /issues/2/comments": map[string]any{},
	})
	file := writePlan(t, plan.Plan{Tag: "v1.2.0", Commit: strings.Repeat("a", 40), PullRequest: 2})
	needs := `{"validate":{"result":"success","outputs":{}},"publish":{"result":"success","outputs":{}},"downstream":{"result":"failure","outputs":{}}}`
	args := []string{"report", "--needs", needs, "--branch", "main", "--merged", merged, "--plan", file,
		"--downstream", "fabricahq/homebrew-tap:update.yml", "--downstream", "fabricahq/scoop-bucket:update.yml", "--downstream", "fabricahq/winget:update.yml"}
	if code, out, errOut := cli(t, args...); code != 0 {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
	body := api.sent("PATCH /pulls/2")
	if !strings.Contains(api.sent("POST /issues/2/comments"), "**downstream** job failed") {
		t.Fatal(api.requests)
	}
	for _, want := range []string{
		"- ✅ [fabricahq/homebrew-tap `update.yml`]",
		"- ❌ [fabricahq/scoop-bucket `update.yml`]",
		"- ✅ [fabricahq/winget `update.yml`]",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("lacks %q:\n%s", want, body)
		}
	}
}

func TestDownstreamSkipsPrereleases(t *testing.T) {
	output := actionsFiles(t)
	api := newAPI(t, map[string]any{})
	code, out, _ := cli(t, "downstream", "--tag", "v1.2.0-rc.1", "--target", "fabricahq/homebrew-tap:update.yml")
	if code != 0 || !strings.Contains(out, "prerelease") || len(api.requests) != 0 || output("output") != "" {
		t.Fatalf("%d %s %v %q", code, out, api.requests, output("output"))
	}
	if code, _, errOut := cli(t, "downstream", "--tag", "v1.2.0", "--target", "homebrew-tap"); code != 1 || !strings.Contains(errOut, "owner/name:workflow.yml") {
		t.Fatal(errOut)
	}
}

// fakeGH puts a gh on PATH that passes attestation checks except for files named bad*, and
// logs its arguments.
func fakeGH(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	script := "#!/bin/sh\necho \"$@\" >> " + log + "\ncase \"$3\" in */bad*) echo 'no attestation'; exit 1;; esac\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

// Publish refuses a build run that planned another release, and assets without an
// attestation from the Release workflow, before it writes anything.
func TestPublishChecksTheBuildBeforeWriting(t *testing.T) {
	api := newAPI(t, map[string]any{})
	p := plan.Plan{Tag: "v1.2.0", Commit: strings.Repeat("a", 40), Notes: "Notes", Head: strings.Repeat("c", 40), Merged: merged, PullRequest: 2}
	file := writePlan(t, p)
	other := p
	other.Commit = strings.Repeat("b", 40)
	code, _, errOut := cli(t, "publish", "--plan", file, "--built-plan", writePlan(t, other), "--branch", "main")
	if code != 1 || !strings.Contains(errOut, "the run that checked and built the release planned") {
		t.Fatal(errOut)
	}

	log := fakeGH(t)
	assets := t.TempDir()
	for _, name := range []string{"good.tar.gz", "bad.tar.gz"} {
		if err := os.WriteFile(filepath.Join(assets, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	code, _, errOut = cli(t, "publish", "--plan", file, "--built-plan", file, "--branch", "main", "--assets", assets, "--signer-workflow", ".github/workflows/release-planner.yml")
	if code != 1 || !strings.Contains(errOut, "bad.tar.gz has no build attestation from .github/workflows/release-planner.yml in fabricahq/example") || len(api.requests) != 0 {
		t.Fatalf("%d %s %v", code, errOut, api.requests)
	}
	data, _ := os.ReadFile(log)
	if !strings.Contains(string(data), "attestation verify "+filepath.Join(assets, "bad.tar.gz")+" --repo fabricahq/example --signer-workflow fabricahq/example/.github/workflows/release-planner.yml --deny-self-hosted-runners") {
		t.Fatal(string(data))
	}
}
