---
title: How Release Planner works
description: Commands, file formats, checks, and the release workflow, for agents and maintainers who need the details.
---

This page is the reference for agents working in a repository that uses Release Planner, and for anyone debugging a release.

## Start here

When asked to make a release, draft, revise, or correct release notes, or retry a failed release, print the release procedure for the repository's pinned version and follow it:

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
| `validate --base <ref> [--head <ref>] [--repository <owner/name>] [--out <file>] [--ci [--head-repository <owner/name>]]` | Agent, CI | Validates the release pull request whose head is `--head` against the release branch at `--base`, and prints the release plan as JSON, including the release commit. |
| `validate --ci --merged <sha> [--out <file>]` | CI | After the merge: finds the pull request merged as `<sha>`, plans its release commit, checks the merged notes are the approved ones, and finds the pull request's run whose checks and assets the release reuses. |
| `validate --rules` | Anyone | Lists the release notes rules. |
| `publish --plan <file> --branch <name> [--built-plan <file>] [--assets <dir> [--signer-workflow <path>]]` | CI | Tags the release commit and publishes the release, and replaces the notes of published releases the plan edits. Refuses unless the plan's merged commit is its pull request merged into the branch. `--built-plan` must plan the same release. With `--assets`, verifies each file's build attestation from `--signer-workflow` with `gh attestation verify`, uploads the files to a draft, verifies GitHub's checksums for them, and publishes last. Needs `GITHUB_TOKEN` and `GITHUB_REPOSITORY`. |
| `report --needs <json> --branch <name> (--pull-request <n> \| --merged <sha>) [--plan <file>] [--assets <dir>]` | CI | Writes the release status comment on the release pull request, from the workflow's `needs` context and the plan. Warns instead of failing when it can't comment. |
| `downstream --tag <tag> --target <owner/name:workflow.yml>...` | CI | Starts each target workflow with inputs `tag` and `version`, unless the tag is a prerelease. Needs `GITHUB_TOKEN` that can run workflows in the targets. |
| `version` | Anyone | Prints the running version. |

`install` and `check` refuse to run when the running version differs from the version the config pins, because they would render the wrong files.

## Files

| Path | Owner | Purpose |
| --- | --- | --- |
| `.release-planner/config.yml` | Repository | Settings. See [Configuration](/customize/configuration/). |
| `.release-planner/policy.md` | Repository | What users depend on, how to choose versions, who reads the notes, and what to always include or leave out. Wins over the guide where they differ. |
| The file `release-notes-style.file` names, conventionally `.release-planner/release-notes-style.md` | Repository, optional | Appends to or replaces the default release notes style, as `release-notes-style.mode` says. |
| `_releases/v<version>.md` | Release pull request | The notes for one release, published word for word. Editing a published version's file in a later pull request replaces that release's notes. |
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

To revise the notes, edit the file in place. Never regenerate it or paste one copy over another. The release branch moving on doesn't change the release. To include newer changes, merge the release branch into the release pull request's branch; the release commit moves to the new merge base. Rerun `inventory --head "$(git merge-base HEAD origin/<branch>)"` and add the new entries.

To correct a published release's notes, edit its file in a pull request of its own. That's a notes edit, not a release: merging replaces the release's notes on GitHub and never changes its tag or assets.

## What `validate` checks

`validate` finds the release commit, the newest commit the head shares with the release branch (`git merge-base`), and reads what the head changes on top of it:

- Adding or correcting the notes file of an untagged version requests that release.
- Changing the notes file of a tagged version is a notes edit.
- Deleting an untagged version's notes file withdraws that request.
- Moving a tagged version's notes file unchanged out of the `release-notes-dir` the release commit configures, such as into a new one, changes nothing. Moving any other file into a tagged version's notes path adds that file, so it's a notes edit.

It fails, and nothing is published, when:

- a pull request that requests a release or edits notes also changes any other file
- it requests more than one release
- a tagged version's notes file is deleted
- the filename isn't a SemVer 2.0.0 version with a leading `v`, or has build metadata
- the notes are empty
- another notes file exists without a tag (a pending request)
- the version isn't newer than every existing version tag
- there are no releases yet and the version isn't `first-version`
- the previous release isn't an ancestor of the release commit

A version's tag may already exist only on the release commit, where an interrupted publication created it. A pull request that neither requests a release nor edits notes produces a plan with an empty `tag` and no `edits`, and may change anything. Run `validate --base origin/<branch> --head HEAD` after committing the notes, and before opening the release pull request; it must print the intended version and release commit.

After the merge, `validate --ci --merged <sha>` finds the pull request merged as `<sha>` through the GitHub API and plans it again, with the release branch before the merge, the merged commit's first parent, as the base. That gives the same release commit for a merge commit, a squash, or a rebase. It fetches the pull request's head if no branch holds it, and refuses notes on the release branch that differ from the pull request's head. A direct push of notes publishes nothing.

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

