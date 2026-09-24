---
title: How Release Planner works
description: Commands, file formats, checks, and the release workflow, for agents and maintainers who need the details.
---

This page is the reference for agents working in a repository that uses Release Planner, and for anyone debugging a release.

## Start here

When asked to make a release, draft or revise release notes, or retry a failed release, print the release procedure for the repository's pinned version and follow it:

```sh
release-planner guide
```

First check that `release-planner version` prints the `version` in `.release-planner/config.yml`. If it doesn't, install that version:

```sh
curl -fsSL https://raw.githubusercontent.com/fabricahq/release-planner/<version>/install.sh | sh -s -- --version <version>
```

For a commit SHA pin, use `go install github.com/fabricahq/release-planner/cmd/release-planner@<sha>` instead. The `AGENTS.md` section and the `release` skill that `install` generates say exactly this. The guide includes the repository's release notes style and settings, so it is the complete set of instructions. The rest of this page explains what the commands do.

Never tag, publish, or merge a release pull request. The maintainer approves a release by merging.

## Commands

Run every command from the repository root, with the pinned version installed. Each accepts `--dir` to run against another directory.

| Command | Used by | What it does |
| --- | --- | --- |
| `init [--version <tag>] [--first-version <version>]` | Maintainer | Creates `.release-planner/config.yml` and a starter `policy.md`. Refuses if the config exists. |
| `install [--force]` | Maintainer | Writes or updates the generated files. Refuses to overwrite hand edits or files it didn't write unless `--force`. Writes nothing if anything is refused. |
| `check` | CI, maintainer | Exits 1 if a generated file is missing, stale, or edited by hand. Writes nothing. |
| `uninstall [--force]` | Maintainer | Deletes the generated files and the `AGENTS.md` section. |
| `guide [--default-style]` | Agent | Prints the release procedure, or only the default release notes style. |
| `inventory [--head <ref>] [--repository <owner/name>] [--offline]` | Agent | Prints JSON describing everything since the previous release, including each pull request author's GitHub handle. |
| `draft [--repository <owner/name>] <version>` | Agent | Creates the notes file for a version. |
| `plan --base <ref> [--head <ref>] [--out <file>]` | CI, agent | Validates the release request between two commits and prints the plan as JSON. |
| `publish --plan <file> --commit <sha> --branch <name> [--assets <dir>]` | CI | Tags the approved commit and publishes the release. Refuses unless the commit is a pull request merged into the branch. With `--assets`, uploads the directory's files to a draft, verifies GitHub's checksums for them, and publishes last. Needs `GITHUB_TOKEN` and `GITHUB_REPOSITORY`. |
| `version` | Anyone | Prints the running version. |

`install` and `check` refuse to run when the running version differs from the version the config pins, because they would render the wrong files.

## Files

| Path | Owner | Purpose |
| --- | --- | --- |
| `.release-planner/config.yml` | Repository | Settings. See [Configuration](/customize/configuration/). |
| `.release-planner/policy.md` | Repository | What users depend on, how to choose versions, who reads the notes, and what to always include or leave out. Wins over the guide where they differ. |
| The file `release-notes-style.file` names, conventionally `.release-planner/release-notes-style.md` | Repository, optional | Appends to or replaces the default release notes style, as `release-notes-style.mode` says. |
| `releases/v<version>.md` | Release pull request | The notes for one release, published word for word. |
| `.github/workflows/release-planner.yml` | Generated | The release workflow. |
| `.agents/skills/release/SKILL.md`, `.claude/skills/release/SKILL.md` | Generated | Tell agents to run `guide` when asked to release. |
| `AGENTS.md`, between the `release-planner:begin` and `release-planner:end` markers | Generated | The same pointer, for agents that read `AGENTS.md`. |

Each generated file carries a `release-planner:generated <version> sha256:<digest>` marker line, and the `AGENTS.md` section's begin marker carries the same fields. The digest covers the rest of the content, so `install` can distinguish an unedited older version, which it updates, from a hand edit, which it refuses to overwrite.

If `policy.md` is missing or still contains `TODO:` prompts, stop and ask the maintainer to write it. Offer to draft it from the repository's history for their review.

## Inventory

`inventory` prints everything since the newest version tag that the head contains:

