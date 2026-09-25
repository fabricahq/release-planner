---
title: Downstream workflows
description: Start workflows in other repositories after each new release, such as a Homebrew formula update.
---

Downstream workflows run in other repositories after each new stable release is published, with the release's tag and version. A common one updates a Homebrew formula in a tap repository.

## Configure the targets

List each workflow in `.release-planner/config.yml`, then run `release-planner install` and commit the result:

```yaml
downstream:
  - repository: fabricahq/homebrew-tap
    workflow: update-code-rules.yml
```

Every target repository must belong to the same owner, so one GitHub App token covers them all.

Each target workflow needs a `workflow_dispatch` trigger with two string inputs. It runs on the target repository's default branch:

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

## When they run

After a new release publishes, the `downstream` job starts the target workflows, one job per target, named `downstream (<repository>:<workflow>)`. It doesn't run for:

- prereleases, whose version has a `-` suffix, such as `v1.2.0-rc.1`
- notes edits to a published release
- a release that failed to publish

A failed trigger doesn't affect the published release. The release status in the release pull request's description lists each target with ✅ or ❌ and a link to its workflow's runs. To try again, use **Re-run failed jobs** on the release run: it runs only the targets whose jobs failed, so a target workflow that already started doesn't run twice. Publishing is skipped because it already succeeded.

## Create the GitHub App

The job uses a GitHub App's token, limited to the target repositories, rather than a personal access token.

1. Create a GitHub App owned by the targets' owner (**Settings → Developer settings → GitHub Apps → New GitHub App**). Turn off **Webhook**. Under **Repository permissions**, set **Actions** to **Read and write**, and leave everything else at no access.
2. Install the app on the target repositories only.
3. Note the app's **Client ID**, and generate a **private key**.

## Set up the downstream environment

The job runs in an environment named `downstream`, which holds the app's credentials and runs only from your release branch.

1. Open **Settings → Environments** in the repository that releases, and choose **New environment**. Name it `downstream`.
2. Under **Deployment branches and tags**, add a branch rule for your release branch only.
3. Add the variable `DOWNSTREAM_APP_CLIENT_ID`, set to the app's Client ID.
4. Add the secret `DOWNSTREAM_APP_PRIVATE_KEY`, set to the app's private key.
5. Leave **Required reviewers** off, unless you want to approve each downstream run.

The release workflow warns on each release pull request if the `downstream` environment is missing or doesn't restrict branches. GitHub doesn't let a workflow's token read an environment's variables or secrets, so the `downstream` job checks them itself before minting the token, and fails with a message naming whichever is missing.

## Example: a Homebrew tap

In `fabricahq/homebrew-tap`, a workflow `update-code-rules.yml` takes `tag` and `version`, downloads the release's archives, updates the formula's URLs and checksums, and commits the change. The releasing repository lists it under `downstream`, and the GitHub App is installed on the tap only, so the release workflow can start that workflow but can't change anything else.
