package distribution

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// serveRelease serves one release's archive and SHA256SUMS the way GitHub's download URLs do.
func serveRelease(t *testing.T, version string, binary []byte, corrupt bool) *httptest.Server {
	t.Helper()
	archive, err := Archive([]Entry{{Name: Binary, Mode: 0o755, Data: binary}, {Name: "LICENSE.md", Mode: 0o644, Data: []byte("MIT")}})
	if err != nil {
		t.Fatal(err)
	}
	name := ArchiveName(version, runtime.GOOS+"/"+runtime.GOARCH)
	sums := Checksums(map[string][]byte{name: archive})
	if corrupt {
		archive = append(archive, 0)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/download/"+version+"/"+name, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(archive) })
	mux.HandleFunc("/download/"+version+"/SHA256SUMS", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(sums)) })
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/tag/"+version, http.StatusFound)
	})
	mux.HandleFunc("/tag/", func(w http.ResponseWriter, _ *http.Request) {})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func install(t *testing.T, server *httptest.Server, args ...string) (string, string, error) {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("install.sh supports Linux and macOS")
	}
	dir := t.TempDir()
	script, _ := filepath.Abs("../../install.sh")
	cmd := exec.Command("sh", append([]string{script, "--install-dir", dir}, args...)...)
	cmd.Env = append(os.Environ(), "RELEASE_PLANNER_DOWNLOAD_URL="+server.URL+"/download", "RELEASE_PLANNER_LATEST_URL="+server.URL+"/latest")
	out, err := cmd.CombinedOutput()
	return dir, string(out), err
}

func TestInstallScript(t *testing.T) {
	server := serveRelease(t, "v1.2.3", []byte("#!/bin/sh\necho v1.2.3\n"), false)

	dir, out, err := install(t, server, "--version", "v1.2.3")
	if err != nil || !strings.Contains(out, "Installed release-planner v1.2.3") {
		t.Fatalf("%v: %s", err, out)
	}
	got, err := exec.Command(filepath.Join(dir, Binary)).Output()
	if err != nil || strings.TrimSpace(string(got)) != "v1.2.3" {
		t.Fatalf("installed binary: %q %v", got, err)
	}

	// Without --version, the script follows GitHub's latest-release redirect.
	if _, out, err := install(t, server); err != nil || !strings.Contains(out, "v1.2.3") {
		t.Fatalf("latest: %v: %s", err, out)
	}
}

func TestInstallScriptRefusesBadDownloads(t *testing.T) {
	server := serveRelease(t, "v1.2.3", []byte("bin"), true)
	dir, out, err := install(t, server, "--version", "v1.2.3")
	if err == nil || !strings.Contains(out, "checksum mismatch") {
		t.Fatalf("%v: %s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, Binary)); !os.IsNotExist(statErr) {
		t.Fatal("installed a binary that failed verification")
	}
	if _, out, err := install(t, server, "--version", "main"); err == nil || !strings.Contains(out, "not a release version") {
		t.Fatalf("%v: %s", err, out)
	}
}
