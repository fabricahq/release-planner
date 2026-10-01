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

- `.github/workflows/release-planner.yml`, the [GitHub Actions workflow](https://docs.github.com/en/actions/concepts/workflows-and-actions/workflows) that publishes releases.
- A **Releases** section in `AGENTS.md`, which tells any agent how to release.
- A `release` skill in `.agents/skills/` and `.claude/skills/`, so agents recognize "let's release" automatically.

## 5. Protect your releases

In your repository settings on GitHub:

- **Require pull requests** for your main branch, with your CI as required status checks, and **require branches to be up to date** before merging (or use a merge queue). This is what guarantees that the commit you release was tested.
- **Block force pushes** to and deletion of the main branch.
- **Create the `release` environment,** as described below. If a [post-publish workflow](/customize/post-publish/) is in another repository, also create the `dispatch` environment.
- **Turn on immutable releases**, so published tags and files can't be changed. Release notes stay editable.

### Create the `release` environment

The job that publishes a release runs in an environment named `release`. The environment makes sure it runs only from your main branch.

1. Open **Settings → Environments** and choose **New environment**. Name it `release`.
2. Under **Deployment branches and tags**, choose **Selected branches and tags**, then **Add deployment branch or tag rule**. Choose **Branch**, enter `main` (or your release branch), and save.
3. Leave **Required reviewers** off. Merging the release pull request is the approval. With reviewers on, every release stops and waits for you to approve it again in the Actions tab.

If you skip this step, GitHub creates the environment the first time a release runs, but without the branch restriction.

### Publish with a release GitHub App

This is optional. By default, the publish job writes with the workflow's own token. A dedicated GitHub App, whose key only the `release` environment holds, does two things that token can't:

- **It can tag a commit whose workflow files have since changed.** GitHub refuses to let the workflow's token create a release, even a draft, on a commit whose `.github/workflows/` files match no branch: publishing then fails with `Resource not accessible by integration`. That happens when a workflow change, such as a Release Planner upgrade, merges while a release pull request is open and the release pull request's branch is deleted when it merges, or when you retry a release after such a change. Set up the App if that can happen in your repository.
- **It lets a ruleset reserve release tags for it.** Without it, anyone who can push can create a `v*` tag outside Release Planner.

To set it up:

1. Create a GitHub App owned by the repository's owner (**Settings → Developer settings → GitHub Apps → New GitHub App**). Turn off **Webhook**. Under **Repository permissions**, set **Contents** and **Workflows** to **Read and write**, and leave everything else at no access. Subscribe to no events.
2. Install the App on this repository only.
3. Note the App's **Client ID**, and generate a **private key**.
4. In the `release` environment, add the variable `RELEASE_APP_CLIENT_ID`, set to the Client ID, and the secret `RELEASE_APP_PRIVATE_KEY`, set to the private key.
5. Optionally, add a tag ruleset (**Settings → Rules → Rulesets → New tag ruleset**) that targets `v*`, restricts creations, updates, and deletions, and lists only the App and repository admins under **Bypass list**. Add it after the App works: the ruleset refuses the workflow's own token, with `Cannot create ref due to creations being restricted`.

The publish job mints a token for this repository only, with **Contents** and **Workflows** write, and uses it only to write; it reads with the workflow's token. With neither setting, it publishes with the workflow's token as before. With only one, it fails and names the missing one.

Tags and releases the App creates start workflows, which those made with the workflow's token never do: `create` and `push` for the tag, and the `release` events `published`, then `released` or `prereleased`, plus `created` for a release without assets. A release with assets is staged as a draft, and drafts start no workflows. They fire before the release's assets are attested from the release branch, so don't deploy or redistribute from them. Start that work as a [post-publish workflow](/customize/post-publish/) instead, which runs after the attestation, or have it verify the attestation first. To find workflows these events start, run `grep -lE 'release:|tags:' .github/workflows/*`.

## 6. Commit and release

Commit the new files. Then tell your agent "let's release." See [Make a release](/start-here/release/).
