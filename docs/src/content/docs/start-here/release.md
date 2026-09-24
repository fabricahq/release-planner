---
title: Make a release
description: Ask for a release, review the notes, and merge to publish.
---

## Ask for a release

Tell your agent "let's release." It works through the release procedure on its own and opens a pull request titled **Release v&lt;version&gt;**. The pull request adds one file with the release notes, and its description explains:

- the previous release and the version the agent chose, and why
- every pull request since the previous release, and where each one appears in the notes, or why it was left out

The agent never merges its own release pull request.

## Review the notes

Use the link in the pull request description to edit the notes file on GitHub. Rewrite anything you like: the notes you merge are published exactly as written. Check that:

- the version is right for the changes, especially if anything breaks compatibility
- the opening sentences say what matters most
- nothing important is missing

To change the version, ask the agent to redo the release for the version you want, or rename the file yourself, for example from `releases/v1.1.0.md` to `releases/v2.0.0.md`, and update the **Full Changelog** link at the end. You can also ask the agent to revise the notes; it keeps your edits.

Saving edits publishes nothing.

## Merge to publish

Merging the pull request is the approval. The release workflow then:

1. Checks the request: one notes file, a version newer than every existing release, and no unfinished draft text.
2. Runs your [release checks](/customize/release-checks/), if you configured any.
3. Tags the merged commit and publishes the notes as the GitHub release.

If anything fails, nothing is tagged or published.

## If publishing fails

Open the failed run in the repository's **Actions** tab. Once the problem is fixed, use **Re-run all jobs** on that run. Re-running is always safe: if the release was already published, the workflow changes nothing.

Published releases and tags never change. To correct a published release, make a new one. You can edit the text of a published release on GitHub by hand, if only the wording needs fixing.