```json
{
  "head": "5ba22db…",
  "previous": "v1.0.0",
  "candidates": { "major": "v2.0.0", "minor": "v1.1.0", "patch": "v1.0.1" },
  "pendingRequests": [],
  "unmergedNewerTags": [],
  "pullRequests": [7],
  "commits": [
    { "sha": "1d7ee9b…", "author": "Ada", "subject": "Add Svelte runes rules",
      "title": "Add Svelte runes rules", "onBranch": false },
    { "sha": "5ba22db…", "author": "Ada", "subject": "Merge pull request #7 from example/svelte",
      "title": "Add a Svelte group", "pullRequest": 7, "authorHandle": "ada", "onBranch": true }
  ],
  "warnings": []
}
```

- `candidates` holds the next patch, minor, and major versions. With no previous release, it holds only `first`, the configured first version.
- `pendingRequests` lists notes files without a tag: a release that was approved but never published. Resolve these before preparing another release.
- `unmergedNewerTags` lists version tags newer than `previous` that the head doesn't contain. Stop and ask.
- `title` is the pull request's title for merge and squash commits, read from the merge commit's body or the squash subject.
- `onBranch` is true for commits made directly on the release branch: pull request merges and direct commits. The others are commits inside merged branches.
- `author` is the git author name, not a GitHub handle. `authorHandle` is the pull request author's GitHub handle, which `inventory` looks up on GitHub for each pull request.
- `warnings` explains anything `inventory` couldn't look up, such as a handle when GitHub was unreachable. It never fails for that reason.

To look up handles, `inventory` uses `GITHUB_TOKEN`, `GH_TOKEN`, or the GitHub CLI's login, in that order, or no token for a public repository. It reads the repository from the `origin` remote unless given `--repository`. Pass `--offline` to skip the lookups.

## Drafts

`draft <version>` creates `releases/<version>.md` containing:

1. A placeholder opening line starting `TODO: Open with one or two sentences`.
2. `## Pull Requests`, a plain list of each pull request merged into the release branch and each direct commit, in merge order, as `- <title> in #<number>`. `draft` doesn't group or format the list; the release notes style decides that.
3. A closing line: `**Full Changelog**: <compare link>`, or for a first release, a link to the tagged source.

The agent replaces the placeholder, adds headings and entries following the release notes style, arranges the Pull Requests entries as the style says, and removes entries the inventory leaves out. Every pull request stays listed. The default style groups the entries by conventional-commit type and writes each as `- <title> by @<handle> in #<number>`, with the handle taken from the pull request, never guessed. `draft` refuses a version that isn't newer than every tag, a first release that doesn't match `first-version`, an existing file, or an unpublished earlier request. It reads the GitHub repository from the `origin` remote unless given `--repository`.

## What `plan` checks

`plan` compares two commits and finds the release request between them. It fails, and nothing is published, when:

- more than one notes file was added or changed
- a notes file for an already-tagged version changed (published notes are immutable)
- the filename isn't a SemVer 2.0.0 version with a leading `v`, or has build metadata
- the notes are empty, still contain the draft placeholder, or have a `##` heading with nothing under it
- another notes file exists without a tag (a pending request)
- the version isn't newer than every existing version tag
- there are no releases yet and the version isn't `first-version`
- the version's tag already exists on a different commit
- the previous release isn't an ancestor of the head

A range with no notes change produces a plan with an empty `tag`, meaning no release was requested. Run `plan --base origin/<branch> --head HEAD` before opening the release pull request; it must print the intended version.

## The release workflow

The generated workflow runs when a pull request changes `releases/`, `.release-planner/`, or the workflow itself, and when a notes file lands on the release branch.

1. **plan** (read-only): installs the pinned Release Planner, checks out the head with full history, runs `check`, then `plan`. For a release tag pin, installing downloads the release, verifies the build attestation of its `SHA256SUMS`, and checks the archive against it; a commit SHA pin is built from source with Go. On a pull request, this is the whole run: it validates the request without publishing.
2. **validate** (optional): runs the repository's [release checks](/customize/release-checks/) on the planned commit.
3. **publish**: the only job with permission to write. It runs Release Planner at the pinned version and none of the repository's code. Before writing, it confirms that the version tags haven't changed since planning, that the previous release is published, and that any existing tag or release matches the plan. It then creates the tag and release, and afterward verifies that the tag points to the approved commit. If a matching release is already published, it changes nothing.

The publish job uses the `release` environment and runs one publication at a time across the repository.

## Retrying a failed release

Read the failed run and any existing tag or release before acting. Prefer **Re-run all jobs** on the failed run. For a manual retry, run the workflow on the release branch with the **Base SHA** and **Approved head SHA** from the failed run's summary; never substitute the latest commit.

Published tags and releases never move. If validation failed before tagging, fix the problem in a separate pull request, then correct the untagged notes in a new release pull request, or withdraw the request by deleting its file.
