---
title: Set up a repository
description: Add Release Planner to a GitHub repository.
---

You need a repository on GitHub, with GitHub Actions enabled. Release Planner works with GitHub only for now.

Run these commands from the repository root.

## 1. Install Release Planner

Release Planner is a single command, `release-planner`, for macOS and Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/fabricahq/release-planner/main/install.sh | sh
```

This installs the latest release into `~/.local/bin` after verifying its checksum. To install a specific version, add `-s -- --version v0.3.2` after `sh`. If you have [Go](https://go.dev/dl/), `go install github.com/fabricahq/release-planner/cmd/release-planner@v0.3.2` works too.

## 2. Create the configuration

```sh
release-planner init --first-version v1.0.0
```

This creates `.release-planner/config.yml` and a starter release policy, `.release-planner/policy.md`. The config pins the Release Planner version you just installed. Set `--first-version` to the version your first release should have.

## 3. Write your release policy

Open `.release-planner/policy.md` and replace each `TODO:` prompt. The policy tells the agent what your users depend on and how you choose versions. Until you fill it in, the agent stops and asks instead of guessing.

See [Release policy](/customize/policy/) for examples.

## 4. Generate the release files

```sh
release-planner install
```

This writes:

- `.github/workflows/release-planner.yml`, the workflow that publishes releases.
- A **Releases** section in `AGENTS.md`, which tells any agent how to release.
- A `release` skill in `.agents/skills/` and `.claude/skills/`, so agents recognize "let's release" automatically.

## 5. Protect your releases

In your repository settings on GitHub:

- **Require pull requests** for your main branch, with your CI as required status checks, and **require branches to be up to date** before merging (or use a merge queue). This is what guarantees that the commit you release was tested.
- **Block force pushes** to and deletion of the main branch.
- **Create the `release` environment,** as described below. If you use [downstream workflows](/customize/downstream/), also create the `downstream` environment.
- **Turn on immutable releases**, so published tags and files can't be changed. Release notes stay editable.

### Create the `release` environment

The job that publishes a release runs in an environment named `release`. The environment makes sure it runs only from your main branch.

1. Open **Settings → Environments** and choose **New environment**. Name it `release`.
2. Under **Deployment branches and tags**, choose **Selected branches and tags**, then **Add deployment branch or tag rule**. Choose **Branch**, enter `main` (or your release branch), and save.
3. Leave **Required reviewers** off. Merging the release pull request is the approval. With reviewers on, every release stops and waits for you to approve it again in the Actions tab.

If you skip this step, GitHub creates the environment the first time a release runs, but without the branch restriction.

## 6. Commit and release

Commit the new files. Then tell your agent "let's release." See [Make a release](/start-here/release/).
