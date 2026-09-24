// Package plan validates release requests and describes what a new release would contain.
//
// A release request is one new or edited <notes-dir>/v<semver>.md file between two commits.
// A plan binds that file's version and notes to the head commit, which the release tags.
// Ported from Code Rules' internal/release planner.
package plan

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/fabricahq/release-planner/internal/gitrepo"
	"github.com/fabricahq/release-planner/internal/semver"
)

// Plan is the exact input to publication. An empty Tag means the range requests no release.
type Plan struct {
	// Tags lists the version tags observed at planning, excluding this release,
	// so publication can refuse to act on a stale plan.
	Tags       []string `json:"tags"`
	Tag        string   `json:"tag"`
	Version    string   `json:"version"`
	Commit     string   `json:"commit"`
	Previous   string   `json:"previous"`
	Notes      string   `json:"notes"`
	Prerelease bool     `json:"prerelease"`
}

// DraftOpening starts every notes file that draft creates; the agent must replace it.
const DraftOpening = "TODO: Open with one or two sentences on what this release means for its readers."

// lintNotes rejects notes that still look like an unfinished draft.
func lintNotes(name, notes string) error {
	if strings.Contains(notes, DraftOpening) {
		return fmt.Errorf("%s: replace the TODO opening line with the release summary", name)
	}
	lines := strings.Split(strings.ReplaceAll(notes, "\r\n", "\n"), "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "## ") {
			continue
		}
		empty := true
		for _, next := range lines[i+1:] {
			if strings.HasPrefix(next, "## ") {
				break
			}
			if strings.TrimSpace(next) != "" {
				empty = false
				break
			}
		}
		if empty {
			return fmt.Errorf("%s: %q has no content; fill it in or delete it", name, line)
		}
	}
	return nil
}

// Options configures where requests live and which version starts the history.
type Options struct {
	NotesDir     string
	FirstVersion string
}

// NotesTag returns the tag a notes path requests, or "" for any other file.
func NotesTag(notesDir, name string) string {
	base := path.Base(name)
	if path.Dir(name) != strings.Trim(notesDir, "/") || !strings.HasPrefix(base, "v") || !strings.HasSuffix(base, ".md") {
		return ""
	}
	return strings.TrimSuffix(base, ".md")
}

func notesFiles(ctx context.Context, repo gitrepo.Repo, notesDir, commit string) ([]string, error) {
	out, err := repo.Run(ctx, "ls-tree", "-r", "--name-only", "-z", commit, "--", strings.Trim(notesDir, "/")+"/")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, name := range strings.Split(out, "\x00") {
		if name != "" && NotesTag(notesDir, name) != "" {
			files = append(files, name)
		}
	}
	return files, nil
}

// Read validates the release request between base and head.
//
// Tagged requests are immutable; untagged ones can be corrected or withdrawn after a failed
// publication. Versions must advance every existing tag, the first release must use the
// configured first version, and a new request may not strand an earlier untagged one.
// An existing tag is accepted only at head, so retries never move a published version.
func Read(ctx context.Context, repo gitrepo.Repo, opts Options, base, head string) (Plan, error) {
	empty := Plan{Tags: []string{}}
	base, err := repo.Resolve(ctx, base)
	if err != nil {
		return empty, err
	}
	head, err = repo.Resolve(ctx, head)
	if err != nil {
		return empty, err
	}
	if !repo.IsAncestor(ctx, base, head) {
		return empty, fmt.Errorf("release base must be an ancestor of head")
	}
	tags, err := repo.Tags(ctx)
	if err != nil {
		return empty, err
	}

	dir := strings.Trim(opts.NotesDir, "/")
	changed, err := repo.Run(ctx, "diff", "--name-status", "-z", "--no-renames", base, head, "--", dir+"/")
	if err != nil {
		return empty, err
	}
	fields := strings.Split(strings.TrimSuffix(changed, "\x00"), "\x00")
	var requested string
	for i := 0; i+1 < len(fields); i += 2 {
		status, name := fields[i], fields[i+1]
		tag := NotesTag(dir, name)
		if tag == "" {
			continue
		}
		if status != "A" {
			// Published notes are history; later corrections belong in a new release. The one
			// exception is retrying the approved range itself: a corrected request whose tag
			// publication already created at this head.
			if slices.Contains(tags, tag) {
				target, err := repo.Resolve(ctx, "refs/tags/"+tag)
				if err != nil {
					return empty, err
				}
				if status != "M" || target != head {
					return empty, fmt.Errorf("tagged release notes are immutable: %s", name)
				}
			}
			if status == "D" {
				continue
			}
			if status != "M" {
				return empty, fmt.Errorf("unsupported release note change: %s", name)
			}
		}
		if requested != "" {
			return empty, fmt.Errorf("submit one release notes file per change")
		}
		requested = name
	}
	if requested == "" {
		return empty, nil
	}

	tag := NotesTag(dir, requested)
	current, ok := semver.Parse(tag)
	if !ok {
		return empty, fmt.Errorf("%s: name the file v<MAJOR>.<MINOR>.<PATCH>[-prerelease].md, without build metadata", requested)
	}
	notes, err := repo.Run(ctx, "show", head+":"+requested)
	if err != nil {
		return empty, err
	}
	if strings.TrimSpace(notes) == "" {
		return empty, fmt.Errorf("release notes must not be empty")
	}
	if err := lintNotes(requested, notes); err != nil {
		return empty, err
	}

	files, err := notesFiles(ctx, repo, dir, head)
	if err != nil {
		return empty, err
	}
	for _, name := range files {
		if pending := NotesTag(dir, name); name != requested && !slices.Contains(tags, pending) {
			return empty, fmt.Errorf("resolve untagged release request %s before requesting %s", name, tag)
		}
	}

	observed := []string{}
	var previous string
	var latest semver.Version
	for _, existing := range tags {
		other, ok := semver.Parse(existing)
		if !ok {
			continue
		}
		if existing == tag {
			target, err := repo.Resolve(ctx, "refs/tags/"+tag)
			if err != nil {
				return empty, err
			}
			if target != head {
				return empty, fmt.Errorf("tag %s already points to another commit", tag)
			}
			continue
		}
		observed = append(observed, existing)
		if semver.Compare(current, other) <= 0 {
			return empty, fmt.Errorf("%s must be newer than existing tag %s", tag, existing)
		}
		if previous == "" || semver.Compare(other, latest) > 0 {
			previous, latest = existing, other
		}
	}
	slices.Sort(observed)
	if previous == "" && tag != opts.FirstVersion {
		return empty, fmt.Errorf("the first release must be %s", opts.FirstVersion)
	}
	if previous != "" && !repo.IsAncestor(ctx, "refs/tags/"+previous, head) {
		return empty, fmt.Errorf("previous release %s must be an ancestor of %s", previous, head)
	}
	return Plan{Tags: observed, Tag: tag, Version: strings.TrimPrefix(tag, "v"), Commit: head,
		Previous: previous, Notes: notes, Prerelease: current.IsPrerelease()}, nil
}
