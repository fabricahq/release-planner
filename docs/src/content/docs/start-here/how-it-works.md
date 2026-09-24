---
title: How it works
description: What Release Planner does behind the scenes, and what it keeps in your repository.
---

Release Planner splits a release between three parts: Release Planner itself, your agent, and you.

## Who does what

- **Release Planner does the bookkeeping.** It finds your last release, lists every commit and pull request merged since, and suggests the possible next versions.
- **Your agent does the writing.** It follows a written procedure: it reads those changes, applies your [release policy](/customize/policy/) to choose the version, writes the notes in your [release notes style](/customize/release-notes-style/), and opens the release pull request.
- **You approve.** You review the notes, edit anything you like, and merge. Nothing is published until you merge.

## The release notes are a file

The release pull request adds one file, `releases/v<version>.md`. Editing that file is how you edit the release, and its name sets the version. After the release, the file stays in your repository as a record of what you published.

## Merging starts the release

When you merge the release pull request, a GitHub Actions workflow:

1. checks the request: one notes file, a version newer than every existing release, and no unfinished draft text
2. runs any [release checks](/customize/release-checks/) you set up
3. tags the commit you merged and publishes the notes, word for word, as the GitHub release

The tag is created only at the last step, so a version never exists without the notes you approved. If anything fails, nothing is published.

## What lives in your repository

You write and own these files:

```text
.release-planner/
  config.yml                 # your settings
  policy.md                  # how you choose versions
  release-notes-style.md     # optional: how the notes are written
releases/
  v1.0.0.md                  # one notes file per release
```

Release Planner generates these from your settings, and keeps them up to date when you upgrade:

- `.github/workflows/release-planner.yml`, the workflow that publishes releases
- a **Releases** section in `AGENTS.md`, which tells any agent how to release
- a `release` skill in `.agents/skills/` and `.claude/skills/`, so agents recognize "let's release"

You don't edit the generated files. To change them, change your settings. See [Configuration](/customize/configuration/).
