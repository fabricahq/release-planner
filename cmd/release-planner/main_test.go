package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cli(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestUsage(t *testing.T) {
	if code, out, _ := cli(t); code != 0 || !strings.Contains(out, "inventory") {
		t.Fatalf("%d %s", code, out)
	}
	if code, _, errOut := cli(t, "tag"); code != 2 || !strings.Contains(errOut, `unknown command "tag"`) {
		t.Fatalf("%d %s", code, errOut)
	}
}

func TestInstallCheckGuide(t *testing.T) {
	dir := t.TempDir()
	config := "version: v0.1.0\nfirst-version: v1.0.0\nvalidate:\n  run: make test\n"
	if err := os.WriteFile(filepath.Join(dir, "release-planner.yml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := cli(t, "install", "--dir", dir); code != 0 || !strings.Contains(out, "created   .github/workflows/release-planner.yml") {
		t.Fatalf("install: %d %s %s", code, out, errOut)
	}
	if code, out, _ := cli(t, "install", "--dir", dir); code != 0 || !strings.Contains(out, "Nothing to do.") {
		t.Fatalf("second install: %d %s", code, out)
	}
	if code, out, _ := cli(t, "check", "--dir", dir); code != 0 || !strings.Contains(out, "match") {
		t.Fatalf("check: %d %s", code, out)
	}
	workflow := filepath.Join(dir, ".github/workflows/release-planner.yml")
	data, _ := os.ReadFile(workflow)
	if err := os.WriteFile(workflow, append(data, "# edit\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := cli(t, "check", "--dir", dir); code != 1 || !strings.Contains(errOut, ".github/workflows/release-planner.yml: edited") {
		t.Fatalf("check after edit: %d %s", code, errOut)
	}
	if code, out, _ := cli(t, "guide", "--dir", dir); code != 0 || !strings.Contains(out, "# Prepare a release (Release Planner v0.1.0)") {
		t.Fatalf("guide: %d %s", code, out)
	}
}

func TestMissingConfig(t *testing.T) {
	if code, _, errOut := cli(t, "install", "--dir", t.TempDir()); code != 1 || !strings.Contains(errOut, "release-planner.yml not found") {
		t.Fatalf("%d %s", code, errOut)
	}
}
