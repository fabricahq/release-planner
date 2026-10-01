package main

import (
	"path/filepath"
	"strings"
	"testing"
)

const prePublishConfig = "pre-publish:\n  - workflow: migrate-database.yml\n"

// migrateWorkflow is a pre-publish workflow whose job runs in environment.
func migrateWorkflow(environment string) string {
	return `name: Migrate the database
on:
  workflow_call:
    inputs:
      ref:
        type: string
        required: true
      tag:
        type: string
        required: true
      version:
        type: string
        required: true
jobs:
  migrate:
    runs-on: ubuntu-latest
    environment: ` + environment + `
    steps:
      - run: make migrate
`
}

// validate reads the environment from the workflow in the run's checkout, the one that runs,
// so a workflow moved to another environment after install is checked where it now runs.
func TestValidateReadsTheEnvironmentFromTheWorkflowThatRuns(t *testing.T) {
	o, merged := laterOrigin(t, prePublishConfig, nil, func(o *origin) {})
	o.git("checkout", "-q", "-b", "move", merged)
	o.write(".github/workflows/migrate-database.yml", migrateWorkflow("staging"))
	moved := o.repo.commit("Move the migration to staging")
	actionsFiles(t)
	mergedAPI(t, o, merged, production(nil))
	// A manual retry runs from a checkout of a later commit, here the one that moved it.
	dir := o.checkout(t)
	(&repo{t: t, dir: dir}).git("checkout", "-q", moved)
	if p, _, errOut := validateMerged(t, dir, merged); p.Tag != "" || !strings.Contains(errOut, "The staging environment doesn't exist") {
		t.Fatalf("%+v %s", p, errOut)
	}
}

// production is the recommended pre-publish environment: only main can use it.
func production(routes map[string]any) map[string]any {
	all := map[string]any{
		"GET /environments/production":                            map[string]any{"deployment_branch_policy": map[string]bool{"custom_branch_policies": true}, "protection_rules": []any{}},
		"GET /environments/production/deployment-branch-policies": map[string]any{"branch_policies": []any{map[string]string{"name": "main", "type": "branch"}}},
		"GET /releases": []any{map[string]any{"tag_name": "v0.9.0", "draft": false}},
	}
	for k, v := range routes {
		all[k] = v
	}
	return all
}

// laterOrigin is an origin whose v0.9.0 is published, and whose pull request #2 requests v1.0.0
// while main gains more, with later changing main after the merge.
func laterOrigin(t *testing.T, config string, onMain, later func(o *origin)) (*origin, string) {
	t.Helper()
	o := newOrigin(t, config)
	o.git("tag", "v0.9.0", o.release)
	if onMain != nil {
		onMain(o)
	}
	merged := o.merge("merge")
	if later != nil {
		later(o)
	}
	return o, merged
}

