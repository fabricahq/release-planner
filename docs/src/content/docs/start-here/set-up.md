---
title: Set up a repository
description: Add Release Planner to a GitHub repository.
---

You need [Go](https://go.dev/dl/) installed. Nothing else needs installing: every command runs with `go run` at a pinned version.

Run these commands from the repository root.

## 1. Create the configuration

```sh
go run github.com/fabricahq/release-planner/cmd/release-planner@v0.1.0 init --first-version v1.0.0
```

This creates `.release-planner/config.yml` and a starter release policy, `.release-planner/policy.md`. Set `--first-version` to the version your first release should have.

## 2. Write your release policy

Open `.release-planner/policy.md` and replace each `TODO:` prompt. The policy tells the agent what your users depend on and how you choose versions. Until you fill it in, the agent stops and asks instead of guessing.

See [Release policy](/customize/policy/) for examples.

## 3. Generate the release files

```sh
go run github.com/fabricahq/release-planner/cmd/release-planner@v0.1.0 install
```

This writes:

- `.github/workflows/release-planner.yml`, the workflow that publishes releases.
- A **Releases** section in `AGENTS.md`, which tells any agent how to release.
- A `release` skill in `.agents/skills/` and `.claude/skills/`, so agents recognize "let's release" automatically.

## 4. Protect your releases

In your repository settings on GitHub:

- **Require pull requests** for your main branch, with your CI as required status checks, and **require branches to be up to date** before merging (or use a merge queue). This is what guarantees that the commit you release was tested.
- **Block force pushes** to and deletion of the main branch.
- **Create an environment named `release`** that allows only the main branch, with no required reviewers. Merging the release pull request is the approval.
- **Turn on immutable releases**, so published releases and their tags can't be changed.

## 5. Commit and release

Commit the new files. Then tell your agent "let's release." See [Make a release](/start-here/release/).
