package plan

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/fabricahq/release-planner/internal/gitrepo"
)

var opts = Options{NotesDir: "releases", FirstVersion: "v1.0.0"}

// fixture is a throwaway repository with one initial commit.
type fixture struct {
	t       *testing.T
	repo    gitrepo.Repo
	initial string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, repo: gitrepo.Repo{Dir: t.TempDir()}}
	f.git("init", "-q", "-b", "main")
	f.write("README.md", "Fixture")
	f.initial = f.commit("Initial")
	return f
}

func (f *fixture) git(args ...string) string {
	f.t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
	cmd.Dir = f.repo.Dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (f *fixture) write(name, body string) {
	f.t.Helper()
	file := filepath.Join(f.repo.Dir, name)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) commit(message string) string {
	f.git("add", "-A")
	f.git("commit", "-q", "-m", message)
	return f.git("rev-parse", "HEAD")
}

func (f *fixture) plan(base, head string) (Plan, error) {
	return Read(context.Background(), f.repo, opts, base, head)
}

func TestReadValidatesReleaseRequests(t *testing.T) {
	for _, tc := range []struct{ name, tag, notes, existing, want string }{
		{"first", "v1.0.0", "## ✨ New Features\nFirst release.\n", "", ""},
		{"wrong-first", "v0.1.0", "Notes", "", "first release"},
		{"empty", "v1.0.0", " \n", "", "empty"},
		{"invalid", "v01.1.0", "Notes", "", "name the file"},
		{"partial", "v1.0", "Notes", "", "name the file"},
		{"metadata", "v1.0.0+build.1", "Notes", "", "build metadata"},
		{"patch", "v1.0.1", "Notes", "v1.0.0", ""},
		{"minor", "v1.1.0", "Notes", "v1.0.0", ""},
		{"major", "v2.0.0", "Notes", "v1.9.0", ""},
		{"prerelease", "v1.1.0-rc.1", "Notes", "v1.0.0", ""},
		{"backwards", "v1.0.0", "Notes", "v1.1.0", "newer"},
		{"prerelease-after-release", "v1.1.0-rc.1", "Notes", "v1.1.0", "newer"},
		{"reused", "v1.0.0", "Notes", "v1.0.0", "another commit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.existing != "" {
				f.git("tag", tc.existing)
			}
			f.write("releases/"+tc.tag+".md", tc.notes)
			head := f.commit("Release")
			p, err := f.plan(f.initial, head)
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("got %v, want %q", err, tc.want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if p.Tag != tc.tag || p.Notes != tc.notes || p.Commit != head || p.Previous != tc.existing || p.Prerelease != strings.Contains(tc.tag, "-") {
				t.Fatalf("unexpected plan: %+v", p)
			}
			f.git("tag", tc.tag)
			retry, err := f.plan(f.initial, head)
			if err != nil || !reflect.DeepEqual(retry, p) {
				t.Fatalf("retry at the tagged commit: %+v %v", retry, err)
			}
		})
	}
}

func TestReadWithoutNotesRequestsNoRelease(t *testing.T) {
	f := newFixture(t)
	f.write("practices/rule.md", "Rule")
	p, err := f.plan(f.initial, f.commit("Rule"))
	if err != nil || p.Tag != "" {
		t.Fatal(p, err)
	}
}

func TestReadRejectsEditsToTaggedNotes(t *testing.T) {
	f := newFixture(t)
	f.write("releases/v1.0.0.md", "Original notes")
	first := f.commit("Notes")
	f.git("tag", "v1.0.0")
	f.write("releases/v1.0.0.md", "Uncommitted edit")
	if p, err := f.plan(f.initial, first); err != nil || p.Notes != "Original notes" {
		t.Fatal(p, err)
	}
	f.commit("Edit")
	if _, err := f.plan(first, "HEAD"); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatal(err)
	}
	f.git("tag", "-d", "v1.0.0")
	base := f.git("rev-parse", "HEAD")
	f.write("releases/v1.0.0.md", "Edited again")
	f.write("releases/v1.1.0.md", "Second")
	f.commit("More notes")
	if _, err := f.plan(base, "HEAD"); err == nil || !strings.Contains(err.Error(), "one release") {
		t.Fatal(err)
	}
}

