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
| `inventory [--head <ref>] [--repository <owner/name>] [--offline]` | Agent | Prints JSON describing everything since the previous release, including each pull request author's GitHub handle and the lines that list each change in the notes. |
| `validate --base <ref> [--head <ref>] [--repository <owner/name>] [--out <file>] [--ci --event <name>]` | Agent, CI | Validates the release request between two commits and its notes, and prints the release plan as JSON. |
| `validate --rules` | Anyone | Lists the release notes rules. |
| `publish --plan <file> --commit <sha> --branch <name> [--assets <dir>]` | CI | Tags the approved commit and publishes the release. Refuses unless the commit is a pull request merged into the branch. With `--assets`, uploads the directory's files to a draft, verifies GitHub's checksums for them, and publishes last. Needs `GITHUB_TOKEN` and `GITHUB_REPOSITORY`. |
| `version` | Anyone | Prints the running version. |

`install` and `check` refuse to run when the running version differs from the version the config pins, because they would render the wrong files.

## Files

| Path | Owner | Purpose |
| --- | --- | --- |
| `.release-planner/config.yml` | Repository | Settings. See [Configuration](/customize/configuration/). |
| `.release-planner/policy.md` | Repository | What users depend on, how to choose versions, who reads the notes, and what to always include or leave out. Wins over the guide where they differ. |
| The file `release-notes-style.file` names, conventionally `.release-planner/release-notes-style.md` | Repository, optional | Appends to or replaces the default release notes style, as `release-notes-style.mode` says. |
| `_releases/v<version>.md` | Release pull request | The notes for one release, published word for word. |
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
      "title": "Add a Svelte group", "pullRequest": 7, "authorHandle": "ada", "onBranch": true,
      "entry": "- Add a Svelte group by @ada in #7" }
  ],
  "newContributors": [
    { "handle": "ada", "pullRequest": 7, "entry": "- @ada made their first contribution in #7" }
  ],
  "closing": "**Full Changelog**: https://github.com/example/rules/compare/v1.0.0...<version>",
  "warnings": []
}
```

- `candidates` holds the next patch, minor, and major versions. With no previous release, it holds only `first`, the configured first version.
- `pendingRequests` lists notes files without a tag: a release that was approved but never published. Resolve these before preparing another release.
- `unmergedNewerTags` lists version tags newer than `previous` that the head doesn't contain. Stop and ask.
- `title` is the pull request's title for merge and squash commits, read from the merge commit's body or the squash subject.
- `onBranch` is true for commits made directly on the release branch: pull request merges and direct commits, including those brought in by merging the release branch into a release pull request's branch. The others are commits inside merged pull requests.
- Commits that change only the notes directory, such as a release request and its merge, are left out.
- `author` is the git author name, not a GitHub handle. `authorHandle` is the pull request author's GitHub handle, which `inventory` looks up on GitHub for each pull request.
- `newContributors` lists each author with no commits in the previous release, with the first pull request in this release that is theirs. In a first release, every author is new. Bots are left out.
- `entry` is the line that lists a change under `## Pull Requests`: `- <title> by @<handle> in #<number>` for each pull request merged into the release branch, without ` by @<handle>` when the handle is unknown, and `- <title> in <commit URL>` for each direct commit. Merges of other branches have none.
- `newContributors` entries are the lines for `## New Contributors`.
- `closing` is the notes' last line. After a previous release, replace `<version>` with the version you chose.
- `warnings` explains anything `inventory` couldn't look up, such as a handle when GitHub was unreachable. It never fails for that reason.

To look up handles, `inventory` uses `GITHUB_TOKEN`, `GH_TOKEN`, or the GitHub CLI's login, in that order, or no token for a public repository. It reads the repository, for lookups and links, from the `origin` remote unless given `--repository`. Pass `--offline` to skip the lookups. Without a repository, the links name `<owner>/<name>` for you to replace.

## Notes

The agent writes `_releases/<version>.md` in one pass: the sections the release notes style and policy call for, with no introduction; then `## Pull Requests`, with every `entry` from `inventory` grouped under `###` headings; then `## New Contributors`, if any; then the `closing` line. Titles in entries can be edited; the pull request number or commit link must stay.

