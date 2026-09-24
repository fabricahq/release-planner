package publish

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
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
	HTMLURL    string `json:"html_url"`
}

var nextLink = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// errNotFound distinguishes an absent resource from a failed request.
type errNotFound struct{}

func (errNotFound) Error() string { return "not found" }

func (g *GitHub) do(ctx context.Context, method, target string, body any, out any) (next string, err error) {
	if !strings.HasPrefix(target, "http") {
		target = strings.TrimRight(g.BaseURL, "/") + "/repos/" + g.Repository + target
	}
	var reader io.Reader
	if body != nil {
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
	req.Header.Set("Authorization", "Bearer "+g.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := g.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return "", err
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
			return "", fmt.Errorf("%s %s: decode response: %w", method, req.URL.Path, err)
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

// CreateRelease publishes a release, creating the tag on commit if it does not exist yet.
func (g *GitHub) CreateRelease(ctx context.Context, tag, commit, notes string, prerelease bool) (*Release, error) {
	var r Release
	_, err := g.do(ctx, http.MethodPost, "/releases", map[string]any{
		"tag_name": tag, "target_commitish": commit, "name": tag, "body": notes,
		"draft": false, "prerelease": prerelease, "make_latest": latest(!prerelease),
	}, &r)
	return &r, err
}

// PublishDraft makes an existing draft release public.
func (g *GitHub) PublishDraft(ctx context.Context, id int64, makeLatest bool) (*Release, error) {
	var r Release
	_, err := g.do(ctx, http.MethodPatch, fmt.Sprintf("/releases/%d", id), map[string]any{
		"draft": false, "make_latest": latest(makeLatest),
	}, &r)
	return &r, err
}
