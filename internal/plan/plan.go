// Package plan validates release pull requests and describes what merging one publishes.
//
// A release pull request changes only release notes files, <release-notes-dir>/v<semver>.md,
// on top of the release commit: the newest commit it shares with the release branch. Adding
// the notes of an unpublished version requests that release, which tags the release commit
// with the notes; the release commit doesn't contain them. Changing the notes of a tagged
// version edits them, and changes nothing else about that release.
// Ported from Code Rules' internal/release planner.
package plan

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/fabricahq/release-planner/internal/config"
	"github.com/fabricahq/release-planner/internal/gitrepo"
	"github.com/fabricahq/release-planner/internal/notes"
	"github.com/fabricahq/release-planner/internal/semver"
)

// Plan is the exact input to publication. A plan with no Tag and no Edits requests nothing.
type Plan struct {
	// Tags lists the version tags observed at planning, excluding this release,
	// so publication can refuse to act on a stale plan.
	Tags    []string `json:"tags"`
	Tag     string   `json:"tag"`
	Version string   `json:"version"`
	// Commit is the release commit, which the tag points to.
	Commit     string `json:"commit"`
	Previous   string `json:"previous"`
	Notes      string `json:"notes"`
	Prerelease bool   `json:"prerelease"`

	// File is the requested notes file, and Findings the rules its notes break.
	File     string          `json:"file,omitempty"`
	Findings []notes.Finding `json:"findings,omitempty"`
	// Edits change the notes of versions that are already tagged.
	Edits []Edit `json:"edits,omitempty"`

	// Head is the pull request's head commit.
	Head string `json:"head"`
	// PullRequest, Merged, and MergedBy describe the merge that approved the plan, once merged:
	// the pull request, the commit it merged as on the release branch, and who merged it.
	PullRequest int    `json:"pullRequest,omitempty"`
	Merged      string `json:"merged,omitempty"`
	MergedBy    string `json:"mergedBy,omitempty"`
	// BuildRun is the workflow run whose release checks and assets the release uses, and
	// Reused is true when that is the pull request's run rather than the current one.
	BuildRun int64 `json:"buildRun,omitempty"`
	Reused   bool  `json:"reused,omitempty"`
	// Warnings are problems with the repository's settings that don't block the plan.
	Warnings []string `json:"warnings,omitempty"`
}

// Edit replaces the notes of a tagged version.
type Edit struct {
	Tag      string          `json:"tag"`
	File     string          `json:"file"`
	Notes    string          `json:"notes"`
	Findings []notes.Finding `json:"findings,omitempty"`
	// HandEdited is true when the release's notes on GitHub differ from the file's previous
	// content, so merging discards an edit made on GitHub.
	HandEdited bool `json:"handEdited,omitempty"`
}

// Empty reports whether the plan requests nothing.
func (p Plan) Empty() bool { return p.Tag == "" && len(p.Edits) == 0 }

// Options configures where requests live and which version starts the history.
type Options struct {
	NotesDir     string
	FirstVersion string
	// RulesOff are notes rules to skip.
	RulesOff []string
	// Repository is the GitHub owner/name, for checking the notes' closing link, or "" to
	// check it without the repository.
	Repository string
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

// change is one file a pull request changes, with git's status letter.
type change struct{ status, name string }

// diff lists the files head changes on top of commit. A tagged version's notes moved
// unchanged from the notes directory commit configures, such as to a new notes directory,
// are not a change. Any other file moved into the notes directory adds that file.
func diff(ctx context.Context, repo gitrepo.Repo, dir string, tags []string, commit, head string) ([]change, error) {
	// Only exact renames: a file rewritten while moving is a change.
	out, err := repo.Run(ctx, "diff", "--name-status", "-z", "--find-renames=100%", commit, head)
	if err != nil {
		return nil, err
	}
	var was string
	var changes []change
	fields := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		status, name := fields[i], fields[i+1]
		if !strings.HasPrefix(status, "R") {
			changes = append(changes, change{status, name})
			continue
		}
		i++
		if i+1 >= len(fields) {
			return nil, fmt.Errorf("git diff: incomplete rename of %s", name)
		}
		to := fields[i+1]
		if tag := NotesTag(dir, to); tag != "" && slices.Contains(tags, tag) {
			if was == "" {
				// A config missing at commit, or unreadable, sets the default directory.
				data, _ := repo.Run(ctx, "show", commit+":"+config.File)
				was = config.NotesDirIn([]byte(data))
			}
			if NotesTag(was, name) == tag {
				continue
			}
		}
		changes = append(changes, change{"D", name}, change{"A", to})
	}
	return changes, nil
}

