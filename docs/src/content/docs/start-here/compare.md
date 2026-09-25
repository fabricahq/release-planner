---
title: Release Planner vs. other tools
description: Which kind of release tool Release Planner is, how it compares with the others of its kind, and who should use something else.
---

There are quite a few release automation tools available today. They all streamline releasing and let you customize how, but they differ in the human workflow: how release notes are prepared, reviewed, and finalized.

The approaches to human workflows broadly fall into three categories:

- **Computed release notes**, generated from commit messages or pull request titles.
- **Written release notes**, written by a person or an agent, usually in a dedicated release pull request.
- **Notes left to you**, as with build and packaging tools, which build and ship your software but don't prepare release notes.

## Where Release Planner fits

Release Planner is a **written release notes** tool, designed agent-first:

- **Your agent writes** the notes for the whole release, following your [`policy.md`](/customize/policy/).
- **Release Planner computes** whatever it can deterministically, such as every change since the last release, and checks the agent's notes against [release notes rules](/customize/release-notes-style/#release-notes-rules).
- **You review and merge** one pull request to publish.

## Release Planner vs. computed release notes tools

Tools like semantic-release, release-please, and git-cliff compute the version and changelog from your commit messages, so nobody has to write notes. The quality of the release notes ultimately depends on the quality of the commit messages.

With Release Planner, your agent reads the pull requests themselves, so contributors don't need a commit convention, and the notes can group related changes and explain what they mean at a high level for your users. You review the version, the notes, and any built files in one pull request before anything is published.

## Release Planner vs. other written release notes tools

Tools like Changesets and towncrier have each pull request author write a short note, and collect the notes into a release.

With Release Planner, contributors write nothing extra. At release time, your agent writes the notes for the whole release, guided by your [release policy](/customize/policy/): who reads your notes, what counts as a breaking change, how to choose the version, and what to always or never mention. The notes read as one release rather than a list of fragments. You still have the final say: change anything in the release pull request before you merge it.

## Release Planner vs. build and packaging tools

Tools like GoReleaser build and package your software, and Release Planner doesn't replace them. Run them from your [build workflow](/customize/release-assets/) with their own publishing turned off, and Release Planner publishes the files with your notes, then starts any [downstream workflows](/customize/downstream/), such as a Homebrew tap update.

## Who Release Planner is not for

- **You want every merge released automatically, with no one approving.** Try semantic-release.
- **You publish many packages from one repository, each with its own version.** Release Planner releases one version per repository. Try Changesets.
- **You can't use an AI agent, or need notes produced the same way every time.** Try release-please or git-cliff.
- **You're not on GitHub.** Release Planner currently works exclusively with GitHub releases and runs on GitHub Actions.
