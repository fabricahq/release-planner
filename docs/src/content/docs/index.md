---
title: What is Release Planner?
description: A simple, agent-assisted way to publish well-written GitHub releases.
---

Release Planner makes it easy for your GitHub repository to publish release notes people actually want to read. Your coding agent does the tedious work of reviewing what changed and drafting the notes. You stay in charge of what ships: nothing is published until you approve it.

Release Planner works with GitHub only for now. It publishes GitHub releases, and it runs on GitHub Actions.

## What making a release looks like

With Release Planner set up, here's how a new release happens:

1. **You:** Ask for a release. Tell your agent "let's release."
2. **Automated:** Release Planner gathers every commit and pull request merged since your last release and hands that list to your agent. The agent reads through the changes, works out what they mean for your users, and drafts the notes.
3. **Automated:** Your agent opens a pull request titled **Release v1.1.0**. It contains the drafted release notes and explains why the agent chose that version.
4. **You:** Review the notes and change anything you like. Nothing is published while the pull request is open. Meanwhile, the release workflow checks the release and builds any files it ships, and keeps the release status in the pull request description up to date.
5. **You:** Merge the pull request. Merging is what approves and starts the release.
6. **Automated:** A GitHub Actions workflow tags the release commit, the commit your release pull request branched from, and publishes your new version on the repository's **Releases** page, with exactly the notes you approved and the files the pull request built.

You do three things: ask, review, and merge. There's no changelog to maintain, no commit message conventions to follow, and no release checklist to remember.

## What the release notes look like

Here's an example of what your readers see on GitHub:

<div class="release-example">
<div class="release-example-header"><strong>v1.1.0</strong> <span>Latest</span></div>

This release adds rules for Svelte and fixes a misleading Zustand example. No existing rules changed, so upgrading requires no changes to your configuration.

#### ✨ New Features

**Svelte rules**

A new `techs/svelte` group with six rules for writing Svelte 5 components with runes, including when to use `$derived` instead of `$effect`. (#7, #9)

#### 🐛 Squashed Bugs

- `techs/zustand/rehydrate-persisted-stores-after-hydration` no longer shows reading storage during server rendering. (#8)

#### Pull Requests

**✨ Features**

- feat: add a Svelte group by @ada in #7
- feat(svelte): add examples for stores and snippets by @grace in #9

**🐛 Bug Fixes**

- fix(zustand): correct the hydration example by @ada in #8

**Full Changelog**: v1.0.0...v1.1.0

</div>

Release Planner comes with an opinionated release notes style, shown above.

The style is fully customizable. You can add your own rules on top of it, such as always naming affected API endpoints, or replace it entirely with your own headings and format. See [Release notes style](/customize/release-notes-style/).

## Next steps

- [How it works](/start-here/how-it-works/)
- [Set up a repository](/start-here/set-up/)
- [Make a release](/start-here/release/)
- [Customize your releases](/customize/policy/)
