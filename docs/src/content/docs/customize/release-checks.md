---
title: Release checks
description: Run extra checks on a release before it is published.
---

Release checks are optional. They run on the exact commit being released, before anything is tagged. If they fail, nothing is published.

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
validate:
  go: '1.27.x'       # optional toolchains: go, node, python
  run: |
    go install golang.org/x/vuln/cmd/govulncheck@latest
    govulncheck ./...
```

The script runs on a standard GitHub-hosted Linux runner, without your repository's secrets. Install anything else it needs in the script itself.

## Run one of your workflows

For checks that need secrets, service containers, other runners, or a matrix, point to one of your own workflows:

```yaml
validate:
  workflow: release-checks.yml
```

The release workflow calls it with the commit to check and your repository's secrets. It never receives the token that can publish releases.

Your workflow must accept a `ref` input and check out that ref. On a retry, the release commit isn't the latest commit, so checking out the default would test the wrong code:

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

`release-planner install` and `check` fail if the workflow can't be called this way.

After changing `validate`, run `release-planner install` again and commit the result.
