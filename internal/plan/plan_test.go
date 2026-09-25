package plan

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
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

// branch commits files on a new branch off main, like a release pull request's, and returns
// its head. An empty body deletes the file. Main is checked out again afterwards.
func (f *fixture) branch(name string, files map[string]string) string {
	f.t.Helper()
	f.git("checkout", "-q", "-b", name, "main")
	for file, body := range files {
		if body == "" {
			f.git("rm", "-q", file)
		} else {
			f.write(file, body)
		}
	}
	head := f.commit("Change " + name)
	f.git("checkout", "-q", "main")
	return head
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.existing != "" {
				f.git("tag", tc.existing)
				f.write("code.txt", "after "+tc.existing)
				f.commit("Change code")
			}
			release := f.git("rev-parse", "main")
			head := f.branch("release", map[string]string{"releases/" + tc.tag + ".md": tc.notes})
			p, err := f.plan("main", head)
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("got %v, want %q", err, tc.want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if p.Tag != tc.tag || p.Notes != tc.notes || p.Commit != release || p.Head != head || p.Previous != tc.existing || p.Prerelease != strings.Contains(tc.tag, "-") || len(p.Edits) != 0 {
				t.Fatalf("unexpected plan: %+v", p)
			}
			// An interrupted publication tagged the release commit; retrying plans the same release.
			f.git("tag", tc.tag, release)
			retry, err := f.plan("main", head)
			if err != nil || !reflect.DeepEqual(retry, p) {
				t.Fatalf("retry at the tagged commit: %+v %v", retry, err)
			}
		})
	}
}

// The release commit is the newest commit the pull request shares with the release branch,
// before and after the merge, however it merges.
func TestReleaseCommitIsTheMergeBase(t *testing.T) {
	f := newFixture(t)
	f.write("releases/v1.0.0.md", "First\n")
	f.git("checkout", "-q", "-b", "release")
	f.git("add", "-A")
	f.git("commit", "-q", "-m", "Request v1.0.0")
	f.write("releases/v1.0.0.md", "First, revised\n")
	head := f.commit("Revise v1.0.0")
	f.git("checkout", "-q", "main")
	f.write("later.txt", "after the pull request opened")
	later := f.commit("Later change")

	if p, err := f.plan("main", head); err != nil || p.Commit != f.initial || p.Notes != "First, revised\n" {
		t.Fatalf("pull request: %+v %v", p, err)
	}
	merged := map[string]func() string{
		"merge commit": func() string {
			f.git("merge", "-q", "--no-ff", "release", "-m", "Merge pull request #2 from o/release")
			return f.git("rev-parse", "HEAD")
		},
		"squash": func() string {
			f.git("merge", "-q", "--squash", "release")
			return f.commit("Release v1.0.0 (#2)")
		},
		"rebase": func() string {
			f.git("cherry-pick", "--allow-empty", f.initial+".."+head)
			return f.git("rev-parse", "HEAD")
		},
	}
	for name, merge := range merged {
		t.Run(name, func(t *testing.T) {
			f.git("reset", "-q", "--hard", later)
			commit := merge()
			if p, err := f.plan(commit+"^1", head); err != nil || p.Commit != f.initial || p.Tag != "v1.0.0" {
				t.Fatalf("after merge: %+v %v", p, err)
			}
		})
	}

	// Merging the release branch into the pull request moves the release commit to include it.
	f.git("reset", "-q", "--hard", later)
	f.git("checkout", "-q", "release")
	f.git("merge", "-q", "--no-ff", "main", "-m", "Merge branch 'main' into release")
	updated := f.git("rev-parse", "HEAD")
	f.git("checkout", "-q", "main")
	if p, err := f.plan("main", updated); err != nil || p.Commit != later {
		t.Fatalf("after merging main: %+v %v", p, err)
	}
}

