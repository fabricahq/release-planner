package main

import (
	"encoding/json"
	"path/filepath"
	"slices"
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

// runCheckout clones origin the way actions/checkout does in a run for commit sha, such as
// Re-run failed jobs on the merge's run: it moves origin/main back to sha, whatever main is now.
func (o *origin) runCheckout(t *testing.T, sha string) string {
	t.Helper()
	dir := o.checkout(t)
	r := &repo{t: t, dir: dir}
	r.git("update-ref", "refs/remotes/origin/main", sha)
	r.git("checkout", "-q", sha)
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
		"GET /git/ref/heads/main":                              map[string]any{"object": map[string]string{"type": "commit", "sha": o.git("rev-parse", "main")}},
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
// assets, so the merge reuses it while its artifacts last: the one plan it uploaded, or with
// assets, exactly the plan and assets its one release-binding names. Anything else builds again.
func TestValidateAfterMergeReusesThePullRequestRun(t *testing.T) {
	run := func(id int, head, repository string) map[string]any {
		return map[string]any{"id": id, "head_sha": head, "event": "pull_request", "conclusion": "success", "head_repository": map[string]string{"full_name": repository}}
	}
	// artifacts lists a run's artifacts, given as "id:name".
	artifacts := func(list ...string) map[string]any {
		var all []any
		for _, a := range list {
			id, name, _ := strings.Cut(a, ":")
			all = append(all, map[string]any{"id": json.Number(id), "name": name, "expired": false})
		}
		return map[string]any{"artifacts": all}
	}
	const assets = "release-assets:\n  workflow: build-release.yml\n"
	const rebuild = "build=true\nbuild-run=100\nbuilt-plan-artifact=\nbuilt-assets-artifact=\n"
	for name, tc := range map[string]struct {
		config string
		routes map[string]any
		want   string
	}{
		"reuse": {"", map[string]any{"/runs": []any{run(77, "", "fabricahq/example")}, "/77": artifacts("5:release-plan")}, "build=false\nbuild-run=77\nbuilt-plan-artifact=5\nbuilt-assets-artifact=\n"},
		"reuse with assets": {assets, map[string]any{"/runs": []any{run(77, "", "fabricahq/example")},
			"/77": artifacts("5:release-plan", "6:release-assets", "7:release-binding-5-6")}, "build=false\nbuild-run=77\nbuilt-plan-artifact=5\nbuilt-assets-artifact=6\n"},
		"assets expired": {assets, map[string]any{"/runs": []any{run(77, "", "fabricahq/example")}, "/77": artifacts("5:release-plan", "7:release-binding-5-6")}, rebuild},
		"no binding":     {assets, map[string]any{"/runs": []any{run(77, "", "fabricahq/example")}, "/77": artifacts("5:release-plan", "6:release-assets")}, rebuild},
		"two bindings": {assets, map[string]any{"/runs": []any{run(77, "", "fabricahq/example")},
			"/77": artifacts("5:release-plan", "6:release-assets", "8:release-assets", "7:release-binding-5-6", "9:release-binding-5-8")}, rebuild},
		"binding names another artifact": {assets, map[string]any{"/runs": []any{run(77, "", "fabricahq/example")},
			"/77": artifacts("5:release-plan", "6:release-assets", "7:release-binding-5-8")}, rebuild},
		"two plans":  {"", map[string]any{"/runs": []any{run(77, "", "fabricahq/example")}, "/77": artifacts("5:release-plan", "8:release-plan")}, rebuild},
		"fork run":   {"", map[string]any{"/runs": []any{run(77, "", "someone/example")}, "/77": artifacts("5:release-plan")}, rebuild},
		"other head": {"", map[string]any{"/runs": []any{run(77, strings.Repeat("e", 40), "fabricahq/example")}, "/77": artifacts("5:release-plan")}, rebuild},
		"newest usable": {"", map[string]any{"/runs": []any{run(78, "", "someone/example"), run(77, "", "fabricahq/example")}, "/77": artifacts("5:release-plan")},
			"build=false\nbuild-run=77\nbuilt-plan-artifact=5\n"},
		"lookup fails": {"", map[string]any{"/runs": status{500, "boom"}}, rebuild},
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
			if p.Tag != "v1.0.0" || !strings.Contains(output("output"), tc.want) || p.Reused != strings.Contains(tc.want, "build=false") {
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

// A run publishes only what its merge approved: once a later commit on main changes the
// notes file, by withdrawing the request or requesting the version again, the old run refuses.
func TestValidateAfterMergeRefusesAWithdrawnRequest(t *testing.T) {
	for name, tc := range map[string]struct {
		after func(o *origin)
		want  string
	}{
		"withdrawn": {func(o *origin) {
			o.git("rm", "-q", "_releases/v1.0.0.md")
			o.repo.commit("Fix the migration and withdraw v1.0.0 (#4)")
		}, "so v1.0.0 was withdrawn or requested again"},
		"requested again": {func(o *origin) {
			o.write("_releases/v1.0.0.md", "Notes with the fix\n")
			o.repo.commit("Release v1.0.0 (#5)")
		}, "so v1.0.0 was withdrawn or requested again"},
		// A pull request whose changes to the notes cancel out requests nothing, so the
		// request stands, at the same release commit, with the same notes.
		"deleted and added back on one side branch": {func(o *origin) {
			o.git("checkout", "-q", "-b", "side")
			o.git("rm", "-q", "_releases/v1.0.0.md")
			o.repo.commit("Withdraw v1.0.0")
			o.write("_releases/v1.0.0.md", "Approved notes\n")
			o.repo.commit("Request v1.0.0 again")
			o.git("checkout", "-q", "main")
			o.git("merge", "-q", "--no-ff", "side", "-m", "Merge pull request #6 from fabricahq/side")
		}, ""},
		// Rebased instead, each of the branch's commits is a step of main, so the deletion counts
		// and the run refuses, though the notes end up unchanged.
		"deleted and added back on one branch, rebased": {func(o *origin) {
			o.git("checkout", "-q", "-b", "side")
			o.git("rm", "-q", "_releases/v1.0.0.md")
			o.repo.commit("Withdraw v1.0.0")
			o.write("_releases/v1.0.0.md", "Approved notes\n")
			o.repo.commit("Request v1.0.0 again")
			o.git("checkout", "-q", "main")
			o.git("cherry-pick", "main..side")
		}, "so v1.0.0 was withdrawn or requested again"},
		// Two pull requests withdraw it, then request it again, at a newer release commit.
		"deleted, then added back": {func(o *origin) {
			o.git("rm", "-q", "_releases/v1.0.0.md")
			o.repo.commit("Withdraw v1.0.0 (#6)")
			o.write("_releases/v1.0.0.md", "Approved notes\n")
			o.repo.commit("Release v1.0.0 (#7)")
		}, "so v1.0.0 was withdrawn or requested again"},
		"deleted in an octopus merge": {func(o *origin) {
			for _, b := range []string{"a", "b"} {
				o.git("checkout", "-q", "-b", b, "main")
				o.write(b+".go", "package "+b+"\n")
				o.repo.commit("Change " + b)
			}
			o.git("checkout", "-q", "main")
			o.git("merge", "-q", "--no-ff", "--no-commit", "a", "b")
			o.git("rm", "-q", "_releases/v1.0.0.md")
			o.repo.commit("Merge branches a and b")
		}, "so v1.0.0 was withdrawn or requested again"},
		// A branch that started before the merge merges main in, dropping the notes, and main
		// is fast-forwarded to it: main's history no longer runs through the approving merge.
		"fast-forwarded to a merge that dropped the notes": {func(o *origin) {
			o.git("checkout", "-q", "-b", "feature", o.release)
			o.write("feature.go", "package feature\n")
			o.repo.commit("Feature")
			o.git("merge", "-q", "--no-ff", "--no-commit", "main")
			o.git("rm", "-qf", "_releases/v1.0.0.md")
			o.repo.commit("Merge main into feature")
			o.git("checkout", "-q", "main")
			o.git("merge", "-q", "--ff-only", "feature")
		}, "main's history no longer runs through pull request #2's merge"},
		// The same branch keeps the notes and merges back with a merge commit: nothing changed.
		"an older branch merges main in, keeping the notes, then merges back": {func(o *origin) {
			o.git("checkout", "-q", "-b", "feature", o.release)
			o.write("feature.go", "package feature\n")
			o.repo.commit("Feature")
			o.git("merge", "-q", "--no-ff", "main", "-m", "Merge main into feature")
			o.git("checkout", "-q", "main")
			o.git("merge", "-q", "--no-ff", "feature", "-m", "Merge pull request #8 from fabricahq/feature")
		}, ""},
		"deleted in a merge commit": {func(o *origin) {
			o.git("checkout", "-q", "-b", "fix")
			o.write("fix.go", "package fix\n")
			o.repo.commit("Fix the migration")
			o.git("checkout", "-q", "main")
			o.git("merge", "-q", "--no-ff", "--no-commit", "fix")
			o.git("rm", "-q", "_releases/v1.0.0.md")
			o.repo.commit("Merge pull request #11 from fabricahq/fix")
		}, "so v1.0.0 was withdrawn or requested again"},
		"replaced in a merge commit": {func(o *origin) {
			o.git("checkout", "-q", "-b", "fix")
			o.write("fix.go", "package fix\n")
			o.repo.commit("Fix the migration")
			o.git("checkout", "-q", "main")
			o.git("merge", "-q", "--no-ff", "--no-commit", "fix")
			o.write("_releases/v1.0.0.md", "Notes with the fix\n")
			o.repo.commit("Merge pull request #12 from fabricahq/fix")
		}, "so v1.0.0 was withdrawn or requested again"},
		"unrelated change": {func(o *origin) {
			o.write("later.go", "package later // changed\n")
			o.repo.commit("Unrelated change (#7)")
		}, ""},
		"already tagged": {func(o *origin) {
			o.git("tag", "v1.0.0", o.release)
			o.write("_releases/v1.0.0.md", "Corrected after publication\n")
			o.repo.commit("Edit the v1.0.0 release notes (#8)")
		}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			o := newOrigin(t, "")
			merged := o.merge("merge")
			tc.after(o)
			output := actionsFiles(t)
			mergedAPI(t, o, merged, nil)
			p, out, errOut := validateMerged(t, o.runCheckout(t, merged), merged)
			if tc.want == "" {
				if p.Tag != "v1.0.0" {
					t.Fatalf("%+v\n%s %s", p, out, errOut)
				}
				return
			}
			// The withdrawal names the main commit that made it.
			if name == "withdrawn" && !strings.Contains(errOut, "_releases/v1.0.0.md changed on main after pull request #2 merged (in "+o.git("rev-parse", "--short", "main")+"), ") {
				t.Fatal(errOut)
			}
			if p.Tag != "" || !strings.Contains(errOut, tc.want) {
				t.Fatalf("%+v\n%s %s", p, out, errOut)
			}
			if !strings.Contains(output("output"), "withdrawn=") {
				t.Fatalf("outputs lack withdrawn:\n%s", output("output"))
			}
		})
	}
}

// An old notes edit's run doesn't restore older notes over a newer approved edit.
func TestValidateAfterMergeRefusesAReplacedNotesEdit(t *testing.T) {
	o := newOrigin(t, "")
	o.merge("merge")
	o.git("tag", "v1.0.0", o.release)
	o.git("checkout", "-q", "-b", "edit")
	o.write("_releases/v1.0.0.md", "First correction\n")
	head := o.repo.commit("Correct the v1.0.0 notes")
	o.git("checkout", "-q", "main")
	o.git("merge", "-q", "--no-ff", "edit", "-m", "Merge pull request #9 from fabricahq/edit")
	edit := o.git("rev-parse", "HEAD")
	o.write("_releases/v1.0.0.md", "Second correction\n")
	o.repo.commit("Correct the v1.0.0 notes again (#10)")
	actionsFiles(t)
	mergedAPI(t, o, edit, map[string]any{
		"GET /commits/" + edit + "/pulls": []any{map[string]any{"number": 9, "merged_at": "2026-09-25T12:00:00Z", "merge_commit_sha": edit, "base": map[string]string{"ref": "main"}}},
		"GET /pulls/9":                    map[string]any{"head": map[string]any{"sha": head, "repo": map[string]string{"full_name": "fabricahq/example"}}, "merged_by": map[string]string{"login": "mona"}},
	})
	if _, _, errOut := validateMerged(t, o.runCheckout(t, edit), edit); !strings.Contains(errOut, "so this run doesn't replace the v1.0.0 notes; the newer change's run does") {
		t.Fatal(errOut)
	}
}

// publish applies the same check as validate, so both of Astra's reproductions hold through the
// publish command: a fast-forward that dropped the notes refuses before any API write, and an
// older branch merged back with the notes kept goes on to publish.
func TestPublishChecksTheReleaseBranchWhateverItsMergeShape(t *testing.T) {
	for name, tc := range map[string]struct {
		keep bool
		ff   bool
	}{
		"fast-forwarded to a merge that dropped the notes": {false, true},
		"merged back, keeping the notes":                   {true, false},
	} {
		t.Run(name, func(t *testing.T) {
			o := newOrigin(t, "")
			merged := o.merge("merge")
			actionsFiles(t)
			mergedAPI(t, o, merged, nil)
			file := filepath.Join(t.TempDir(), "plan.json")
			if code, _, errOut := cli(t, "validate", "--dir", o.checkout(t), "--ci", "--merged", merged, "--out", file); code != 0 {
				t.Fatal(errOut)
			}
			o.git("checkout", "-q", "-b", "feature", o.release)
			o.write("feature.go", "package feature\n")
			o.repo.commit("Feature")
			o.git("merge", "-q", "--no-ff", "--no-commit", "main")
			if !tc.keep {
				o.git("rm", "-qf", "_releases/v1.0.0.md")
			}
			o.repo.commit("Merge main into feature")
			o.git("checkout", "-q", "main")
			if tc.ff {
				o.git("merge", "-q", "--ff-only", "feature")
			} else {
				o.git("merge", "-q", "--no-ff", "feature", "-m", "Merge pull request #8 from fabricahq/feature")
			}
			api := newAPI(t, map[string]any{"GET /git/ref/heads/main": map[string]any{"object": map[string]string{"type": "commit", "sha": o.git("rev-parse", "main")}}})
			code, _, errOut := cli(t, "publish", "--dir", o.runCheckout(t, merged), "--plan", file, "--built-plan", file, "--branch", "main")
			passed := slices.Contains(api.requests, "GET /commits/"+merged+"/pulls")
			refused := strings.Contains(errOut, "main's history no longer runs through pull request #2's merge")
			if code != 1 || passed != tc.keep || refused == tc.keep {
				t.Fatalf("%d %s %v", code, errOut, api.requests)
			}
		})
	}
}

// Re-run failed jobs reuses validate's plan, so publish checks the release branch again
// before it writes: a request withdrawn since the merge never publishes.
func TestPublishRefusesARequestWithdrawnAfterValidation(t *testing.T) {
	o := newOrigin(t, "")
	merged := o.merge("merge")
	actionsFiles(t)
	mergedAPI(t, o, merged, nil)
	file := filepath.Join(t.TempDir(), "plan.json")
	if code, _, errOut := cli(t, "validate", "--dir", o.checkout(t), "--ci", "--merged", merged, "--out", file); code != 0 {
		t.Fatal(errOut)
	}
	o.git("rm", "-q", "_releases/v1.0.0.md")
	o.repo.commit("Fix the migration and withdraw v1.0.0 (#4)")
	api := newAPI(t, map[string]any{"GET /git/ref/heads/main": map[string]any{"object": map[string]string{"type": "commit", "sha": o.git("rev-parse", "main")}}})
	output := actionsFiles(t)
	code, _, errOut := cli(t, "publish", "--dir", o.runCheckout(t, merged), "--plan", file, "--built-plan", file, "--branch", "main")
	if code != 1 || !strings.Contains(errOut, "so v1.0.0 was withdrawn or requested again") || strings.Join(api.requests, ",") != "GET /git/ref/heads/main" {
		t.Fatalf("%d %s %v", code, errOut, api.requests)
	}
	// The report says the run stopped on purpose.
	if output("output") != "release=withdrawn\n" {
		t.Fatalf("outputs %q", output("output"))
	}
}
