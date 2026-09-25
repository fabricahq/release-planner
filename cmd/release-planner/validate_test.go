package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/fabricahq/release-planner/internal/plan"
)

// origin is a GitHub-like repository whose release pull request #2, from the branch release,
// adds v1.0.0's notes while main moves on. release is the release commit, and head the pull
// request's head.
type origin struct {
	*repo
	release, head string
}

func newOrigin(t *testing.T, config string) *origin {
	t.Helper()
	o := &origin{repo: newRepo(t)}
	// GitHub serves any commit a ref reaches, including refs/pull/<n>/head.
	o.git("config", "uploadpack.allowReachableSHA1InWant", "true")
	o.write(".release-planner/config.yml", "schema-version: 1\nversion: v0.1.0\nfirst-version: v1.0.0\n"+config)
	o.release = o.repo.commit("Adopt Release Planner (#1)")
	o.git("checkout", "-q", "-b", "release")
	o.write("_releases/v1.0.0.md", "Draft notes\n")
	o.repo.commit("Release v1.0.0")
	o.write("_releases/v1.0.0.md", "Approved notes\n")
	o.head = o.repo.commit("Revise the notes")
	o.git("checkout", "-q", "main")
	o.write("later.go", "package later\n")
	o.repo.commit("Later change (#3)")
	return o
}

// merge merges the pull request into main the way GitHub does for strategy, deletes its
// branch, and returns the commit main moved to.
func (o *origin) merge(strategy string) string {
	switch strategy {
	case "merge":
		o.git("merge", "-q", "--no-ff", "release", "-m", "Merge pull request #2 from fabricahq/release")
	case "squash":
		o.git("merge", "-q", "--squash", "release")
		o.git("commit", "-q", "-m", "Release v1.0.0 (#2)")
	case "rebase":
		o.git("cherry-pick", o.release+"..release")
	}
	o.git("update-ref", "refs/pull/2/head", o.head)
	o.git("branch", "-q", "-D", "release")
	return o.git("rev-parse", "HEAD")
}

// checkout clones origin the way the workflow's checkout does: branches and tags, but not
// pull request heads.
func (o *origin) checkout(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "checkout")
	o.git("clone", "-q", "--no-local", "file://"+o.dir, dir)
	return dir
}

// mergedAPI serves pull request #2 merged as merged, and whatever else routes adds.
func mergedAPI(t *testing.T, o *origin, merged string, routes map[string]any) *fakeAPI {
	t.Helper()
	all := map[string]any{
		"GET /commits/" + merged + "/pulls":                    []any{map[string]any{"number": 2, "merged_at": "2026-09-25T12:00:00Z", "merge_commit_sha": merged, "base": map[string]string{"ref": "main"}}},
		"GET /pulls/2":                                         map[string]any{"head": map[string]any{"sha": o.head, "repo": map[string]string{"full_name": "fabricahq/example"}}, "merged_by": map[string]string{"login": "mona"}},
		"GET /environments/release":                            map[string]any{"deployment_branch_policy": map[string]bool{"custom_branch_policies": true}, "protection_rules": []any{}},
		"GET /environments/release/deployment-branch-policies": map[string]any{"branch_policies": []any{map[string]string{"name": "main", "type": "branch"}}},
	}
	for k, v := range routes {
		all[k] = v
	}
	t.Setenv("GITHUB_WORKFLOW_REF", "fabricahq/example/.github/workflows/release-planner.yml@refs/heads/main")
	t.Setenv("GITHUB_RUN_ID", "100")
	return newAPI(t, all)
}

func validateMerged(t *testing.T, dir, merged string) (plan.Plan, string, string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "plan.json")
	code, out, errOut := cli(t, "validate", "--dir", dir, "--ci", "--merged", merged, "--out", file)
	if code != 0 {
		return plan.Plan{}, out, errOut
	}
	p, err := readPlan(file)
	if err != nil {
		t.Fatal(err)
	}
	return p, out, errOut
}

