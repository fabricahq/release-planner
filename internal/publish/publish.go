// Package publish tags the approved commit and publishes the approved notes as a GitHub release.
//
// Every remote fact is verified before and after writing, and a retry after a successful
// publication makes no writes. Callers must serialize publication across versions.
package publish

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/fabricahq/release-planner/internal/plan"
	"github.com/fabricahq/release-planner/internal/semver"
)

var (
	commitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
	assetName = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._+-]*$`)
)

// Result reports what publication did.
type Result struct {
	URL string
	// AlreadyPublished is true when a matching release existed and nothing was written.
	AlreadyPublished bool
}

// File is a release asset read into memory, so uploads can't pick up later edits.
type File struct {
	Name   string
	Data   []byte
	Digest string // sha256:<hex>
}

// ReadAssets reads every file in dir as a release asset.
func ReadAssets(dir string) ([]File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []File
	for _, e := range entries {
		if !e.Type().IsRegular() {
			return nil, fmt.Errorf("%s: release assets must be regular files", filepath.Join(dir, e.Name()))
		}
		if !assetName.MatchString(e.Name()) {
			return nil, fmt.Errorf("%s: asset names may use letters, digits, and . _ + - only", e.Name())
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		files = append(files, File{Name: e.Name(), Data: data, Digest: "sha256:" + hex.EncodeToString(sum[:])})
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%s has no files to attach", dir)
	}
	return files, nil
}

// Publish verifies the plan against GitHub, then creates the tag and release. With assets,
// it stages them on a draft, verifies GitHub's stored checksums, and publishes last, because
// immutable releases freeze their assets at publication.
//
// Merging the release pull request is the approval, so the commit must be the result of
// merging a pull request into branch. A direct push of release notes publishes nothing.
func Publish(ctx context.Context, gh *GitHub, p plan.Plan, commit, branch string, assets []File) (Result, error) {
	if p.Tag == "" {
		return Result{}, fmt.Errorf("the plan requests no release")
	}
	if !commitSHA.MatchString(p.Commit) || p.Commit != commit {
		return Result{}, fmt.Errorf("the plan's commit %q does not match the approved commit %q", p.Commit, commit)
	}
	if strings.TrimSpace(p.Notes) == "" {
		return Result{}, fmt.Errorf("the plan has no release notes")
	}
	pr, err := gh.MergedPullRequest(ctx, commit, branch)
	if err != nil {
		return Result{}, err
	}
	if pr == 0 {
		return Result{}, fmt.Errorf("%s is not the merge of a pull request into %s; a release is approved by merging its pull request, so a direct push publishes nothing", commit, branch)
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

	release, err := gh.ReleaseByTag(ctx, p.Tag)
	if err != nil {
		return Result{}, err
	}
	if release != nil && (release.Name != p.Tag || normalize(release.Body) != normalize(p.Notes) || release.Prerelease != p.Prerelease) {
		return Result{}, fmt.Errorf("an existing %s release differs from the approved notes; resolve it by hand", p.Tag)
	}
	if release != nil && release.Draft && existing == "" && release.TargetCommitish != commit {
		// Publishing this draft would create the tag on its own target, not the approved commit.
		return Result{}, fmt.Errorf("an existing %s draft targets %s, not the approved %s; delete the draft and retry", p.Tag, release.TargetCommitish, commit)
	}
	if release != nil && !release.Draft {
		// A published release is immutable; accept it only if it is exactly what was approved.
		if err := verifyAssets(ctx, gh, release, assets); err != nil {
			return Result{}, fmt.Errorf("%s is already published, but %v", p.Tag, err)
		}
		return Result{URL: release.HTMLURL, AlreadyPublished: true}, verifyTag(ctx, gh, p.Tag, commit)
	}

	switch {
	case release == nil && len(assets) == 0:
		release, err = gh.CreateRelease(ctx, p.Tag, commit, p.Notes, p.Prerelease, false)
	case release == nil:
		release, err = gh.CreateRelease(ctx, p.Tag, commit, p.Notes, p.Prerelease, true)
	}
	if err != nil {
		return Result{}, err
	}
	if release.Draft {
		if err := stageAssets(ctx, gh, release, assets); err != nil {
			return Result{}, err
		}
		fresh, err := gh.Release(ctx, release.ID)
		if err != nil {
			return Result{}, err
		}
		if err := verifyAssets(ctx, gh, fresh, assets); err != nil {
			return Result{}, fmt.Errorf("staged %s draft: %v", p.Tag, err)
		}
		if release, err = gh.PublishDraft(ctx, release.ID, commit, !p.Prerelease); err != nil {
			return Result{}, err
		}
	}
	return Result{URL: release.HTMLURL}, verifyTag(ctx, gh, p.Tag, commit)
}

// stageAssets uploads assets a draft lacks. A retry after an interrupted upload skips
// assets that already match; an asset with the same name but different content stops it.
func stageAssets(ctx context.Context, gh *GitHub, draft *Release, assets []File) error {
	for _, a := range assets {
		i := slices.IndexFunc(draft.Assets, func(r Asset) bool { return r.Name == a.Name })
		if i >= 0 {
			digest, err := gh.AssetDigest(ctx, draft.Assets[i])
			if err != nil {
				return err
			}
			if digest != a.Digest {
				return fmt.Errorf("the %s draft already has a different %s; delete that asset from the draft and retry", draft.TagName, a.Name)
			}
			continue
		}
		if err := gh.UploadAsset(ctx, draft, a.Name, a.Data); err != nil {
			return err
		}
	}
	return nil
}

// verifyAssets checks that a release holds exactly the expected assets, by GitHub's stored checksums.
func verifyAssets(ctx context.Context, gh *GitHub, release *Release, assets []File) error {
	if len(release.Assets) != len(assets) {
		return fmt.Errorf("it has %d assets, not the %d approved", len(release.Assets), len(assets))
	}
	for _, a := range assets {
		i := slices.IndexFunc(release.Assets, func(r Asset) bool { return r.Name == a.Name })
		if i < 0 {
			return fmt.Errorf("its assets lack %s", a.Name)
		}
		if release.Assets[i].Size != int64(len(a.Data)) {
			return fmt.Errorf("its %s has the wrong size", a.Name)
		}
		digest, err := gh.AssetDigest(ctx, release.Assets[i])
		if err != nil {
			return err
		}
		if digest != a.Digest {
			return fmt.Errorf("its %s has checksum %s, not %s", a.Name, digest, a.Digest)
		}
	}
	return nil
}

func verifyTag(ctx context.Context, gh *GitHub, tag, commit string) error {
	tagged, err := gh.TagCommit(ctx, tag)
	if err != nil {
		return err
	}
	if tagged != commit {
		return fmt.Errorf("%s does not point to the approved commit after publication", tag)
	}
	return nil
}

// GitHub may normalize line endings and trailing whitespace in a release body.
func normalize(s string) string {
	return strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), " \t\n")
}
