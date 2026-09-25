package plan

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/fabricahq/release-planner/internal/gitrepo"
	"github.com/fabricahq/release-planner/internal/notes"
	"github.com/fabricahq/release-planner/internal/semver"
)

// Inventory describes what a release cut at Head would contain, for the agent drafting notes.
type Inventory struct {
	Head     string `json:"head"`
	Previous string `json:"previous"`
	// Candidates holds patch, minor, and major after a previous release, or first otherwise.
	Candidates map[string]string `json:"candidates"`
	// PendingRequests are notes files with no tag: an earlier release that never published.
	PendingRequests []string `json:"pendingRequests"`
	// UnmergedNewerTags are version tags newer than Previous that Head does not contain.
	UnmergedNewerTags []string `json:"unmergedNewerTags"`
	PullRequests      []int    `json:"pullRequests"`
	Commits           []Commit `json:"commits"`
	// NewContributors are authors whose first merged pull request is in this release.
	NewContributors []NewContributor `json:"newContributors"`
	// Closing is the notes' last line. After a previous release, replace <version> in it.
	Closing string `json:"closing"`
	// Warnings explain information inventory could not gather, such as GitHub handles.
	Warnings []string `json:"warnings"`
}

// NewContributor is an author whose first merged pull request is in the release.
type NewContributor struct {
	Handle      string `json:"handle"`
	PullRequest int    `json:"pullRequest"`
	// Entry is the line that lists them under ## New Contributors.
	Entry string `json:"entry,omitempty"`
}

// Commit is one commit in the release range.
type Commit struct {
	SHA     string `json:"sha"`
	Author  string `json:"author"`
	Subject string `json:"subject"`
	// Title is the pull request's title for a merge or squash commit, else the subject.
	Title       string `json:"title"`
	PullRequest int    `json:"pullRequest,omitempty"`
	// AuthorHandle is the pull request author's GitHub handle, when inventory could look it up.
	AuthorHandle string `json:"authorHandle,omitempty"`
	// OnBranch is true for commits made directly on the release branch: pull request
	// merges and direct commits, but not the commits inside a merged branch.
	OnBranch bool `json:"onBranch"`
	// Entry is the line that lists this change under ## Pull Requests, for listed changes.
	Entry string `json:"entry,omitempty"`

	merge bool
}

// Merge commits and squash merges both name their pull request in the subject.
var pullRequest = regexp.MustCompile(`^Merge pull request #(\d+)|\(#(\d+)\)$`)

// Take lists everything since the newest version tag reachable from head.
func Take(ctx context.Context, repo gitrepo.Repo, opts Options, head string) (Inventory, error) {
	head, err := repo.Resolve(ctx, head)
	if err != nil {
		return Inventory{}, err
	}
	tags, err := repo.Tags(ctx)
	if err != nil {
		return Inventory{}, err
	}
	inv := Inventory{Head: head, PendingRequests: []string{}, UnmergedNewerTags: []string{}, PullRequests: []int{}, Commits: []Commit{}, NewContributors: []NewContributor{}, Warnings: []string{}}
	var latest semver.Version
	for _, t := range tags {
		v, ok := semver.Parse(t)
		if ok && repo.IsAncestor(ctx, "refs/tags/"+t, head) && (inv.Previous == "" || semver.Compare(v, latest) > 0) {
			inv.Previous, latest = t, v
		}
	}
	if inv.Previous != "" {
		p, mi, ma := semver.Candidates(latest)
		inv.Candidates = map[string]string{"patch": p.String(), "minor": mi.String(), "major": ma.String()}
		for _, t := range tags {
			if v, ok := semver.Parse(t); ok && semver.Compare(v, latest) > 0 {
				inv.UnmergedNewerTags = append(inv.UnmergedNewerTags, t)
			}
		}
		slices.SortFunc(inv.UnmergedNewerTags, func(a, b string) int { return semver.Compare(semver.MustParse(a), semver.MustParse(b)) })
	} else {
		inv.Candidates = map[string]string{"first": opts.FirstVersion}
	}

	if inv.Commits, err = changes(ctx, repo, opts.NotesDir, inv.Previous, head); err != nil {
		return Inventory{}, err
	}
	for _, c := range inv.Commits {
		if c.PullRequest != 0 && !slices.Contains(inv.PullRequests, c.PullRequest) {
			inv.PullRequests = append(inv.PullRequests, c.PullRequest)
		}
	}
	slices.Sort(inv.PullRequests)

	files, err := notesFiles(ctx, repo, opts.NotesDir, head)
	if err != nil {
		return Inventory{}, err
	}
	for _, name := range files {
		if !slices.Contains(tags, NotesTag(opts.NotesDir, name)) {
			inv.PendingRequests = append(inv.PendingRequests, name)
		}
	}
	return inv, nil
}

