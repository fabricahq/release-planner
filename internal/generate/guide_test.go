package generate

import (
	"strings"
	"testing"

	"github.com/fabricahq/release-planner/internal/config"
)

func TestGuide(t *testing.T) {
	c, err := config.Parse([]byte("version: v0.2.0\nfirst-version: v1.0.0\nnotes-dir: docs/releases\nbranch: trunk\nvalidate:\n  run: make test\n"))
	if err != nil {
		t.Fatal(err)
	}
	guide := Guide(c)
	for _, want := range []string{
		"# Prepare a release (Release Planner v0.2.0)",
		"go run github.com/fabricahq/release-planner/cmd/release-planner@v0.2.0\n",
		"Read `docs/releases/README.md`.",
		"release-planner inventory --head origin/trunk",
		"With no previous release, use `v1.0.0`.",
		"with this repository's headings from `release-planner.yml`",
		"  - `## ✨ New Features`: what users can do that they couldn't before.\n",
		"  - `## ⛓️‍💥 Breaking Changes`: who is affected, the old and new behavior, and exact migration steps. Also mention these in the opening sentences.\n",
		"or for a first release, a link to the tagged source",
	} {
		if !strings.Contains(guide, want) {
			t.Errorf("guide lacks %q", want)
		}
	}
	if strings.Contains(guide, "standard headings") {
		t.Error("guide still says standard headings")
	}

	c.Notes.Sections = []config.Section{{Heading: "🔒 Security", Include: "vulnerabilities fixed, with their CVE IDs.", SummarizeFirst: true}}
	guide = Guide(c)
	if !strings.Contains(guide, "  - `## 🔒 Security`: vulnerabilities fixed, with their CVE IDs. Also mention these in the opening sentences.\n") || strings.Contains(guide, "New Features") {
		t.Errorf("custom sections:\n%s", guide)
	}
}
