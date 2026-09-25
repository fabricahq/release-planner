package publish

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// GitHub is the small part of the REST API that publication needs.
type GitHub struct {
	BaseURL    string // such as https://api.github.com
	Token      string
	Repository string // owner/name
	HTTP       *http.Client
}

// Release is a GitHub release, draft or published.
type Release struct {
	ID         int64  `json:"id"`
	TagName    string `json:"tag_name"`
	Name       string `json:"name"`
	Body       string `json:"body"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	// TargetCommitish is the commit a draft's tag will be created on when it is published.
	TargetCommitish string  `json:"target_commitish"`
	HTMLURL         string  `json:"html_url"`
	UploadURL       string  `json:"upload_url"`
	Assets          []Asset `json:"assets"`
}

// Asset is a file attached to a release.
type Asset struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"` // sha256:<hex>, computed by GitHub
	URL    string `json:"url"`
}

var nextLink = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// errNotFound distinguishes an absent resource from a failed request.
type errNotFound struct{}

func (errNotFound) Error() string { return "not found" }

// raw is a request body sent as-is rather than encoded as JSON.
type raw struct {
	contentType string
	data        []byte
}

// trusted checks that an absolute URL taken from an API response, such as a pagination
// link, an upload URL, or an asset URL, belongs to this repository on this GitHub, so the
// token is never sent anywhere else.
func (g *GitHub) trusted(target string) error {
	base, err := url.Parse(g.BaseURL)
	if err != nil {
		return err
	}
	u, err := url.Parse(target)
	if err != nil {
		return err
	}
	host := u.Host == base.Host || (base.Host == "api.github.com" && u.Host == "uploads.github.com")
	repo := "/repos/" + g.Repository + "/"
	if u.Scheme == base.Scheme && host && u.User == nil {
		for _, prefix := range []string{strings.TrimRight(base.Path, "/") + repo, repo, "/api/uploads" + repo} {
			if strings.HasPrefix(u.Path, prefix) && !strings.Contains(u.Path, "/../") {
				return nil
			}
		}
	}
	return fmt.Errorf("refusing to send the token to %s, which is outside %s on %s", u.Redacted(), g.Repository, base.Host)
}

func (g *GitHub) do(ctx context.Context, method, target string, body any, out any) (next string, err error) {
	if !strings.HasPrefix(target, "http") {
		target = strings.TrimRight(g.BaseURL, "/") + "/repos/" + g.Repository + target
	} else if err := g.trusted(target); err != nil {
		return "", err
	}
	var reader io.Reader
	contentType := "application/json"
	switch b := body.(type) {
	case nil:
	case raw:
		reader, contentType = bytes.NewReader(b.data), b.contentType
	default:
		data, err := json.Marshal(body)
		if err != nil {
			return "", err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if g.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.Token)
	}
	if body != nil {
		req.Header.Set("Content-Type", contentType)
	}
	client := g.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s %s: %v", method, req.URL.Path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return "", fmt.Errorf("%s %s: read response: %v", method, req.URL.Path, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return "", errNotFound{}
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("%s %s: %s: %s", method, req.URL.Path, resp.Status, strings.TrimSpace(string(data)))
	}
	if m := nextLink.FindStringSubmatch(resp.Header.Get("Link")); m != nil {
		next = m[1]
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return "", fmt.Errorf("%s %s: decode response: %v", method, req.URL.Path, err)
		}
	}
	return next, nil
}

// VersionTags lists every tag whose name starts with v.
func (g *GitHub) VersionTags(ctx context.Context) ([]string, error) {
	var tags []string
	target := "/git/matching-refs/tags/v?per_page=100"
	for target != "" {
		var refs []struct {
			Ref string `json:"ref"`
		}
		next, err := g.do(ctx, http.MethodGet, target, nil, &refs)
		if err != nil {
			return nil, err
		}
		for _, r := range refs {
			tags = append(tags, strings.TrimPrefix(r.Ref, "refs/tags/"))
		}
		target = next
	}
	return tags, nil
}

