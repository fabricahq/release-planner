package publish

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/fabricahq/release-planner/internal/plan"
)

const (
	// approved is the release commit, and head and merged the release pull request's head
	// and the commit it merged as.
	approved = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	other    = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	head     = "cccccccccccccccccccccccccccccccccccccccc"
	merged   = "dddddddddddddddddddddddddddddddddddddddd"
)

type ref struct{ typ, sha string }

var mergedAt = "2026-09-24T12:00:00Z"

// fakeGitHub stores tags and releases in memory and serves the endpoints the publisher uses.
type fakeGitHub struct {
	mu          sync.Mutex
	tags        map[string]ref
	annotated   map[string]string
	releases    []Release
	targets     map[int64]string
	writes      []string
	pageSize    int
	host        string
	hideDigests bool
	content     map[int64][]byte
	pulls       map[int]pull
	server      *httptest.Server
}

// pull is a pull request as the commits/{sha}/pulls endpoint lists it for commit.
type pull struct {
	commit, merge, base, head string
	mergedAt                  *string
}

func newFake() *fakeGitHub {
	return &fakeGitHub{tags: map[string]ref{}, annotated: map[string]string{}, targets: map[int64]string{}, content: map[int64][]byte{}, pageSize: 100,
		pulls: map[int]pull{7: {commit: merged, merge: merged, base: "main", head: head, mergedAt: &mergedAt}}}
}

