# Fabrica Release Planner

<p>
  <a href="LICENSE.md"><img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-blue"></a>
</p>

The full documentation is in [`docs/`](docs/), a site you can run locally.

## What is Release Planner?

Release Planner is an open source release process for GitHub repositories where an agent drafts the release and a human approves it. Your agent proposes the version and writes the notes, you edit them in a pull request, and merging that pull request tags and publishes the release.

Release Planner works with GitHub only for now. It publishes GitHub releases, and it runs on GitHub Actions.

## Why use it?

Release notes are worth reading only when someone writes them for readers. Tools that generate notes from commit messages produce a changelog, not an explanation, and writing good notes by hand for every release is tedious.

Agents are good at the tedious part: reading every commit and pull request since the last release, working out what changed for users, and proposing a version. They shouldn't decide on their own what ships. Release Planner splits the work: the agent prepares everything, you approve it with a merge, and automation publishes exactly what you approved.

## How does it work?

1. **You tell your agent "let's release."**
2. **The agent works out the next release.** It runs `release-planner guide` for the procedure and `release-planner inventory` for every change since the previous release, the candidate versions, and a ready-made line for each pull request with its author. Then it applies your release policy and picks the version.
3. **The agent opens a release PR.** It writes `_releases/v<version>.md` in your release notes style, checks it with `release-planner validate`, and explains its choice of version in the PR.
4. **The Release workflow checks the release on the pull request.** The release commit is the newest commit the release PR shares with your release branch. The workflow validates the request, runs any release checks and builds any release assets on the release commit, and keeps one status comment on the PR up to date.
5. **You edit the notes and merge.** Change anything you like. Saving edits publishes nothing.
6. **Merging publishes the release.** The workflow tags the release commit and publishes the notes file word for word as the GitHub release, with the files the PR built, then starts any downstream workflows.

The tag is created only at that last step, on the release commit you approved, so a version never exists without its approved notes. To correct published notes later, edit the file in a new pull request; merging updates the release on GitHub.

## How do I set it up on a repository?

From the repository root:

1. **Install Release Planner**, a single command for macOS and Linux:

   ```sh
   curl -fsSL https://raw.githubusercontent.com/fabricahq/release-planner/main/install.sh | sh
   ```

   It installs the latest release into `~/.local/bin` after verifying its checksum. Add `-s -- --version v0.1.0` after `sh` for a specific version. With Go, `go install github.com/fabricahq/release-planner/cmd/release-planner@v0.1.0` also works.

2. **Create the configuration:**

   ```sh
   release-planner init --first-version v1.0.0
   ```

   This creates `.release-planner/config.yml`, pinned to the version you installed, and a starter release policy, `.release-planner/policy.md`. The config explains each setting in comments.

