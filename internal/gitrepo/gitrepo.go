// Package gitrepo runs read-only git commands against a working copy.
package gitrepo

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Repo is a git working copy.
type Repo struct {
	Dir string
}

// Run returns a git command's standard output, or an error that includes its standard error.
func (r Repo) Run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.Dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// Resolve returns the full commit SHA a ref points to, peeling annotated tags.
func (r Repo) Resolve(ctx context.Context, ref string) (string, error) {
	out, err := r.Run(ctx, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	return strings.TrimSpace(out), err
}

// IsAncestor reports whether ancestor is reachable from descendant.
func (r Repo) IsAncestor(ctx context.Context, ancestor, descendant string) bool {
	cmd := exec.CommandContext(ctx, "git", "merge-base", "--is-ancestor", ancestor, descendant)
	cmd.Dir = r.Dir
	return cmd.Run() == nil
}

// Tags lists tag names starting with v.
func (r Repo) Tags(ctx context.Context) ([]string, error) {
	out, err := r.Run(ctx, "tag", "--list", "v*")
	return strings.Fields(out), err
}
