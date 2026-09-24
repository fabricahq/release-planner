---
title: What is Release Planner?
description: A simple, agent-assisted way to publish well-written GitHub releases.
---

Release Planner makes it easy for your GitHub repository to publish releases people actually want to read. Your coding agent does the tedious work of reviewing what changed and drafting the notes. You stay in charge of what ships: nothing is published until you approve it.

## What making a release looks like

With Release Planner set up, here's how a new release happens:

<div class="release-steps">

| Step | Who | What happens |
| --: | --- | --- |
| 1 | <span class="step-badge human">Human</span> | Ask for a release. Tell your agent "let's release." |
| 2 | <span class="step-badge">Automated</span> | Release Planner gathers every commit and pull request merged since your last release and hands that list to your agent. The agent reads through the changes, works out what they mean for your users, and drafts the notes. |
| 3 | <span class="step-badge">Automated</span> | Your agent opens a pull request titled **Release v1.1.0**. It contains the drafted release notes and explains why the agent chose that version. |
| 4 | <span class="step-badge human">Human</span> | Review the notes and change anything you like. Nothing is published while the pull request is open. |
| 5 | <span class="step-badge human">Human</span> | Merge the pull request. Merging is what approves and starts the release. |
| 6 | <span class="step-badge">Automated</span> | A GitHub Actions workflow tags the commit you merged and publishes your new version on the repository's **Releases** page, with exactly the notes you approved. |

</div>

The steps marked **Human** are yours: ask, review, and merge. Everything else happens on its own. There's no changelog to maintain, no commit message conventions to follow, and no release checklist to remember.

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

#### What's Changed

- Add a Svelte group in #7
- Correct the Zustand hydration example in #8
- Add Svelte examples for stores and snippets in #9

**Full Changelog**: v1.0.0...v1.1.0

</div>

Release Planner comes with an opinionated release notes style, shown above.

The style is fully customizable. You can add your own rules on top of it, such as always naming affected API endpoints, or replace it entirely with your own headings and format. See [Release notes style](/customize/release-notes-style/).

## Next steps

- [How it works](/start-here/how-it-works/)
- [Set up a repository](/start-here/set-up/)
- [Make a release](/start-here/release/)
- [Customize your releases](/customize/policy/)
