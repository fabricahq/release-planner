package plan

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/fabricahq/release-planner/internal/config"
	"github.com/fabricahq/release-planner/internal/gitrepo"
)

// WithdrawnError says the release branch no longer holds what a plan's merge approved: a later
// commit changed a notes file the plan acts on, or the branch's history no longer runs through
// the merge, so the approval can't be confirmed.
type WithdrawnError struct {
	File, Tag, Branch string
	// Commit is the branch commit that changed the file, or the branch's tip when Rewritten.
	Commit      string
	PullRequest int
	// Edit is true when the file is a notes edit's, and false when it's the requested release's.
	Edit bool
	// Rewritten is true when the branch's first-parent history doesn't pass through the merge.
	Rewritten bool
}

func (e WithdrawnError) Error() string {
	if e.Rewritten {
		what := fmt.Sprintf("that %s is still what it approved, and doesn't publish it. Request it again in a new release pull request", e.Tag)
		if e.Edit {
			what = fmt.Sprintf("that the %s notes are still what it approved, and doesn't replace them", e.Tag)
		}
		return fmt.Sprintf("%s's history no longer runs through pull request #%d's merge (%s is now at %s), so this run can't confirm %s", e.Branch, e.PullRequest, e.Branch, shortSHA(e.Commit), what)
	}
	changed := fmt.Sprintf("%s changed on %s after pull request #%d merged (in %s), ", e.File, e.Branch, e.PullRequest, shortSHA(e.Commit))
	if e.Edit {
		return changed + fmt.Sprintf("so this run doesn't replace the %s notes; the newer change's run does", e.Tag)
	}
	return changed + fmt.Sprintf("so %s was withdrawn or requested again. This run doesn't publish it; any newer request publishes from its own run", e.Tag)
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// StillApproved checks a merged plan against tip, the release branch's current commit: the
// branch must still hold exactly what the plan's merge approved, for the requested release
// until its version is tagged, and for every edited release.
//
// The steps are the commits of the release branch's first-parent history. A pull request merged
// with a merge commit or squashed lands as one step; a rebased one lands as one step per
// rebased commit, the last of which is its merged commit. A request stands when:
//
//   - the plan's merged commit is on tip's first-parent history;
//   - no later step changed the notes file compared with the step before it, so nothing
//     withdrew it, edited it, or requested the version again; an exact move into the notes
//     directory tip configures isn't a change; and
//   - tip holds the approved notes.
//
// Commits on the branches that merge commits merged never count, so a branch that merged the
// release branch in, or deleted and restored the file, changes nothing unless its merge does.
// A rebased pull request's commits do count, each one, so one that deletes and restores the
// file refuses even though the notes end unchanged; that fails in the safe direction. A history rewritten so it no longer runs through the merge, as by a fast-forward to a
// branch that started before it, can't be checked, so it refuses.
//
// tip must come from the branch itself, not origin/branch: in a re-run, actions/checkout moves
// origin/branch back to the run's commit.
func StillApproved(ctx context.Context, repo gitrepo.Repo, branch, tip string, p Plan) error {
	type file struct {
		name, tag, notes string
		edit             bool
	}
	var files []file
	if p.Tag != "" {
		if _, err := repo.Resolve(ctx, "refs/tags/"+p.Tag); err != nil {
			files = append(files, file{p.File, p.Tag, p.Notes, false})
		}
	}
	for _, e := range p.Edits {
		files = append(files, file{e.File, e.Tag, e.Notes, true})
	}
	if len(files) == 0 {
		return nil
	}
	first := files[0]
	if onPath, err := firstParentStep(ctx, repo, tip, p.Merged); err != nil {
		return err
	} else if !onPath {
		return WithdrawnError{File: first.name, Tag: first.tag, Branch: branch, Commit: tip, PullRequest: p.PullRequest, Edit: first.edit, Rewritten: true}
	}

	data, _ := repo.Run(ctx, "show", tip+":"+config.File)
	dir := config.NotesDirIn([]byte(data))
	for _, f := range files {
		withdrawn := WithdrawnError{File: f.name, Tag: f.tag, Branch: branch, PullRequest: p.PullRequest, Edit: f.edit}
		paths := []string{f.name}
		current := path.Join(dir, f.tag+".md")
		if current != f.name {
			paths = append(paths, current)
		}
		// Each step after the merge, compared with the step before it.
		out, err := repo.Run(ctx, append([]string{"log", "--first-parent", "--diff-merges=first-parent", "--format=%x00%H", "--name-status", "--find-renames=100%", tip, "^" + p.Merged, "--"}, paths...)...)
		if err != nil {
			return err
		}
		for _, entry := range strings.Split(out, "\x00") {
			lines := strings.Split(strings.TrimSpace(entry), "\n")
			if lines[0] == "" {
				continue
			}
			for _, line := range lines[1:] {
				fields := strings.Split(line, "\t")
				// An exact move between the old and new notes directories keeps the approved notes.
				if len(fields) == 3 && fields[0] == "R100" && slices.Contains(paths, fields[1]) && slices.Contains(paths, fields[2]) {
					continue
				}
				if strings.TrimSpace(line) != "" {
					withdrawn.Commit = lines[0]
					return withdrawn
				}
			}
		}
		if held, err := repo.Run(ctx, "show", tip+":"+current); err != nil || held != f.notes {
			withdrawn.Commit = tip
			return withdrawn
		}
	}
	return nil
}

// firstParentStep reports whether commit is tip or on tip's first-parent history.
func firstParentStep(ctx context.Context, repo gitrepo.Repo, tip, commit string) (bool, error) {
	if tip == commit {
		return true, nil
	}
	// The steps after commit, newest first, stopping where the walk reaches commit's history.
	out, err := repo.Run(ctx, "rev-list", "--first-parent", tip, "^"+commit)
	if err != nil {
		return false, err
	}
	steps := strings.Fields(out)
	if len(steps) == 0 {
		return false, nil
	}
	parent, err := repo.Resolve(ctx, steps[len(steps)-1]+"^1")
	return err == nil && parent == commit, nil
}
