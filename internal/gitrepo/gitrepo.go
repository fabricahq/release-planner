// Package gitrepo runs read-only git commands against a working copy.
package gitrepo

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
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
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
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

// MergeBase returns the best common ancestor of two commits.
func (r Repo) MergeBase(ctx context.Context, a, b string) (string, error) {
	out, err := r.Run(ctx, "merge-base", a, b)
	return strings.TrimSpace(out), err
}

// Tags lists tag names starting with v.
func (r Repo) Tags(ctx context.Context) ([]string, error) {
	out, err := r.Run(ctx, "tag", "--list", "v*")
	return strings.Fields(out), err
}

var ownerName = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// GitHubRepository reads owner/name from the origin remote, for links to the repository.
func (r Repo) GitHubRepository(ctx context.Context) (string, error) {
	url, err := r.Run(ctx, "remote", "get-url", "origin")
	if err != nil {
		return "", fmt.Errorf("find the GitHub repository from the origin remote, or pass --repository owner/name: %v", err)
	}
	url = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(url), "/"), ".git")
	parts := strings.FieldsFunc(url, func(r rune) bool { return r == '/' || r == ':' })
	if len(parts) < 2 || !ValidRepository(parts[len(parts)-2]+"/"+parts[len(parts)-1]) {
		return "", fmt.Errorf("could not read owner/name from origin %q; pass --repository owner/name", url)
	}
	return parts[len(parts)-2] + "/" + parts[len(parts)-1], nil
}

// ValidRepository reports whether s is a GitHub repository's owner/name.
func ValidRepository(s string) bool { return ownerName.MatchString(s) }