3. **Write your release policy** in `.release-planner/policy.md`. See [Write your release policy](#write-your-release-policy).

4. **Generate the release files:**

   ```sh
   release-planner install
   ```

   This writes:
   - `.github/workflows/release-planner.yml`, the Release workflow.
   - A **Releases** section in `AGENTS.md`, which any agent that reads `AGENTS.md` follows.
   - A `release` skill in `.agents/skills/` (read by Codex, Gemini CLI, VS Code, and other agents) and in `.claude/skills/` (read by Claude Code), so "let's release" triggers the procedure automatically.

5. **Protect releases** in your repository settings:
   - Require pull requests for your release branch, with your CI as required status checks, and require branches to be up to date before merging (or use a merge queue). This is what guarantees that the commit you release was tested.
   - Block force pushes to and deletion of that branch.
   - Create an environment named `release` (Settings → Environments). Under **Deployment branches and tags**, add a branch rule for your release branch only. Leave **Required reviewers** off: the merge is the approval, and reviewers would make every release wait for a second approval in the Actions tab. The Release workflow warns on each release pull request if the environment is missing, doesn't allow your release branch, allows any branch, or requires reviewers.
   - If you use [downstream workflows](#downstream-workflows), create the `downstream` environment the same way, with the GitHub App's credentials.
   - Turn on immutable releases, so published tags and files can't be changed. Notes stay editable.

6. **Commit the files**, then tell your agent "let's release."

## What goes where

```text
.release-planner/
  config.yml                  # you write it: version pin and settings
  policy.md                   # you write it: versioning, audience, always and never
  release-notes-style.md      # optional, if config.yml names it: how to write the notes
_releases/
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
| `version` | required | The Release Planner version to use: a release tag, or a full commit SHA. Bumping it upgrades the GitHub Actions workflow (`.github/workflows/release-planner.yml`) and the agent guide together; run `release-planner install` after changing it. |
| `first-version` | `v0.1.0` | The version your first release must use. |
| `release-checks` | none | Optional release-only checks. See [Release checks](#release-checks). |
| `release-assets` | none | Your workflow that builds the files attached to each release. See [Release assets](#release-assets). |
| `downstream` | none | Workflows in other repositories to run after each new stable release. See [Downstream workflows](#downstream-workflows). |
| `release-notes-rules` | every rule on | Release notes rules to turn off, by ID, such as `no-long-heading: off`. See [Release notes rules](#release-notes-rules). |
| `release-notes-style` | none | Your own release notes style: `file`, the markdown file that holds it, and `mode`, `append` or `replace`. See [Release notes style](#release-notes-style). |
| `release-notes-dir` | `_releases` | Where the release notes files live. |
| `release-branch` | `main` | The branch that release pull requests merge into, and releases are published from. |

### Release checks

Your required pull request checks already test what merges, including the release PR, so a release doesn't need to run them again. Use `release-checks` only for checks your pull requests don't run, such as:

- **A smoke test of what you ship:** build the real package or binary from the release commit and use it the way a user would.
- **An API compatibility check** against the previous release, which catches a breaking change released under a minor version. For example: `gorelease`, `cargo-semver-checks`, `buf breaking`, or an OpenAPI diff.
- **A fresh vulnerability scan,** because advisories are published after code merges. For example: `govulncheck`, `npm audit`, or `osv-scanner`.
- **Upgrade tests** from the previous release, or **slow suites** that don't run per PR: end-to-end, the full platform matrix, or benchmarks.

The checks run on the release commit while the release PR is open, so a failure shows in its status comment before you approve. If they fail, nothing is published. After the merge, the release reuses the PR's result, or runs the checks again on the same commit if it can't. Configure them one of two ways.

A Bash script, with optional toolchains. Any failing command stops the release:

```yaml
release-checks:
  go: '1.27.x'       # also node and python
  run: |
    go install golang.org/x/vuln/cmd/govulncheck@latest
    govulncheck ./...
```

Or one of your own workflows, for checks that need secrets, service containers, other runners, or a matrix:

```yaml
release-checks:
  workflow: release-checks.yml
```

The Release workflow calls it with the commit to check and your repository's secrets. It never receives the token that can write releases. The workflow must accept a `ref` input and check out that ref, because the release commit is usually not the latest commit. `install` and `check` fail if it can't be called this way:

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

### Release assets

To attach files to each release, name a workflow that builds them:

```yaml
release-assets:
  workflow: build-release.yml
```

The Release workflow calls it on the release PR with string inputs `ref` (the release commit), `tag`, and `version` (without the `v`), read-only and without secrets. It must check out `ref` and upload the files as one artifact named `release-assets`. An `attest` job then attests each file's build provenance, and the status comment lists the files with their SHA-256. When you merge, publishing verifies each file's attestation came from this repository's `release-planner.yml`, stages the files on a draft release, checks GitHub's checksums, and publishes last. If the PR's files are gone, the release builds them again from the release commit first. See the [docs](docs/src/content/docs/customize/release-assets.md) for an example.

### Downstream workflows

To start workflows in other repositories after each new stable release, such as a Homebrew formula update, list them:

```yaml
downstream:
  - repository: fabricahq/homebrew-tap
    workflow: update-code-rules.yml
```

Each needs a `workflow_dispatch` trigger with string inputs `tag` and `version`. A `downstream` job, in an environment named `downstream`, mints a GitHub App token for those repositories from the variable `DOWNSTREAM_APP_CLIENT_ID` and the secret `DOWNSTREAM_APP_PRIVATE_KEY`, and starts each workflow. Prereleases and notes edits start nothing. A failure doesn't affect the published release; the status comment shows each target's result. See the [docs](docs/src/content/docs/customize/downstream.md) to create the App.

### Release notes style

The release notes style tells the agent how to write the notes: which headings to use, and how to write each entry. Release Planner's default style describes only what your readers would notice or care about, leaving out implementation details unless they help readers understand a change's value or act on it. It uses these headings, in the order your policy gives, leaving out any that are empty:

- ✨ New Features
- ⬆️ Improvements
- 🐛 Squashed Bugs
- ⛓️‍💥 Breaking Changes

Each entry ends with the pull requests it covers, such as (#7) or (#7, #9).

To change it, write your style in a markdown file, such as `.release-planner/release-notes-style.md`, and name it in `config.yml` with how it applies:

```yaml
release-notes-style:
  file: .release-planner/release-notes-style.md
  mode: append   # append or replace
```

- With `mode: append`, the agent follows the default style and then yours. Use it to add rules while keeping improvements to the default style when you upgrade.
- With `mode: replace`, the agent follows only yours. Start from the default with `release-planner guide --default-style > .release-planner/release-notes-style.md`, then edit it. A replaced style doesn't change when you upgrade.

Release Planner reads only the file `config.yml` names, and fails if that file is missing or empty.

For example, to append:

```markdown
- Under New Features, name each new rule by its ID, such as `techs/react/server-auth-actions`, and link to the rule file at the new tag.
- Under Breaking Changes, list every removed or renamed rule ID with the `exclude` or `replace` entry importers must update.
```

Or to replace, using [Keep a Changelog](https://keepachangelog.com) headings:

```markdown
- Use only these headings, leaving out empty ones: `## Added`, `## Changed`, `## Deprecated`, `## Removed`, `## Fixed`, `## Security`.
- Write one bullet per change, in the past tense, ending with the pull request link.
```

List the same headings, in the order you want, under **Order of the release notes** in your policy.

Whatever the style says, every release ends with the parts `inventory` writes: the **Pull Requests** section listing every pull request with its author, grouped as the style says, any **New Contributors**, and the **Full Changelog** link (or, for a first release, a link to the tagged source).

### Release notes rules

`release-planner validate` checks the notes against these rules. Run `release-planner validate --rules` to list them.

| Rule | What it enforces |
| --- | --- |
| `no-empty-heading` | Every heading has content before the next heading at its level or above. |
| `no-duplicate-heading` | No `##` heading appears twice, and no `###` heading appears twice under the same `##`. |
| `no-long-heading` | `##` and `###` headings are at most 80 characters. |
| `require-pull-requests-last` | One `## Pull Requests` section, which, with an optional `## New Contributors`, comes last, followed only by the closing line. |
| `list-every-change` | `## Pull Requests` lists every pull request and direct commit since the previous release exactly once, and nothing else. Entries are matched by number or commit, so you can edit their titles. |
| `require-closing-link` | One closing line: the **Full Changelog** link, or for a first release, the link to its source. |

When your agent runs `validate`, a broken rule fails, so the agent fixes it. In the Release workflow, a broken rule is a warning in the status comment that doesn't block the release: you may break a rule on purpose. To stop checking a rule, turn it off in `config.yml`:

```yaml
release-notes-rules:
  no-long-heading: off
```

## Write your release policy

`.release-planner/policy.md` is a plain markdown file that describes how your project does releases. The agent reads it at the start of every release, before choosing a version or writing the notes, and it wins over Release Planner's default guidance. `init` creates a starter with five sections of sensible defaults, each a short list to edit:

- **Breaking changes.** Common things users depend on, such as commands, configuration keys, and APIs. Keep what applies, delete the rest, add your own.
- **Choosing a version.** What makes a release major, minor, or patch. If your first release is before 1.0.0, it also covers 0.x releases: SemVer lets anything change before 1.0, so the starter shifts each rule down one place: a minor bump for breaking changes and new features, and a patch bump for everything else.
- **Who reads the release notes.** Your readers, developers by default.
- **Order of the release notes.** The order changes appear in: new features, improvements, bug fixes, then breaking changes by default. Reorder it to taste.
- **Always and never.** Anything the notes must always include or leave out.

Each section ends with a `TODO:` line. Until you delete every one, the agent stops and asks instead of guessing a version. You can add sections of your own; the agent reads the whole file. The starter opens with a prompt, in a comment, that you can give your agent to fill it in for your review. The docs have [example policies](docs/src/content/docs/customize/policy.md) for a library, a CLI, and a service.

## Commands

Everyone runs the version your config pins. Agents check `release-planner version` before a release, and the generated workflow downloads that release and verifies its build attestation and checksums before running it.

| Command | Who runs it | What it does |
| --- | --- | --- |
| `init` | You | Creates `.release-planner/config.yml` and a starter policy. |
| `install [--force]` | You | Writes or updates the generated files from your config. Safe to run any number of times. |
| `check` | CI and you | Fails if a generated file is missing, stale, or edited by hand. Changes nothing. |
| `uninstall [--force]` | You | Deletes the generated files and the `AGENTS.md` section. Leaves `.release-planner/` and your notes. |
| `guide [--default-style]` | Agent | Prints the release procedure for this version, or only the default release notes style. |
| `inventory [--head <ref>] [--repository <owner/name>] [--offline]` | Agent | Lists the previous release, every commit since it with its pull request author's GitHub handle, the candidate next versions, and the lines that list each change, the new contributors, and the closing link in the notes. |
| `validate --base <ref> [--head <ref>] [--repository <owner/name>] [--ci]` | Agent and CI | Validates a release pull request against the release branch, and prints the tag, release commit, and notes to publish, and any notes edits. Broken [release notes rules](#release-notes-rules) fail, or with `--ci` are warnings. In the Release workflow, it also warns when the `release` or `downstream` environment isn't set up as recommended. `validate --ci --merged <sha>` plans the publication a merge approved. `validate --rules` lists the rules. |
| `publish --plan <file> --branch <name> [--built-plan <file>] [--assets <dir> [--signer-workflow <path>]]` | CI | Tags the release commit and publishes the approved notes, with optional attested files staged on a draft and verified first, and replaces edited notes of published releases. |
| `report --needs <json> --branch <name> (--pull-request <n> \| --merged <sha>)` | CI | Writes the release status comment on the release pull request. |
| `downstream --tag <tag> --target <owner/name:workflow.yml>...` | CI | Starts downstream workflows for a new stable release. |
| `version` | Anyone | Prints the running version. |

## Upgrading

Install the new version, change `version` in `.release-planner/config.yml` to match, then regenerate the files and commit the result:

```sh
curl -fsSL https://raw.githubusercontent.com/fabricahq/release-planner/main/install.sh | sh -s -- --version v0.3.0
release-planner install
```

## FAQs

### What does `validate` check before publishing?

- The release commit: the newest commit the release PR shares with the release branch. The PR may change only release notes files on top of it.
- At most one release request per PR, alongside any edits to published notes.
- A SemVer 2.0.0 version with no build metadata, newer than every existing tag.
- Your configured first version, if the repository has no releases yet.
- Notes that aren't empty.

It also checks the [release notes rules](#release-notes-rules), which warn without blocking the release.

It also refuses to delete notes that are already tagged, and it won't let a new request skip an earlier one that never published. After the merge, it checks the merged notes are the ones the PR approved. The publish step refuses to run if version tags changed after validation or if the previous release is still a draft, and afterward it verifies that the tag points to the release commit.

### What if publication fails?

Nothing is tagged until `validate`, your release checks, and any asset build pass. The status comment names the failed job and mentions whoever merged. Use **Re-run failed jobs** on the failed run, or start the Release workflow by hand with `merged-commit` set to the commit the release PR merged as. A retry publishes the same release commit, and after a successful publication makes no changes, keeping any notes edited since. Published tags and files never move; fix problems in a new release. Notes can be corrected in a new PR.

### Can I publish binaries or other assets?

Yes, with [release assets](#release-assets). They're built and attested on the release PR, then uploaded to a draft release, checked against GitHub's checksums, and published last, because immutable releases freeze a release's files once it is published.

### Why not release-please or semantic-release?

Those tools derive versions and notes from commit message conventions. Release Planner has your agent read the actual changes and write notes for people, and requires your approval before anything ships. It needs no commit message conventions.

## How do I contribute?

Open a pull request! Run the checks from the repository root before you push:

```sh
gofmt -l . && go vet ./... && go test ./...
```

## License

Release Planner is [MIT licensed](LICENSE.md), with copyright attributed to Fabrica Systems LLC. The planner and publisher are adapted from the release tooling in [Code Rules](https://github.com/fabricahq/code-rules).
