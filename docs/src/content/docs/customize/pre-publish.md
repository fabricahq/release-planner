---
title: Pre-publish workflows
description: Run your own GitHub Actions workflows after you approve a release and before it's tagged, such as one that applies database migrations.
---

A pre-publish workflow is one of your own GitHub Actions workflows that Release Planner runs after you approve a release by merging its pull request, and before it tags and publishes it; if the workflow fails, the release isn't published.

## Why it's useful

Some releases need a change outside the repository before anyone can use them. The common case is a database. Say your app, which releases with Release Planner, is about to release version 1.3.0, and 1.3.0's code expects a database column that doesn't exist yet:

- **Migrate after publishing**, and there's a window where your 1.3.0 is published and ready to deploy, but its column doesn't exist yet. If the migration then fails, you have a published release you can't deploy.
- **Migrate before approving**, on the release pull request, and you change production for a release you might never approve, or that changes before you merge it.

What you want is to apply the change after you approve the release and before it's published, and to publish only if the change succeeded. A pre-publish workflow that applies your database migrations does exactly that:

- **If it succeeds**, Release Planner publishes the release as usual, so every published release has its migrations in place.
- **If it fails**, nothing is tagged or published. Fix the cause and use **Re-run failed jobs**, or withdraw the release.

It's optional, and it never runs on the release pull request: merging is the approval, and nothing with side effects runs before it.

## Set it up

List your workflows under `pre-publish` in `.release-planner/config.yml`, then run `release-planner install` and commit the result:

```yaml
pre-publish:
  - workflow: migrate-database.yml
```

`migrate-database.yml` is an example. Release Planner doesn't provide a migration workflow: the workflow is yours, named and written for your project, and it can do anything that must happen before a release is published. This page uses a database migration as its running example.

Each workflow must:

- **Accept three string inputs**, which the release workflow passes in:
  - `ref`: the full SHA of the release commit
  - `tag`: the release's tag, such as `v1.2.0`
  - `version`: the version without the `v`, such as `1.2.0`
- **Check out `ref`**, so it runs the code being released.
- **Run every job in the same [GitHub environment](https://docs.github.com/en/actions/how-tos/deploy/configure-and-manage-deployments/manage-environments)**, named literally on each job.
- **Not require secrets to be passed in.** The release workflow passes none, so keep them in the environment instead.

A GitHub environment, such as `production`, is a named set of deployment rules, variables, and secrets in your repository's settings. Your workflow's environment holds what it needs to reach the outside world, such as the variables naming a cloud role. It also limits that access to your release branch; see [Set up the environment](#set-up-the-environment).

You name the environment only in the workflow, because GitHub requires it on the jobs. Release Planner reads it from there to check the environment's settings.

For example, this workflow runs a project's own `make migrate` in `production`:

```yaml
name: Migrate the database
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

`release-planner install` and `release-planner check` fail if a workflow is missing an input, doesn't name the same environment on every job, or requires secrets. Run `release-planner install` again after you move a workflow to another environment or rename it.

The release status on each release pull request names each workflow by its `name:`, such as **Migrate the database**, so give each one a name that says what it does.

## Make it safe to run again, late, and twice at once

Your workflow can end up running more than once for the same release: after a failure, on a retry, or after the release is already out. A run for an older release can also happen after one for a newer release, and two runs can overlap. So make sure your workflow is:

- **Idempotent:** running it again, even after it stopped partway, gives the same result as running it once.
- **Safe to run late:** an older release's run, coming after a newer release's, changes nothing that matters. Versioned migration tools, such as goose, skip migrations that are already applied.
- **Safe to run concurrently:** two overlapping runs don't interfere. Take a lock, such as goose's Postgres session lock, or make overlapping runs harmless.

## Keep it compatible with what's running

Your workflow changes things before the new version is tagged, let alone deployed. Production may be serving the previous release, an older one if a deployment failed or is still in progress, or whatever you'd roll back to. Keep every change compatible with all of them. For a database, add the new shape in one release, and remove the old one only after no version that might still run, or be rolled back to, uses it.

Release Planner publishes releases in order, but it doesn't know what's deployed. If a change needs a particular version deployed first, check that in your workflow.

## What it can reach

The release workflow gives your workflow at most two permissions:

- **`contents: read`:** read access to the repository.
- **`id-token: write`:** lets it request an OIDC token, which your cloud provider can exchange for credentials.

Your workflow's own `permissions:` can narrow these but not widen them. A workflow that doesn't reach a cloud provider can leave out `id-token: write`, and then gets no OIDC token. It never gets your repository's secrets or a token that can publish releases. Anything else it needs, such as secrets and variables, comes from its environment.

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

After you merge, once the release checks and assets have passed, and right before the release is tagged. It runs for every release, prereleases included. It doesn't run on the release pull request, or for notes edits to a published release.

You can list several workflows. Each runs in its own job, named `pre-publish (<workflow>)`, and they run in parallel; the release is published only once all of them succeed. Each one may use its own environment. Steps that must run in order, such as a migration and then a check of its result, belong in one workflow.

Releases publish in order. If an earlier release you merged isn't published yet, a newer one waits: its run stops before your workflow, and its pull request says which release it's waiting for. Once that release is published or withdrawn, use **Re-run failed jobs** on the newer release's run. It checks the latest published release and every version between it and this one, tagged or not. Versions below the latest published release don't hold anything up: it was published after them, so their workflows already had their turn.

## If it fails

The release isn't tagged or published. The pull request description shows the failed job, and a comment mentions whoever merged. What to do depends on where the cause is:

- **Outside your repository**, such as an unreachable database or an expired credential: fix it, then use **Re-run failed jobs** on the release run. Only the workflows that failed run again, then the release is published.
- **In your workflow file**, such as `migrate-database.yml`: fix it in a pull request, then run the **Release** workflow manually on your release branch, with **merged-commit** set to the commit the release pull request merged as. **Re-run failed jobs** would use the workflow file from the original run.
- **In the release commit**, such as a migration that fails: withdraw the release and release again. In the pull request that fixes the cause, also delete the release's notes file, such as `_releases/v1.2.0.md`. After it merges, ask your agent for a release. It can reuse the same version, since that version was never tagged, and the new release commit includes the fix. If the old run had started attaching [release assets](/customize/release-assets/), it left an unpublished draft release for that version: delete the draft on GitHub before you merge the new release pull request, or its run stops and asks you to.

Once you've withdrawn a release, don't re-run its old run. Publishing checks for the withdrawal immediately before each write, so it publishes nothing, but your workflow runs once more from the old commit first. If you merge the withdrawal while that run is publishing, it stops at its next write; anything it already made public stays.

If publishing fails after your workflow succeeded, **Re-run failed jobs** runs only the publishing.

## Requirements

- The workflow can't be `release-planner.yml`, or the workflow you use for release checks or release assets.
- The environment can't be `release` or `dispatch`.
- Don't skip every job in the workflow with `if`. GitHub then reports the call as skipped, and the release isn't published. To do nothing, skip steps instead.
