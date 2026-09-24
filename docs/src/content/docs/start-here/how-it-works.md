---
title: How it works
description: What Release Planner does behind the scenes, and what it keeps in your repository.
---

- **Release Planner does the bookkeeping; your agent does the writing.** Release Planner finds your last release, lists everything merged since, and suggests the possible next versions. Your agent follows a written procedure: it reads those changes, applies your [release policy](/customize/policy/) to choose the version, and writes the notes in your [release notes style](/customize/release-notes-style/).
- **The release notes are a file in your repository.** The release pull request adds one file, `releases/v<version>.md`. Editing that file is how you edit the release.
- **Merging is the approval.** A GitHub Actions workflow checks the request, runs any [release checks](/customize/release-checks/) you set up, then tags the commit you merged and publishes the notes word for word. The tag is created only then, so a version never exists without the notes you approved.

## What lives in your repository

```text
.release-planner/
  config.yml                 # your settings
  policy.md                  # how you choose versions
  release-notes-style.md     # optional: how the notes are written
releases/
  v1.0.0.md                  # one notes file per release
```

Release Planner also generates a workflow, an agent skill, and a short section of `AGENTS.md`. You don't edit those; they come from your settings.
