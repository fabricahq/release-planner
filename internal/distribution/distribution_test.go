package distribution

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchiveIsReproducible(t *testing.T) {
	entries := []Entry{{Name: Binary, Mode: 0o755, Data: []byte("bin")}, {Name: "LICENSE.md", Mode: 0o644, Data: []byte("MIT")}}
	a, err := Archive(entries)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Archive(entries)
	if !bytes.Equal(a, b) {
		t.Fatal("archives differ between builds")
	}
	gz, err := gzip.NewReader(bytes.NewReader(a))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	hdr, err := tr.Next()
	if err != nil || hdr.Name != Binary || hdr.Mode != 0o755 || hdr.ModTime.Unix() != 0 {
		t.Fatalf("%+v %v", hdr, err)
	}
	data, _ := io.ReadAll(tr)
	if string(data) != "bin" {
		t.Fatal(string(data))
	}
}

func TestChecksums(t *testing.T) {
	got := Checksums(map[string][]byte{"b": []byte("b"), "a": []byte("a")})
	want := "ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb  a\n3e23e8160039594a33894f6564e1b1348bbd7a0088d42c4acb73eeaed59c009d  b\n"
	if got != want {
		t.Fatalf("got %q", got)
	}
	if ArchiveName("v1.2.0", "linux/arm64") != "release-planner_1.2.0_linux_arm64.tar.gz" {
		t.Fatal(ArchiveName("v1.2.0", "linux/arm64"))
	}
}

// Building every target compiles four binaries, so it is skipped with -short.
func TestBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("cross-compiles every target")
	}
	source, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "dist")
	files, err := Build(context.Background(), source, "v9.9.9", out)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(Targets)+1 || files[len(files)-1] != "SHA256SUMS" {
		t.Fatal(files)
	}
	sums, _ := os.ReadFile(filepath.Join(out, "SHA256SUMS"))
	if strings.Count(string(sums), "\n") != len(Targets) {
		t.Fatal(string(sums))
	}
	// The native binary reports the stamped version.
	archive, _ := os.ReadFile(filepath.Join(out, ArchiveName("v9.9.9", "linux/amd64")))
	gz, _ := gzip.NewReader(bytes.NewReader(archive))
	tr := tar.NewReader(gz)
	if _, err := tr.Next(); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), Binary)
	data, _ := io.ReadAll(tr)
	if err := os.WriteFile(bin, data, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(bin, "version").Output(); err != nil || strings.TrimSpace(string(out)) != "v9.9.9" {
		t.Fatalf("version: %q %v", out, err)
	}
	if _, err := Build(context.Background(), source, "v9.9.9", out); err == nil {
		t.Fatal("overwrote an existing output directory")
	}
}
