package generate

import (
	"strings"
	"testing"

	"github.com/fabricahq/release-planner/internal/config"
)

func guideFor(t *testing.T, style, mode string) string {
	t.Helper()
	yaml := "schema-version: 1\nversion: v0.2.0\nfirst-version: v1.0.0\nnotes-dir: docs/releases\nbranch: trunk\nvalidate:\n  run: make test\n"
	if mode != "" {
		yaml += "release-notes-style:\n  file: .release-planner/release-notes-style.md\n  mode: " + mode + "\n"
	}
	c, err := config.Parse([]byte(yaml), style)
	if err != nil {
		t.Fatal(err)
	}
	return Guide(c)
}

func TestGuideRendersTheConfiguredSettings(t *testing.T) {
	guide := guideFor(t, "", "")
	for _, want := range []string{
		"# Prepare a release (Release Planner v0.2.0)",
		"curl -fsSL https://raw.githubusercontent.com/fabricahq/release-planner/v0.2.0/install.sh | sh -s -- --version v0.2.0\n",
		"Read `.release-planner/policy.md`.",
		"release-planner inventory --head origin/trunk",
		"With no previous release, use `v1.0.0`.",
		"It creates `docs/releases/<version>.md` with a TODO opening line",
		"- Replace the TODO line, and don't leave empty headings. `plan` rejects both.\n",
		"### Release notes style\n\n" + strings.TrimSpace(DefaultStyle()) + "\n\n## 6. Validate",
	} {
		if !strings.Contains(guide, want) {
			t.Errorf("guide lacks %q", want)
		}
	}
}

func TestGuideAppendsOrReplacesTheReleaseNotesStyle(t *testing.T) {
	custom := "- Use only `## Added`, `## Changed`, and `## Fixed`."

	appended := guideFor(t, custom, "append")
	if !strings.Contains(appended, "### Release notes style\n\n"+strings.TrimSpace(DefaultStyle())+"\n\nThis repository adds:\n\n"+custom+"\n\n## 6.") {
		t.Errorf("append:\n%s", appended)
	}

	replaced := guideFor(t, custom, "replace")
	if !strings.Contains(replaced, "### Release notes style\n\n"+custom+"\n\n## 6.") || strings.Contains(replaced, "New Features") {
		t.Errorf("replace:\n%s", replaced)
	}
	// The fixed rules around the style apply whatever the style says.
	if !strings.Contains(replaced, "Keep `## What's Changed` and the closing line") {
		t.Error("replace dropped the fixed rules")
	}
}