// With a pre-publish workflow, a release waits after its merge, before anything runs, while an
// earlier release isn't published: the previous one, or one merged on main that isn't tagged.
// Once that release is published or withdrawn, Re-run failed jobs runs validate again.
func TestValidateAfterMergeWaitsForEarlierReleases(t *testing.T) {
	pending := func(o *origin) {
		o.write("_releases/v0.9.5.md", "Pending\n")
		o.repo.commit("Release v0.9.5 (#4)")
	}
	withdraw := func(o *origin) {
		o.git("rm", "-q", "_releases/v0.9.5.md")
		o.repo.commit("Withdraw v0.9.5 (#5)")
	}
	for name, tc := range map[string]struct {
		config         string
		onMain, later  func(o *origin)
		routes         map[string]any
		want, waitsFor string
		waitsForFile   string
	}{
		"earlier request pending": {prePublishConfig, pending, nil, nil,
			"_releases/v0.9.5.md is on main, but v0.9.5 isn't published, so v1.0.0 waits for it. Publish v0.9.5, or withdraw it by deleting _releases/v0.9.5.md in a pull request. Then use Re-run failed jobs on this run", "v0.9.5", "_releases/v0.9.5.md"},
		// Versions at or below the previous release don't hold it up, whatever their state:
		// the previous release was published after them, so a deleted old release never blocks.
		"older release below the previous one tagged but unpublished": {prePublishConfig, func(o *origin) {
			o.write("_releases/v0.9.5.md", "Tagged by hand\n")
			o.write("_releases/v0.9.7.md", "Published\n")
			o.repo.commit("Release v0.9.5 and v0.9.7 (#4)")
			o.git("tag", "v0.9.5", o.release)
			o.git("tag", "v0.9.7", o.release)
		}, nil, map[string]any{"GET /releases": []any{map[string]any{"tag_name": "v0.9.0", "draft": false}, map[string]any{"tag_name": "v0.9.7", "draft": false}},
			"GET /git/ref/tags/v0.9.5": map[string]any{"object": map[string]string{"type": "commit", "sha": strings.Repeat("a", 40)}}}, "", "", ""},
		"older release below the previous one deleted, notes untagged": {prePublishConfig, func(o *origin) {
			o.write("_releases/v0.8.0.md", "Its release was deleted\n")
			o.repo.commit("Release v0.8.0 (#4)")
		}, nil, nil, "", "", ""},
		// The tag appears on GitHub after the checkout read the tags: the plan doesn't know it,
		// but publication is read from GitHub, so the release still waits.
		"earlier release tagged after the plan was read": {prePublishConfig, pending, nil,
			map[string]any{"GET /git/ref/tags/v0.9.5": map[string]any{"object": map[string]string{"type": "commit", "sha": strings.Repeat("a", 40)}}},
			"v0.9.5 is tagged but has no published release, so v1.0.0 waits for it", "v0.9.5", ""},
		"earlier releases all published": {prePublishConfig, func(o *origin) {
			o.write("_releases/v0.9.5.md", "Published\n")
			o.repo.commit("Release v0.9.5 (#4)")
			o.git("tag", "v0.9.5", o.release)
		}, nil, map[string]any{"GET /releases": []any{map[string]any{"tag_name": "v0.9.0", "draft": false}, map[string]any{"tag_name": "v0.9.5", "draft": false}}}, "", "", ""},
		"earlier request withdrawn": {prePublishConfig, pending, withdraw, nil, "", "", ""},
		"later request pending":     {prePublishConfig, func(o *origin) { o.write("_releases/v1.1.0.md", "Later\n"); o.repo.commit("Release v1.1.0 (#4)") }, nil, nil, "", "", ""},
		"previous release missing": {prePublishConfig, nil, nil, map[string]any{"GET /releases": []any{}},
			"v0.9.0 is tagged but has no published release, so v1.0.0 waits for it. Publish v0.9.0 by re-running its release run; if its tag isn't on its release commit, delete the tag first. Then use Re-run failed jobs on this run", "v0.9.0", ""},
		"previous release a draft": {prePublishConfig, nil, nil, map[string]any{"GET /releases": []any{map[string]any{"tag_name": "v0.9.0", "draft": true}}},
			"v0.9.0 is tagged but has no published release", "v0.9.0", ""},
		"without pre-publish": {"", pending, nil, map[string]any{"GET /releases": []any{}}, "", "", ""},
	} {
		t.Run(name, func(t *testing.T) {
			o, merged := laterOrigin(t, tc.config, tc.onMain, tc.later)
			output := actionsFiles(t)
			mergedAPI(t, o, merged, production(tc.routes))
			p, out, errOut := validateMerged(t, o.runCheckout(t, merged), merged)
			if tc.want == "" {
				if p.Tag != "v1.0.0" || strings.Contains(output("output"), "waiting-for=v") {
					t.Fatalf("%+v\n%s %s\n%s", p, out, errOut, output("output"))
				}
				return
			}
			if p.Tag != "" || !strings.Contains(errOut, tc.want) {
				t.Fatalf("%+v\n%s %s", p, out, errOut)
			}
			if want := "waiting-for=" + tc.waitsFor + "\nwaiting-for-file=" + tc.waitsForFile + "\n"; !strings.Contains(output("output"), want) {
				t.Fatalf("outputs lack %q:\n%s", want, output("output"))
			}
		})
	}
}

// On the release pull request, an earlier unpublished release is a warning: it may publish
// before the merge.
func TestValidateWarnsThatAReleaseWillWait(t *testing.T) {
	o := newOrigin(t, prePublishConfig)
	o.write("_releases/v0.9.5.md", "Pending\n")
	o.git("tag", "v0.9.0", o.release)
	base := o.repo.commit("Release v0.9.5 (#4)")
	output := actionsFiles(t)
	newAPI(t, production(nil))
	file := filepath.Join(t.TempDir(), "plan.json")
	if code, _, errOut := cli(t, "validate", "--dir", o.dir, "--ci", "--base", base, "--head", o.head, "--out", file); code != 0 {
		t.Fatal(errOut)
	}
	if p, err := readPlan(file); err != nil || !strings.Contains(strings.Join(p.Warnings, "\n"), "v0.9.5 merged before this release and isn't published yet. After you merge, this release waits until v0.9.5 is published or withdrawn.") {
		t.Fatalf("%+v %v\n%s", p.Warnings, err, output("summary"))
	}
}