// Read validates the release pull request whose head is head, on a release branch at base.
//
// The release commit is the newest commit head shares with base, and head may change only
// release notes files on top of it. It may request one release, by adding or correcting an
// untagged version's notes, and edit the notes of tagged versions; a pull request that does
// neither requests nothing, and may change anything. Tagged notes can't be deleted. Versions
// must advance every existing tag, the first release must use the configured first version,
// and a new request may not strand an earlier untagged one. A version's tag may already
// exist only on the release commit, where an interrupted publication created it.
// The plan's Findings list the notes rules the notes break, which don't make it invalid.
func Read(ctx context.Context, repo gitrepo.Repo, opts Options, base, head string) (Plan, error) {
	empty := Plan{Tags: []string{}}
	head, err := repo.Resolve(ctx, head)
	if err != nil {
		return empty, err
	}
	base, err = repo.Resolve(ctx, base)
	if err != nil {
		return empty, err
	}
	commit, err := repo.MergeBase(ctx, base, head)
	if err != nil {
		return empty, fmt.Errorf("%s shares no history with the release branch at %s: %v", head, base, err)
	}
	empty.Head = head
	tags, err := repo.Tags(ctx)
	if err != nil {
		return empty, err
	}

	dir := strings.Trim(opts.NotesDir, "/")
	changes, err := diff(ctx, repo, dir, tags, commit, head)
	if err != nil {
		return empty, err
	}
	var requested string
	var edits []Edit
	var others []string
	for _, c := range changes {
		tag := NotesTag(dir, c.name)
		if tag == "" {
			others = append(others, c.name)
			continue
		}
		tagged := slices.Contains(tags, tag)
		if tagged && c.status != "D" {
			// An interrupted publication may have tagged the release commit already; the
			// request is still that release, not an edit.
			target, err := repo.Resolve(ctx, "refs/tags/"+tag)
			if err != nil {
				return empty, err
			}
			tagged = target != commit
		}
		switch {
		case c.status == "D" && tagged:
			return empty, fmt.Errorf("%s holds the notes of the tagged %s, so it can't be deleted; edit it instead", c.name, tag)
		case c.status == "D":
			// Withdraws a request that never published.
		case c.status != "A" && c.status != "M":
			return empty, fmt.Errorf("unsupported release notes change: %s", c.name)
		case tagged:
			edits = append(edits, Edit{Tag: tag, File: c.name})
		case requested != "":
			return empty, fmt.Errorf("request one release per pull request, not both %s and %s", requested, c.name)
		default:
			requested = c.name
		}
	}
	if requested == "" && len(edits) == 0 {
		return empty, nil
	}
	if len(others) > 0 {
		if len(others) > 3 {
			others = append(others[:3], fmt.Sprintf("%d more", len(others)-3))
		}
		return empty, fmt.Errorf("a release pull request may change only release notes files, but this one also changes %s; move those changes to another pull request and merge it first", strings.Join(others, ", "))
	}

	for i, e := range edits {
		text, err := notesText(ctx, repo, head, e.File)
		if err != nil {
			return empty, err
		}
		target, err := repo.Resolve(ctx, "refs/tags/"+e.Tag)
		if err != nil {
			return empty, err
		}
		edits[i].Notes = text
		if version, ok := semver.Parse(e.Tag); ok {
			previous := ""
			var latest semver.Version
			for _, t := range tags {
				if v, ok := semver.Parse(t); ok && semver.Compare(v, version) < 0 && (previous == "" || semver.Compare(v, latest) > 0) {
					previous, latest = t, v
				}
			}
			if edits[i].Findings, err = check(ctx, repo, opts, text, e.Tag, previous, target); err != nil {
				return empty, err
			}
		}
	}
	if requested == "" {
		empty.Edits = edits
		return empty, nil
	}

	tag := NotesTag(dir, requested)
	current, ok := semver.Parse(tag)
	if !ok {
		return empty, fmt.Errorf("%s: name the file v<MAJOR>.<MINOR>.<PATCH>[-prerelease].md, without build metadata", requested)
	}
	text, err := notesText(ctx, repo, head, requested)
	if err != nil {
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
		if !ok || existing == tag {
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
	if previous != "" && !repo.IsAncestor(ctx, "refs/tags/"+previous, commit) {
		return empty, fmt.Errorf("previous release %s must be an ancestor of the release commit %s; merge %s into this branch", previous, commit, previous)
	}
	findings, err := check(ctx, repo, opts, text, tag, previous, commit)
	if err != nil {
		return empty, err
	}
	return Plan{Tags: observed, Tag: tag, Version: strings.TrimPrefix(tag, "v"), Commit: commit,
		Previous: previous, Notes: text, Prerelease: current.IsPrerelease(),
		File: requested, Findings: findings, Edits: edits, Head: head}, nil
}

func notesText(ctx context.Context, repo gitrepo.Repo, head, name string) (string, error) {
	text, err := repo.Run(ctx, "show", head+":"+name)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("%s: release notes must not be empty", name)
	}
	return text, nil
}

// check lists the notes rules text breaks as the notes of tag, released at commit after previous.
func check(ctx context.Context, repo gitrepo.Repo, opts Options, text, tag, previous, commit string) ([]notes.Finding, error) {
	commits, err := changes(ctx, repo, strings.Trim(opts.NotesDir, "/"), previous, commit)
	if err != nil {
		return nil, err
	}
	release := notes.Release{Version: tag, Previous: previous, Repository: opts.Repository}
	for _, c := range commits {
		if Listed(c) {
			release.Changes = append(release.Changes, notes.Change{PullRequest: c.PullRequest, SHA: c.SHA})
		}
	}
	return notes.Check(text, release, opts.RulesOff), nil
}