// TagCommit resolves a tag to its commit, peeling annotated tags. It returns "" if the tag is absent.
func (g *GitHub) TagCommit(ctx context.Context, tag string) (string, error) {
	var ref struct {
		Object struct{ Type, SHA string } `json:"object"`
	}
	_, err := g.do(ctx, http.MethodGet, "/git/ref/tags/"+url.PathEscape(tag), nil, &ref)
	if _, missing := err.(errNotFound); missing {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if ref.Object.Type != "tag" {
		return ref.Object.SHA, nil
	}
	var annotated struct {
		Object struct{ SHA string } `json:"object"`
	}
	if _, err := g.do(ctx, http.MethodGet, "/git/tags/"+ref.Object.SHA, nil, &annotated); err != nil {
		return "", err
	}
	return annotated.Object.SHA, nil
}

// ReleaseByTag finds a release, including drafts, which GitHub's by-tag lookup omits. It returns nil if absent.
func (g *GitHub) ReleaseByTag(ctx context.Context, tag string) (*Release, error) {
	target := "/releases?per_page=100"
	for target != "" {
		var page []Release
		next, err := g.do(ctx, http.MethodGet, target, nil, &page)
		if err != nil {
			return nil, err
		}
		for i := range page {
			if page[i].TagName == tag {
				return &page[i], nil
			}
		}
		target = next
	}
	return nil, nil
}

func latest(yes bool) string {
	if yes {
		return "true"
	}
	return "false"
}

// Release reads one release by ID.
func (g *GitHub) Release(ctx context.Context, id int64) (*Release, error) {
	var r Release
	_, err := g.do(ctx, http.MethodGet, fmt.Sprintf("/releases/%d", id), nil, &r)
	return &r, err
}

// CreateRelease creates a release that makes the tag on commit when published. A draft
// creates no tag until it is published.
func (g *GitHub) CreateRelease(ctx context.Context, tag, commit, notes string, prerelease, draft bool) (*Release, error) {
	var r Release
	body := map[string]any{
		"tag_name": tag, "target_commitish": commit, "name": tag, "body": notes,
		"draft": draft, "prerelease": prerelease,
	}
	if !draft {
		body["make_latest"] = latest(!prerelease)
	}
	_, err := g.do(ctx, http.MethodPost, "/releases", body, &r)
	return &r, err
}

// UploadAsset attaches a file to a draft release.
func (g *GitHub) UploadAsset(ctx context.Context, draft *Release, name string, data []byte) error {
	base, _, _ := strings.Cut(draft.UploadURL, "{")
	if base == "" {
		return fmt.Errorf("the %s draft has no upload URL", draft.TagName)
	}
	_, err := g.do(ctx, http.MethodPost, base+"?name="+url.QueryEscape(name), raw{"application/octet-stream", data}, nil)
	return err
}

// AssetDigest returns GitHub's stored sha256 for an asset, downloading it only when
// GitHub did not report one.
func (g *GitHub) AssetDigest(ctx context.Context, a Asset) (string, error) {
	if strings.HasPrefix(a.Digest, "sha256:") {
		return a.Digest, nil
	}
	if err := g.trusted(a.URL); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/octet-stream")
	if g.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.Token)
	}
	client := g.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download release asset %s from %s: %v", a.Name, g.Repository, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("download release asset %s from %s: %s", a.Name, g.Repository, resp.Status)
	}
	h := sha256.New()
	if _, err := io.Copy(h, resp.Body); err != nil {
		return "", fmt.Errorf("download release asset %s from %s: %v", a.Name, g.Repository, err)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// PullRequest is a merged pull request.
type PullRequest struct {
	Number int
	// Head is the pull request's head commit, and HeadRepository the owner/name it came from.
	Head, HeadRepository string
	MergedBy             string
}

// MergedPullRequest returns the pull request into branch that the commit merged, or nil if
// the commit isn't a pull request's merge, squash, or rebase result.
func (g *GitHub) MergedPullRequest(ctx context.Context, commit, branch string) (*PullRequest, error) {
	var pulls []struct {
		Number         int     `json:"number"`
		MergedAt       *string `json:"merged_at"`
		MergeCommitSHA string  `json:"merge_commit_sha"`
		Base           struct {
			Ref string `json:"ref"`
		} `json:"base"`
	}
	_, err := g.do(ctx, http.MethodGet, "/commits/"+commit+"/pulls?per_page=100", nil, &pulls)
	if errors.As(err, new(errNotFound)) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, p := range pulls {
		if p.MergedAt == nil || p.MergeCommitSHA != commit || p.Base.Ref != branch {
			continue
		}
		var pr struct {
			Head struct {
				SHA  string `json:"sha"`
				Repo *struct {
					FullName string `json:"full_name"`
				} `json:"repo"`
			} `json:"head"`
			MergedBy *struct {
				Login string `json:"login"`
			} `json:"merged_by"`
		}
		if _, err := g.do(ctx, http.MethodGet, fmt.Sprintf("/pulls/%d", p.Number), nil, &pr); err != nil {
			return nil, fmt.Errorf("get pull request #%d in %s: %v", p.Number, g.Repository, err)
		}
		merged := &PullRequest{Number: p.Number, Head: pr.Head.SHA}
		if pr.Head.Repo != nil {
			merged.HeadRepository = pr.Head.Repo.FullName
		}
		if pr.MergedBy != nil {
			merged.MergedBy = pr.MergedBy.Login
		}
		return merged, nil
	}
	return nil, nil
}

// PullRequestAuthor returns the GitHub handle of the pull request's author.
func (g *GitHub) PullRequestAuthor(ctx context.Context, number int) (string, error) {
	var pr struct {
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	if _, err := g.do(ctx, http.MethodGet, fmt.Sprintf("/pulls/%d", number), nil, &pr); err != nil {
		return "", fmt.Errorf("get pull request #%d in %s: %v", number, g.Repository, err)
	}
	if pr.User.Login == "" {
		return "", fmt.Errorf("pull request #%d in %s has no author", number, g.Repository)
	}
	return pr.User.Login, nil
}

// ContributedBefore reports whether the handle authored any commit reachable from ref.
func (g *GitHub) ContributedBefore(ctx context.Context, handle, ref string) (bool, error) {
	var commits []struct {
		SHA string `json:"sha"`
	}
	query := url.Values{"sha": {ref}, "author": {handle}, "per_page": {"1"}}
	if _, err := g.do(ctx, http.MethodGet, "/commits?"+query.Encode(), nil, &commits); err != nil {
		return false, fmt.Errorf("list commits by @%s in %s at %s: %v", handle, g.Repository, ref, err)
	}
	return len(commits) > 0, nil
}

// Environment is the part of a deployment environment's settings that Release Planner checks.
type Environment struct {
	// DeploymentBranchPolicy is nil when any branch can deploy.
	DeploymentBranchPolicy *struct {
		ProtectedBranches    bool `json:"protected_branches"`
		CustomBranchPolicies bool `json:"custom_branch_policies"`
	} `json:"deployment_branch_policy"`
	ProtectionRules []struct {
		Type string `json:"type"`
	} `json:"protection_rules"`

	// BranchRules are the name patterns of the environment's custom branch rules, when it has any.
	BranchRules []string `json:"-"`
	// BranchProtected reports whether the release branch is protected, when the environment
	// allows only protected branches.
	BranchProtected bool `json:"-"`
}

// Environment returns a deployment environment's settings, or nil if it doesn't exist. When
// the environment restricts which branches can deploy, it also reads what decides whether the
// release branch can: the custom branch rules, or whether the branch is protected.
func (g *GitHub) Environment(ctx context.Context, name, branch string) (*Environment, error) {
	var env Environment
	_, err := g.do(ctx, http.MethodGet, "/environments/"+url.PathEscape(name), nil, &env)
	if errors.As(err, new(errNotFound)) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get the %s environment in %s: %v", name, g.Repository, err)
	}
	policy := env.DeploymentBranchPolicy
	if policy != nil && policy.CustomBranchPolicies {
		target := "/environments/" + url.PathEscape(name) + "/deployment-branch-policies?per_page=100"
		for target != "" {
			var page struct {
				BranchPolicies []struct {
					Name string `json:"name"`
					Type string `json:"type"`
				} `json:"branch_policies"`
			}
			if target, err = g.do(ctx, http.MethodGet, target, nil, &page); err != nil {
				return nil, fmt.Errorf("list the %s environment's branch rules in %s: %v", name, g.Repository, err)
			}
			for _, rule := range page.BranchPolicies {
				// Rules without a type predate tag rules and apply to branches.
				if rule.Type == "" || rule.Type == "branch" {
					env.BranchRules = append(env.BranchRules, rule.Name)
				}
			}
		}
	}
	if policy != nil && policy.ProtectedBranches {
		var b struct {
			Protected bool `json:"protected"`
		}
		if _, err := g.do(ctx, http.MethodGet, "/branches/"+url.PathEscape(branch), nil, &b); err != nil {
			return nil, fmt.Errorf("check whether %s is protected in %s: %v", branch, g.Repository, err)
		}
		env.BranchProtected = b.Protected
	}
	return &env, nil
}

// branchRule matches a branch name against a deployment branch rule, which uses fnmatch
// patterns: * and ? don't match /, ** does, and [...] is a character class.
func branchRule(pattern, branch string) bool {
	var re strings.Builder
	re.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; {
		case c == '*' && i+1 < len(pattern) && pattern[i+1] == '*':
			re.WriteString(".*")
			i++
		case c == '*':
			re.WriteString("[^/]*")
		case c == '?':
			re.WriteString("[^/]")
		case c == '[':
			end := strings.IndexByte(pattern[i:], ']')
			if end < 0 {
				re.WriteString(regexp.QuoteMeta(pattern[i:]))
				i = len(pattern)
				continue
			}
			class := pattern[i+1 : i+end]
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}
			re.WriteString("[" + class + "]")
			i += end
		default:
			re.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	re.WriteString("$")
	matched, err := regexp.MatchString(re.String(), branch)
	return err == nil && matched
}

// Pages that explain how to set up the release and downstream environments.
const (
	EnvironmentDocs           = "https://release-planner.fabricahq.com/start-here/set-up/#create-the-release-environment"
	DownstreamEnvironmentDocs = "https://release-planner.fabricahq.com/customize/downstream/#set-up-the-downstream-environment"
)

// EnvironmentWarnings explains how an environment differs from the recommended setup, which
// docs describes: it exists, only the release branch can deploy to it, and the merge is the
// only approval. The differences are warnings, not errors, because a repository may choose them.
func EnvironmentWarnings(name, branch, docs string, env *Environment) []string {
	if env == nil {
		return []string{fmt.Sprintf("The %s environment doesn't exist. GitHub will create it the first time a job uses it with no deployment branch rule, so a workflow on any branch could use it. Create it with a branch rule for your release branch: %s", name, docs)}
	}
	var warnings []string
	switch policy := env.DeploymentBranchPolicy; {
	case policy == nil:
		warnings = append(warnings, fmt.Sprintf("The %s environment has no deployment branch rule, so a workflow on any branch could use it. Add a branch rule for your release branch: %s", name, docs))
	case policy.ProtectedBranches && !env.BranchProtected:
		warnings = append(warnings, fmt.Sprintf("The %s environment allows only protected branches, and %s isn't protected, so its jobs can't run. Protect %s, or add a branch rule for it: %s", name, branch, branch, docs))
	case policy.CustomBranchPolicies && !slices.ContainsFunc(env.BranchRules, func(rule string) bool { return branchRule(rule, branch) }):
		warnings = append(warnings, fmt.Sprintf("The %s environment's branch rules don't include %s, so its jobs can't run. Add a branch rule for %s: %s", name, branch, branch, docs))
	}
	for _, rule := range env.ProtectionRules {
		if rule.Type == "required_reviewers" {
			warnings = append(warnings, fmt.Sprintf("The %s environment requires reviewers, so every release waits for a second approval after the merge. Merging the release pull request is the approval; remove the reviewers unless you want both: %s", name, docs))
		}
	}
	return warnings
}

// PublishDraft makes an existing draft release public, creating its tag on commit if the
// tag doesn't exist yet.
func (g *GitHub) PublishDraft(ctx context.Context, id int64, commit string, makeLatest bool) (*Release, error) {
	var r Release
	_, err := g.do(ctx, http.MethodPatch, fmt.Sprintf("/releases/%d", id), map[string]any{
		"draft": false, "target_commitish": commit, "make_latest": latest(makeLatest),
	}, &r)
	return &r, err
}

// UpdateNotes replaces a release's notes, leaving its tag and assets as they are.
func (g *GitHub) UpdateNotes(ctx context.Context, id int64, notes string) error {
	_, err := g.do(ctx, http.MethodPatch, fmt.Sprintf("/releases/%d", id), map[string]any{"body": notes}, nil)
	return err
}

// Comment is a comment on an issue or pull request.
type Comment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
}

