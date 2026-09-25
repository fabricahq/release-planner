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
2. **The agent works out the next release.** It runs `release-planner guide` for the procedure and `release-planner inventory` for every change since the previous release and the candidate versions. Then it applies your release policy and picks the version.
3. **The agent opens a release PR.** `release-planner draft` creates `releases/v<version>.md` with the raw material: every pull request with its author, new contributors, and the closing link. The agent writes the notes in your release notes style and explains its choice of version in the PR.
4. **You edit the notes and merge.** Change anything you like. Saving edits publishes nothing.
5. **Merging publishes the release.** The generated Release workflow checks the request, runs any release checks you configured, tags the merged commit, and publishes the notes file word for word as the GitHub release.

The tag is created only at that last step, on the exact commit you approved, so a version never exists without its approved notes.

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
   - Turn on immutable releases, so published tags and releases can't be changed.

6. **Commit the files**, then tell your agent "let's release."

## What goes where

```text
.release-planner/
  config.yml                  # you write it: version pin and settings
  policy.md                   # you write it: versioning, audience, always and never
  release-notes-style.md      # optional, if config.yml names it: how to write the notes
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
| `version` | required | The Release Planner version to use: a release tag, or a full commit SHA. Bumping it upgrades the GitHub Actions workflow (`.github/workflows/release-planner.yml`), the agent guide, and the release checks together; run `release-planner install` after changing it. |
| `first-version` | `v0.1.0` | The version your first release must use. |
| `validate` | none | Optional release-only checks. See [Release checks](#release-checks). |
| `release-notes-style` | none | Your own release notes style: `file`, the markdown file that holds it, and `mode`, `append` or `replace`. See [Release notes style](#release-notes-style). |
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

The release notes style tells the agent how to write the notes: how to open, which headings to use, and how to write each entry. Release Planner's default style opens with one or two sentences on what the release means for its readers, then uses these headings, in the order your policy gives, leaving out any that are empty:

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
- Open with one sentence naming the most important change.
- Use only these headings, leaving out empty ones: `## Added`, `## Changed`, `## Deprecated`, `## Removed`, `## Fixed`, `## Security`.
- Write one bullet per change, in the past tense, ending with the pull request link.
```

List the same headings, in the order you want, under **Order of the release notes** in your policy.

Whatever the style says, every release keeps the parts `draft` writes: the **Pull Requests** section listing every pull request with its author, grouped as the style says, any **New Contributors**, and the **Full Changelog** link (or, for a first release, a link to the tagged source).

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
| `inventory [--head <ref>] [--offline]` | Agent | Lists the previous release, every commit since it with its pull request author's GitHub handle, and the candidate next versions. |
| `draft <version>` | Agent | Checks the version and creates the notes file with every pull request and its author, new contributors, and the closing link. |
| `plan --base <ref> [--head <ref>]` | CI and agent | Validates a release request and prints the tag, commit, and notes to publish. In the Release workflow, it also warns, without failing, when the `release` environment isn't set up as recommended. |
| `publish --plan <file> --commit <sha> --branch <name> [--assets <dir>]` | CI | Tags the approved commit and publishes the approved notes, with optional files staged on a draft and verified first. |
| `version` | Anyone | Prints the running version. |

## Upgrading

Install the new version, change `version` in `.release-planner/config.yml` to match, then regenerate the files and commit the result:

```sh
curl -fsSL https://raw.githubusercontent.com/fabricahq/release-planner/main/install.sh | sh -s -- --version v0.2.0
release-planner install
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

The publisher supports it: `release-planner publish --assets <dir>` uploads the files to a draft release, verifies GitHub's checksums for them, and publishes last, because immutable releases freeze a release's files once it is published. Release Planner ships its own binaries this way. The generated workflow doesn't attach files yet; that's planned.

### Why not release-please or semantic-release?

Those tools derive versions and notes from commit message conventions. Release Planner has your agent read the actual changes and write notes for people, and requires your approval before anything ships. It needs no commit message conventions.

## How do I contribute?

Open a pull request! Run the checks from the repository root before you push:

```sh
gofmt -l . && go vet ./... && go test ./...
```

## License

Release Planner is [MIT licensed](LICENSE.md), with copyright attributed to Fabrica Systems LLC. The planner and publisher are adapted from the release tooling in [Code Rules](https://github.com/fabricahq/code-rules).