func TestCorrectOrWithdrawUntaggedRequest(t *testing.T) {
	f := newFixture(t)
	f.write("releases/v1.0.0.md", "Original")
	original := f.commit("Request")
	f.write("releases/v1.0.0.md", "Corrected")
	corrected := f.commit("Correct")
	if p, err := f.plan(original, corrected); err != nil || p.Tag != "v1.0.0" || p.Notes != "Corrected" {
		t.Fatal(p, err)
	}
	f.git("rm", "-q", "releases/v1.0.0.md")
	f.commit("Withdraw")
	if p, err := f.plan(corrected, "HEAD"); err != nil || p.Tag != "" {
		t.Fatal(p, err)
	}
}

// Publication can create the tag and then fail. Retrying the same corrected range must
// still plan, while any later edit to the now-published notes stays immutable.
func TestRetriesCorrectedRequestAfterTagCreated(t *testing.T) {
	f := newFixture(t)
	f.write("releases/v1.0.0.md", "Original")
	original := f.commit("Request")
	f.write("releases/v1.0.0.md", "Corrected")
	corrected := f.commit("Correct")
	f.git("tag", "v1.0.0", corrected)
	if p, err := f.plan(original, corrected); err != nil || p.Tag != "v1.0.0" || p.Notes != "Corrected" {
		t.Fatal(p, err)
	}
	f.write("releases/v1.0.0.md", "Edited after publication")
	f.commit("Edit")
	if _, err := f.plan(corrected, "HEAD"); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatal(err)
	}
}

func TestIgnoresMalformedVersionTags(t *testing.T) {
	f := newFixture(t)
	f.git("tag", "v-preview")
	f.write("releases/v1.0.0.md", "Approved notes")
	f.commit("Release")
	if p, err := f.plan(f.initial, "HEAD"); err != nil || len(p.Tags) != 0 {
		t.Fatal(p, err)
	}
}

func TestPendingRequestCannotBeSkipped(t *testing.T) {
	f := newFixture(t)
	f.git("tag", "v1.0.0")
	f.write("releases/v1.1.0.md", "Pending release")
	base := f.commit("Pending request")
	f.write("releases/v1.2.0.md", "Later release")
	f.commit("Later request")
	if _, err := f.plan(base, "HEAD"); err == nil || !strings.Contains(err.Error(), "untagged release request") {
		t.Fatal(err)
	}
	f.git("tag", "v1.1.0", base)
	if p, err := f.plan(base, "HEAD"); err != nil || p.Tag != "v1.2.0" || p.Previous != "v1.1.0" {
		t.Fatal(p, err)
	}
	f.git("tag", "-d", "v1.1.0")
	f.git("rm", "-q", "releases/v1.1.0.md")
	f.commit("Withdraw pending request")
	if p, err := f.plan(base, "HEAD"); err != nil || p.Tag != "v1.2.0" || p.Previous != "v1.0.0" {
		t.Fatal(p, err)
	}
}

func TestReadUsesTheConfiguredNotesDirectory(t *testing.T) {
	f := newFixture(t)
	f.write("docs/releases/v1.0.0.md", "Notes")
	f.write("releases/v9.0.0.md", "Outside the configured directory")
	f.commit("Release")
	p, err := Read(context.Background(), f.repo, Options{NotesDir: "docs/releases", FirstVersion: "v1.0.0"}, f.initial, "HEAD")
	if err != nil || p.Tag != "v1.0.0" {
		t.Fatal(p, err)
	}
}

