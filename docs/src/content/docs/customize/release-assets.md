---
title: Release assets
description: Build files on the release pull request and attach them to the release.
---

Release assets are optional files attached to each GitHub release, such as binaries, archives, or a checksum manifest. One of your workflows builds them from the release commit while the release pull request is open, so a broken build shows up before you approve anything. Merging publishes exactly the files the pull request built.

## Set it up

Name your build workflow in `.release-planner/config.yml`, then run `release-planner install` and commit the result:

```yaml
release-assets:
  workflow: build-release.yml
```

The release workflow calls it with three string inputs:

- `ref`: the full SHA of the release commit
- `tag`: the release's tag, such as `v1.2.0`
- `version`: the version without the leading `v`, such as `1.2.0`

Your workflow must check out `ref`, build whatever the release needs, run any checks on what it built, and upload the files as **one artifact named `release-assets`**, with the files at its top level. For example:

```yaml
name: Build release
on:
  workflow_call:
    inputs:
      ref:
        type: string
        required: true
      tag:
        type: string
        required: true
      version:
        type: string
        required: true
permissions:
  contents: read
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with:
          ref: ${{ inputs.ref }}
          persist-credentials: false
      - run: make dist VERSION="$VERSION"
        env:
          VERSION: ${{ inputs.version }}
      - uses: actions/upload-artifact@v7
        with:
          name: release-assets
          path: dist/
          if-no-files-found: error
```

`release-planner install` and `check` fail if the workflow doesn't declare the three inputs as strings. Release Planner doesn't look inside the files, beyond requiring at least one, all regular files, with names made of letters, digits, and `.`, `_`, `+`, and `-`.

## What happens to the files

1. **On the release pull request**, the release workflow builds the assets from the release commit. The build runs with read access to the repository and none of its secrets.
2. An `attest` job signs each file's [build provenance](https://docs.github.com/en/actions/security-for-github-actions/using-artifact-attestations), so anyone can check it came from your release workflow.
3. The status comment on the pull request lists each file with its size and SHA-256.
4. **When you merge**, the publish job downloads the files the pull request built, verifies each file's attestation was made by this repository's `release-planner.yml`, uploads them to a draft release, checks GitHub's checksums, and publishes last.

If the pull request's files are gone, because the pull request's run didn't finish before the merge or its artifacts expired, the release workflow builds and attests them again from the same release commit after the merge.

A pull request from a fork builds nothing, because its workflow can't sign attestations; the files are built after the merge instead.

## Requirements

- Artifact attestations are available for public repositories, and for private and internal repositories on GitHub Enterprise Cloud.
- The workflow can't be `release-planner.yml`, the release workflow itself.

Once a release is published, its files never change. Editing a release's notes later leaves them as they are.
