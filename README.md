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
2. **The agent works out the next release.** The planner lists the previous release, every commit since it, and the candidate next versions. The agent reads the changes, applies your version policy, and picks the version.
3. **The agent opens a release PR.** The PR adds one file, `releases/v<version>.md`, containing the drafted notes. The PR description explains the chosen version and lists every change the notes account for.
4. **You edit the notes and merge.** Change anything you like in the PR. Saving edits publishes nothing.
5. **Merging publishes the release.** The Release workflow validates the merged commit, tags it with the version, and publishes the notes file verbatim as the GitHub release.

The tag is created only at that final step, on the exact commit you approved, so a version never exists without its approved notes.

## How do I set it up on a repository?

1. **Add the workflow.** Copy [`docs/release.yml`](docs/release.yml) to `.github/workflows/release.yml`. Then:
   - Replace both `RELEASE_PLANNER_SHA` placeholders with a full commit SHA from this repository. Pin a SHA rather than a branch, so that release tooling changes only when you review the change.
   - Set `first-version` to the version your first release should use.
   - Replace the steps of the `validate` job with your repository's tests or builds. They run on the exact commit being released, before anything is tagged.

2. **Describe your release policy.** Create `releases/README.md` with your version policy, what counts as your public contract, your first version, and the command that validates a release. Your agent reads this file before every release. See this repository's [`releases/README.md`](releases/README.md) for an example.

3. **Teach your agent the procedure.** Copy [`skills/release/SKILL.md`](skills/release/SKILL.md) to `.claude/skills/release/SKILL.md` for Claude Code. For other agents, add a line to `AGENTS.md`:

   ```markdown
   When asked to make a release, draft or revise release notes, or retry a failed release, follow the release skill in `.claude/skills/release/SKILL.md`. The agent prepares the release PR; the maintainer approves publication by merging it.
   ```

4. **Protect the release.** In your repository settings:
   - Create an environment named `release` that allows only the `main` branch, with no required reviewers. The merge is the approval.
   - Require pull requests for changes to `main`, and block force pushes and deletion.
   - Turn on immutable releases, so that published tags and releases can't be changed.

5. **Try it.** Tell your agent "let's release."

## FAQs

### How does the agent choose the version?

It runs `release_planner.py inventory`, which reports the previous release and the next patch, minor, and major versions. The agent reads every change since the previous release and applies the version policy in your `releases/README.md`. It explains its choice in the PR, so you can change the version by renaming the notes file.

### What does the planner check?

Before anything is published, the planner requires:

- exactly one new or edited notes file per change
- a SemVer 2.0.0 version with no build metadata
- a version newer than every existing tag, with the previous release reachable from the approved commit
- your configured first version, if the repository has no releases yet

It also refuses to edit notes that are already tagged, and it won't let a new request skip an earlier one that never published. The publish step refuses to run if version tags changed after planning or if the previous release is still a draft. After publishing, it verifies that the tag points to the approved commit.

### What if publication fails?

Nothing is tagged until validation passes. Re-run the failed workflow run, or start the Release workflow by hand with the **Base SHA** and **Approved head SHA** from the failed run's summary. A retry after a successful publication makes no changes. Published tags never move: fix problems in a new release.

### Can I publish binaries or other assets?

Not with the `publish` action yet: it publishes notes only. Immutable releases freeze a release's assets when it is published, so assets must be uploaded to a draft first. To ship assets today, use the `plan` action with your own publisher that stages assets on a draft, verifies them, and then publishes, as [Code Rules](https://github.com/fabricahq/code-rules) does for its signed binaries.

### Why not release-please or semantic-release?

Those tools derive versions and notes from commit message conventions. Release Planner has your agent read the actual changes and write notes for people, and requires your approval before anything ships. It needs no commit message conventions.

## How do I contribute?

Open a pull request! Run the tests from the repository root before you push:

```sh
python3 -m unittest discover -s src
```

The planner and publisher use only the Python standard library, Bash, `jq`, and the GitHub CLI, all of which GitHub-hosted runners provide.

## License

Release Planner is [MIT licensed](LICENSE.md), with copyright attributed to Fabrica Systems LLC. The planner is adapted from the release tooling in [Code Rules](https://github.com/fabricahq/code-rules).