// Comments lists the comments on an issue or pull request.
func (g *GitHub) Comments(ctx context.Context, number int) ([]Comment, error) {
	var all []Comment
	target := fmt.Sprintf("/issues/%d/comments?per_page=100", number)
	for target != "" {
		var page []Comment
		next, err := g.do(ctx, http.MethodGet, target, nil, &page)
		if err != nil {
			return nil, fmt.Errorf("list the comments on #%d in %s: %v", number, g.Repository, err)
		}
		all = append(all, page...)
		target = next
	}
	return all, nil
}

// CreateComment comments on an issue or pull request.
func (g *GitHub) CreateComment(ctx context.Context, number int, body string) error {
	_, err := g.do(ctx, http.MethodPost, fmt.Sprintf("/issues/%d/comments", number), map[string]string{"body": body}, nil)
	return err
}

// Description is a pull request's description, with what it takes to tell whether a run
// still reports on the pull request's current state.
type Description struct {
	Body string
	// Head is the pull request's head commit, and Open is false once it's merged or closed.
	Head string
	Open bool
}

// PullRequestDescription returns a pull request's description and its head commit.
func (g *GitHub) PullRequestDescription(ctx context.Context, number int) (Description, error) {
	var pr struct {
		Body  *string `json:"body"`
		State string  `json:"state"`
		Head  struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if _, err := g.do(ctx, http.MethodGet, fmt.Sprintf("/pulls/%d", number), nil, &pr); err != nil {
		return Description{}, fmt.Errorf("get pull request #%d in %s: %v", number, g.Repository, err)
	}
	d := Description{Head: pr.Head.SHA, Open: pr.State == "open"}
	if pr.Body != nil {
		d.Body = *pr.Body
	}
	return d, nil
}

// UpdatePullRequestBody replaces a pull request's description, open or merged.
func (g *GitHub) UpdatePullRequestBody(ctx context.Context, number int, body string) error {
	_, err := g.do(ctx, http.MethodPatch, fmt.Sprintf("/pulls/%d", number), map[string]string{"body": body}, nil)
	return err
}

// Run is a workflow run.
type Run struct {
	ID             int64  `json:"id"`
	HeadSHA        string `json:"head_sha"`
	Event          string `json:"event"`
	Conclusion     string `json:"conclusion"`
	HeadRepository struct {
		FullName string `json:"full_name"`
	} `json:"head_repository"`
}

// SuccessfulPullRequestRuns lists the successful pull request runs of a workflow file, such
// as release-planner.yml, for a head commit, newest first.
func (g *GitHub) SuccessfulPullRequestRuns(ctx context.Context, workflow, head string) ([]Run, error) {
	var page struct {
		Runs []Run `json:"workflow_runs"`
	}
	query := url.Values{"head_sha": {head}, "event": {"pull_request"}, "status": {"success"}, "per_page": {"100"}}
	if _, err := g.do(ctx, http.MethodGet, "/actions/workflows/"+url.PathEscape(workflow)+"/runs?"+query.Encode(), nil, &page); err != nil {
		return nil, fmt.Errorf("list the %s runs for %s in %s: %v", workflow, head, g.Repository, err)
	}
	return page.Runs, nil
}

// Artifacts lists the names of a workflow run's artifacts that haven't expired.
func (g *GitHub) Artifacts(ctx context.Context, run int64) ([]string, error) {
	var names []string
	target := fmt.Sprintf("/actions/runs/%d/artifacts?per_page=100", run)
	for target != "" {
		var page struct {
			Artifacts []struct {
				Name    string `json:"name"`
				Expired bool   `json:"expired"`
			} `json:"artifacts"`
		}
		next, err := g.do(ctx, http.MethodGet, target, nil, &page)
		if err != nil {
			return nil, fmt.Errorf("list the artifacts of run %d in %s: %v", run, g.Repository, err)
		}
		for _, a := range page.Artifacts {
			if !a.Expired {
				names = append(names, a.Name)
			}
		}
		target = next
	}
	return names, nil
}

// ArtifactID returns the ID of a workflow run's unexpired artifact with the name, or 0 if it
// has none.
func (g *GitHub) ArtifactID(ctx context.Context, run int64, name string) (int64, error) {
	var page struct {
		Artifacts []struct {
			ID      int64  `json:"id"`
			Name    string `json:"name"`
			Expired bool   `json:"expired"`
		} `json:"artifacts"`
	}
	query := url.Values{"name": {name}, "per_page": {"100"}}
	if _, err := g.do(ctx, http.MethodGet, fmt.Sprintf("/actions/runs/%d/artifacts?%s", run, query.Encode()), nil, &page); err != nil {
		return 0, fmt.Errorf("list the artifacts of run %d in %s: %v", run, g.Repository, err)
	}
	for _, a := range page.Artifacts {
		if a.Name == name && !a.Expired {
			return a.ID, nil
		}
	}
	return 0, nil
}

// RunJob is one job of a workflow run, at its latest attempt.
type RunJob struct {
	Name       string `json:"name"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"html_url"`
	RunAttempt int    `json:"run_attempt"`
}

// Jobs lists a workflow run's jobs, each at its latest attempt, which stands after Re-run
// failed jobs, in the order the API lists them.
func (g *GitHub) Jobs(ctx context.Context, run int64) ([]RunJob, error) {
	var jobs []RunJob
	index := map[string]int{}
	target := fmt.Sprintf("/actions/runs/%d/jobs?filter=all&per_page=100", run)
	for target != "" {
		var page struct {
			Jobs []RunJob `json:"jobs"`
		}
		next, err := g.do(ctx, http.MethodGet, target, nil, &page)
		if err != nil {
			return nil, fmt.Errorf("list the jobs of run %d in %s: %v", run, g.Repository, err)
		}
		for _, j := range page.Jobs {
			switch i, ok := index[j.Name]; {
			case !ok:
				index[j.Name] = len(jobs)
				jobs = append(jobs, j)
			case j.RunAttempt > jobs[i].RunAttempt:
				jobs[i] = j
			}
		}
		target = next
	}
	return jobs, nil
}

// DispatchWorkflow runs a workflow that has a workflow_dispatch trigger on the repository's
// default branch, with the given inputs.
func (g *GitHub) DispatchWorkflow(ctx context.Context, workflow string, inputs map[string]string) error {
	var repo struct {
		DefaultBranch string `json:"default_branch"`
	}
	if _, err := g.do(ctx, http.MethodGet, "", nil, &repo); err != nil {
		return fmt.Errorf("get %s: %v", g.Repository, err)
	}
	body := map[string]any{"ref": repo.DefaultBranch, "inputs": inputs}
	if _, err := g.do(ctx, http.MethodPost, "/actions/workflows/"+url.PathEscape(workflow)+"/dispatches", body, nil); err != nil {
		return fmt.Errorf("run %s in %s: %v", workflow, g.Repository, err)
	}
	return nil
}
