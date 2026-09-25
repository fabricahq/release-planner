package notes

import (
	"slices"
	"strings"
	"testing"
)

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

func TestChangesCountOnlyUnderPullRequests(t *testing.T) {
	notes := strings.Replace(valid, "- feat: add login command, fixing #3 by @octocat in #7\n", "- feat: add login command\n", 1)
	// The bullet no longer links #7, so it's flagged, and #7 is missing.
	if got := rules(Check(notes, release, nil)); !slices.Equal(got, []string{"list-every-change", "list-every-change"}) {
		t.Fatal(got)
	}
}

func TestPullRequestsEntriesMustLinkAChange(t *testing.T) {
	at := strings.Index(valid, "\n\n## New Contributors")
	todo := valid[:at] + "\n- TODO: review the previous draft" + valid[at:]
	if got := rules(Check(todo, release, nil)); !slices.Equal(got, []string{"list-every-change"}) {
		t.Fatal(got)
	}
	detail := valid[:at] + "\n  - A nested detail, with no link\n\n```md\n- example in #99\n```" + valid[at:]
	if f := Check(detail, release, nil); len(f) != 0 {
		t.Fatal(f)
	}
}

func TestPullRequestMergedTwiceIsOneChange(t *testing.T) {
	twice := release
	twice.Changes = append(append([]Change{}, release.Changes...), release.Changes[0])
	if f := Check(valid, twice, nil); len(f) != 0 {
		t.Fatal(f)
	}
	missing := strings.Replace(valid, "- feat: add login command, fixing #3 by @octocat in #7\n", "", 1)
	got := rules(Check(missing, twice, nil))
	if n := len(slices.DeleteFunc(slices.Clone(got), func(r string) bool { return r != "list-every-change" })); n != 1 {
		t.Fatal(got)
	}
}

func TestClosingLinkNamesThisRepository(t *testing.T) {
	known := release
	known.Repository = "octo/other"
	if got := rules(Check(valid, known, nil)); !slices.Equal(got, []string{"require-closing-link"}) {
		t.Fatal(got)
	}
	known.Repository = "octo/example"
	if f := Check(valid, known, nil); len(f) != 0 {
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

func TestNearMissesAreReportedAsWrong(t *testing.T) {
	lower := strings.Replace(valid, "## Pull Requests", "## Pull requests", 1)
	f := Check(lower, release, nil)
	if i := slices.IndexFunc(f, func(f Finding) bool { return f.Rule == "require-pull-requests-last" }); i < 0 || !strings.Contains(f[i].Message, "name this heading exactly") {
		t.Fatal(f)
	}
	reworded := strings.Replace(valid, "**Full Changelog**: https://github.com/octo/example/compare/v1.0.0...v1.1.0", "**Full Changelog** since v1.0.0", 1)
	f = Check(reworded, release, nil)
	if i := slices.IndexFunc(f, func(f Finding) bool { return f.Rule == "require-closing-link" }); i < 0 || !strings.Contains(f[i].Message, "doesn't match") {
		t.Fatal(f)
	}
}
