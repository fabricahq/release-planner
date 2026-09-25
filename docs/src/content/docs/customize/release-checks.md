---
title: Release checks
description: Run extra checks on a release before it is published.
---

Release checks are optional. They run on the release commit while the release pull request is open, so a failure shows up in the pull request's status comment before you approve anything. If they fail, nothing is published. When you merge, the release reuses that result; if the pull request's run can't be reused, for example because you merged before it finished, the checks run again on the same commit first.

## Do you need them?

Usually not for your regular tests. Your required pull request checks already test what merges, including the release pull request, so running them again at release wouldn't catch anything new.

Release checks are for things your pull requests don't check:

- **A smoke test of what you ship.** Build the real package or binary from the release commit and use it the way a user would.
- **An API compatibility check** against the previous release, which catches a breaking change released under a minor version. For example: `gorelease`, `cargo-semver-checks`, `buf breaking`, or an OpenAPI diff.
- **A fresh vulnerability scan**, because new advisories are published after code merges. For example: `govulncheck`, `npm audit`, or `osv-scanner`.
- **Upgrade tests** from the previous release, or **slow suites** you don't run on every pull request.

## Run a script

For simple checks, add a script to `.release-planner/config.yml`. Any failing command stops the release.

```yaml
release-checks:
  go: '1.27.x'       # optional toolchains: go, node, python
  run: |
    go install golang.org/x/vuln/cmd/govulncheck@latest
    govulncheck ./...
```

The script runs on a standard GitHub-hosted Linux runner, without your repository's secrets. Install anything else it needs in the script itself. To build files for the release, use [release assets](/customize/release-assets/) instead.

## Run one of your workflows

For checks that need secrets, service containers, other runners, or a matrix, point to one of your own workflows:

```yaml
release-checks:
  workflow: release-checks.yml
```

The release workflow calls it with the commit to check and your repository's secrets. It never receives the token that can publish releases. It runs on release pull requests from branches in your repository; a pull request from a fork gets no secrets, so its checks run after the merge instead.

Your workflow must accept a `ref` input and check out that ref. The release commit is usually not the latest commit, so checking out the default would test the wrong code:

```yaml
on:
  workflow_call:
    inputs:
      ref:
        type: string
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with:
          ref: ${{ inputs.ref }}
      - run: make release-checks
```

`release-planner install` and `check` fail if the workflow can't be called this way, or if it's `release-planner.yml` itself.

After changing `release-checks`, run `release-planner install` again and commit the result.
