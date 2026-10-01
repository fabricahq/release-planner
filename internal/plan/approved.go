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

// WithdrawnError says a commit on the release branch after a plan's merge changed a notes file
// the plan acts on, so the merge no longer approves what the file held.
type WithdrawnError struct {
	File, Tag, Branch string
	// Commit is the newest commit that changed the file after the merge.
	Commit      string
	PullRequest int
	// Edit is true when the file is a notes edit's, and false when it's the requested release's.
	Edit bool
}

func (e WithdrawnError) Error() string {
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

// StillApproved checks a merged plan against tip, the release branch's current commit:
// no commit after the plan's merge may have changed the notes files it acts on. Deleting a
// request's file withdraws it, and changing it requests that version again, so an older run
// must not publish it; a later notes edit replaces an older one. The requested release's file
// is checked until its version is tagged, and every edited file always. Moving a file
// unchanged into the notes directory the tip configures isn't a change.
//
// --full-history also finds a file deleted and added back on a side branch, which git's
// default history simplification hides.
//
// tip must come from the branch itself, not origin/branch: in a re-run, actions/checkout moves
// origin/branch back to the run's commit.
func StillApproved(ctx context.Context, repo gitrepo.Repo, branch, tip string, p Plan) error {
	data, _ := repo.Run(ctx, "show", tip+":"+config.File)
	dir := config.NotesDirIn([]byte(data))
	check := func(file, tag string, edit bool) error {
		paths := []string{file}
		if moved := path.Join(dir, tag+".md"); moved != file {
			paths = append(paths, moved)
		}
		out, err := repo.Run(ctx, append([]string{"log", "--full-history", "--format=%x00%H", "--name-status", "--find-renames=100%", tip, "^" + p.Merged, "--"}, paths...)...)
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
					return WithdrawnError{File: file, Tag: tag, Branch: branch, Commit: lines[0], PullRequest: p.PullRequest, Edit: edit}
				}
			}
		}
		return nil
	}
	if p.Tag != "" {
		if _, err := repo.Resolve(ctx, "refs/tags/"+p.Tag); err != nil {
			if err := check(p.File, p.Tag, false); err != nil {
				return err
			}
		}
	}
	for _, e := range p.Edits {
		if err := check(e.File, e.Tag, true); err != nil {
			return err
		}
	}
	return nil
}
