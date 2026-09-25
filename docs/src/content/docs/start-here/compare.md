---
title: Release Planner vs. other tools
description: Which kind of release tool Release Planner is, how it compares with the others of its kind, and who should use something else.
---

Release tools fall into two camps:

- **Generated notes.** semantic-release, release-please, git-cliff and GitHub's generated release notes build notes from commit messages or pull request titles. The notes are a list of changes, and they're only as good as everyone's commit messages and labels.
- **Written notes.** Changesets and towncrier have people write the notes. Each pull request adds a short note, and the tool collects them into the release.

Release Planner is in the written-notes camp. The difference is who writes the notes, and when.

## How it's different

With Changesets or towncrier, every contributor writes a note in every pull request. With Release Planner, your agent writes the notes once, at release time, from everything that merged. You review them in one pull request.

- **Nobody has to remember anything.** Contributors don't add note files, follow commit conventions, or label pull requests.
- **The notes read as one release, not a pile of fragments.** Your agent can combine three pull requests into one feature, leave out internal changes, and explain why a change matters to your users.
- **You review the whole release at once**, including the version. Your agent picks it from your [release policy](/customize/policy/) and says why.

Release Planner also:

- **Ships exactly what you approved.** Checks and [builds](/customize/release-assets/) run on the release pull request, and merging publishes those same files.
- **Checks the notes.** [Release notes rules](/customize/release-notes-style/#release-notes-rules) catch common problems, whether your agent or you wrote the text.
- **Lets you fix notes later.** Edit a published version's notes in a pull request, and the GitHub release updates when it merges.

Already use a build tool like GoReleaser? Keep it, and run it from your [build workflow](/customize/release-assets/).

## Who it's not for

- **You want every merge released automatically, with no one approving.** Try semantic-release.
- **You publish many packages from one repository, each with its own version.** Release Planner releases one version per repository. Try Changesets.
- **You can't use an AI agent, or need notes produced the same way every time.** Try release-please or git-cliff.
- **You're not on GitHub.** Release Planner publishes GitHub releases and runs on GitHub Actions.
