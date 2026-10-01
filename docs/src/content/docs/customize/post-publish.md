---
title: Post-publish workflows
description: Run your own GitHub Actions workflows after a release is published, such as one that deploys it or updates a Homebrew formula.
---

A post-publish workflow is one of your own GitHub Actions workflows that Release Planner runs after it publishes a release. It can be in this repository, such as one that deploys the release, or in another repository you own, such as one that updates a Homebrew formula in a tap. If it fails, the release stays published.

## Set it up

List your workflows under `post-publish` in `.release-planner/config.yml`, then run `release-planner install` and commit the result:

```yaml
post-publish:
  - workflow: deploy.yml
  - repository: your-org/homebrew-tap
    workflow: update-formula.yml
```

An entry without `repository` is a workflow in this repository. An entry with `repository` is a workflow in that repository, which must belong to the same owner as every other repository you list. The workflows are yours: Release Planner doesn't provide them, and these names are examples.

## A workflow in this repository

The release workflow calls it with the same three string inputs as a [pre-publish workflow](/customize/pre-publish/): `ref`, the full SHA of the release commit; `tag`, such as `v1.2.0`; and `version`, such as `1.2.0`. Your workflow must check out `ref`, and every job in it must run in the same [GitHub environment](https://docs.github.com/en/actions/how-tos/deploy/configure-and-manage-deployments/manage-environments), which holds whatever it needs to reach the outside world. You name the environment once, on the workflow's jobs, because GitHub requires it there; Release Planner reads it from the workflow to check its settings. For example:

```yaml
name: Deploy
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
  id-token: write
jobs:
  deploy:
    runs-on: ubuntu-latest
    timeout-minutes: 15
    environment: production
    steps:
      - uses: actions/checkout@v7
        with:
          ref: ${{ inputs.ref }}
          persist-credentials: false
      - run: make deploy VERSION="$VERSION"
        env:
          VERSION: ${{ inputs.version }}
```

The workflow gets read access to the repository and an OIDC token, and never your repository's secrets or a token that publishes releases. `release-planner install` and `check` fail if it doesn't declare the three inputs as strings, if its jobs don't all name the same environment literally, or if it requires secrets. The release status names it by its `name:`, such as **Deploy**.

### Set up the environment

Set up its environment the way the [pre-publish page describes](/customize/pre-publish/#set-up-the-environment): **Selected branches and tags**, with a branch rule for your release branch only, and **Required reviewers** off unless you want to approve each run. The release workflow warns on each release pull request if the environment is missing or lets anything but your release branch use it, and after you merge, it refuses to publish if the environment is missing, unrestricted, or doesn't let your release branch use it, so a release never publishes with a post-publish workflow that can't run.

## A workflow in another repository

The release workflow starts it with a `workflow_dispatch` event on that repository's default branch, with two string inputs:

```yaml
on:
  workflow_dispatch:
    inputs:
      tag:
        description: 'Release tag, such as v1.2.0'
        type: string
        required: true
      version:
        description: 'Version without the leading v, such as 1.2.0'
        type: string
        required: true
```

It starts the workflow with a GitHub App's token, limited to that repository, rather than a personal access token.

### Create the GitHub App

1. Create a GitHub App owned by the repositories' owner (**Settings → Developer settings → GitHub Apps → New GitHub App**). Turn off **Webhook**. Under **Repository permissions**, set **Actions** to **Read and write**, and leave everything else at no access.
2. Install the app on the repositories your post-publish workflows are in, and no others.
3. Note the app's **Client ID**, and generate a **private key**.

### Set up the dispatch environment

The jobs that start these workflows run in an environment named `dispatch`, which holds the app's credentials and runs only from your release branch.

1. Open **Settings → Environments** in the repository that releases, and choose **New environment**. Name it `dispatch`.
2. Under **Deployment branches and tags**, add a branch rule for your release branch only.
3. Add the variable `DISPATCH_APP_CLIENT_ID`, set to the app's Client ID.
4. Add the secret `DISPATCH_APP_PRIVATE_KEY`, set to the app's private key.
5. Leave **Required reviewers** off, unless you want to approve each run.

The release workflow warns on each release pull request if the `dispatch` environment is missing or doesn't restrict branches. GitHub doesn't let a workflow's token read an environment's variables or secrets, so each job checks them itself before it mints the token, and fails with a message naming both if either is missing.

## When they run

After a new release publishes, and with [release assets](/customize/release-assets/), after the published assets are attested from your release branch. Each workflow runs in its own job, named `post-publish (<workflow>)` or `post-publish (<repository>:<workflow>)`, and they run in parallel, so steps that must run in order belong in one workflow.

They don't run for:

- prereleases, whose version has a `-` suffix, such as `v1.2.0-rc.1`, unless the entry sets `prereleases: true`
- notes edits to a published release
- a release that failed to publish

```yaml
post-publish:
  - workflow: deploy-preview.yml
    prereleases: true
```

## If one fails

The release stays published. The release status in the release pull request's description lists each post-publish job with ✅ or ❌ and a link to it, and a comment mentions whoever merged. Once the cause is fixed, use **Re-run failed jobs** on the release run: it runs only the post-publish jobs that failed, so a workflow that already ran doesn't run twice. Publishing is skipped because it already succeeded.

## Example: a Homebrew tap

In `fabricahq/homebrew-tap`, a workflow `update-code-rules.yml` takes `tag` and `version`, downloads the release's archives, checks they were attested by the releasing repository's `release-planner.yml` on `refs/heads/main`, updates the formula's URLs and checksums, and commits the change. The releasing repository lists it under `post-publish` with `repository: fabricahq/homebrew-tap`, and the GitHub App is installed on the tap only, so the release workflow can start that workflow but can't change anything else.

## Upgrade from `downstream`

Release Planner v0.5.0 replaces `downstream` with `post-publish`. A config that still has `downstream` fails until you change it:

1. Rename `downstream:` to `post-publish:`, and keep its entries as they are.
2. GitHub can't rename an environment, so [set up a `dispatch` environment](#set-up-the-dispatch-environment) with the same branch rule as your `downstream` one, with the variable `DISPATCH_APP_CLIENT_ID` and the secret `DISPATCH_APP_PRIVATE_KEY` set to the values of `DOWNSTREAM_APP_CLIENT_ID` and `DOWNSTREAM_APP_PRIVATE_KEY`. The same GitHub App works.
3. Pin v0.5.0, run `release-planner install`, and commit.
4. Once a release has run its post-publish workflows, delete the `downstream` environment.

## Requirements

- A workflow in this repository can't be `release-planner.yml`, or a workflow you use for release checks, release assets, or pre-publish.
- Its environment can't be `release` or `dispatch`.
- Every repository you list must belong to the same owner, so one GitHub App covers them.
