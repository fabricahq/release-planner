# Fabrica Release Planner

<p>
  <a href="LICENSE.md"><img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-blue"></a>
</p>

The full documentation is in [`docs/`](docs/), a site you can run locally.

## What is Release Planner?

Release Planner is an open source release process for GitHub repositories where an agent drafts the release and a human approves it. Your agent proposes the version and writes the notes, you edit them in a pull request, and merging that pull request tags and publishes the release.

## Why use it?

Release notes are worth reading only when someone writes them for readers. Tools that generate notes from commit messages produce a changelog, not an explanation, and writing good notes by hand for every release is tedious.

Agents are good at the tedious part: reading every commit and pull request since the last release, working out what changed for users, and proposing a version. They shouldn't decide on their own what ships. Release Planner splits the work: the agent prepares everything, you approve it with a merge, and automation publishes exactly what you approved.

## How does it work?

1. **You tell your agent "let's release."**
2. **The agent works out the next release.** It runs `release-planner guide` for the procedure and `release-planner inventory` for every change since the previous release and the candidate versions. Then it applies your release policy and picks the version.
3. **The agent opens a release PR.** `release-planner draft` creates `releases/v<version>.md` with a linked list of the pull requests. The agent writes the notes in your release notes style and explains its choice of version in the PR.
4. **You edit the notes and merge.** Change anything you like. Saving edits publishes nothing.
5. **Merging publishes the release.** The generated Release workflow checks the request, runs any release checks you configured, tags the merged commit, and publishes the notes file word for word as the GitHub release.

The tag is created only at that last step, on the exact commit you approved, so a version never exists without its approved notes.

## How do I set it up on a repository?

