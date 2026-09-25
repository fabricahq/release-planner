# Fabrica Release Planner

<p>
  <a href="LICENSE.md"><img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-blue"></a>
</p>

Release Planner helps you publish GitHub releases with notes people actually want to read. Your coding agent does the tedious part: it reviews everything that changed since your last release, picks the next version, and drafts the notes in a pull request. You edit the notes as much as you like, and when you merge, the release is published exactly as you approved it.

It is for maintainers who already work with a coding agent, such as Claude Code or Codex, and want release notes written for readers. It works only with repositories hosted on GitHub that use GitHub Actions, and the command runs on macOS and Linux.

The full documentation is at <https://release-planner.fabricahq.com>.

## Why use it?

When you're ready to release, you want your agent to do as much of the work as possible, while making sure it follows your release process and misses nothing.

Release Planner splits the work. Deterministic code handles the parts that must be exact: it finds everything that shipped since your last release and checks that the notes account for every change. Your agent handles the judgment, drafting the notes according to your customizable release policy. Then you review the notes in a pull request, edit them to your liking, and merge to release.

## How does it work?

1. You tell your agent "let's release."
2. The agent runs `release-planner` to list every change since the previous release, applies your release policy to choose the version, and opens a pull request that adds the notes file, such as `_releases/v1.2.0.md`. The release is the commit that pull request starts from.
3. On the pull request, the generated Release workflow checks the version and the notes, runs any release checks you configured, and builds any files to attach. The pull request's description shows the release's status as it goes.
4. You edit the notes in the pull request. Saving edits publishes nothing.
5. You merge. The workflow tags the release commit and publishes the notes word for word, with the files the pull request built.

The tag is created only at the last step, so a version never exists without the notes you approved. [How it works](https://release-planner.fabricahq.com/start-here/how-it-works/) describes the files and the command in more detail.

## How do I set it up?

You need a GitHub repository with GitHub Actions enabled. Run these commands from the repository root.

1. Install the latest release. The script verifies the download's checksum and installs `release-planner` into `~/.local/bin`.

   ```sh
   curl -fsSL https://raw.githubusercontent.com/fabricahq/release-planner/main/install.sh | sh
   ```

2. Create the configuration and a starter release policy. Set `--first-version` to the version your first release should have.

   ```console
   $ release-planner init --first-version v1.0.0
   created   .release-planner/config.yml
   created   .release-planner/policy.md
   Next: fill in .release-planner/policy.md, review the settings in .release-planner/config.yml, then run release-planner install.
   ```

3. Fill in `.release-planner/policy.md`: replace each `TODO:` line with what your users depend on, how you choose versions, and who reads your notes. Until every `TODO:` line is gone, the agent stops and asks instead of guessing a version. [Release policy](https://release-planner.fabricahq.com/customize/policy/) has examples and a prompt you can give your agent to draft it.

4. Generate the GitHub Actions workflow that publishes releases, along with the agent instructions.

   ```console
   $ release-planner install
   created   .github/workflows/release-planner.yml
   created   .agents/skills/release/SKILL.md
   created   .claude/skills/release/SKILL.md
   added     AGENTS.md  (Releases section)
   Commit these files. The Release workflow runs release-planner check on every release pull request.
   ```

5. Commit and push the files, then tell your agent "let's release." It opens a pull request titled **Release v1.0.0** with the drafted notes, and merging it publishes the release.

Before you merge your first release, protect your release branch and create the `release` environment, as [Set up a repository](https://release-planner.fabricahq.com/start-here/set-up/#5-protect-your-releases) describes. That guide also covers installing a specific version.

## Limits

- **GitHub only.** Release Planner publishes GitHub releases and runs on GitHub Actions. Other hosts, such as GitLab, are not supported.
- **macOS and Linux only.** There is no Windows build of the command.
- **One version per repository.** Releases are tagged with SemVer versions such as `v1.2.3`, so separate versions for packages in a monorepo, and calendar versions, are not supported.
- **A person approves every release.** Each release waits for someone to merge its release pull request. If you want every merge released automatically from commit message conventions, [release-please](https://github.com/googleapis/release-please) or [semantic-release](https://github.com/semantic-release/semantic-release) fits better.

## Learn more

- [Set up a repository](https://release-planner.fabricahq.com/start-here/set-up/): the full setup, including branch protection and the `release` environment
- [Make a release](https://release-planner.fabricahq.com/start-here/release/): review the notes, merge, and retry a failed release
- [Release policy](https://release-planner.fabricahq.com/customize/policy/), [release notes style](https://release-planner.fabricahq.com/customize/release-notes-style/), and [release checks](https://release-planner.fabricahq.com/customize/release-checks/): customize how releases are prepared and checked
- [Release assets](https://release-planner.fabricahq.com/customize/release-assets/) and [downstream workflows](https://release-planner.fabricahq.com/customize/downstream/): attach built files to each release, and start workflows in other repositories after it
- [Configuration](https://release-planner.fabricahq.com/customize/configuration/): every setting, the generated files, and upgrading
- [For agents](https://release-planner.fabricahq.com/for-agents/): every command, file format, and check

## How do I contribute?

Pull requests are welcome. [CONTRIBUTING.md](CONTRIBUTING.md) explains how to check your change and work on the documentation site.

## License

Release Planner is [MIT licensed](LICENSE.md), with copyright attributed to Fabrica Systems LLC. The planner and publisher are adapted from the release tooling in [Code Rules](https://github.com/fabricahq/code-rules).
