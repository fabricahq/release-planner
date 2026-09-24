# Fabrica Release Planner

<p>
  <a href="LICENSE.md"><img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-blue"></a>
</p>

## What is Release Planner?

Release Planner is an open source release process for GitHub repositories where an agent drafts the release and a human approves it. Your agent proposes the version and writes the notes, you edit them in a pull request, and merging that pull request tags and publishes the release.

## Why use it?

Release notes are worth reading only when someone writes them for readers. Tools that generate notes from commit messages produce a changelog, not an explanation, and writing good notes by hand for every release is tedious.

Agents are good at the tedious part: reading every commit and pull request since the last release, working out what changed for users, and proposing a version. They shouldn't decide on their own what ships. Release Planner splits the work: the agent prepares everything, you approve it with a merge, and automation publishes exactly what you approved.

## How does it work?

1. **You tell your agent "let's release."**
2. **The agent works out the next release.** It runs `release-planner guide` for the procedure and `release-planner inventory` for every change since the previous release and the candidate versions. Then it applies your release policy and picks the version.
3. **The agent opens a release PR.** `release-planner draft` creates `releases/v<version>.md` with your headings and a linked list of pull requests. The agent writes the notes and explains its choice of version in the PR.
4. **You edit the notes and merge.** Change anything you like. Saving edits publishes nothing.
5. **Merging publishes the release.** The generated Release workflow validates the merged commit, tags it, and publishes the notes file word for word as the GitHub release.

The tag is created only at that last step, on the exact commit you approved, so a version never exists without its approved notes.

## How do I set it up on a repository?

1. **Write `release-planner.yml`** at the repository root:

   ```yaml
   # The Release Planner version to use. Bumping it upgrades everything below together.
   version: v0.1.0
   # The version your first release must use.
   first-version: v1.0.0
   # Your checks, run on the exact commit being released, before anything is tagged.
   validate:
     go: '1.27.x'     # optional; also node and python
     run: make test
   ```

2. **Generate the release files.** From the repository root, with [Go](https://go.dev/dl/) installed:

   ```sh
   go run github.com/fabricahq/release-planner/cmd/release-planner@v0.1.0 install
   ```

   This writes:
   - `.github/workflows/release-planner.yml`, the Release workflow.
   - A **Releases** section in `AGENTS.md`, which any agent that reads `AGENTS.md` follows.
   - A `release` skill in `.agents/skills/` (read by Codex, Gemini CLI, VS Code, and other agents) and in `.claude/skills/` (read by Claude Code), so "let's release" triggers the procedure automatically.
   - `releases/README.md`, a starter release policy for you to fill in.

3. **Write your release policy** in `releases/README.md`. See [Write your release policy](#write-your-release-policy).

4. **Protect releases** in your repository settings:
   - Create an environment named `release` that allows only your release branch, with no required reviewers. The merge is the approval.
   - Require pull requests for that branch, and block force pushes and deletion.
   - Turn on immutable releases, so published tags and releases can't be changed.

5. **Commit the files**, then tell your agent "let's release."

## Commands

In CI and in agent instructions, Release Planner runs with `go run github.com/fabricahq/release-planner/cmd/release-planner@<version>`, at the version `release-planner.yml` pins. Nothing needs installing beyond Go.

| Command | Who runs it | What it does |
| --- | --- | --- |
| `install [--force]` | You | Writes or updates the generated files from `release-planner.yml`. Safe to run any number of times. |
| `check` | CI and you | Fails if a generated file is missing, stale, or edited by hand. Changes nothing. |
| `uninstall [--force]` | You | Deletes the generated files and the `AGENTS.md` section. Leaves your policy and notes. |
| `guide` | Agent | Prints the release procedure for this version. |
| `inventory [--head <ref>]` | Agent | Lists the previous release, every commit since it, and the candidate next versions. |
| `draft <version>` | Agent | Creates the notes file with your headings, a linked list of pull requests, and the comparison link. |
| `plan --base <ref> [--head <ref>]` | CI and agent | Validates a release request and prints the tag, commit, and notes to publish. |
| `publish --plan <file> --commit <sha>` | CI | Tags the approved commit and publishes the approved notes. |
| `version` | Anyone | Prints the running version. |

## Upgrading

Change `version` in `release-planner.yml`, then run `install` at the new version and commit the result:

```sh
go run github.com/fabricahq/release-planner/cmd/release-planner@v0.2.0 install
```

`check` runs in the Release workflow on every release PR, so a version bump without a reinstall, or a hand edit to a generated file, fails CI instead of drifting.

## Customizing

### Release note headings

By default, notes use four headings: ✨ New Features, ⬆️ Improvements, 🐛 Squashed Bugs, and ⛓️‍💥 Breaking Changes. To use your own, list them in `release-planner.yml`. Your list replaces the defaults:

```yaml
notes:
  sections:
    - heading: "🚀 Features"
      include: what users can do that they couldn't before.
    - heading: "🔒 Security"
      include: vulnerabilities fixed, with their CVE IDs.
      summarize-first: true   # also mention these in the opening sentences
    - heading: "🐛 Fixes"
      include: the trigger, the previous wrong behavior, and the corrected result.
```

`draft` writes these headings, and `guide` lists each one followed by its `include` text, so write `include` to read naturally after a colon. Every release ends with the same footer: **What's Changed**, the **Full Changelog** link (or, for a first release, a link to the tagged source), and **New Contributors** when there are any.

### Other settings

| Key | Default | Meaning |
| --- | --- | --- |
| `notes-dir` | `releases` | Where release requests and your policy live. |
| `branch` | `main` | The branch whose notes changes publish releases. |
| `validate.go`, `validate.node`, `validate.python` | none | Toolchain versions to set up before `validate.run`. |

### Generated files

Don't edit generated files; change `release-planner.yml` and run `install`. Your own text in `AGENTS.md` is safe: `install` finds its section by the `<!-- release-planner:begin -->` and `<!-- release-planner:end -->` markers and never touches anything outside them. Each generated file and section records a digest of its content, so `install` can tell an unedited older version, which it upgrades, from a hand edit, which it refuses to overwrite without `--force`.

## Write your release policy

`releases/README.md` is where you tell the agent how your repository releases. `install` creates a starter with four prompts. Until you replace them, the agent stops and asks instead of guessing a version.

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

## FAQs

### What does the planner check before publishing?

- Exactly one new or edited notes file per change.
- A SemVer 2.0.0 version with no build metadata, newer than every existing tag.
- Your configured first version, if the repository has no releases yet.
- Notes with no leftover draft prompt and no empty headings.

It also refuses to edit notes that are already tagged, and it won't let a new request skip an earlier one that never published. The publish step refuses to run if version tags changed after planning or if the previous release is still a draft, and afterward it verifies that the tag points to the approved commit.

### What if publication fails?

Nothing is tagged until validation passes. Re-run the failed workflow run, or start the Release workflow by hand with the **Base SHA** and **Approved head SHA** from the failed run's summary. A retry after a successful publication makes no changes. Published tags never move; fix problems in a new release.

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
