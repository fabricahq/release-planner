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

Your workflow must check out `ref`, build whatever the release needs, run any checks on what it built, and upload the files as **one artifact named `release-assets`**, with the files at its top level. It must also **output that upload's ID as `artifact-id`**: the release workflow downloads the files by that ID, so no other job can replace them by uploading under the same name. For example:

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
    outputs:
      artifact-id:
        value: ${{ jobs.build.outputs.artifact-id }}
permissions:
  contents: read
jobs:
  build:
    runs-on: ubuntu-latest
    outputs:
      artifact-id: ${{ steps.upload.outputs.artifact-id }}
    steps:
      - uses: actions/checkout@v7
        with:
          ref: ${{ inputs.ref }}
          persist-credentials: false
      - run: make dist VERSION="$VERSION"
        env:
          VERSION: ${{ inputs.version }}
      - id: upload
        uses: actions/upload-artifact@v7
        with:
          name: release-assets
          path: dist/
          if-no-files-found: error
```

`release-planner install` and `check` fail if the workflow doesn't declare the three inputs as strings, or the `artifact-id` output. Release Planner doesn't look inside the files, beyond requiring at least one, all regular files, with names made of letters, digits, and `.`, `_`, `+`, and `-`.

## What happens to the files

1. **On the release pull request**, the release workflow builds the assets from the release commit. The build runs with read access to the repository and none of its secrets.
2. An `attest` job signs each file's [build provenance](https://docs.github.com/en/actions/security-for-github-actions/using-artifact-attestations), so anyone can check it came from your release workflow with `gh attestation verify <file> --repo <owner>/<name>`. It also records which plan and files the release uses, in the name of a `release-binding-<plan>-<assets>` artifact. It runs after your release checks, so nothing from your repository runs after it.
3. The release status in the pull request description lists each file with its size, and links a zip of them all to download. It's a workflow artifact, so only signed-in users who can read the repository can download it, until it expires. Once the release is published, each file links to its download on the release instead.
4. **When you merge**, the publish job downloads the files the pull request built, verifies each file's attestation was made by this repository's `release-planner.yml`, uploads them to a draft release, checks GitHub's checksums, and publishes last.
5. **After publishing**, an `attest-release` job downloads the published files and attests them again, this time from the release branch. Only files that were approved, verified, and published get this attestation, so anything that installs them, such as a Homebrew tap, can require it:

   ```sh
   gh attestation verify <file> --repo <owner>/<name> \
     --signer-workflow <owner>/<name>/.github/workflows/release-planner.yml \
     --source-ref refs/heads/main
   ```

   Downstream workflows start only after this job succeeds.

If the pull request's files are gone, because the pull request's run didn't finish before the merge or its artifacts expired, the release workflow builds and attests them again from the same release commit after the merge. It does the same if the run has more than one `release-binding` artifact, since then it can't tell which files the pull request approved.

A pull request from a fork builds nothing, because its workflow can't sign attestations; the files are built after the merge instead.

Keep the build reproducible. If an upload is interrupted and the files have to be built again, a draft that already holds different files stops publication until you delete them from the draft. A published release's files never change.

## Upgrade to v0.5.0

From v0.5.0, the release workflow downloads the assets by the ID of the upload your workflow made, instead of by name. `release-planner check` fails until your workflow declares the `artifact-id` output:

1. Give the upload step an `id`, such as `upload`.
2. Add `artifact-id: ${{ steps.upload.outputs.artifact-id }}` to the `outputs` of the job that uploads.
3. Add an `artifact-id` output to `workflow_call`, set from that job's output, as in the example above.
4. Pin v0.5.0 in `.release-planner/config.yml`, run `release-planner install`, and commit everything in one pull request.

## Requirements

- Artifact attestations are available for public repositories, and for private and internal repositories on GitHub Enterprise Cloud.
- The workflow can't be `release-planner.yml`, the release workflow itself.

Once a release is published, its files never change. Editing a release's notes later leaves them as they are.
