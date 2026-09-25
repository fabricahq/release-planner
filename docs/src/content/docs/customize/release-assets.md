---
title: Release assets
description: Build, attest, and attach files such as binaries to each release.
---

Release assets are optional. They're files attached to each release, such as binaries for each platform and a checksum manifest. One of your workflows builds them from the exact commit being released, and the Release workflow attests them and publishes them with the notes.

## Name your build workflow

Add it to `.release-planner/config.yml`:

```yaml
release-assets:
  workflow: build-release.yml
```

After any [release checks](/customize/release-checks/) pass, the Release workflow calls it with the commit to build, the tag, and the version, which is the tag without its leading `v`. It runs with read access to your repository and none of its secrets, so its jobs can't ask for more permissions than `contents: read`.

Your workflow must:

- Accept string inputs named `ref`, `tag`, and `version`, and check out `ref`. On a retry, the release commit isn't the latest commit, so checking out the default would build the wrong code.
- Run whatever checks the release needs before uploading, such as tests of the built binaries. Release Planner doesn't check what you build, so `release-checks` is optional when your build already covers them.
- Upload the files, flat, as one artifact named `release-assets`.

```yaml
on:
  workflow_call:
    inputs:
      ref:
        type: string
      tag:
        type: string
      version:
        type: string
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with:
          ref: ${{ inputs.ref }}
      - run: make dist VERSION="$VERSION" DEST=dist
        env:
          VERSION: ${{ inputs.version }}
      - uses: actions/upload-artifact@v7
        with:
          name: release-assets
          path: dist/
          if-no-files-found: error
```

`release-planner install` and `check` fail if the workflow can't be called this way.

## What happens next

1. **attest** signs a build provenance attestation for every file, in the `release` environment. Users can check a download with `gh attestation verify <file> --repo <owner>/<name>`.
2. **publish** uploads the files to a draft release, verifies GitHub's checksums for them, and publishes last, because immutable releases freeze a release's files once it's published.

Publication fails without writing anything if the artifact is empty, or holds anything but files named with letters, digits, and `. _ + -`.

A retry builds the files again. Keep the build reproducible, so a retry after an interrupted upload produces the same files; otherwise publication stops and asks you to delete the mismatched files from the draft release. A release that's already published never changes.

After changing `release-assets`, run `release-planner install` again and commit the result.
