package publish

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/fabricahq/release-planner/internal/plan"
)

const (
	approved = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	other    = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

type ref struct{ typ, sha string }

// fakeGitHub stores tags and releases in memory and serves the endpoints the publisher uses.
type fakeGitHub struct {
	mu        sync.Mutex
	tags      map[string]ref
	annotated map[string]string
	releases  []Release
	writes    []string
	pageSize  int
}

func newFake() *fakeGitHub {
	return &fakeGitHub{tags: map[string]ref{}, annotated: map[string]string{}, pageSize: 100}
}

func (f *fakeGitHub) release(tag string, draft bool, body string) {
	f.releases = append(f.releases, Release{ID: int64(len(f.releases) + 1), TagName: tag, Name: tag, Body: body, Draft: draft})
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer token" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/repos/fabricahq/example")
	send := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch {
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
		send([]Release{f.releases[page-1]})
	case r.Method == http.MethodPost && path == "/releases":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		tag := body["tag_name"].(string)
		f.writes = append(f.writes, "create "+tag)
		if _, ok := f.tags[tag]; !ok {
			f.tags[tag] = ref{"commit", body["target_commitish"].(string)}
		}
		rel := Release{ID: int64(len(f.releases) + 1), TagName: tag, Name: body["name"].(string), Body: body["body"].(string),
			Prerelease: body["prerelease"].(bool), HTMLURL: "https://github.com/fabricahq/example/releases/tag/" + tag}
		f.releases = append(f.releases, rel)
		send(rel)
	case r.Method == http.MethodPatch && strings.HasPrefix(path, "/releases/"):
		var id int64
		fmt.Sscanf(strings.TrimPrefix(path, "/releases/"), "%d", &id)
		f.writes = append(f.writes, "publish draft")
		rel := &f.releases[id-1]
		rel.Draft = false
		if _, ok := f.tags[rel.TagName]; !ok {
			f.tags[rel.TagName] = ref{"commit", approved}
		}
		rel.HTMLURL = "https://github.com/fabricahq/example/releases/tag/" + rel.TagName
		send(rel)
	default:
		http.Error(w, "unexpected "+r.Method+" "+path, http.StatusTeapot)
	}
}

func run(t *testing.T, f *fakeGitHub, p plan.Plan) (Result, error) {
	t.Helper()
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	gh := &GitHub{BaseURL: server.URL, Token: "token", Repository: "fabricahq/example", HTTP: server.Client()}
	return Publish(context.Background(), gh, p, approved)
}

func minor() plan.Plan {
	return plan.Plan{Tags: []string{"v1.0.0"}, Tag: "v1.1.0", Version: "1.1.0", Commit: approved, Previous: "v1.0.0", Notes: "## Notes\n"}
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

func TestFirstRelease(t *testing.T) {
	f := newFake()
	if _, err := run(t, f, plan.Plan{Tags: []string{}, Tag: "v1.0.0", Commit: approved, Notes: "First"}); err != nil {
		t.Fatal(err)
	}
}

func TestPrerelease(t *testing.T) {
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

func TestRefusals(t *testing.T) {
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
		"commit mismatch":   {nil, func(p *plan.Plan) { p.Commit = other }, "does not match"},
		"no release":        {nil, func(p *plan.Plan) { p.Tag = "" }, "requests no release"},
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
