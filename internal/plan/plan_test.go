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

func TestReleaseRequests(t *testing.T) {
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
		{"unfinished draft", "v1.0.0", DraftOpening + "\n\n## What's Changed\n\n- x\n", "", "replace the TODO opening"},
		{"empty heading", "v1.0.0", "Summary.\n\n## ✨ New Features\n\n## What's Changed\n\n- x\n", "", `"## ✨ New Features" has no content`},
		{"empty last heading", "v1.0.0", "Summary.\n\n## What's Changed\n\n", "", "has no content"},
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

func TestNoRequest(t *testing.T) {
	f := newFixture(t)
	f.write("practices/rule.md", "Rule")
	p, err := f.plan(f.initial, f.commit("Rule"))
	if err != nil || p.Tag != "" {
		t.Fatal(p, err)
	}
}

func TestNotesOwnership(t *testing.T) {
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

func TestCustomNotesDirectory(t *testing.T) {
	f := newFixture(t)
	f.write("docs/releases/v1.0.0.md", "Notes")
	f.write("releases/v9.0.0.md", "Outside the configured directory")
	f.commit("Release")
	p, err := Read(context.Background(), f.repo, Options{NotesDir: "docs/releases", FirstVersion: "v1.0.0"}, f.initial, "HEAD")
	if err != nil || p.Tag != "v1.0.0" {
		t.Fatal(p, err)
	}
}

func TestInventory(t *testing.T) {
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
		!reflect.DeepEqual(inv.PendingRequests, []string{"releases/v1.1.0.md"}) || len(inv.Commits) != 2 {
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