// After the merge, validate finds the pull request and plans its release commit, whichever
// way it merged, fetching a squashed or rebased pull request's head that no branch holds.
func TestValidateAfterMergePlansTheReleaseCommit(t *testing.T) {
	for _, strategy := range []string{"merge", "squash", "rebase"} {
		t.Run(strategy, func(t *testing.T) {
			o := newOrigin(t, "")
			merged := o.merge(strategy)
			dir := o.checkout(t)
			output := actionsFiles(t)
			mergedAPI(t, o, merged, nil)
			p, out, errOut := validateMerged(t, dir, merged)
			if p.Tag != "v1.0.0" || p.Commit != o.release || p.Head != o.head || p.Merged != merged || p.PullRequest != 2 || p.MergedBy != "mona" || p.Notes != "Approved notes\n" {
				t.Fatalf("%+v\n%s %s", p, out, errOut)
			}
			for _, want := range []string{"tag=v1.0.0\n", "commit=" + o.release + "\n", "publish=true\n", "build=true\n", "build-run=100\n"} {
				if !strings.Contains(output("output"), want) {
					t.Errorf("outputs lack %q:\n%s", want, output("output"))
				}
			}
		})
	}
}

// A manual retry runs from the release branch's current checkout; it still plans the merge
// with the notes directory it merged under.
func TestValidateAfterMergeUsesTheNotesDirectoryItMergedUnder(t *testing.T) {
	o := newOrigin(t, "")
	merged := o.merge("merge")
	o.git("mv", "_releases", "releases")
	o.write(".release-planner/config.yml", "schema-version: 1\nversion: v0.1.0\nfirst-version: v1.0.0\nrelease-notes-dir: releases\n")
	o.repo.commit("Move the release notes (#4)")
	dir := o.checkout(t)
	actionsFiles(t)
	mergedAPI(t, o, merged, nil)
	if p, out, errOut := validateMerged(t, dir, merged); p.Tag != "v1.0.0" || p.File != "_releases/v1.0.0.md" || p.Notes != "Approved notes\n" {
		t.Fatalf("%+v\n%s %s", p, out, errOut)
	}
}

func TestValidateAfterMergeRefusesWhatThePullRequestDidNotApprove(t *testing.T) {
	o := newOrigin(t, "")
	direct := o.git("rev-parse", "HEAD")
	actionsFiles(t)
	mergedAPI(t, o, direct, map[string]any{"GET /commits/" + direct + "/pulls": []any{}})
	if _, _, errOut := validateMerged(t, o.checkout(t), direct); !strings.Contains(errOut, "a direct push publishes nothing") {
		t.Fatal(errOut)
	}

	// The merge changed the approved notes on the way in.
	o.git("merge", "-q", "--no-ff", "--no-commit", "release")
	o.write("_releases/v1.0.0.md", "Changed while merging\n")
	merged := o.repo.commit("Merge pull request #2 from fabricahq/release")
	mergedAPI(t, o, merged, nil)
	if _, _, errOut := validateMerged(t, o.checkout(t), merged); !strings.Contains(errOut, "differs from _releases/v1.0.0.md at pull request #2's head") {
		t.Fatal(errOut)
	}

	if code, _, errOut := cli(t, "validate", "--dir", o.dir, "--ci", "--merged", "main"); code != 1 || !strings.Contains(errOut, "full SHA") {
		t.Fatal(errOut)
	}
}