func (f *fakeGitHub) release(tag string, draft bool, body string) {
	f.releases = append(f.releases, Release{ID: int64(len(f.releases) + 1), TagName: tag, Name: tag, Body: body, Draft: draft, TargetCommitish: approved,
		HTMLURL: "https://github.com/fabricahq/example/releases/tag/" + tag})
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer token" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/repos/fabricahq/example")
	f.host = r.Host
	send := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch {
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/commits/"):
		sha := strings.TrimSuffix(strings.TrimPrefix(path, "/commits/"), "/pulls")
		var pulls []map[string]any
		for number, pr := range f.pulls {
			if pr.commit == sha {
				pulls = append(pulls, map[string]any{"number": number, "merged_at": pr.mergedAt, "merge_commit_sha": pr.merge, "base": map[string]string{"ref": pr.base}})
			}
		}
		send(pulls)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/pulls/"):
		var number int
		fmt.Sscanf(strings.TrimPrefix(path, "/pulls/"), "%d", &number)
		send(map[string]any{"head": map[string]any{"sha": f.pulls[number].head, "repo": map[string]string{"full_name": "fabricahq/example"}}, "merged_by": map[string]string{"login": "mona"}})
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/assets"):
		var id int64
		fmt.Sscanf(strings.TrimPrefix(path, "/releases/"), "%d", &id)
		data, _ := io.ReadAll(r.Body)
		name := r.URL.Query().Get("name")
		f.writes = append(f.writes, "upload "+name)
		sum := sha256.Sum256(data)
		assetID := int64(1000 + len(f.content))
		f.content[assetID] = data
		asset := Asset{ID: assetID, Name: name, Size: int64(len(data)), Digest: "sha256:" + hex.EncodeToString(sum[:]),
			URL: fmt.Sprintf("http://%s/repos/fabricahq/example/releases/assets/%d", r.Host, assetID)}
		f.releases[id-1].Assets = append(f.releases[id-1].Assets, asset)
		send(asset)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/releases/assets/"):
		var id int64
		fmt.Sscanf(strings.TrimPrefix(path, "/releases/assets/"), "%d", &id)
		_, _ = w.Write(f.content[id])
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/releases/"):
		var id int64
		fmt.Sscanf(strings.TrimPrefix(path, "/releases/"), "%d", &id)
		send(f.view(f.releases[id-1]))
	case r.Method == http.MethodGet && path == "/git/matching-refs/tags/v":
		var refs []map[string]string
		for t := range f.tags {
			if strings.HasPrefix(t, "v") {
				refs = append(refs, map[string]string{"ref": "refs/tags/" + t})
			}
		}
		send(refs)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/git/ref/tags/"):
		t, ok := f.tags[strings.TrimPrefix(path, "/git/ref/tags/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		send(map[string]any{"object": map[string]string{"type": t.typ, "sha": t.sha}})
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/git/tags/"):
		send(map[string]any{"object": map[string]string{"sha": f.annotated[strings.TrimPrefix(path, "/git/tags/")]}})
	case r.Method == http.MethodGet && path == "/releases":
		// Serve one release per page to exercise pagination.
		page := 0
		fmt.Sscanf(r.URL.Query().Get("page"), "%d", &page)
		if page < 1 {
			page = 1
		}
		if page < len(f.releases) {
			w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/fabricahq/example/releases?per_page=100&page=%d>; rel="next"`, r.Host, page+1))
		}
		if page > len(f.releases) {
			send([]Release{})
			return
		}
		send([]Release{f.view(f.releases[page-1])})
	case r.Method == http.MethodPost && path == "/releases":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		tag := body["tag_name"].(string)
		draft := body["draft"].(bool)
		f.writes = append(f.writes, map[bool]string{true: "draft ", false: "create "}[draft]+tag)
		id := int64(len(f.releases) + 1)
		f.targets[id] = body["target_commitish"].(string)
		// Like GitHub, a draft creates no tag until it is published.
		if _, ok := f.tags[tag]; !ok && !draft {
			f.tags[tag] = ref{"commit", f.targets[id]}
		}
		rel := Release{ID: id, TagName: tag, Name: body["name"].(string), Body: body["body"].(string), Draft: draft, TargetCommitish: f.targets[id],
			Prerelease: body["prerelease"].(bool), HTMLURL: "https://github.com/fabricahq/example/releases/tag/" + tag,
			UploadURL: fmt.Sprintf("http://%s/repos/fabricahq/example/releases/%d/assets{?name,label}", r.Host, id)}
		f.releases = append(f.releases, rel)
		send(f.view(rel))
	case r.Method == http.MethodPatch && strings.HasPrefix(path, "/releases/"):
		var id int64
		fmt.Sscanf(strings.TrimPrefix(path, "/releases/"), "%d", &id)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		rel := &f.releases[id-1]
		if notes, ok := body["body"].(string); ok {
			f.writes = append(f.writes, "edit "+rel.TagName)
			rel.Body = notes
			send(f.view(*rel))
			return
		}
		f.writes = append(f.writes, "publish draft")
		rel.Draft = false
		// Like GitHub, a target in the request replaces the draft's own.
		target := rel.TargetCommitish
		if t, ok := body["target_commitish"].(string); ok {
			target = t
		}
		if _, ok := f.tags[rel.TagName]; !ok {
			f.tags[rel.TagName] = ref{"commit", target}
		}
		rel.HTMLURL = "https://github.com/fabricahq/example/releases/tag/" + rel.TagName
		send(f.view(*rel))
	default:
		http.Error(w, "unexpected "+r.Method+" "+path, http.StatusTeapot)
	}
}

// view returns a release as the API would, optionally without GitHub's asset digests.
func (f *fakeGitHub) view(r Release) Release {
	if f.hideDigests {
		r.Assets = append([]Asset(nil), r.Assets...)
		for i := range r.Assets {
			r.Assets[i].Digest = ""
		}
	}
	return r
}

func run(t *testing.T, f *fakeGitHub, p plan.Plan) (Result, error) {
	return runWith(t, f, p, nil)
}

func runWith(t *testing.T, f *fakeGitHub, p plan.Plan, assets []File) (Result, error) {
	t.Helper()
	// Keep one server per fake, so URLs it returned earlier stay on the same origin.
	if f.server == nil {
		f.server = httptest.NewServer(f)
		t.Cleanup(f.server.Close)
	}
	gh := &GitHub{BaseURL: f.server.URL, Token: "token", Repository: "fabricahq/example", HTTP: f.server.Client()}
	return Publish(context.Background(), gh, p, "main", assets)
}

func minor() plan.Plan {
	return plan.Plan{Tags: []string{"v1.0.0"}, Tag: "v1.1.0", Version: "1.1.0", Commit: approved, Previous: "v1.0.0", Notes: "## Notes\n",
		Head: head, PullRequest: 7, Merged: merged}
}

func withPrevious() *fakeGitHub {
	f := newFake()
	f.tags["v1.0.0"] = ref{"commit", other}
	f.release("v1.0.0", false, "First")
	return f
}

func TestCreatesTagAndRelease(t *testing.T) {
	f := withPrevious()
	res, err := run(t, f, minor())
	if err != nil {
		t.Fatal(err)
	}
	if res.AlreadyPublished || res.URL != "https://github.com/fabricahq/example/releases/tag/v1.1.0" {
		t.Fatalf("%+v", res)
	}
	if f.tags["v1.1.0"].sha != approved || f.releases[1].Body != "## Notes\n" || len(f.writes) != 1 {
		t.Fatalf("tags %v releases %+v writes %v", f.tags, f.releases, f.writes)
	}
}

func TestPublishesAFirstRelease(t *testing.T) {
	f := newFake()
	if _, err := run(t, f, plan.Plan{Tags: []string{}, Tag: "v1.0.0", Commit: approved, Notes: "First", Head: head, PullRequest: 7, Merged: merged}); err != nil {
		t.Fatal(err)
	}
}

func TestMarksPrereleases(t *testing.T) {
	f := withPrevious()
	p := minor()
	p.Tag, p.Prerelease = "v1.1.0-rc.1", true
	if _, err := run(t, f, p); err != nil {
		t.Fatal(err)
	}
	if !f.releases[1].Prerelease {
		t.Fatal("not marked as a prerelease")
	}
}

func TestRetryAfterPublicationMakesNoWrites(t *testing.T) {
	f := withPrevious()
	if _, err := run(t, f, minor()); err != nil {
		t.Fatal(err)
	}
	f.writes = nil
	res, err := run(t, f, minor())
	if err != nil || !res.AlreadyPublished || len(f.writes) != 0 {
		t.Fatalf("%+v %v %v", res, err, f.writes)
	}
}

// Retrying the original release after a later notes edit keeps the edited notes, but still
// refuses other assets or a tag on another commit.
func TestRetryAfterANotesEditKeepsTheNotes(t *testing.T) {
	f := withPrevious()
	if _, err := run(t, f, minor()); err != nil {
		t.Fatal(err)
	}
	f.releases[1].Body = "## Corrected notes"
	f.writes = nil
	res, err := run(t, f, minor())
	if err != nil || !res.AlreadyPublished || !res.NotesChanged || len(f.writes) != 0 || f.releases[1].Body != "## Corrected notes" {
		t.Fatalf("%+v %v %v", res, err, f.writes)
	}
	if _, err := runWith(t, f, minor(), files(t, map[string]string{"a.tar.gz": "a"})); err == nil || !strings.Contains(err.Error(), "is already published, but") {
		t.Fatal(err)
	}
	f.tags["v1.1.0"] = ref{"commit", other}
	if _, err := run(t, f, minor()); err == nil || !strings.Contains(err.Error(), "already points to") {
		t.Fatal(err)
	}
}

func TestPublishesMatchingDraft(t *testing.T) {
	f := withPrevious()
	f.release("v1.1.0", true, "## Notes")
	f.tags["v1.1.0"] = ref{"commit", approved}
	if _, err := run(t, f, minor()); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 1 || f.writes[0] != "publish draft" {
		t.Fatal(f.writes)
	}
}

func TestAcceptsAnnotatedTagOnApprovedCommit(t *testing.T) {
	f := withPrevious()
	f.tags["v1.1.0"] = ref{"tag", "tagobject"}
	f.annotated["tagobject"] = approved
	if _, err := run(t, f, minor()); err != nil {
		t.Fatal(err)
	}
}

func TestRefusesUnsafePublication(t *testing.T) {
	for name, tc := range map[string]struct {
		setup func(*fakeGitHub)
		plan  func(*plan.Plan)
		want  string
	}{
		"tag on another commit": {func(f *fakeGitHub) {
			f.tags["v1.1.0"] = ref{"tag", "tagobject"}
			f.annotated["tagobject"] = other
		}, nil, "already points to"},
		"different release": {func(f *fakeGitHub) { f.release("v1.1.0", true, "Other") }, nil, "differs from the approved notes"},
		"tags changed":      {func(f *fakeGitHub) { f.tags["v1.2.0"] = ref{"commit", other} }, nil, "changed since planning"},
		"draft previous":    {func(f *fakeGitHub) { f.releases[0].Draft = true }, nil, "publish v1.0.0 first"},
		"missing previous":  {func(f *fakeGitHub) { f.releases = nil }, nil, "publish v1.0.0 first"},
		"short commit":      {nil, func(p *plan.Plan) { p.Commit = "aaaaaaa" }, "not a full commit SHA"},
		"no release":        {nil, func(p *plan.Plan) { p.Tag = "" }, "requests no release"},
		"not merged":        {nil, func(p *plan.Plan) { p.Merged = "" }, "names no merged pull request"},
		"other head":        {nil, func(p *plan.Plan) { p.Head = other }, "not the planned #7"},
		"other pull":        {nil, func(p *plan.Plan) { p.PullRequest = 8 }, "not the planned #8"},
		"direct push":       {func(f *fakeGitHub) { f.pulls = nil }, nil, "a direct push publishes nothing"},
		"unmerged pull": {func(f *fakeGitHub) {
			f.pulls = map[int]pull{7: {commit: merged, merge: merged, base: "main", head: head}}
		}, nil, "a direct push publishes nothing"},
		"other base": {func(f *fakeGitHub) {
			f.pulls = map[int]pull{7: {commit: merged, merge: merged, base: "dev", head: head, mergedAt: &mergedAt}}
		}, nil, "a direct push publishes nothing"},
		"commit in a pull": {func(f *fakeGitHub) {
			f.pulls = map[int]pull{7: {commit: merged, merge: other, base: "main", head: head, mergedAt: &mergedAt}}
		}, nil, "a direct push publishes nothing"},
	} {
		t.Run(name, func(t *testing.T) {
			f := withPrevious()
			if tc.setup != nil {
				tc.setup(f)
			}
			p := minor()
			if tc.plan != nil {
				tc.plan(&p)
			}
			_, err := run(t, f, p)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
			if len(f.writes) != 0 {
				t.Fatalf("a refused publication wrote: %v", f.writes)
			}
		})
	}
}

// A notes edit replaces only the body of a published release, and does nothing when the
// release already has the notes.
func TestEditsPublishedNotes(t *testing.T) {
	f := withPrevious()
	edit := plan.Plan{Tags: []string{}, Head: head, PullRequest: 7, Merged: merged, Edits: []plan.Edit{{Tag: "v1.0.0", File: "_releases/v1.0.0.md", Notes: "Corrected"}}}
	res, err := run(t, f, edit)
	if err != nil || len(res.Edited) != 1 || !res.Edited[0].Changed || res.Edited[0].URL != "https://github.com/fabricahq/example/releases/tag/v1.0.0" {
		t.Fatalf("%+v %v", res, err)
	}
	if strings.Join(f.writes, ",") != "edit v1.0.0" || f.releases[0].Body != "Corrected" || f.tags["v1.0.0"].sha != other {
		t.Fatalf("writes %v releases %+v", f.writes, f.releases)
	}
	f.writes = nil
	if res, err := run(t, f, edit); err != nil || res.Edited[0].Changed || len(f.writes) != 0 {
		t.Fatalf("repeat: %+v %v %v", res, err, f.writes)
	}

	// With a release, the release publishes first, then the edit.
	both := minor()
	both.Edits = []plan.Edit{{Tag: "v1.0.0", Notes: "Corrected again"}}
	f.writes = nil
	if _, err := run(t, f, both); err != nil || strings.Join(f.writes, ",") != "create v1.1.0,edit v1.0.0" {
		t.Fatalf("%v %v", err, f.writes)
	}

	f.releases[0].Draft = true
	f.writes = nil
	if _, err := run(t, f, edit); err == nil || !strings.Contains(err.Error(), "no published release") || len(f.writes) != 0 {
		t.Fatalf("draft: %v %v", err, f.writes)
	}
}

func files(t *testing.T, contents map[string]string) []File {
	t.Helper()
	dir := t.TempDir()
	for name, body := range contents {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	assets, err := ReadAssets(dir)
	if err != nil {
		t.Fatal(err)
	}
	return assets
}

func TestPublishesAssetsThroughADraft(t *testing.T) {
	f := withPrevious()
	assets := files(t, map[string]string{"tool_linux_amd64.tar.gz": "binary", "SHA256SUMS": "sums"})
	res, err := runWith(t, f, minor(), assets)
	if err != nil {
		t.Fatal(err)
	}
	// The draft comes first and publication last; the uploads between may run in any order.
	if n := len(f.writes); n != 4 || f.writes[0] != "draft v1.1.0" || f.writes[n-1] != "publish draft" {
		t.Fatalf("writes %v", f.writes)
	}
	uploads := slices.Sorted(slices.Values(f.writes[1:3]))
	if want := []string{"upload SHA256SUMS", "upload tool_linux_amd64.tar.gz"}; !slices.Equal(uploads, want) {
		t.Fatalf("uploads %v, want %v", uploads, want)
	}
	if f.tags["v1.1.0"].sha != approved || f.releases[1].Draft || len(f.releases[1].Assets) != 2 || res.AlreadyPublished {
		t.Fatalf("%+v %+v", f.releases[1], res)
	}

	// A retry after success changes nothing.
	f.writes = nil
	res, err = runWith(t, f, minor(), assets)
	if err != nil || !res.AlreadyPublished || len(f.writes) != 0 {
		t.Fatalf("retry: %+v %v %v", res, err, f.writes)
	}
	// A published release with different assets is never accepted.
	other := files(t, map[string]string{"tool_linux_amd64.tar.gz": "binarz", "SHA256SUMS": "sums"})
	if _, err := runWith(t, f, minor(), other); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatal(err)
	}
}

func TestResumesAnInterruptedDraft(t *testing.T) {
	f := withPrevious()
	assets := files(t, map[string]string{"a.tar.gz": "a", "b.tar.gz": "b"})
	// A previous run created the draft and uploaded one asset before failing.
	if _, err := runWith(t, f, minor(), assets[:1]); err != nil {
		t.Fatal(err)
	}
	f.releases[1].Draft = true
	delete(f.tags, "v1.1.0")
	f.writes = nil
	if _, err := runWith(t, f, minor(), assets); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.writes, ",") != "upload b.tar.gz,publish draft" {
		t.Fatalf("writes %v", f.writes)
	}
}

func TestRefusesAConflictingDraftAsset(t *testing.T) {
	f := withPrevious()
	if _, err := runWith(t, f, minor(), files(t, map[string]string{"a.tar.gz": "old"})); err != nil {
		t.Fatal(err)
	}
	f.releases[1].Draft = true
	delete(f.tags, "v1.1.0")
	f.writes = nil
	_, err := runWith(t, f, minor(), files(t, map[string]string{"a.tar.gz": "new"}))
	if err == nil || !strings.Contains(err.Error(), "already has a different a.tar.gz") || len(f.writes) != 0 {
		t.Fatalf("%v %v", err, f.writes)
	}
}

func TestVerifiesAssetsWithoutGitHubDigests(t *testing.T) {
	f := withPrevious()
	f.hideDigests = true
	if _, err := runWith(t, f, minor(), files(t, map[string]string{"a.tar.gz": "a"})); err != nil {
		t.Fatal(err)
	}
}

// A matching draft left on another commit must not be published: its tag would be
// created on that commit instead of the approved one.
func TestRefusesADraftTargetingAnotherCommit(t *testing.T) {
	f := withPrevious()
	f.release("v1.1.0", true, "## Notes\n")
	f.releases[len(f.releases)-1].TargetCommitish = other
	if _, err := run(t, f, minor()); err == nil || !strings.Contains(err.Error(), "draft targets "+other) {
		t.Fatal(err)
	}
	if len(f.writes) != 0 {
		t.Fatalf("wrote before refusing: %v", f.writes)
	}
	if _, ok := f.tags["v1.1.0"]; ok {
		t.Fatal("created the tag")
	}
}

func TestNeverSendsTheTokenElsewhere(t *testing.T) {
	gh := &GitHub{BaseURL: "https://api.github.com", Token: "token", Repository: "fabricahq/example"}
	for target, ok := range map[string]bool{
		"https://api.github.com/repos/fabricahq/example/releases/assets/1":                  true,
		"https://uploads.github.com/repos/fabricahq/example/releases/1/assets?name=a":       true,
		"https://api.github.com/repos/fabricahq/example/releases?page=2":                    true,
		"https://evil.example/repos/fabricahq/example/releases/assets/1":                    false,
		"http://api.github.com/repos/fabricahq/example/releases/assets/1":                   false,
		"https://api.github.com/repos/other/example/releases/assets/1":                      false,
		"https://api.github.com/repos/fabricahq/example/../../other/example/releases/1":     false,
		"https://user@api.github.com/repos/fabricahq/example/releases/assets/1":             false,
		"https://uploads.github.com.evil.example/repos/fabricahq/example/releases/1/assets": false,
	} {
		if err := gh.trusted(target); (err == nil) != ok {
			t.Errorf("trusted(%q) = %v, want ok %v", target, err, ok)
		}
	}
	ghes := &GitHub{BaseURL: "https://ghe.example/api/v3", Token: "token", Repository: "fabricahq/example"}
	for _, target := range []string{"https://ghe.example/api/v3/repos/fabricahq/example/releases/assets/1", "https://ghe.example/api/uploads/repos/fabricahq/example/releases/1/assets"} {
		if err := ghes.trusted(target); err != nil {
			t.Error(err)
		}
	}
	// An asset URL outside the repository is refused before any request is made.
	if _, err := gh.AssetDigest(context.Background(), Asset{Name: "a", URL: "https://evil.example/a"}); err == nil || !strings.Contains(err.Error(), "refusing to send the token") {
		t.Fatal(err)
	}
}

func TestReadAssetsRejectsUnsafeNames(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "has space.tar.gz"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAssets(dir); err == nil {
		t.Fatal("accepted an unsafe name")
	}
	if _, err := ReadAssets(t.TempDir()); err == nil || !strings.Contains(err.Error(), "no files") {
		t.Fatal(err)
	}
}

// Contributor lookups say which repository, pull request, handle, and release they checked.
func TestContributorLookupErrorsNameWhatTheyChecked(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	gh := &GitHub{BaseURL: server.URL, Repository: "fabricahq/example", HTTP: server.Client()}
	if _, err := gh.PullRequestAuthor(context.Background(), 7); err == nil || !strings.Contains(err.Error(), "get pull request #7 in fabricahq/example: not found") {
		t.Fatal(err)
	}
	if _, err := gh.ContributedBefore(context.Background(), "octocat", "v1.0.0"); err == nil || !strings.Contains(err.Error(), "list commits by @octocat in fabricahq/example at v1.0.0: not found") {
		t.Fatal(err)
	}
}

func TestEnvironmentWarningsExplainRiskySettings(t *testing.T) {
	env := func(body string, rules []string, protected bool) *Environment {
		t.Helper()
		var e Environment
		if err := json.Unmarshal([]byte(body), &e); err != nil {
			t.Fatal(err)
		}
		e.BranchRules, e.BranchProtected = rules, protected
		return &e
	}
	const custom = `{"deployment_branch_policy":{"protected_branches":false,"custom_branch_policies":true},"protection_rules":[{"type":"branch_policy"}]}`
	const protectedOnly = `{"deployment_branch_policy":{"protected_branches":true,"custom_branch_policies":false},"protection_rules":[]}`
	for name, tc := range map[string]struct {
		env  *Environment
		want []string
	}{
		"missing":                 {nil, []string{"doesn't exist"}},
		"no branch rule":          {env(`{"deployment_branch_policy":null,"protection_rules":[]}`, nil, false), []string{"no deployment branch rule"}},
		"reviewers":               {env(`{"deployment_branch_policy":{"protected_branches":false,"custom_branch_policies":true},"protection_rules":[{"type":"required_reviewers"},{"type":"branch_policy"}]}`, []string{"main"}, false), []string{"requires reviewers"}},
		"both":                    {env(`{"deployment_branch_policy":null,"protection_rules":[{"type":"required_reviewers"}]}`, nil, false), []string{"no deployment branch rule", "requires reviewers"}},
		"recommended":             {env(custom, []string{"main"}, false), nil},
		"pattern includes branch": {env(custom, []string{"develop", "ma*"}, false), nil},
		"rules exclude branch":    {env(custom, []string{"develop"}, false), []string{"branch rules don't include main"}},
		"no rules yet":            {env(custom, nil, false), []string{"branch rules don't include main"}},
		"protected":               {env(protectedOnly, nil, true), nil},
		"unprotected":             {env(protectedOnly, nil, false), []string{"main isn't protected"}},
	} {
		t.Run(name, func(t *testing.T) {
			got := EnvironmentWarnings("release", "main", EnvironmentDocs, tc.env)
			if len(got) != len(tc.want) {
				t.Fatalf("got %q, want %d warnings", got, len(tc.want))
			}
			for i, want := range tc.want {
				if !strings.Contains(got[i], want) || !strings.Contains(got[i], EnvironmentDocs) {
					t.Errorf("warning %q lacks %q or the docs link", got[i], want)
				}
			}
		})
	}
}

func TestBranchRulesUseFnmatchPatterns(t *testing.T) {
	for _, tc := range []struct {
		pattern, branch string
		want            bool
	}{
		{"main", "main", true},
		{"main", "maint", false},
		{"release/*", "release/v1", true},
		{"release/*", "release/v1/fix", false},
		{"release/**", "release/v1/fix", true},
		{"v?", "v1", true},
		{"[mt]ain", "tain", true},
		{"[!m]ain", "main", false},
		{"a.b", "axb", false},
	} {
		if got := branchRule(tc.pattern, tc.branch); got != tc.want {
			t.Errorf("branchRule(%q, %q) = %v, want %v", tc.pattern, tc.branch, got, tc.want)
		}
	}
}

func TestEnvironmentReadsSettingsOrReportsMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/fabricahq/example/environments/release":
			_, _ = w.Write([]byte(`{"deployment_branch_policy":{"protected_branches":false,"custom_branch_policies":true},"protection_rules":[{"type":"required_reviewers"}]}`))
		case "/repos/fabricahq/example/environments/release/deployment-branch-policies":
			_, _ = w.Write([]byte(`{"branch_policies":[{"name":"main","type":"branch"},{"name":"v*","type":"tag"}]}`))
		case "/repos/fabricahq/example/environments/protected":
			_, _ = w.Write([]byte(`{"deployment_branch_policy":{"protected_branches":true,"custom_branch_policies":false},"protection_rules":[]}`))
		case "/repos/fabricahq/example/branches/main":
			_, _ = w.Write([]byte(`{"name":"main","protected":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	gh := &GitHub{BaseURL: server.URL, Token: "token", Repository: "fabricahq/example", HTTP: server.Client()}
	env, err := gh.Environment(context.Background(), "release", "main")
	if err != nil || env == nil || len(env.ProtectionRules) != 1 || !slices.Equal(env.BranchRules, []string{"main"}) {
		t.Fatalf("%+v %v", env, err)
	}
	if env, err := gh.Environment(context.Background(), "protected", "main"); err != nil || !env.BranchProtected {
		t.Fatalf("protected: %+v %v", env, err)
	}
	if env, err := gh.Environment(context.Background(), "staging", "main"); env != nil || err != nil {
		t.Fatalf("missing environment: %+v %v", env, err)
	}
}
