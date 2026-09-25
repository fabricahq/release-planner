---
title: Make a release
description: Ask for a release, review the notes, and merge to publish.
---

## Ask for a release

Tell your agent "let's release." It works through the release procedure on its own and opens a pull request titled **Release v&lt;version&gt;**. The pull request adds one file with the release notes. The agent's part of its description is one sentence on why it chose the version, and any change it left out of the notes that you might expect to see.

The agent never merges its own release pull request.

The release workflow writes the rest of the description around that sentence, and updates it on every push:

- at the top, a link to edit the notes on GitHub, what merging does, and the version, the release commit, and the previous release
- at the bottom, each job with a link to it, including your [release checks](/customize/release-checks/), any [release notes rules](/customize/release-notes-style/#release-notes-rules) the notes break, and any [release assets](/customize/release-assets/) with a link to download them

It leaves the agent's sentence, and anything you add around it, alone.

## Review the notes

Use the link in the pull request description to edit the notes file on GitHub. Rewrite anything you like: the notes you merge are published exactly as written. Check that:

- the version is right for the changes, especially if anything breaks compatibility
- the notes lead with what matters most
- nothing important is missing

To change the version, ask the agent to redo the release for the version you want, or rename the file yourself, for example from `_releases/v1.1.0.md` to `_releases/v2.0.0.md`, and update the **Full Changelog** link at the end. You can also ask the agent to revise the notes; it keeps your edits.

Saving edits publishes nothing, and doesn't change the release commit.

The release contains the changes up to the commit the pull request branched from. If you want changes merged since then in the release, ask the agent to include them; it merges the main branch into the pull request and adds them to the notes.

## Merge to publish

Merging the pull request is the approval. Wait for the release status to show the checks passed first. The release workflow then tags the release commit and publishes the notes as the GitHub release, with the files the pull request built, and updates the description with a link to it. If you configured [downstream workflows](/customize/downstream/), it starts them next.

If anything fails, the top of the description names the failed job and how to retry, and the workflow comments on the pull request to mention you, since GitHub doesn't notify anyone about an edited description.

## If publishing fails

Open the run the description links to. Once the problem is fixed, use **Re-run failed jobs** on that run. Re-running is always safe: it publishes the same release commit and files, and if the release was already published, the workflow changes nothing.

## Correct published notes

Published tags and files never change, but notes can. Ask your agent to fix the notes of a release, or edit its file, such as `_releases/v1.1.0.md`, in a pull request of its own. Its description says merging updates that release's notes, and warns if someone edited them on GitHub since, because merging replaces those edits. Merging updates the release on GitHub. Deleting a published release's notes file isn't allowed.