// A release pull request changes only notes files. Any pull request that doesn't request a
// release or edit notes may change anything.
func TestReleasePullRequestsChangeOnlyNotes(t *testing.T) {
	f := newFixture(t)
	for name, files := range map[string]map[string]string{
		"code":   {"releases/v1.0.0.md": "Notes", "main.go": "package main"},
		"index":  {"releases/v1.0.0.md": "Notes", "releases/index.md": "# Releases"},
		"config": {"releases/v1.0.0.md": "Notes", ".release-planner/config.yml": "schema-version: 1"},
	} {
		head := f.branch(name, files)
		if _, err := f.plan("main", head); err == nil || !strings.Contains(err.Error(), "may change only release notes files") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if p, err := f.plan("main", f.branch("ordinary", map[string]string{"main.go": "package main"})); err != nil || !p.Empty() || p.Head == "" {
		t.Fatal(p, err)
	}
	head := f.branch("two", map[string]string{"releases/v1.0.0.md": "One", "releases/v1.1.0.md": "Two"})
	if _, err := f.plan("main", head); err == nil || !strings.Contains(err.Error(), "one release per pull request") {
		t.Fatal(err)
	}
}

// published sets up a tagged v1.0.0 whose notes merged after the tagged commit, as they do.
func published(t *testing.T) *fixture {
	f := newFixture(t)
	f.git("tag", "v1.0.0")
	f.write("releases/v1.0.0.md", "Published notes\n")
	f.commit("Release v1.0.0 (#1)")
	return f
}

func TestEditsTheNotesOfTaggedVersions(t *testing.T) {
	f := published(t)
	head := f.branch("edit", map[string]string{"releases/v1.0.0.md": "Corrected notes\n"})
	p, err := f.plan("main", head)
	if err != nil || p.Tag != "" || p.Commit != "" || len(p.Edits) != 1 {
		t.Fatalf("%+v %v", p, err)
	}
	if e := p.Edits[0]; e.Tag != "v1.0.0" || e.File != "releases/v1.0.0.md" || e.Notes != "Corrected notes\n" || len(e.Findings) == 0 {
		t.Fatalf("%+v", e)
	}

	// One pull request may request a release and edit earlier notes.
	head = f.branch("both", map[string]string{"releases/v1.0.0.md": "Corrected notes\n", "releases/v1.1.0.md": "Second\n"})
	if p, err := f.plan("main", head); err != nil || p.Tag != "v1.1.0" || p.Previous != "v1.0.0" || len(p.Edits) != 1 {
		t.Fatalf("%+v %v", p, err)
	}

	if _, err := f.plan("main", f.branch("emptied", map[string]string{"releases/v1.0.0.md": "\n"})); err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Fatal(err)
	}
}

func TestTaggedNotesCantBeDeleted(t *testing.T) {
	f := published(t)
	if _, err := f.plan("main", f.branch("delete", map[string]string{"releases/v1.0.0.md": ""})); err == nil || !strings.Contains(err.Error(), "can't be deleted") {
		t.Fatal(err)
	}
	// Deleting a request that never published withdraws it.
	f.write("releases/v1.1.0.md", "Never published\n")
	f.commit("Release v1.1.0 (#2)")
	if p, err := f.plan("main", f.branch("withdraw", map[string]string{"releases/v1.1.0.md": ""})); err != nil || !p.Empty() {
		t.Fatal(p, err)
	}
}

// Moving tagged notes, unchanged, to a new notes directory changes nothing, alongside the
// config change that moves it. Rewriting them on the way is not a move.
func TestMovedNotesAreNotChanges(t *testing.T) {
	f := published(t)
	moved := Options{NotesDir: "_releases", FirstVersion: "v1.0.0"}
	f.git("checkout", "-q", "-b", "move")
	f.git("mv", "releases", "_releases")
	f.write(".release-planner/config.yml", "release-notes-dir: _releases\n")
	head := f.commit("Move the notes")
	f.git("checkout", "-q", "main")
	if p, err := Read(context.Background(), f.repo, moved, "main", head); err != nil || !p.Empty() {
		t.Fatal(p, err)
	}

	f.git("checkout", "-q", "move")
	f.write("_releases/v1.0.0.md", "Rewritten while moving\n")
	head = f.commit("Rewrite")
	f.git("checkout", "-q", "main")
	if _, err := Read(context.Background(), f.repo, moved, "main", head); err == nil || !strings.Contains(err.Error(), "may change only release notes files") {
		t.Fatal(err)
	}
}

