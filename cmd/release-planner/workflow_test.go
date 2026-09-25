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

func TestReportCommentsOnThePullRequest(t *testing.T) {
	output := actionsFiles(t)
	api := newAPI(t, map[string]any{"GET /issues/7/comments": []any{}, "POST /issues/7/comments": map[string]any{}})
	file := writePlan(t, plan.Plan{Tag: "v1.2.0", Commit: strings.Repeat("a", 40), Previous: "v1.1.0"})
	needs := `{"validate":{"result":"success","outputs":{"tag":"v1.2.0","build":"true"}},"publish":{"result":"skipped","outputs":{}}}`
	code, out, errOut := cli(t, "report", "--needs", needs, "--branch", "main", "--pull-request", "7", "--plan", file, "--assets", filepath.Join(t.TempDir(), "none"))
	if code != 0 || !strings.Contains(out, "Updated the release status on #7") {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
	if body := api.sent("POST /issues/7/comments"); !strings.Contains(body, "Merging publishes v1.2.0.") || !strings.Contains(body, "release-planner:status") {
		t.Fatal(body)
	}
	if summary := output("summary"); !strings.Contains(summary, "Merging publishes v1.2.0.") || strings.Contains(summary, "release-planner:status") {
		t.Fatal(summary)
	}

	// Without a plan, a failure on a pull request isn't taken for a release.
	api = newAPI(t, map[string]any{"GET /issues/7/comments": []any{}})
	missing := filepath.Join(t.TempDir(), "release-plan.json")
	if code, _, errOut := cli(t, "report", "--needs", `{"validate":{"result":"failure"}}`, "--branch", "main", "--pull-request", "7", "--plan", missing); code != 0 || len(api.requests) != 1 {
		t.Fatalf("%d %s %v", code, errOut, api.requests)
	}
}

// After the merge, a failure is reported on the merged pull request, to whoever merged it,
// even when validation failed before it named the pull request.
func TestReportMentionsWhoMergedAFailedRelease(t *testing.T) {
	actionsFiles(t)
	t.Setenv("GITHUB_RUN_ID", "100")
	api := newAPI(t, map[string]any{
		"GET /commits/" + merged + "/pulls": []any{map[string]any{"number": 2, "merged_at": "2026-09-25T12:00:00Z", "merge_commit_sha": merged, "base": map[string]string{"ref": "main"}}},
		"GET /pulls/2":                      map[string]any{"head": map[string]any{"sha": strings.Repeat("c", 40)}, "merged_by": map[string]string{"login": "mona"}},
		"GET /issues/2/comments":            []any{map[string]any{"id": 5, "body": "<!-- release-planner:status -->\nold"}},
		"PATCH /issues/comments/5":          map[string]any{},
	})
	needs := `{"validate":{"result":"failure","outputs":{}},"publish":{"result":"skipped","outputs":{}}}`
	if code, out, errOut := cli(t, "report", "--needs", needs, "--branch", "main", "--merged", merged, "--plan", filepath.Join(t.TempDir(), "missing.json")); code != 0 {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
	body := api.sent("PATCH /issues/comments/5")
	if !strings.Contains(body, "The validate job failed") || !strings.Contains(body, "@mona Once the cause is fixed") || !strings.Contains(body, "https://github.com/fabricahq/example/actions/runs/100") {
		t.Fatal(body)
	}
}

// A fork's pull request can't be commented on; the report still succeeds.
func TestReportDegradesWithoutWriteAccess(t *testing.T) {
	actionsFiles(t)
	newAPI(t, map[string]any{"GET /issues/7/comments": []any{}, "POST /issues/7/comments": status{403, map[string]string{"message": "Resource not accessible by integration"}}})
	file := writePlan(t, plan.Plan{Tag: "v1.2.0", Commit: strings.Repeat("a", 40)})
	code, out, errOut := cli(t, "report", "--needs", `{"validate":{"result":"success","outputs":{}}}`, "--branch", "main", "--pull-request", "7", "--plan", file)
	if code != 0 || !strings.Contains(out, "::warning title=Release status::Couldn't update the release status comment on #7") {
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
	var results []struct{ Repository, Workflow, Error string }
	if err := json.Unmarshal([]byte(strings.TrimPrefix(strings.TrimSpace(output("output")), "results=")), &results); err != nil ||
		len(results) != 2 || results[0].Error != "" || !strings.Contains(results[1].Error, "fabricahq/scoop-bucket") {
		t.Fatal(results, err, output("output"))
	}
}

func TestDownstreamSkipsPrereleases(t *testing.T) {
	output := actionsFiles(t)
	api := newAPI(t, map[string]any{})
	code, out, _ := cli(t, "downstream", "--tag", "v1.2.0-rc.1", "--target", "fabricahq/homebrew-tap:update.yml")
	if code != 0 || !strings.Contains(out, "prerelease") || len(api.requests) != 0 || output("output") != "results=[]\n" {
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
