---
name: release
description: Prepare a release PR with drafted release notes, for repositories that publish with fabricahq/release-planner. Use when asked to release, issue or cut a release, draft or revise release notes, or retry a failed release.
---

# Prepare a release

The maintainer approves a release by merging a PR that adds `releases/v<version>.md`. On merge, the repository's Release workflow tags the merged commit and publishes that file verbatim as the GitHub release. Your job is to open that PR with accurate notes. You never tag, publish, or merge.

Read `releases/README.md` first. It holds this repository's version policy, what counts as its public contract, its first version, and how to validate a release. Where it differs from this skill, it wins.

## 1. Get the planner

Use the Release Planner version the workflow pins, so your plan matches CI:

```sh
sha=$(grep -oE 'fabricahq/release-planner/plan@[0-9a-f]{40}' .github/workflows/release.yml | head -1 | cut -d@ -f2)
git clone --quiet https://github.com/fabricahq/release-planner.git /tmp/release-planner
git -C /tmp/release-planner checkout --quiet "$sha"
planner="python3 /tmp/release-planner/src/release_planner.py --first <first version from releases/README.md>"
```

In the Release Planner repository itself, use `src/release_planner.py` from the checkout instead.

## 2. Establish the range

Fetch `main` and tags, then run `$planner inventory --head origin/main`. It reports the previous release, every commit since it, the PR numbers it found, and the candidate next versions.

- If `pendingRequests` is not empty, an earlier request never published. Inspect it and its workflow run with the maintainer before preparing another.
- If `unmergedNewerTags` is not empty, a tag exists that `main` does not contain. Stop and ask.
- List GitHub releases too. A draft release, or a tag without a published release, is unfinished work to resolve first.

## 3. Account for every change

Read every commit since the previous release and the PR each belongs to, including direct commits. Read the relevant diffs, tests, and docs. Build an inventory that maps every commit or PR to a release-note entry or a reason for omitting it. Account for reverts and superseded work. Internal refactors, CI changes, and docs-only changes may appear only in the inventory.

## 4. Choose the version

Pick from the inventory's `candidates` using the policy in `releases/README.md`. With no previous release, use the first version it names. Explain the increment in the PR description, and flag uncertainty rather than inventing compatibility guarantees.

## 5. Draft the notes

On a release branch, such as `release/v<version>`, create exactly one `releases/v<version>.md`. The filename sets the version, and the contents become the release body verbatim. Use these headings when they have content, and omit empty ones:

- `## ✨ New Features`: what users can do that they couldn't before.
- `## ⬆️ Improvements`: better behavior, performance, or usability of something that existed.
- `## 🐛 Squashed Bugs`: the trigger, the previous wrong behavior, and the corrected result.
- `## ⛓️‍💥 Breaking Changes`: who is affected, the old and new contract, and exact migration steps. Also call these out near the top.
- `## What's Changed`: linked PR and commit inventory.
- `## New Contributors`: verified first contributions only, with credit.

Give significant changes a `###` heading and a sentence on why they matter. Use bullets for small changes, code blocks for commands, and before/after examples where they help. Group related commits into one entry. Use absolute links, since the notes render on the release page. End with one **Full Changelog** link comparing the actual previous and new tags. For a first release, link to the tagged source and say it is the first release.

## 6. Validate and open the PR

Commit the notes, then run:

```sh
$planner plan --base origin/main --head HEAD
```

Also run the validation `releases/README.md` names. Then open a PR titled `Release v<version>`. In its description:

- Link directly to the notes file's GitHub editor: `https://github.com/<owner>/<repo>/edit/<branch>/releases/v<version>.md`.
- Say that saving edits changes nothing published, and that merging publishes the release.
- Record the previous tag, the analyzed `main` SHA, the chosen increment and why, and the full inventory.

Leave the PR for the maintainer. Do not merge it.

## 7. Revise on request

When asked to revise, fetch the release branch and keep the maintainer's edits. If `main` has advanced, add the new commits to the inventory and notes and bring the branch up to date. Keep feature work out of the release PR. You are done when every change through the analyzed SHA is accounted for and CI is green.

## Retrying a failed release

Read the failed Release run and any existing tag or release before acting. Prefer **Re-run all jobs** on that run. For a manual retry, run the Release workflow on `main` with the **Base SHA** and **Approved head SHA** from the failed run's summary. Never substitute the latest `main`. Published tags and releases never move: fix problems in a new release. If validation failed before tagging, fix the code in a separate PR, then correct the untagged notes in a new release PR, or withdraw the request by deleting its file.
