package notes

import (
	"os"
	"slices"
	"strings"
	"testing"
)

var release = Release{Version: "v1.1.0", Previous: "v1.0.0", Changes: []Change{{PullRequest: 7}, {SHA: "abcdef0123456789abcdef0123456789abcdef01"}}}

const valid = `## ✨ New Features

### Log in from the command line

You can now log in without a browser. (#7)

## Pull Requests

### ✨ Features

- feat: add login command, fixing #3 by @octocat in #7

### 🧹 Chores

- Tidy the README in https://github.com/octo/example/commit/abcdef0

## New Contributors

- @octocat made their first contribution in #7

**Full Changelog**: https://github.com/octo/example/compare/v1.0.0...v1.1.0
`

func rules(findings []Finding) []string {
	var ids []string
	for _, f := range findings {
		ids = append(ids, f.Rule)
	}
	return ids
}

func TestValidNotesPass(t *testing.T) {
	if f := Check(valid, release, nil); len(f) != 0 {
		t.Fatal(f)
	}
}

// Only entries under ## Pull Requests list a change: a (#7) in the prose doesn't.
// Every top-level bullet under ## Pull Requests must link a change; nested bullets and fenced
// code are neither entries nor findings.
// A pull request merged more than once is one change: listed once, and reported once if
// it's missing.
// With the repository known, a closing link to another repository is wrong.
// Headings inside fenced code are not headings.
func TestFencedCodeIsNotAHeading(t *testing.T) {
	notes := strings.Replace(valid, "You can now log in without a browser. (#7)", "You can now log in:\n\n```sh\n## "+strings.Repeat("x", 100)+"\n## Pull Requests\n```", 1)
	if f := Check(notes, release, nil); len(f) != 0 {
		t.Fatal(f)
	}
	// Tilde fences too, and a fence closes only with at least as many of its own character.
	notes = strings.Replace(valid, "You can now log in without a browser. (#7)", "You can now log in:\n\n~~~~md\n## Pull Requests\n```\n~~~\n- example in #99\n~~~~", 1)
	if f := Check(notes, release, nil); len(f) != 0 {
		t.Fatal(f)
	}
}

func TestFirstReleaseClosingLine(t *testing.T) {
	first := strings.Replace(valid, "**Full Changelog**: https://github.com/octo/example/compare/v1.0.0...v1.1.0", Closing("octo/example", "", "v1.0.0"), 1)
	if f := Check(first, Release{Version: "v1.0.0", Changes: release.Changes}, nil); len(f) != 0 {
		t.Fatal(f)
	}
	if got := Closing("octo/example", "v1.0.0", "v1.1.0"); !strings.Contains(valid, got+"\n") {
		t.Fatalf("closing %q", got)
	}
}

func TestRulesTurnedOffAreSkipped(t *testing.T) {
	notes := strings.Replace(valid, "### Log in from the command line", "### "+strings.Repeat("x", MaxHeadingLength+1), 1)
	if f := Check(notes, release, []string{"no-long-heading"}); len(f) != 0 {
		t.Fatal(f)
	}
}

// The v0.2.0 notes briefly held a stale copy of an earlier draft pasted into the middle:
// repeated sections, and sentence fragments starting with "Pull Requests" as headings.
func TestCatchesSplicedNotes(t *testing.T) {
	data, err := os.ReadFile("testdata/spliced-v0.2.0.md")
	if err != nil {
		t.Fatal(err)
	}
	got := rules(Check(string(data), v020, nil))
	for _, rule := range []string{"no-duplicate-heading", "no-long-heading", "require-pull-requests-last"} {
		if !slices.Contains(got, rule) {
			t.Errorf("want %s, got %v", rule, got)
		}
	}
	if strings.Count(string(data), "\n") < 100 || !strings.Contains(string(data), "## Pull Requests`, which replaces") {
		t.Fatal("testdata is not the spliced file")
	}
}

var v020 = Release{Version: "v0.2.0", Previous: "v0.1.0", Changes: []Change{{PullRequest: 4}, {PullRequest: 5}, {PullRequest: 6}, {PullRequest: 7}, {PullRequest: 8}, {PullRequest: 9}, {PullRequest: 10}}}

// The published v0.2.0 notes follow every rule.
func TestPublishedNotesPass(t *testing.T) {
	data, err := os.ReadFile("testdata/v0.2.0.md")
	if err != nil {
		t.Fatal(err)
	}
	if f := Check(string(data), v020, nil); len(f) != 0 {
		t.Fatal(f)
	}
}

// Near misses are named as wrong, not missing: a Pull Requests heading in another case, and a
// closing line that's been reworded.