The generated workflow runs when a pull request changes `_releases/`, `.release-planner/`, or the workflow itself, when a notes file lands on the release branch, and when started by hand to retry. Everything that can fail runs on the pull request; nothing on a pull request can write to releases or tags.

| Job | Runs | Permissions | What it does |
| --- | --- | --- | --- |
| **validate** | Always | `contents: read`, `actions: read`, `pull-requests: read` | Installs the pinned Release Planner, checks out the head with full history, runs `check`, then `validate --ci`: `--base`/`--head` on a pull request, `--merged` after the merge. Writes the release plan and step outputs, and uploads the plan as the `release-plan` artifact. |
| **release-checks** | Optional; when validate says this run builds | Default; the workflow form gets the repository's secrets | Runs the [release checks](/customize/release-checks/) on the release commit. |
| **release-assets** | Optional; when validate says this run builds | `contents: read`, no secrets | Calls the [build workflow](/customize/release-assets/) with `ref`, `tag`, and `version`; it uploads the `release-assets` artifact. |
| **attest** | After release-assets | `contents: read`, `id-token: write`, `attestations: write` | Attests each asset's build provenance. Runs no repository code. |
| **publish** | After the merge, when the plan requests a release or edits notes, and nothing failed | `contents: write`, `pull-requests: read`, `actions: read`, `attestations: read` with assets | In the `release` environment, one at a time. Downloads the plan, and the plan and assets of the run that built the release; checks they plan the same release and verifies each asset's attestation; then tags the release commit and publishes, and replaces edited notes. Runs no repository code. |
| **downstream** | Optional; after a new stable release publishes | `contents: read`, plus a GitHub App token for the targets | In the `downstream` environment. Starts each [downstream workflow](/customize/downstream/). |
| **report** | Always, after the others | `contents: read`, `actions: read`, `pull-requests: write` | Writes the status comment with `report`. |

**Validate outputs.** `tag`, `version`, `commit` (the release commit), `publish` (`true` after the merge when there's something to publish), `build` (`true` when this run must run the release checks and build the assets), and `build-run` (the run whose checks and assets the release uses).

**Who builds.** On a pull request from a branch in the repository, the pull request's run checks and builds the release commit. A fork's pull request builds nothing, because its token can't attest. After the merge, `validate` looks for the pull request's successful run of the same workflow at the pull request's head, from the repository itself, whose `release-plan` and any `release-assets` artifacts haven't expired. If it finds one, the push run reuses it: release-checks, release-assets, and attest are skipped, and publish downloads that run's artifacts, which are trusted because the pull request's head changes only notes on top of the release commit. Otherwise, the push run checks, builds, and attests the release commit itself.

**Settings warnings.** For a release request or notes edit, `validate` warns, without failing, if the `release` environment doesn't exist yet, lets any branch deploy, doesn't let the release branch deploy, or requires reviewers; with `downstream` configured, it checks the `downstream` environment the same way. Before publishing, `publish` confirms that the version tags haven't changed since validation, that the previous release is published, and that any existing tag or release matches the plan, and afterward verifies that the tag points to the release commit. If a matching release is already published, it changes nothing; its notes may differ from the plan's, since a later pull request may have edited them.

## The status comment

The report job keeps one comment on the release pull request, found by the hidden marker `<!-- release-planner:status -->` and edited in place. On the pull request it shows the version and tag, the release commit, the previous release, broken release notes rules, the jobs' results, and the assets with their sizes and SHA-256; for a notes edit, which releases merging updates, with a warning if their notes on GitHub were edited since they merged. After the merge, the same comment shows the published release or updated notes, the downstream workflows, or the failed job with a link to the run and the retry, mentioning whoever merged. It never creates a comment on a pull request that doesn't request a release or edit notes. When the token can't comment, as on a fork's pull request, it warns and writes the report to the step summary instead.

## Retrying a failed release

Read the status comment, the failed run, and any existing tag or release before acting. Prefer **Re-run failed jobs** on the failed run: it reuses the same plan and files. For a manual retry, run the workflow on the release branch with `merged-commit` set to the full SHA of the commit the release pull request merged as; never another commit. It plans the same release commit, and reuses the pull request's run when it can; otherwise it builds the assets again, and publish refuses a draft that already holds different files. A retry that finds the release already published checks its tag and files, and keeps its notes even if a later pull request edited them.

Published tags and assets never move. If the run failed before tagging, fix the cause in a separate pull request, then correct the untagged notes in a new release pull request, whose release commit then includes the fix, or withdraw the request by deleting its file.
