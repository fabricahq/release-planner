# Release Planner agent guide

The [README](README.md) explains what Release Planner does and how repositories adopt it.

Release Planner is one Go command, `cmd/release-planner`, with packages under `internal/`. The files it generates into other repositories, the starter config and policy, the default release notes style, and the agent release procedure all come from `internal/generate/templates`. Every adopting repository regenerates those files on upgrade, so keep their output deterministic.

Keep dependencies to the standard library and `go.yaml.in/yaml/v3`. The publish job runs this code with a token that can write releases.

Run `gofmt -l . && go vet ./... && go test ./...` before you push.

The documentation site lives in `docs/`; see its README. When a command, config key, or generated file changes, update the page that describes it in the same pull request, and the README if its quick start or limits change.

## Releases

This repository releases itself with its own code from the approved commit, through `.github/workflows/release.yml`. When asked to make a release, draft or revise release notes, or retry a failed release, run `go run ./cmd/release-planner guide` and follow it. Read `.release-planner/policy.md` first. You prepare the release pull request; the maintainer approves the release by merging it. Never tag, publish, or merge.
