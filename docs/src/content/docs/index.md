---
title: What is Release Planner?
description: Agent-drafted, maintainer-approved releases for GitHub repositories.
---

Release Planner is a release process for GitHub repositories. Your agent drafts the release, and you approve it by merging a pull request.

The agent proposes the version and writes the release notes. You edit them like any other pull request. When you merge, Release Planner tags the commit you approved and publishes your notes, word for word, as the GitHub release.

## Why use it?

Good release notes explain what changed for the people who use your project. Notes generated from commit messages read like a changelog, and writing good notes by hand for every release is tedious.

Agents handle the tedious part well: reading every change since the last release and working out what it means for users. They shouldn't decide on their own what ships. With Release Planner, the agent prepares everything, and nothing is published until you merge.

## How it works

1. **You tell your agent "let's release."**
2. **The agent works out the next release.** It reads every commit and pull request since the previous release, applies your release policy, and picks the version.
3. **The agent opens a release pull request.** It adds one file, `releases/v<version>.md`, containing the notes, and explains its choice of version.
4. **You edit the notes and merge.** Change anything you like. Saving edits publishes nothing.
5. **Merging publishes the release.** A GitHub Actions workflow tags the merged commit and publishes the notes.

The tag is created only at that last step, so a version never exists without the notes you approved.

## What lives in your repository

```text
.release-planner/
  config.yml                 # you write it: settings
  policy.md                  # you write it: how you choose versions
  release-notes-style.md     # optional: how the notes are written
releases/
  v1.0.0.md                  # one notes file per release
```

Release Planner also generates a workflow, an agent skill, and a short section of `AGENTS.md`. You don't edit those; they come from your config.

## Next steps

- [Set up a repository](/start-here/set-up/)
- [Make a release](/start-here/release/)
- [Customize your releases](/customize/policy/)
