---
title: Pre-publish workflow
description: Run one of your workflows after you approve a release and before it's tagged, such as one that applies database migrations.
---

A pre-publish workflow is optional. It runs after you merge the release pull request and before the release is tagged, so if it fails, the release isn't published. A common one applies database migrations, so every published release has its schema in place.

It never runs on the release pull request. Merging is the approval, and nothing with side effects runs before it.

## Set it up

Name your workflow and the environment its jobs run in, in `.release-planner/config.yml`, then run `release-planner install` and commit the result:

```yaml
pre-publish:
  workflow: migrate.yml
  environment: production
```

The release workflow calls it with three string inputs: `ref`, the full SHA of the release commit; `tag`, such as `v1.2.0`; and `version`, such as `1.2.0`. Your workflow must check out `ref`, and every job in it must run in the environment you named:

```yaml
name: Migrate
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
  migrate:
    runs-on: ubuntu-latest
    timeout-minutes: 10
    environment: production
    steps:
      - uses: actions/checkout@v7
        with:
          ref: ${{ inputs.ref }}
          persist-credentials: false
      - run: make migrate
```

`release-planner install` and `check` fail if the workflow doesn't declare the three inputs as strings, if a job doesn't run in the environment, or if it requires secrets.

## Make it safe to run again, late, and twice at once

Your workflow may run more than once for the same release: after it fails, on a retry, or after the release is already out. An older release's run can also come after a newer release's. Two runs can overlap. Make each of these harmless:

- **Again:** running it twice, including after it stopped partway, gives the same result.
- **Late:** running an older release's workflow after a newer one's changes nothing that matters. Versioned migration tools, such as goose, skip migrations that are already applied.
- **Twice at once:** take a lock, such as goose's Postgres session lock, or make overlapping runs harmless.

## Keep it compatible with what's running

Your workflow changes things before the new version is tagged, let alone deployed. Production may be serving the previous release, an older one if a deployment failed or is still in progress, or whatever you'd roll back to. Keep every change compatible with all of them. For a database, add the new shape in one release, and remove the old one only after no version that might still run, or be rolled back to, uses it.

Release Planner publishes releases in order, but it doesn't know what's deployed. If a change needs a particular version deployed first, check that in your workflow.

## What it can reach

The workflow gets read access to the repository and an OIDC token. It never receives your repository's secrets or a token that publishes releases. Everything else comes from the environment.

Trust exactly the environment's OIDC subject in your cloud provider. Look up its format:

```sh
gh api repos/<owner>/<name>/actions/oidc/customization/sub
```

If `use_immutable_subject` is `true`, the subject is `<sub_claim_prefix>:environment:<environment>`, such as `repo:octo-org@123/app@456:environment:production`. Otherwise it's `repo:<owner>/<name>:environment:<environment>`, unless you customized it.

## Set up the environment

These settings keep the environment's credentials away from anything but your release branch. Release Planner checks them, but it can't enforce them for your other workflows.

1. Open **Settings → Environments**, and create or open the environment.
2. Under **Deployment branches and tags**, choose **Selected branches and tags**, and add a branch rule for your release branch only. Remove any other rule.
3. Add the variables and secrets the workflow needs.
4. Leave **Required reviewers** off, unless you want to approve each run again after merging.

A pull request's run can't use the environment, even if it changes the workflow. A `pull_request_target` workflow runs as your release branch, though, so don't name the environment in one. If the environment is deleted, GitHub recreates it without restrictions the next time a job names it.

The release workflow warns on each release pull request if the environment is missing or lets anything but your release branch use it. After you merge, it refuses to run your workflow if the environment is missing, unrestricted, or doesn't let your release branch use it.

## When it runs

After you merge, once the release checks and assets have passed, and right before the release is tagged. It runs for prereleases too. It doesn't run on the release pull request, or for notes edits to a published release.

Releases publish in order. If an earlier release you merged isn't published yet, a newer one waits: its run stops before your workflow, and its pull request says which release it's waiting for. Once that release is published or withdrawn, use **Re-run failed jobs** on the newer release's run.

## If it fails

The release isn't tagged or published. The pull request description shows the failed job, and a comment mentions whoever merged. What to do depends on where the cause is:

- **Outside your repository**, such as an unreachable database or an expired credential: fix it, then use **Re-run failed jobs** on the release run. Your workflow runs again, then the release is published.
- **In your workflow file**, such as `migrate.yml`: fix it in a pull request, then run the **Release** workflow manually on your release branch, with **merged-commit** set to the commit the release pull request merged as. **Re-run failed jobs** would use the workflow file from the original run.
- **In the release commit**, such as a migration that fails: withdraw the release and release again. In the pull request that fixes the cause, also delete the release's notes file, such as `_releases/v1.2.0.md`. After it merges, ask your agent for a release. It can reuse the same version, since that version was never tagged, and the new release commit includes the fix.

Once you've withdrawn a release, don't re-run its old run. It refuses to publish, but your workflow runs once more from the old commit first.

If publishing fails after your workflow succeeded, **Re-run failed jobs** runs only the publishing.

## Requirements

- The workflow can't be `release-planner.yml`, or the workflow you use for release checks or release assets.
- The environment can't be `release` or `downstream`.
- Don't skip every job in the workflow with `if`. GitHub then reports the call as skipped, and the release isn't published. To do nothing, skip steps instead.