With [Go](https://go.dev/dl/) installed, from the repository root:

1. **Create the configuration:**

   ```sh
   go run github.com/fabricahq/release-planner/cmd/release-planner@v0.1.0 init --first-version v1.0.0
   ```

   This creates `.release-planner/config.yml` and a starter release policy, `.release-planner/policy.md`. The config explains each setting in comments.

2. **Write your release policy** in `.release-planner/policy.md`. See [Write your release policy](#write-your-release-policy).

3. **Generate the release files:**

   ```sh
   go run github.com/fabricahq/release-planner/cmd/release-planner@v0.1.0 install
   ```

   This writes:
   - `.github/workflows/release-planner.yml`, the Release workflow.
   - A **Releases** section in `AGENTS.md`, which any agent that reads `AGENTS.md` follows.
   - A `release` skill in `.agents/skills/` (read by Codex, Gemini CLI, VS Code, and other agents) and in `.claude/skills/` (read by Claude Code), so "let's release" triggers the procedure automatically.

4. **Protect releases** in your repository settings:
   - Require pull requests for your release branch, with your CI as required status checks, and require branches to be up to date before merging (or use a merge queue). This is what guarantees that the commit you release was tested.
   - Block force pushes to and deletion of that branch.
   - Create an environment named `release` that allows only your release branch, with no required reviewers. The merge is the approval.
   - Turn on immutable releases, so published tags and releases can't be changed.

5. **Commit the files**, then tell your agent "let's release."

## What goes where

```text
.release-planner/
  config.yml                  # you write it: version pin and settings
  policy.md                   # you write it: versioning, audience, always and never
  release-notes-style.md      # optional: how to write the notes
releases/
  v1.0.0.md                   # the release notes, one file per release
.github/workflows/release-planner.yml   # generated
.agents/skills/release/SKILL.md         # generated
.claude/skills/release/SKILL.md         # generated
AGENTS.md                               # yours, with one generated Releases section
```

Everything in `.release-planner/` belongs to you; Release Planner never rewrites it. Don't edit the generated files; change `.release-planner/config.yml` and run `install`. Your own text in `AGENTS.md` is safe: `install` finds its section by the `<!-- release-planner:begin -->` and `<!-- release-planner:end -->` markers and never touches anything outside them. Each generated file and section records a digest of its content, so `install` can tell an unedited older version, which it upgrades, from a hand edit, which it refuses to overwrite without `--force`. `check` runs in the Release workflow on every release PR, so a stale or hand-edited file fails CI instead of drifting.

## Configuration

`.release-planner/config.yml`:

| Key | Default | Meaning |
| --- | --- | --- |
| `schema-version` | required | The format of the file. Currently `1`. |
| `version` | required | The Release Planner version to use: a release tag, or a full commit SHA. Bumping it upgrades the workflow, the agent guide, and the release checks together. |
| `first-version` | `v0.1.0` | The version your first release must use. |
| `validate` | none | Optional release-only checks. See [Release checks](#release-checks). |
| `release-notes-style` | `append` | How `.release-planner/release-notes-style.md` applies, if it exists. See [Release notes style](#release-notes-style). |
| `notes-dir` | `releases` | Where the release notes files live. |
| `branch` | `main` | The branch whose notes changes publish releases. |

### Release checks

Your required pull request checks already test what merges, including the release PR, so a release doesn't need to run them again. Use `validate` only for checks your pull requests don't run, such as:

- **A smoke test of what you ship:** build the real package or binary from the release commit and use it the way a user would.
- **An API compatibility check** against the previous release, which catches a breaking change released under a minor version. For example: `gorelease`, `cargo-semver-checks`, `buf breaking`, or an OpenAPI diff.
- **A fresh vulnerability scan,** because advisories are published after code merges. For example: `govulncheck`, `npm audit`, or `osv-scanner`.
- **Upgrade tests** from the previous release, or **slow suites** that don't run per PR: end-to-end, the full platform matrix, or benchmarks.

The checks run on the exact commit being released, before anything is tagged. If they fail, nothing is published. Configure them one of two ways.

A Bash script, with optional toolchains. Any failing command stops the release:

```yaml
validate:
  go: '1.27.x'       # also node and python
  run: |
    go install golang.org/x/vuln/cmd/govulncheck@latest
    govulncheck ./...
```

Or one of your own workflows, for checks that need secrets, service containers, other runners, or a matrix:

```yaml
validate:
  workflow: release-checks.yml
```

The Release workflow calls it with the commit to check and your repository's secrets. It never receives the token that can write releases. The workflow must accept a `ref` input and check out that ref, because on a retry the release commit is not the latest commit. `install` and `check` fail if it can't be called this way:

```yaml
on:
  workflow_call:
    inputs:
      ref:
        type: string
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with:
          ref: ${{ inputs.ref }}
```

### Release notes style

The release notes style tells the agent how to write the notes: how to open, which headings to use, and how to write each entry. Release Planner's default style opens with one or two sentences on what the release means for its readers, then uses these headings, leaving out any that are empty:

- ✨ New Features
- ⬆️ Improvements
- 🐛 Squashed Bugs
- ⛓️‍💥 Breaking Changes

Each entry ends with the pull requests it covers, such as (#7) or (#7, #9).

To change it, create `.release-planner/release-notes-style.md`:

- With `release-notes-style: append`, the default, the agent follows the default style and then yours. Use it to add rules while keeping improvements to the default style when you upgrade.
- With `release-notes-style: replace`, the agent follows only yours. Start from the default with `release-planner guide --default-style > .release-planner/release-notes-style.md`, then edit it. A replaced style doesn't change when you upgrade.

For example, to append:

```markdown
- Under New Features, name each new rule by its ID, such as `techs/react/server-auth-actions`, and link to the rule file at the new tag.
- Under Breaking Changes, list every removed or renamed rule ID with the `exclude` or `replace` entry importers must update.
```

Or to replace, using [Keep a Changelog](https://keepachangelog.com) headings:

```markdown
- Open with one sentence naming the most important change.
- Use only these headings, in this order, leaving out empty ones: `## Added`, `## Changed`, `## Deprecated`, `## Removed`, `## Fixed`, `## Security`.
- Write one bullet per change, in the past tense, ending with the pull request link.
```

Whatever the style says, every release keeps the parts `draft` writes: the **What's Changed** list, and the **Full Changelog** link (or, for a first release, a link to the tagged source).

## Write your release policy

`.release-planner/policy.md` tells the agent how your repository releases. `init` creates a starter with four prompts. Until you replace them, the agent stops and asks instead of guessing a version.

- **What users depend on.** What must stay compatible: commands and flags, configuration, APIs, file formats, and identifiers other projects reference.
- **Choosing a version.** What makes a release major, minor, or patch, in terms of that list.
- **Who reads the release notes.** Who they are and what they need first.
- **Always and never.** Anything the notes must always include or leave out.

Example policies:

<details>
<summary>A library of rules that other projects import</summary>

```markdown
## What users depend on
Group IDs and rule IDs. Projects list group IDs to import and rule IDs to exclude or replace, and an unknown ID is an error.

## Choosing a version
- Major: removing or renaming a group or rule, or reversing what a rule requires.
- Minor: new rules or groups, or extending a rule to new situations.
- Patch: corrections, clearer wording, and better examples.

## Who reads the release notes
Engineers deciding whether to adopt the new version. Name every rule ID that was added, renamed, or removed.
```

</details>

<details>
<summary>A command-line tool</summary>

```markdown
## What users depend on
Command names, flags, exit codes, configuration keys, and JSON output.

## Choosing a version
- Major: removing or renaming a command, flag, or key, or changing output that scripts parse.
- Minor: new commands, flags, or keys.
- Patch: bug fixes and help-text changes.

## Who reads the release notes
Developers upgrading the tool. Lead with anything that requires them to change configuration or scripts.

## Always and never
Always show the upgrade command. Leave out dependency updates unless they fix a security issue.
```

</details>

<details>
<summary>A service with an HTTP API</summary>

```markdown
## What users depend on
The public API: endpoints, request and response fields, status codes, and authentication.

## Choosing a version
- Major: removing an endpoint or field, or changing its meaning.
- Minor: new endpoints or optional fields.
- Patch: fixes and performance work with no API change.

## Who reads the release notes
API consumers and our support team. Link every change to its API reference page.
```

</details>

## Commands

Release Planner runs with `go run github.com/fabricahq/release-planner/cmd/release-planner@<version>`, at the version your config pins. Nothing needs installing beyond Go.

| Command | Who runs it | What it does |
| --- | --- | --- |
| `init` | You | Creates `.release-planner/config.yml` and a starter policy. |
| `install [--force]` | You | Writes or updates the generated files from your config. Safe to run any number of times. |
| `check` | CI and you | Fails if a generated file is missing, stale, or edited by hand. Changes nothing. |
| `uninstall [--force]` | You | Deletes the generated files and the `AGENTS.md` section. Leaves `.release-planner/` and your notes. |
| `guide [--default-style]` | Agent | Prints the release procedure for this version, or only the default release notes style. |
| `inventory [--head <ref>]` | Agent | Lists the previous release, every commit since it, and the candidate next versions. |
| `draft <version>` | Agent | Creates the notes file with a linked list of pull requests and the closing link. |
| `plan --base <ref> [--head <ref>]` | CI and agent | Validates a release request and prints the tag, commit, and notes to publish. |
| `publish --plan <file> --commit <sha>` | CI | Tags the approved commit and publishes the approved notes. |
| `version` | Anyone | Prints the running version. |

## Upgrading

Change `version` in `.release-planner/config.yml`, then run `install` at the new version and commit the result:

```sh
go run github.com/fabricahq/release-planner/cmd/release-planner@v0.2.0 install
```

## FAQs

### What does the planner check before publishing?

- Exactly one new or edited notes file per change.
- A SemVer 2.0.0 version with no build metadata, newer than every existing tag.
- Your configured first version, if the repository has no releases yet.
- Notes with no leftover draft prompt and no empty headings.

It also refuses to edit notes that are already tagged, and it won't let a new request skip an earlier one that never published. The publish step refuses to run if version tags changed after planning or if the previous release is still a draft, and afterward it verifies that the tag points to the approved commit.

### What if publication fails?

Nothing is tagged until the planner and your release checks pass. Re-run the failed workflow run, or start the Release workflow by hand with the **Base SHA** and **Approved head SHA** from the failed run's summary. A retry after a successful publication makes no changes. Published tags never move; fix problems in a new release.

### Can I publish binaries or other assets?

Not yet. Release Planner publishes notes only. Immutable releases freeze a release's assets when it is published, so assets need a publisher that uploads to a draft first. [Code Rules](https://github.com/fabricahq/code-rules) has one for its signed binaries, and it is a candidate to move here.

### Why not release-please or semantic-release?

Those tools derive versions and notes from commit message conventions. Release Planner has your agent read the actual changes and write notes for people, and requires your approval before anything ships. It needs no commit message conventions.

## How do I contribute?

Open a pull request! Run the checks from the repository root before you push:

```sh
gofmt -l . && go vet ./... && go test ./...
```

## License

Release Planner is [MIT licensed](LICENSE.md), with copyright attributed to Fabrica Systems LLC. The planner and publisher are adapted from the release tooling in [Code Rules](https://github.com/fabricahq/code-rules).