// changes lists the commits after the previous tag, or all of them without one, up to head,
// oldest first, leaving out commits that only change the notes directory: release requests
// and their merges, not changes to release.
//
// A commit is on the release branch if head's first-parent history reaches it, or the
// history of a merge that isn't a pull request's, such as main merged into a release pull
// request's branch. The commits inside a merged pull request are not.
func changes(ctx context.Context, repo gitrepo.Repo, notesDir, previous, head string) ([]Commit, error) {
	logRange := head
	if previous != "" {
		prev, err := repo.Resolve(ctx, "refs/tags/"+previous)
		if err != nil {
			return nil, err
		}
		logRange = prev + ".." + head
	}
	out, err := repo.Run(ctx, "log", "--reverse", "--format=%H%x1f%P%x1f%an%x1f%s%x1f%b%x1e", logRange)
	if err != nil {
		return nil, err
	}
	var commits []Commit
	parents := map[string][]string{}
	for _, record := range strings.Split(out, "\x1e") {
		fields := strings.Split(strings.TrimLeft(record, "\n"), "\x1f")
		if len(fields) != 5 {
			continue
		}
		c := Commit{SHA: fields[0], Author: fields[2], Subject: fields[3], Title: fields[3]}
		parents[c.SHA] = strings.Fields(fields[1])
		c.merge = len(parents[c.SHA]) > 1
		if m := pullRequest.FindStringSubmatch(c.Subject); m != nil {
			c.PullRequest, _ = strconv.Atoi(m[1] + m[2])
			if m[1] != "" {
				// GitHub puts the pull request title on the first line of a merge commit's body.
				if title, _, _ := strings.Cut(strings.TrimSpace(fields[4]), "\n"); title != "" {
					c.Title = strings.TrimSpace(title)
				}
			} else {
				c.Title = strings.TrimSpace(strings.TrimSuffix(c.Subject, m[0]))
			}
		}
		commits = append(commits, c)
	}

	onBranch := map[string]bool{}
	pullRequestMerge := map[string]bool{}
	for _, c := range commits {
		// GitHub can title a merge commit "Merge pull request #N …" or "<title> (#N)".
		pullRequestMerge[c.SHA] = c.merge && c.PullRequest != 0
	}
	for stack := []string{head}; len(stack) > 0; {
		sha := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, inRange := parents[sha]; !inRange || onBranch[sha] {
			continue
		}
		onBranch[sha] = true
		follow := parents[sha]
		if pullRequestMerge[sha] {
			follow = follow[:1]
		}
		stack = append(stack, follow...)
	}

	kept := []Commit{}
	for _, c := range commits {
		c.OnBranch = onBranch[c.SHA]
		if c.OnBranch && nothingToRelease(ctx, repo, notesDir, c) {
			continue
		}
		kept = append(kept, c)
	}
	return kept, nil
}

// nothingToRelease reports whether commit changes nothing outside the notes directory,
// compared with its first parent: a release request or its merge, or an empty direct commit
// such as an empty initial commit. None of them are changes to release.
func nothingToRelease(ctx context.Context, repo gitrepo.Repo, notesDir string, c Commit) bool {
	args := []string{"diff-tree", "--root", "--no-commit-id", "--name-only", "-r", "-z", c.SHA}
	if c.merge {
		args = []string{"diff", "--name-only", "-z", c.SHA + "^1", c.SHA}
	}
	out, err := repo.Run(ctx, args...)
	if err != nil {
		return false
	}
	dir := strings.Trim(notesDir, "/") + "/"
	files := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	if len(files) == 1 && files[0] == "" {
		// Empty: nothing to release, unless it's a pull request, which is always listed.
		return c.PullRequest == 0
	}
	for _, f := range files {
		if !strings.HasPrefix(f, dir) {
			return false
		}
	}
	return true
}

// Listed reports whether the notes list c under ## Pull Requests: a pull request or a direct
// commit on the release branch, but not a merge of another branch.
func Listed(c Commit) bool {
	return c.OnBranch && (c.PullRequest != 0 || !c.merge)
}

// VersionPlaceholder stands for the version in a later release's closing line, which
// inventory writes before the version is chosen.
const VersionPlaceholder = "<version>"

// AddEntries writes the notes' entry for each listed change, crediting its author's handle
// when known, and for each new contributor, and the closing line, with links to github.com/<ownerName>. A pull request
// merged more than once is listed once.
func (inv *Inventory) AddEntries(ownerName string) {
	seen := map[int]bool{}
	for i, c := range inv.Commits {
		if !Listed(c) || seen[c.PullRequest] {
			continue
		}
		if c.PullRequest == 0 {
			inv.Commits[i].Entry = fmt.Sprintf("- %s in https://github.com/%s/commit/%s", c.Title, ownerName, c.SHA[:7])
			continue
		}
		seen[c.PullRequest] = true
		by := ""
		if c.AuthorHandle != "" {
			by = " by @" + c.AuthorHandle
		}
		inv.Commits[i].Entry = fmt.Sprintf("- %s%s in #%d", c.Title, by, c.PullRequest)
	}
	for i, c := range inv.NewContributors {
		inv.NewContributors[i].Entry = fmt.Sprintf("- @%s made their first contribution in #%d", c.Handle, c.PullRequest)
	}
	if inv.Previous == "" {
		inv.Closing = notes.Closing(ownerName, "", inv.Candidates["first"])
	} else {
		inv.Closing = notes.Closing(ownerName, inv.Previous, VersionPlaceholder)
	}
}
