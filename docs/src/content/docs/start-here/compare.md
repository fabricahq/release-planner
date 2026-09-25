---
title: Release Planner vs. other tools
description: Which kind of release tool Release Planner is, how it compares with the others of its kind, and who should use something else.
---

There are a _lot_ of release automation tools available, and they generally fall into three camps:

- **Computed release notes.** These tools deterministically generate release notes as a list of changes from commit messages or pull request titles.
- **Written release notes.** With these tools, humans write the notes, review them in a pull request, and then merge the pull request to kick off the release.
- **Build and packaging tools.** These tools build, package, and distribute your software, and leave the notes to something else.

Release Planner is in the **written release notes** camp. The difference is who writes the notes, and when. It's also not mutually exclusive with other tools: it can publish your GitHub releases on its own, or work alongside the tools you already use to build and distribute your software.

## Release Planner vs. computed release notes tools

Tools like semantic-release and release-please read your commit history, usually written as [Conventional Commits](https://www.conventionalcommits.org/), and compute the next version and a changelog from it. git-cliff and GitHub's generated release notes do the same for the notes alone. Because the notes are computed, nobody has to write them.

Release Planner takes that work away too, but it's agent-first: your coding agent writes the notes, not a template. A computed changelog can only repeat what commit messages say, so it's only as good as everyone's discipline in writing them. Your agent reads the pull requests and their changes, so contributors don't need any convention, and the notes can group related changes, leave out internal ones, and explain what a change means for your users. Because the notes are files in your repository, you can also fix a published release's notes later in a pull request.

Release Planner also separates planning a release from publishing it. The release pull request is the plan: it shows the version and why it was chosen, the notes, the results of your release checks, and the files built for the release. You can review and edit all of it before anything happens. Merging publishes exactly that plan.

## Release Planner vs. other written release notes tools

Written release notes tools like Changesets and towncrier have pull request authors write a short release note with each pull request. The tool later collects them into a release. Because the notes come from the humans who authored the change, they usually read much better than a list of commits.

Release Planner also produces written notes, but writes them once, at release time, instead of in every pull request. Because your agent sees the whole release at once, the notes read as one release rather than a pile of fragments, and you review them in one place, together with the version, which your agent picks from your [release policy](/customize/policy/) and explains. [Release notes rules](/customize/release-notes-style/#release-notes-rules) then check the result, whether your agent or you wrote the text.

## Release Planner vs. build and packaging tools

Tools like GoReleaser build and package your software: cross-compiled binaries, archives, checksums, and package manager formulas. Release Planner doesn't replace them. It decides what's in a release and writes the notes, and leaves building to your own workflow.

Run GoReleaser, or any build tool, from your [build workflow](/customize/release-assets/) with its own publishing turned off. Release Planner builds the files on the release pull request, attests them, publishes them with the notes when you merge, and then starts any [downstream workflows](/customize/downstream/), such as a Homebrew tap update.

## Who it's not for

- **You want every merge released automatically, with no one approving.** Try semantic-release.
- **You publish many packages from one repository, each with its own version.** Release Planner releases one version per repository. Try Changesets.
- **You can't use an AI agent, or need notes produced the same way every time.** Try release-please or git-cliff.
- **You're not on GitHub.** Release Planner publishes GitHub releases and runs on GitHub Actions.
