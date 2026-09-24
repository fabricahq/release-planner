package plan

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/fabricahq/release-planner/internal/gitrepo"
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
	// Warnings explain information inventory could not gather, such as GitHub handles.
	Warnings []string `json:"warnings"`
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
	inv := Inventory{Head: head, PendingRequests: []string{}, UnmergedNewerTags: []string{}, PullRequests: []int{}, Commits: []Commit{}, Warnings: []string{}}
	var latest semver.Version
	for _, t := range tags {
		v, ok := semver.Parse(t)
		if ok && repo.IsAncestor(ctx, "refs/tags/"+t, head) && (inv.Previous == "" || semver.Compare(v, latest) > 0) {
			inv.Previous, latest = t, v
		}
	}
	logRange := head
	if inv.Previous != "" {
		p, mi, ma := semver.Candidates(latest)
		inv.Candidates = map[string]string{"patch": p.String(), "minor": mi.String(), "major": ma.String()}
		prev, err := repo.Resolve(ctx, "refs/tags/"+inv.Previous)
		if err != nil {
			return Inventory{}, err
		}
		logRange = prev + ".." + head
		for _, t := range tags {
			if v, ok := semver.Parse(t); ok && semver.Compare(v, latest) > 0 {
				inv.UnmergedNewerTags = append(inv.UnmergedNewerTags, t)
			}
		}
		slices.SortFunc(inv.UnmergedNewerTags, func(a, b string) int { return semver.Compare(semver.MustParse(a), semver.MustParse(b)) })
	} else {
		inv.Candidates = map[string]string{"first": opts.FirstVersion}
	}

	firstParent, err := repo.Run(ctx, "log", "--first-parent", "--format=%H", logRange)
	if err != nil {
		return Inventory{}, err
	}
	onBranch := strings.Fields(firstParent)
	out, err := repo.Run(ctx, "log", "--reverse", "--format=%H%x1f%an%x1f%s%x1f%b%x1e", logRange)
	if err != nil {
		return Inventory{}, err
	}
	for _, record := range strings.Split(out, "\x1e") {
		fields := strings.Split(strings.TrimLeft(record, "\n"), "\x1f")
		if len(fields) != 4 {
			continue
		}
		c := Commit{SHA: fields[0], Author: fields[1], Subject: fields[2], Title: fields[2], OnBranch: slices.Contains(onBranch, fields[0])}
		if m := pullRequest.FindStringSubmatch(c.Subject); m != nil {
			c.PullRequest, _ = strconv.Atoi(m[1] + m[2])
			if m[1] != "" {
				// GitHub puts the pull request title on the first line of a merge commit's body.
				if title, _, _ := strings.Cut(strings.TrimSpace(fields[3]), "\n"); title != "" {
					c.Title = strings.TrimSpace(title)
				}
			} else {
				c.Title = strings.TrimSpace(strings.TrimSuffix(c.Subject, m[0]))
			}
			if !slices.Contains(inv.PullRequests, c.PullRequest) {
				inv.PullRequests = append(inv.PullRequests, c.PullRequest)
			}
		}
		inv.Commits = append(inv.Commits, c)
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
