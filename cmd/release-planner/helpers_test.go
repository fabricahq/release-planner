package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// repo is a throwaway git repository.
type repo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	r := &repo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", "main")
	return r
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *repo) write(name, body string) {
	r.t.Helper()
	file := filepath.Join(r.dir, name)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repo) commit(message string) string {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "--allow-empty", "-m", message)
	return r.git("rev-parse", "HEAD")
}

// api is a fake GitHub API that serves canned JSON by "METHOD /path" (without the query) and
// records every request.
type fakeAPI struct {
	mu       sync.Mutex
	routes   map[string]any
	requests []string
	bodies   []string
	server   *httptest.Server
}

// status is a canned response with a status code other than 200.
type status struct {
	code int
	body any
}

// newAPI starts a fake API and points GITHUB_API_URL at it, for repository fabricahq/example.
func newAPI(t *testing.T, routes map[string]any) *fakeAPI {
	t.Helper()
	f := &fakeAPI{routes: routes}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		key := r.Method + " " + strings.TrimPrefix(r.URL.Path, "/repos/fabricahq/example")
		body, _ := io.ReadAll(r.Body)
		f.requests = append(f.requests, key)
		f.bodies = append(f.bodies, string(body))
		response, ok := f.routes[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if s, ok := response.(status); ok {
			w.WriteHeader(s.code)
			response = s.body
		}
		if response != nil {
			_ = json.NewEncoder(w).Encode(response)
		}
	}))
	t.Cleanup(f.server.Close)
	t.Setenv("GITHUB_API_URL", f.server.URL)
	t.Setenv("GITHUB_TOKEN", "token")
	t.Setenv("GITHUB_REPOSITORY", "fabricahq/example")
	return f
}

// sent returns the body of the first request to key, or "" if there was none.
func (f *fakeAPI) sent(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, r := range f.requests {
		if r == key {
			return f.bodies[i]
		}
	}
	return ""
}

// actionsFiles points GITHUB_OUTPUT and GITHUB_STEP_SUMMARY at temporary files and returns
// a function that reads one of them.
func actionsFiles(t *testing.T) func(name string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GITHUB_OUTPUT", filepath.Join(dir, "output"))
	t.Setenv("GITHUB_STEP_SUMMARY", filepath.Join(dir, "summary"))
	return func(name string) string {
		data, _ := os.ReadFile(filepath.Join(dir, name))
		return string(data)
	}
}