// After the merge, the pre-publish workflow runs only if its environment keeps its credentials
// to the release branch; on the pull request, the same problems are warnings.
func TestValidateChecksThePrePublishEnvironment(t *testing.T) {
	custom := map[string]any{"deployment_branch_policy": map[string]bool{"custom_branch_policies": true}, "protection_rules": []any{}}
	rules := func(r ...map[string]string) map[string]any {
		var list []any
		for _, x := range r {
			list = append(list, x)
		}
		return map[string]any{"branch_policies": list}
	}
	for name, tc := range map[string]struct {
		env, rules any
		refuse     string
		warn       string
	}{
		"recommended":    {custom, rules(map[string]string{"name": "main", "type": "branch"}), "", ""},
		"missing":        {status{404, nil}, nil, "The production environment doesn't exist", ""},
		"unrestricted":   {map[string]any{"deployment_branch_policy": nil, "protection_rules": []any{}}, nil, "The production environment has no deployment branch rule", ""},
		"other branch":   {custom, rules(map[string]string{"name": "develop", "type": "branch"}), "The production environment's branch rules don't include main", ""},
		"tag rule":       {custom, rules(map[string]string{"name": "main", "type": "branch"}, map[string]string{"name": "v*", "type": "tag"}), "", "The production environment also lets tags matching v* use it"},
		"extra branch":   {custom, rules(map[string]string{"name": "main", "type": "branch"}, map[string]string{"name": "develop", "type": "branch"}), "", "The production environment also lets branches matching develop use it"},
		"protected only": {map[string]any{"deployment_branch_policy": map[string]bool{"protected_branches": true}, "protection_rules": []any{}}, nil, "", "The production environment lets every protected branch use it"},
	} {
		t.Run(name, func(t *testing.T) {
			o, merged := laterOrigin(t, prePublishConfig, nil, nil)
			output := actionsFiles(t)
			mergedAPI(t, o, merged, production(map[string]any{
				"GET /environments/production":                            tc.env,
				"GET /environments/production/deployment-branch-policies": tc.rules,
				"GET /branches/main":                                      map[string]any{"protected": true},
			}))
			p, out, errOut := validateMerged(t, o.checkout(t), merged)
			if tc.refuse != "" {
				if p.Tag != "" || !strings.Contains(errOut, tc.refuse) || !strings.Contains(errOut, "Then use Re-run failed jobs on this run") {
					t.Fatalf("%+v\n%s %s", p, out, errOut)
				}
				return
			}
			warned := strings.Contains(output("summary"), "The production environment")
			if p.Tag != "v1.0.0" || warned != (tc.warn != "") || !strings.Contains(output("summary"), tc.warn) {
				t.Fatalf("%+v\n%s %s\n%s", p, out, errOut, output("summary"))
			}
		})
	}
}

// Each way the message offers out of waiting works: publishing the earlier release, tagged or
// not, or withdrawing an untagged one, and then validating again, as Re-run failed jobs does.
func TestAWaitingReleaseContinuesOnceTheEarlierOneIsPublishedOrWithdrawn(t *testing.T) {
	tagged := strings.Repeat("a", 40)
	for name, tc := range map[string]struct {
		tagged  bool
		unblock func(o *origin, api *fakeAPI)
	}{
		"tagged, then published": {true, func(o *origin, api *fakeAPI) {
			api.set("GET /releases", []any{map[string]any{"tag_name": "v0.9.0"}, map[string]any{"tag_name": "v0.9.5"}})
		}},
		"untagged, then published": {false, func(o *origin, api *fakeAPI) {
			api.set("GET /releases", []any{map[string]any{"tag_name": "v0.9.0"}, map[string]any{"tag_name": "v0.9.5"}})
		}},
		"untagged, then withdrawn": {false, func(o *origin, api *fakeAPI) {
			o.git("rm", "-q", "_releases/v0.9.5.md")
			api.set("GET /git/ref/heads/main", map[string]any{"object": map[string]string{"type": "commit", "sha": o.repo.commit("Withdraw v0.9.5 (#5)")}})
		}},
	} {
		t.Run(name, func(t *testing.T) {
			o, merged := laterOrigin(t, prePublishConfig, func(o *origin) {
				o.write("_releases/v0.9.5.md", "Pending\n")
				o.repo.commit("Release v0.9.5 (#4)")
			}, nil)
			routes := map[string]any{}
			if tc.tagged {
				routes["GET /git/ref/tags/v0.9.5"] = map[string]any{"object": map[string]string{"type": "commit", "sha": tagged}}
			}
			actionsFiles(t)
			api := mergedAPI(t, o, merged, production(routes))
			if p, _, errOut := validateMerged(t, o.runCheckout(t, merged), merged); p.Tag != "" || !strings.Contains(errOut, "waits for it") {
				t.Fatalf("%+v %s", p, errOut)
			}
			tc.unblock(o, api)
			actionsFiles(t)
			if p, _, errOut := validateMerged(t, o.runCheckout(t, merged), merged); p.Tag != "v1.0.0" {
				t.Fatalf("%+v %s", p, errOut)
			}
		})
	}
}

// Each pre-publish workflow's environment is checked, and they may differ: here the second's
// doesn't exist, so the run stops before either runs.
func TestValidateChecksEachPrePublishEnvironment(t *testing.T) {
	o := newOrigin(t, prePublishConfig+"  - workflow: warm-caches.yml\n")
	o.git("tag", "v0.9.0", o.release)
	o.git("checkout", "-q", "main")
	merged := o.merge("merge")
	o.write(".github/workflows/warm-caches.yml", migrateWorkflow("staging"))
	o.repo.commit("Add the cache workflow")
	actionsFiles(t)
	mergedAPI(t, o, merged, production(nil))
	dir := o.checkout(t)
	(&repo{t: t, dir: dir}).git("checkout", "-q", "main")
	if p, _, errOut := validateMerged(t, dir, merged); p.Tag != "" || !strings.Contains(errOut, "The staging environment doesn't exist, so warm-caches.yml can't run safely") {
		t.Fatalf("%+v %s", p, errOut)
	}
}
