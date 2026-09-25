---
title: Release Planner vs. other tools
description: Which kind of release tool Release Planner is, how it compares with the others of its kind, and who should use something else.
---

## The three camps of release automation tools

There are quite a few release automation tools available today, and they broadly fall into one of three camps.

### Computed release notes

Release notes are generated from commit messages or pull request titles.

_Examples: semantic-release, release-please, git-cliff._

### Written release notes

Release notes are written by a person or an agent, usually in a dedicated release pull request.

_Examples: Changesets, towncrier, and **Release Planner**._

### Build and packaging tools

Build and ship your software, and leave the notes to something else.

_Example: GoReleaser._

## Where Release Planner fits

Release Planner is a **written release notes** tool, designed agent-first:

- **Your agent writes** the notes for the whole release, following your [`policy.md`](/customize/policy/).
- **Release Planner computes** whatever it can deterministically, such as every change since the last release, and checks the agent's notes against [release notes rules](/customize/release-notes-style/#release-notes-rules).
- **You review and merge** one pull request to publish.

## Release Planner vs. computed release notes tools

Tools like semantic-release and release-please read your commit history, usually written as [Conventional Commits](https://www.conventionalcommits.org/), and compute the next version and a changelog from it. git-cliff and GitHub's generated release notes do the same for the notes alone. Because the notes are computed, nobody has to write them.

Release Planner takes that work away too, but it's agent-first: your coding agent writes the notes, not a template. A computed changelog can only repeat what commit messages say, so it's only as good as everyone's discipline in writing them. With Release Planner, your agent reads the pull requests and their changes, so contributors don't need any convention, and the notes can group related changes, leave out internal ones, and explain what a change means for your users. Because the notes are files in your repository, you can also fix a published release's notes later in a pull request.

Release Planner also separates planning a release from publishing it. The release pull request is the plan: it shows the version and why it was chosen, the notes, the results of your release checks, and the files built for the release. You can review and edit all of it before anything happens. Merging publishes exactly that plan.

## Release Planner vs. other written release notes tools

Written release notes tools like Changesets and towncrier have pull request authors write a short release note with each pull request. The tool later collects them into a release. Because the notes come from the humans who authored the change, they usually read much better than a list of commits.

Release Planner also produces written notes, but your agent writes them, once, at release time. Contributors don't write anything extra in their pull requests. When you ask for a release, your agent reads every pull request merged since the last one, selects the right "next version" based on your [release policy](/customize/policy/), and writes notes for the whole release. Because it sees the whole release at once, the notes read as one release rather than a pile of fragments.

You still have the final say. The notes and the version arrive together in one pull request, where you can change anything. When you merge the pull request, you'll kick off a release using exactly the release notes you approved.

## Release Planner vs. build and packaging tools

Tools like GoReleaser build and package your software: cross-compiled binaries, archives, checksums, and package manager formulas. Release Planner doesn't replace them. It decides what's in a release and writes the notes, and leaves building to your own workflow.

Run GoReleaser, or any build tool, from your [build workflow](/customize/release-assets/) with its own publishing turned off. Release Planner builds the files on the release pull request, attests them, publishes them with the notes when you merge, and then starts any [downstream workflows](/customize/downstream/), such as a Homebrew tap update.

## Who Release Planner is not for

- **You want every merge released automatically, with no one approving.** Try semantic-release.
- **You publish many packages from one repository, each with its own version.** Release Planner releases one version per repository. Try Changesets.
- **You can't use an AI agent, or need notes produced the same way every time.** Try release-please or git-cliff.
- **You're not on GitHub.** Release Planner currently works exclusively with GitHub releases and runs on GitHub Actions.
