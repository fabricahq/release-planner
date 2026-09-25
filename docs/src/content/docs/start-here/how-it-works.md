---
title: How it works
description: The files Release Planner keeps in your repository, the command that does the work, and what happens when you merge.
---

Release Planner has three parts: a folder of settings you write, a folder of release notes that grows with each release, and a command that does the work.

## Your settings: `.release-planner/`

Everything that controls how your releases work lives in one folder at the root of your repository:

```text
.release-planner/
  config.yml                 # settings, including the Release Planner version to use
  policy.md                  # how you choose versions, and who reads your notes
  release-notes-style.md     # optional, if config.yml names it: how the notes are written
```

You write these files, and Release Planner never changes them. See [Customize](/customize/policy/) for what goes in each one.

## Your releases: `_releases/`

Every release is one Markdown file in the `_releases/` folder, named for its version:

```text
_releases/
  v1.0.0.md
  v1.1.0.md
```

When your agent prepares a release, its pull request adds the next file, such as `_releases/v1.2.0.md`. That file is the release: its contents become the release notes on GitHub, word for word, and its name sets the version. To change the notes, edit the file in the pull request. After the release, the file stays in the folder as a record of what you published.

## The `release-planner` command

Release Planner itself is a small command-line program for macOS and Linux. Your `config.yml` pins its version, and everyone uses exactly that version: your agent checks it before preparing a release, and your release workflow downloads that release and verifies its signature before running it.

The command does three jobs:

- **Sets up your repository.** `release-planner init` creates your settings. `release-planner install` generates the files that connect everything together:
  - a GitHub Actions workflow, `.github/workflows/release-planner.yml`, that publishes releases
  - a short **Releases** section in `AGENTS.md`, and a `release` skill in `.agents/skills/` and `.claude/skills/`, so agents know what to do when you say "let's release"

  You don't edit the generated files. To change them, change your settings and run `install` again.
- **Helps your agent prepare releases.** It gives your agent the release procedure to follow, lists every change since your last release with a ready-made line for each, and checks the notes the agent writes.
- **Publishes releases.** When you merge a release pull request, the workflow runs the command to check the release and publish it.

You'll rarely run it yourself after setup. The full list of commands is in [For agents](/for-agents/).

## What happens when you merge

Merging the release pull request is what starts the release. The workflow then:

1. validates the request: one notes file and a version newer than every existing release
2. runs any [release checks](/customize/release-checks/) you set up
3. builds and attests any [release assets](/customize/release-assets/) you set up
4. tags the commit you merged and publishes the notes, and any assets, as the GitHub release

The tag is created only at the last step, so a version never exists without the notes you approved. If anything fails, nothing is published.