To revise the notes, edit the file in place. Never regenerate it or paste one copy over another. If the release branch moved, merge it into the release pull request's branch, rerun `inventory`, and add the new entries.

## What `validate` checks

`validate` compares two commits and finds the release request between them. It fails, and nothing is published, when:

- more than one notes file was added or changed
- a notes file for an already-tagged version changed (published notes are immutable)
- the filename isn't a SemVer 2.0.0 version with a leading `v`, or has build metadata
- the notes are empty
- another notes file exists without a tag (a pending request)
- the version isn't newer than every existing version tag
- there are no releases yet and the version isn't `first-version`
- the version's tag already exists on a different commit
- the previous release isn't an ancestor of the head

A range with no notes change produces a plan with an empty `tag`, meaning no release was requested. Run `validate --base origin/<branch> --head HEAD` after committing the notes, and before opening the release pull request; it must print the intended version.

It then checks the notes against the release notes rules:

| Rule | What it enforces |
| --- | --- |
| `no-empty-heading` | Every heading has content before the next heading at its level or above. |
| `no-duplicate-heading` | No `##` heading appears twice, and no `###` heading appears twice under the same `##`. |
| `no-long-heading` | `##` and `###` headings are at most 80 characters. |
| `require-pull-requests-last` | Exactly one `## Pull Requests`; it and an optional `## New Contributors` are the last `##` sections, followed only by the closing line. |
| `list-every-change` | Every pull request and direct commit on the release branch from the previous tag to the head is listed exactly once under `## Pull Requests`, matched by `#N` or `/pull/N` and `/commit/<sha>`, and nothing else is. Uses only git, never the network. |
| `require-closing-link` | Exactly one closing line: `**Full Changelog**: …/compare/<previous>...<version>`, or for a first release, `This is the first release. Browse the source at [<version>](…)`. |

Without `--ci`, a broken rule fails, listing every finding with its rule, line, and message. With `--ci`, findings are GitHub warning annotations and step summary entries, and don't fail the run or block publishing, because the maintainer may break a rule on purpose. Rules turned off in `release-notes-rules` are skipped. The closing link must name the repository given by `--repository`, or else `GITHUB_REPOSITORY` with `--ci`, or else the `origin` remote; in a fork's clone, pass the upstream repository.

## The release workflow

The generated workflow runs when a pull request changes `_releases/`, `.release-planner/`, or the workflow itself, and when a notes file lands on the release branch.

1. **validate** (read-only): installs the pinned Release Planner, checks out the head with full history, runs `check`, then `validate --ci`, which writes the release plan and reports broken release notes rules as warnings. For a release tag pin, installing downloads the release, verifies the build attestation of its `SHA256SUMS`, and checks the archive against it; a commit SHA pin is built from source with Go. For a release request, it also checks the `release` environment's settings and warns, without failing, if the environment doesn't exist yet, lets any branch deploy, doesn't let the release branch deploy, or requires reviewers. On a pull request, this is the whole run: it validates the request without publishing.
2. **release-checks** (optional): runs the repository's [release checks](/customize/release-checks/) on the planned commit.
3. **publish**: the only job with permission to write. It runs Release Planner at the pinned version and none of the repository's code. Before writing, it confirms that the version tags haven't changed since validation, that the previous release is published, and that any existing tag or release matches the plan. It then creates the tag and release, and afterward verifies that the tag points to the approved commit. If a matching release is already published, it changes nothing.

The publish job uses the `release` environment and runs one publication at a time across the repository.

## Retrying a failed release

Read the failed run and any existing tag or release before acting. Prefer **Re-run all jobs** on the failed run. For a manual retry, run the workflow on the release branch with the **Base SHA** and **Approved head SHA** from the failed run's summary; never substitute the latest commit.

Published tags and releases never move. If the run failed before tagging, fix the cause in a separate pull request, then correct the untagged notes in a new release pull request, or withdraw the request by deleting its file.
