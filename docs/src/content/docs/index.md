---
title: What is Release Planner?
description: A simple, agent-assisted way to publish well-written GitHub releases.
---

Release Planner makes it easy for your GitHub repository to publish releases people actually want to read. Your coding agent does the tedious work of reviewing what changed and drafting the notes. You stay in charge of what ships: nothing is published until you approve it.

## What making a release looks like

With Release Planner set up, here's how a new release happens:

1. **You ask for a release.** Tell your agent "let's release."
2. **Your agent drafts the release.** Release Planner gathers every commit and pull request merged since your last release and hands that list to your agent. The agent reads through the changes, works out what they mean for your users, and drafts the notes.
3. **Your agent opens a pull request.** A few minutes later, you have a pull request titled **Release v1.1.0**. It contains the drafted release notes and explains why the agent chose that version.
4. **You edit and merge.** Read the notes, change anything you like, and merge the pull request.
5. **The release is live.** Your new version is tagged and published on your repository's **Releases** page, with exactly the notes you approved.

That's the whole process. There's no changelog to maintain, no commit message conventions to follow, and no release checklist to remember.

## What the release notes look like

Here's an example of what your readers see on GitHub:

<div class="release-example">
<div class="release-example-header"><strong>v1.1.0</strong> <span>Latest</span></div>

This release adds rules for Svelte and fixes a misleading Zustand example. No existing rules changed, so upgrading requires no changes to your configuration.

#### ✨ New Features

**Svelte rules**

A new `techs/svelte` group with six rules for writing Svelte 5 components with runes, including when to use `$derived` instead of `$effect`.

#### 🐛 Squashed Bugs

- `techs/zustand/rehydrate-persisted-stores-after-hydration` no longer shows reading storage during server rendering.

#### What's Changed

- Add a Svelte group in #7
- Correct the Zustand hydration example in #8

**Full Changelog**: v1.0.0...v1.1.0

</div>

Release Planner comes with an opinionated release notes style, shown above: open with what the release means for readers, group changes under **New Features**, **Improvements**, **Squashed Bugs**, and **Breaking Changes**, and end with a linked list of every pull request.

The style is fully customizable. You can add your own rules on top of it, such as always naming affected API endpoints, or replace it entirely with your own headings and format. See [Release notes style](/customize/release-notes-style/).

## How it works

Behind the scenes:

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

## Next steps

- [Set up a repository](/start-here/set-up/)
- [Make a release](/start-here/release/)
- [Customize your releases](/customize/policy/)