func TestInventoryListsChangesSinceThePreviousRelease(t *testing.T) {
	f := newFixture(t)
	f.write("a.md", "a")
	f.commit("Add a (#1)")
	inv, err := Take(context.Background(), f.repo, opts, "HEAD")
	if err != nil || inv.Previous != "" || inv.Candidates["first"] != "v1.0.0" || len(inv.Commits) != 2 || !reflect.DeepEqual(inv.PullRequests, []int{1}) {
		t.Fatalf("first release inventory: %+v %v", inv, err)
	}
	if c := inv.Commits[1]; c.Title != "Add a" || c.PullRequest != 1 {
		t.Fatalf("squash commit: %+v", c)
	}

	f.write("releases/v1.0.0.md", "First")
	f.commit("Release v1.0.0")
	f.git("tag", "v1.0.0")
	f.write("b.md", "b")
	f.git("add", "-A")
	f.git("commit", "-q", "-m", "Merge pull request #7 from fabricahq/feature", "-m", "Add the b file")
	f.write("releases/v1.1.0.md", "Pending")
	f.commit("Request v1.1.0")
	inv, err = Take(context.Background(), f.repo, opts, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"patch": "v1.0.1", "minor": "v1.1.0", "major": "v2.0.0"}
	if inv.Previous != "v1.0.0" || !reflect.DeepEqual(inv.Candidates, want) || !reflect.DeepEqual(inv.PullRequests, []int{7}) ||
		!reflect.DeepEqual(inv.PendingRequests, []string{"releases/v1.1.0.md"}) || len(inv.Commits) != 1 {
		// The Request v1.1.0 commit only adds notes, so it's the request, not a change to release.
		t.Fatalf("later inventory: %+v", inv)
	}
	if c := inv.Commits[0]; c.Title != "Add the b file" || c.PullRequest != 7 || !c.OnBranch {
		t.Fatalf("merge commit: %+v", c)
	}

	// A newer tag on a branch main does not contain is surfaced, not silently used.
	f.git("checkout", "-q", "-b", "side")
	f.write("c.md", "c")
	f.commit("Side")
	f.git("tag", "v1.2.0")
	f.git("checkout", "-q", "main")
	inv, err = Take(context.Background(), f.repo, opts, "HEAD")
	if err != nil || inv.Previous != "v1.0.0" || !reflect.DeepEqual(inv.UnmergedNewerTags, []string{"v1.2.0"}) {
		t.Fatalf("unmerged tag: %+v %v", inv, err)
	}
}

// Moving published notes to a new notes directory, byte for byte, requests nothing. Changing
// them on the way, or re-adding them for a tag the notes don't match, is still refused.
func TestRelocatedTaggedNotesAreHistory(t *testing.T) {
	f := newFixture(t)
	f.write("releases/v1.0.0.md", "Published notes")
	published := f.commit("Release v1.0.0")
	f.git("tag", "v1.0.0", published)
	moved := Options{NotesDir: "_releases", FirstVersion: "v1.0.0"}

	f.git("mv", "releases", "_releases")
	relocation := f.commit("Move notes")
	if p, err := Read(context.Background(), f.repo, moved, published, relocation); err != nil || p.Tag != "" {
		t.Fatal(p, err)
	}

	f.write("_releases/v1.0.0.md", "Rewritten while moving")
	f.commit("Rewrite")
	if _, err := Read(context.Background(), f.repo, moved, published, "HEAD"); err == nil || !strings.Contains(err.Error(), "already points to another commit") {
		t.Fatal(err)
	}
}

// A tagged commit with more than one file of the notes' name can't say which was published,
// so a matching copy of the other one isn't accepted as a move.
func TestRelocationNeedsOnePublishedFile(t *testing.T) {
	f := newFixture(t)
	f.write("releases/v1.0.0.md", "Approved")
	f.write("docs/v1.0.0.md", "Rewritten")
	published := f.commit("Release v1.0.0")
	f.git("tag", "v1.0.0", published)
	f.write("_releases/v1.0.0.md", "Rewritten")
	f.commit("Move rewritten notes")
	moved := Options{NotesDir: "_releases", FirstVersion: "v1.0.0"}
	if _, err := Read(context.Background(), f.repo, moved, published, "HEAD"); err == nil || !strings.Contains(err.Error(), "already points to another commit") {
		t.Fatal(err)
	}
}

// A release request's plan lists the notes rules its notes break, without failing.
func TestReadReportsNotesFindings(t *testing.T) {
	f := newFixture(t)
	f.git("tag", "v1.0.0")
	f.write("a.md", "a")
	f.git("add", "-A")
	f.git("commit", "-q", "-m", "Add a (#7)")
	f.write("b.md", "b")
	direct := f.commit("Tidy b")
	notes := "## ✨ New Features\n\nA. (#7)\n\n## Pull Requests\n\n- Add a in #7\n- Tidy b in https://github.com/o/r/commit/" + direct[:7] + "\n\n**Full Changelog**: https://github.com/o/r/compare/v1.0.0...v1.1.0\n"
	f.write("releases/v1.1.0.md", notes)
	head := f.commit("Release v1.1.0")
	p, err := f.plan(direct, head)
	if err != nil || p.Tag != "v1.1.0" || p.File != "releases/v1.1.0.md" || len(p.Findings) != 0 {
		t.Fatal(p, err)
	}

	f.write("releases/v1.1.0.md", strings.Replace(notes, "- Add a in #7\n", "", 1)+"\n## Empty\n")
	head = f.commit("Break the notes")
	p, err = f.plan(direct, head)
	if err != nil || p.Tag != "v1.1.0" {
		t.Fatal(p, err)
	}
	var got []string
	for _, finding := range p.Findings {
		got = append(got, finding.Rule)
	}
	want := []string{"no-empty-heading", "require-pull-requests-last", "require-pull-requests-last", "list-every-change"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v: %v", got, want, p.Findings)
	}
	p, err = Read(context.Background(), f.repo, Options{NotesDir: "releases", FirstVersion: "v1.0.0", ExcludeRules: []string{"no-empty-heading", "require-pull-requests-last", "list-every-change", "require-closing-link"}}, direct, head)
	if err != nil || len(p.Findings) != 0 {
		t.Fatal(p.Findings, err)
	}
}

