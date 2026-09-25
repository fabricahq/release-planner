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

func TestEachRule(t *testing.T) {
	cases := map[string]struct {
		notes string
		rule  string
		line  int
	}{
		"empty ##":             {"## ✨ New Features\n\n## ⬆️ Improvements\n\nBetter. (#7)\n\n" + valid[strings.Index(valid, "## Pull Requests"):], "no-empty-heading", 1},
		"empty ###":            {strings.Replace(valid, "### 🧹 Chores\n\n- Tidy the README in https://github.com/octo/example/commit/abcdef0\n", "### 🧹 Chores\n", 1) + "", "no-empty-heading", 13},
		"### empty before ###": {strings.Replace(valid, "### Log in from the command line\n\nYou can now log in without a browser. (#7)\n", "### Log in\n\n### Another\n\nText.\n", 1), "no-empty-heading", 3},
		"duplicate ##":         {"## ✨ New Features\n\nOne.\n\n## ✨ New Features\n\nTwo.\n\n" + valid[strings.Index(valid, "## Pull Requests"):], "no-duplicate-heading", 5},
		"duplicate ###":        {strings.Replace(valid, "### 🧹 Chores", "### ✨ Features", 1), "no-duplicate-heading", 13},
		"long ###":             {strings.Replace(valid, "### Log in from the command line", "### "+strings.Repeat("x", MaxHeadingLength+1), 1), "no-long-heading", 3},
		"no Pull Requests":     {strings.Replace(valid, "## Pull Requests", "## Changes", 1), "require-pull-requests-last", 0},
		"section after":        {valid[:strings.Index(valid, "**Full")] + "## Thanks\n\nEveryone.\n\n**Full Changelog**: https://github.com/octo/example/compare/v1.0.0...v1.1.0\n", "require-pull-requests-last", 21},
		"text after closing":   {valid + "\nMore.\n", "require-pull-requests-last", 21},
		"contributors first":   {strings.Replace(valid, "## Pull Requests", "## New Contributors\n\n- @a made their first contribution in #7\n\n## Pull Requests", 1), "require-pull-requests-last", 7},
		"missing PR":           {strings.Replace(valid, "- feat: add login command, fixing #3 by @octocat in #7\n", "", 1), "list-every-change", 0},
		"missing commit":       {strings.Replace(valid, "- Tidy the README in https://github.com/octo/example/commit/abcdef0\n", "", 1), "list-every-change", 0},
		"PR twice":             {strings.Replace(valid, "### 🧹 Chores\n", "### 🧹 Chores\n\n- Login again in https://github.com/octo/example/pull/7\n", 1), "list-every-change", 15},
		"PR not in release":    {strings.Replace(valid, "### 🧹 Chores\n", "### 🧹 Chores\n\n- Old work in #3\n", 1), "list-every-change", 15},
		"commit not listed":    {strings.Replace(valid, "commit/abcdef0", "commit/1234567", 1), "list-every-change", 0},
		"no closing line":      {strings.Replace(valid, "**Full Changelog**: https://github.com/octo/example/compare/v1.0.0...v1.1.0\n", "", 1), "require-closing-link", 0},
		"two closing":          {valid + "\n**Full Changelog**: https://github.com/octo/example/compare/v1.0.0...v1.1.0\n", "require-closing-link", 23},
		"wrong closing":        {strings.Replace(valid, "v1.0.0...v1.1.0", "v1.0.0...v1.2.0", 1), "require-closing-link", 21},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			findings := Check(tc.notes, release, nil)
			i := slices.IndexFunc(findings, func(f Finding) bool { return f.Rule == tc.rule })
			if i < 0 {
				t.Fatalf("want %s, got %v", tc.rule, findings)
			}
			if findings[i].Line != tc.line {
				t.Fatalf("want line %d, got %v", tc.line, findings[i])
			}
		})
	}
}

// Only entries under ## Pull Requests list a change: a (#7) in the prose doesn't.
func TestChangesCountOnlyUnderPullRequests(t *testing.T) {
	notes := strings.Replace(valid, "- feat: add login command, fixing #3 by @octocat in #7\n", "- feat: add login command\n", 1)
	if got := rules(Check(notes, release, nil)); !slices.Equal(got, []string{"list-every-change"}) {
		t.Fatal(got)
	}
}

// Headings inside fenced code are not headings.
func TestFencedCodeIsNotAHeading(t *testing.T) {
	notes := strings.Replace(valid, "You can now log in without a browser. (#7)", "You can now log in:\n\n```sh\n## "+strings.Repeat("x", 100)+"\n## Pull Requests\n```", 1)
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

func TestExcludedRulesAreSkipped(t *testing.T) {
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

func TestRules(t *testing.T) {
	want := []string{"no-empty-heading", "no-duplicate-heading", "no-long-heading", "require-pull-requests-last", "list-every-change", "require-closing-link"}
	var ids []string
	for _, r := range Rules {
		ids = append(ids, r.ID)
		if r.Description == "" || !Known(r.ID) {
			t.Errorf("%s: describe it", r.ID)
		}
	}
	if !slices.Equal(ids, want) || Known("no-todo-opening") {
		t.Fatal(ids)
	}
}
