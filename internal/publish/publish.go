// Package publish tags the approved commit and publishes the approved notes as a GitHub release.
//
// Every remote fact is verified before and after writing, and a retry after a successful
// publication makes no writes. Callers must serialize publication across versions.
package publish

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/fabricahq/release-planner/internal/plan"
	"github.com/fabricahq/release-planner/internal/semver"
)

var commitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Result reports what publication did.
type Result struct {
	URL string
	// AlreadyPublished is true when a matching release existed and nothing was written.
	AlreadyPublished bool
}

// Publish verifies the plan against GitHub, then creates the tag and release.
func Publish(ctx context.Context, gh *GitHub, p plan.Plan, commit string) (Result, error) {
	if p.Tag == "" {
		return Result{}, fmt.Errorf("the plan requests no release")
	}
	if !commitSHA.MatchString(p.Commit) || p.Commit != commit {
		return Result{}, fmt.Errorf("the plan's commit %q does not match the approved commit %q", p.Commit, commit)
	}
	if strings.TrimSpace(p.Notes) == "" {
		return Result{}, fmt.Errorf("the plan has no release notes")
	}

	// Refuse to act on a stale plan: the tags seen at planning must be the tags that exist now.
	remote, err := gh.VersionTags(ctx)
	if err != nil {
		return Result{}, err
	}
	var current []string
	for _, t := range remote {
		if _, ok := semver.Parse(t); ok && t != p.Tag {
			current = append(current, t)
		}
	}
	planned := slices.Clone(p.Tags)
	slices.Sort(current)
	slices.Sort(planned)
	if !slices.Equal(current, planned) {
		return Result{}, fmt.Errorf("version tags changed since planning; plan again")
	}

	if p.Previous != "" {
		previous, err := gh.ReleaseByTag(ctx, p.Previous)
		if err != nil {
			return Result{}, err
		}
		if previous == nil || previous.Draft {
			return Result{}, fmt.Errorf("publish %s first", p.Previous)
		}
	}

	existing, err := gh.TagCommit(ctx, p.Tag)
	if err != nil {
		return Result{}, err
	}
	if existing != "" && existing != commit {
		return Result{}, fmt.Errorf("%s already points to %s, not the approved %s", p.Tag, existing, commit)
	}

	result := Result{}
	release, err := gh.ReleaseByTag(ctx, p.Tag)
	if err != nil {
		return Result{}, err
	}
	switch {
	case release != nil && (release.Name != p.Tag || normalize(release.Body) != normalize(p.Notes) || release.Prerelease != p.Prerelease):
		return Result{}, fmt.Errorf("an existing %s release differs from the approved notes; resolve it by hand", p.Tag)
	case release != nil && !release.Draft:
		result.AlreadyPublished = true
	case release != nil:
		if release, err = gh.PublishDraft(ctx, release.ID, !p.Prerelease); err != nil {
			return Result{}, err
		}
	default:
		if release, err = gh.CreateRelease(ctx, p.Tag, commit, p.Notes, p.Prerelease); err != nil {
			return Result{}, err
		}
	}
	result.URL = release.HTMLURL

	tagged, err := gh.TagCommit(ctx, p.Tag)
	if err != nil {
		return Result{}, err
	}
	if tagged != commit {
		return Result{}, fmt.Errorf("%s does not point to the approved commit after publication", p.Tag)
	}
	return result, nil
}

// GitHub may normalize line endings and trailing whitespace in a release body.
func normalize(s string) string {
	return strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), " \t\n")
}