// Main merged into a release pull request's branch brings its pull requests onto the release
// branch; the commits inside a merged pull request stay off it.
func TestChangesFollowMainMergedIntoTheReleaseBranch(t *testing.T) {
	f := newFixture(t)
	f.git("tag", "v1.0.0")
	f.git("checkout", "-q", "-b", "feature")
	f.write("a.md", "a")
	f.commit("Work in progress")
	f.git("checkout", "-q", "main")
	f.git("merge", "-q", "--no-ff", "feature", "-m", "Merge pull request #7 from o/feature", "-m", "Add a")
	f.git("checkout", "-q", "-b", "release")
	f.write("releases/v1.1.0.md", "Notes")
	f.commit("Release v1.1.0")
	f.git("checkout", "-q", "main")
	f.write("b.md", "b")
	f.commit("Add b (#8)")
	f.git("checkout", "-q", "release")
	f.git("merge", "-q", "--no-ff", "main", "-m", "Merge branch 'main' into release")

	inv, err := Take(context.Background(), f.repo, opts, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	inv.AddEntries("o/r")
	var entries []string
	for _, c := range inv.Commits {
		if c.Entry != "" {
			entries = append(entries, c.Entry)
		}
	}
	if !reflect.DeepEqual(entries, []string{"- Add a in #7", "- Add b in #8"}) || !reflect.DeepEqual(inv.PullRequests, []int{7, 8}) {
		t.Fatalf("%v %+v", entries, inv.Commits)
	}
	if inv.Closing != "**Full Changelog**: https://github.com/o/r/compare/v1.0.0...<version>" {
		t.Fatal(inv.Closing)
	}
}

func TestAddEntries(t *testing.T) {
	inv := Inventory{Previous: "v1.0.0", Commits: []Commit{
		{SHA: "1d7ee9b0000000000000000000000000000000000", Subject: "feat: add runes rules", Title: "feat: add runes rules"},
		{SHA: "5ba22db0000000000000000000000000000000000", Subject: "Merge pull request #7 from example/svelte", Title: "feat(svelte): add a Svelte group", PullRequest: 7, OnBranch: true, AuthorHandle: "octocat", merge: true},
		{SHA: "6c33d1e0000000000000000000000000000000000", Subject: "fix!: reject empty rule IDs (#9)", Title: "fix!: reject empty rule IDs", PullRequest: 9, OnBranch: true},
		{SHA: "9c01ab30000000000000000000000000000000000", Subject: "Fix a typo", Title: "Fix a typo", OnBranch: true},
		{SHA: "4e5f6a70000000000000000000000000000000000", Subject: "Merge branch 'main' into feature", Title: "Merge branch 'main' into feature", OnBranch: true, merge: true},
	}}
	inv.AddEntries("fabricahq/example")
	var entries []string
	for _, c := range inv.Commits {
		entries = append(entries, c.Entry)
	}
	want := []string{"", "- feat(svelte): add a Svelte group by @octocat in #7", "- fix!: reject empty rule IDs in #9", "- Fix a typo in https://github.com/fabricahq/example/commit/9c01ab3", ""}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("got %q", entries)
	}

	first := Inventory{Candidates: map[string]string{"first": "v1.0.0"}, NewContributors: []NewContributor{{Handle: "octocat", PullRequest: 7}}}
	first.AddEntries("fabricahq/example")
	if first.Closing != "This is the first release. Browse the source at [v1.0.0](https://github.com/fabricahq/example/tree/v1.0.0)." ||
		first.NewContributors[0].Entry != "- @octocat made their first contribution in #7" {
		t.Fatalf("%+v", first)
	}
}