func TestIgnoresMalformedVersionTags(t *testing.T) {
	f := newFixture(t)
	f.git("tag", "v-preview")
	head := f.branch("release", map[string]string{"releases/v1.0.0.md": "Approved notes"})
	if p, err := f.plan("main", head); err != nil || len(p.Tags) != 0 || p.Tag != "v1.0.0" {
		t.Fatal(p, err)
	}
}

// A request approved but never published must be published, corrected, or withdrawn
// before a later one. Correcting it moves its release commit to the newest shared commit.
func TestPendingRequestCannotBeSkipped(t *testing.T) {
	f := newFixture(t)
	f.git("tag", "v1.0.0")
	f.write("releases/v1.1.0.md", "Pending release")
	pending := f.commit("Pending request (#2)")
	if _, err := f.plan("main", f.branch("later", map[string]string{"releases/v1.2.0.md": "Later release"})); err == nil || !strings.Contains(err.Error(), "untagged release request") {
		t.Fatal(err)
	}
	p, err := f.plan("main", f.branch("correct", map[string]string{"releases/v1.1.0.md": "Corrected release"}))
	if err != nil || p.Tag != "v1.1.0" || p.Commit != pending || p.Notes != "Corrected release" {
		t.Fatal(p, err)
	}
}

func TestReadUsesTheConfiguredNotesDirectory(t *testing.T) {
	f := newFixture(t)
	f.write("releases/v9.0.0.md", "Outside the configured directory")
	f.commit("Unrelated file")
	head := f.branch("release", map[string]string{"docs/releases/v1.0.0.md": "Notes"})
	p, err := Read(context.Background(), f.repo, Options{NotesDir: "docs/releases", FirstVersion: "v1.0.0"}, "main", head)
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
	p, err = Read(context.Background(), f.repo, Options{NotesDir: "releases", FirstVersion: "v1.0.0", RulesOff: []string{"no-empty-heading", "require-pull-requests-last", "list-every-change", "require-closing-link"}}, direct, head)
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

// GitHub can title a pull request's merge commit "<title> (#N)" instead of "Merge pull
// request #N …". Either way, the commits inside the pull request aren't listed on their own.
func TestTitledMergeCommitListsOnlyThePullRequest(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "-q", "-b", "feature")
	f.write("b.md", "b")
	f.commit("Work in progress")
	f.git("checkout", "-q", "main")
	f.git("merge", "-q", "--no-ff", "feature", "-m", "Add the b file (#12)")
	inv, err := Take(context.Background(), f.repo, opts, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	var listed []string
	for _, c := range inv.Commits {
		if Listed(c) {
			listed = append(listed, fmt.Sprintf("%s #%d", c.Title, c.PullRequest))
		}
	}
	if !reflect.DeepEqual(listed, []string{"Initial #0", "Add the b file #12"}) {
		t.Fatalf("listed %v", listed)
	}
}

// Only a versioned notes file makes a commit a release request. Other files in the notes
// directory, such as an index, are changes to release.
func TestOnlyReleaseNotesAreNotChanges(t *testing.T) {
	f := newFixture(t)
	f.write("releases/index.md", "# Releases\n")
	f.commit("Add a releases index")
	f.write("releases/v9.9.9.md", "Notes\n")
	f.commit("Release v9.9.9")
	inv, err := Take(context.Background(), f.repo, opts, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	var subjects []string
	for _, c := range inv.Commits {
		subjects = append(subjects, c.Subject)
	}
	if !slices.Contains(subjects, "Add a releases index") || slices.Contains(subjects, "Release v9.9.9") {
		t.Fatalf("%v", subjects)
	}
}

// An empty commit, such as an empty initial commit, changes nothing, so it isn't a change
// to release.
func TestEmptyCommitsAreNotChanges(t *testing.T) {
	f := newFixture(t)
	f.git("commit", "-q", "--allow-empty", "-m", "Empty")
	inv, err := Take(context.Background(), f.repo, opts, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range inv.Commits {
		if c.Subject == "Empty" {
			t.Fatalf("listed the empty commit: %+v", inv.Commits)
		}
	}
	if len(inv.Commits) != 1 {
		t.Fatalf("%+v", inv.Commits)
	}
}