// The pull request's successful run already checked the release commit and built its
// assets, so the merge reuses it while its artifacts last. Anything else builds again.
func TestValidateAfterMergeReusesThePullRequestRun(t *testing.T) {
	run := func(id int, head, repository string) map[string]any {
		return map[string]any{"id": id, "head_sha": head, "event": "pull_request", "conclusion": "success", "head_repository": map[string]string{"full_name": repository}}
	}
	artifacts := func(names ...string) map[string]any {
		var list []any
		for _, n := range names {
			list = append(list, map[string]any{"name": n, "expired": false})
		}
		return map[string]any{"artifacts": list}
	}
	for name, tc := range map[string]struct {
		config string
		routes map[string]any
		want   string
	}{
		"reuse": {"", map[string]any{"/runs": []any{run(77, "", "fabricahq/example")}, "/77": artifacts("release-plan")}, "build=false\nbuild-run=77\n"},
		"reuse with assets": {"release-assets:\n  workflow: build-release.yml\n",
			map[string]any{"/runs": []any{run(77, "", "fabricahq/example")}, "/77": artifacts("release-plan", "release-assets")}, "build=false\nbuild-run=77\n"},
		"assets expired": {"release-assets:\n  workflow: build-release.yml\n",
			map[string]any{"/runs": []any{run(77, "", "fabricahq/example")}, "/77": artifacts("release-plan")}, "build=true\nbuild-run=100\n"},
		"fork run":   {"", map[string]any{"/runs": []any{run(77, "", "someone/example")}, "/77": artifacts("release-plan")}, "build=true\nbuild-run=100\n"},
		"other head": {"", map[string]any{"/runs": []any{run(77, strings.Repeat("e", 40), "fabricahq/example")}, "/77": artifacts("release-plan")}, "build=true\nbuild-run=100\n"},
		"newest usable": {"", map[string]any{"/runs": []any{run(78, "", "someone/example"), run(77, "", "fabricahq/example")}, "/77": artifacts("release-plan")},
			"build=false\nbuild-run=77\n"},
		"lookup fails": {"", map[string]any{"/runs": status{500, "boom"}}, "build=true\nbuild-run=100\n"},
	} {
		t.Run(name, func(t *testing.T) {
			o := newOrigin(t, tc.config)
			if tc.config != "" {
				o.git("checkout", "-q", "release")
				o.git("reset", "-q", "--hard", o.head)
				o.git("checkout", "-q", "main")
			}
			merged := o.merge("squash")
			output := actionsFiles(t)
			runs := tc.routes["/runs"]
			if list, ok := runs.([]any); ok {
				for _, r := range list {
					if r.(map[string]any)["head_sha"] == "" {
						r.(map[string]any)["head_sha"] = o.head
					}
				}
				runs = map[string]any{"workflow_runs": list}
			}
			mergedAPI(t, o, merged, map[string]any{
				"GET /actions/workflows/release-planner.yml/runs": runs,
				"GET /actions/runs/77/artifacts":                  tc.routes["/77"],
			})
			p, out, errOut := validateMerged(t, o.checkout(t), merged)
			if p.Tag != "v1.0.0" || !strings.Contains(output("output"), tc.want) || p.Reused != strings.Contains(tc.want, "false") {
				t.Fatalf("%+v\n%s\n%s %s", p, output("output"), out, errOut)
			}
			if name == "lookup fails" && !strings.Contains(output("summary"), "Couldn't look for the pull request's run") {
				t.Fatal(output("summary"))
			}
		})
	}
}

// A fork's pull request is validated, but builds nothing: its token can't attest.
func TestValidateForkPullRequestBuildsNothing(t *testing.T) {
	o := newOrigin(t, "")
	base := o.git("rev-parse", "main")
	for repository, want := range map[string]string{"someone/example": "publish=false\nbuild=false\n", "fabricahq/example": "publish=false\nbuild=true\n"} {
		output := actionsFiles(t)
		newAPI(t, map[string]any{})
		if code, _, errOut := cli(t, "validate", "--dir", o.dir, "--ci", "--base", base, "--head", o.head, "--head-repository", repository); code != 0 || !strings.Contains(output("output"), want) {
			t.Fatalf("%s: %s %s", repository, errOut, output("output"))
		}
	}
}

// A notes edit on a pull request flags notes edited on GitHub since they last merged.
func TestValidateFlagsNotesEditedOnGitHub(t *testing.T) {
	o := newOrigin(t, "")
	merged := o.merge("merge")
	o.git("tag", "v1.0.0", o.release)
	o.git("checkout", "-q", "-b", "edit")
	o.write("_releases/v1.0.0.md", "Corrected notes\n")
	head := o.repo.commit("Correct the v1.0.0 notes")
	for body, want := range map[string]bool{"Approved notes\r\n": false, "Edited on GitHub": true} {
		actionsFiles(t)
		newAPI(t, map[string]any{"GET /releases": []any{map[string]any{"tag_name": "v1.0.0", "body": body, "html_url": "https://github.com/fabricahq/example/releases/tag/v1.0.0"}}})
		file := filepath.Join(t.TempDir(), "plan.json")
		code, _, errOut := cli(t, "validate", "--dir", o.dir, "--ci", "--base", merged, "--head", head, "--out", file)
		p, _ := readPlan(file)
		if code != 0 || p.Tag != "" || len(p.Edits) != 1 || p.Edits[0].HandEdited != want || p.Edits[0].Notes != "Corrected notes\n" {
			t.Fatalf("%q: %d %s %+v", body, code, errOut, p)
		}
	}
	actionsFiles(t)
	newAPI(t, map[string]any{"GET /releases": []any{}})
	file := filepath.Join(t.TempDir(), "plan.json")
	if code, _, _ := cli(t, "validate", "--dir", o.dir, "--ci", "--base", merged, "--head", head, "--out", file); code != 0 {
		t.Fatal(code)
	}
	if p, err := readPlan(file); err != nil || len(p.Warnings) == 0 || p.Warnings[0] != "v1.0.0 has no published release, so merging can't edit its notes." {
		t.Fatal(p.Warnings, err)
	}
}
