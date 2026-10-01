# Release Planner agent guide

The [README](README.md) explains what Release Planner does and how repositories adopt it.

Release Planner is one Go command, `cmd/release-planner`, with packages under `internal/`. The files it generates into other repositories, the starter config and policy, the default release notes style, and the agent release procedure all come from `internal/generate/templates`. Every adopting repository regenerates those files on upgrade, so keep their output deterministic.

Keep dependencies to the standard library and `go.yaml.in/yaml/v3`. Every publish job, including this repository's, runs a released build of this code with a token that can write releases.

This repository releases itself the way adopters do, with the workflow `release-planner install` generates, pinned in `.release-planner/config.yml` to the previous release rather than the code being released. That way a release that breaks publishing can't stop its fix from publishing. After a release is out, bump `version` to it and run its `release-planner install` in a separate pull request. Don't edit the generated workflow or skills by hand.

Run `gofmt -l . && go vet ./... && go test ./...` before you push.

The documentation site lives in `docs/`; see its README. When a command, config key, or generated file changes, update the page that describes it in the same pull request, and the README if its quick start or limits change.

<!-- release-planner:begin v0.5.0 sha256:5911c1f9642cb6bb -->
## Releases

This repository publishes releases with [Release Planner](https://github.com/fabricahq/release-planner) v0.5.0. When asked to make a release, draft, revise, or correct release notes, or retry a failed release, print the release procedure and follow it:

```sh
release-planner guide
```

First check that `release-planner version` prints `v0.5.0`. If it doesn't, or `release-planner` isn't installed, install that version:

```sh
curl -fsSL https://raw.githubusercontent.com/fabricahq/release-planner/v0.5.0/install.sh | sh -s -- --version v0.5.0
```

Read `.release-planner/policy.md` first for this repository's release policy. You prepare the release pull request; the maintainer approves the release by merging it. Never tag, publish, or merge.
<!-- release-planner:end -->
