// Package distribution builds the release archives and checksum manifest for Release Planner.
package distribution

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/fabricahq/release-planner/internal/semver"
)

// Targets are the platforms each release ships binaries for.
var Targets = []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64"}

// Binary is the executable's name inside each archive.
const Binary = "release-planner"

// ArchiveName is the file name the installer and the Release workflow download for a target.
func ArchiveName(version, target string) string {
	return fmt.Sprintf("%s_%s_%s.tar.gz", Binary, strings.TrimPrefix(version, "v"), strings.ReplaceAll(target, "/", "_"))
}

// Entry is one file in an archive.
type Entry struct {
	Name string
	Mode int64
	Data []byte
}

// Archive writes a reproducible tar.gz: fixed timestamps and ownership, entries in the order given.
func Archive(entries []Entry) ([]byte, error) {
	var buf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.Name, Mode: e.Mode, Size: int64(len(e.Data)), ModTime: time.Unix(0, 0).UTC(),
			Typeflag: tar.TypeReg, Uname: "root", Gname: "root", Format: tar.FormatPAX}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := tw.Write(e.Data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Checksums renders a SHA256SUMS manifest, sorted by file name.
func Checksums(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	var b strings.Builder
	for _, name := range names {
		sum := sha256.Sum256(files[name])
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	return b.String()
}

// Build cross-compiles every target from source, stamping version into the binary, and writes
// the archives and SHA256SUMS to out, which must not exist yet.
func Build(ctx context.Context, source, version, out string) ([]string, error) {
	if _, ok := semver.Parse(version); !ok {
		return nil, fmt.Errorf("%q is not a release version such as v1.2.0", version)
	}
	if _, err := os.Stat(out); err == nil {
		return nil, fmt.Errorf("%s already exists; use a new output directory", out)
	}
	license, err := os.ReadFile(filepath.Join(source, "LICENSE.md"))
	if err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp("", "release-planner-build")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	archives := map[string][]byte{}
	for _, target := range Targets {
		goos, goarch, _ := strings.Cut(target, "/")
		bin := filepath.Join(work, goos+"_"+goarch)
		cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-buildvcs=false",
			"-ldflags", "-s -w -buildid= -X github.com/fabricahq/release-planner/internal/buildinfo.stamped="+version,
			"-o", bin, "./cmd/release-planner")
		cmd.Dir = source
		cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
		if output, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("build %s: %v: %s", target, err, output)
		}
		data, err := os.ReadFile(bin)
		if err != nil {
			return nil, err
		}
		archive, err := Archive([]Entry{{Name: Binary, Mode: 0o755, Data: data}, {Name: "LICENSE.md", Mode: 0o644, Data: license}})
		if err != nil {
			return nil, err
		}
		archives[ArchiveName(version, target)] = archive
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, err
	}
	var written []string
	for name, data := range archives {
		if err := os.WriteFile(filepath.Join(out, name), data, 0o644); err != nil {
			return nil, err
		}
		written = append(written, name)
	}
	if err := os.WriteFile(filepath.Join(out, "SHA256SUMS"), []byte(Checksums(archives)), 0o644); err != nil {
		return nil, err
	}
	slices.Sort(written)
	return append(written, "SHA256SUMS"), nil
}
