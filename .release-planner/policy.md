# Release policy

Release Planner's own release policy. The agent reads this file before every release.

## Breaking changes

A change is breaking when it changes any of these in a way that forces adopting repositories to change their setup or workflow:

- Commands, their flags, exit codes, and JSON output
- The keys in `.release-planner/config.yml` and what they mean
- The generated files: their paths, their markers, and what the Release workflow does
- The checks `validate` and `publish` enforce, which decide whether a release goes out
- The release archive names and the install script's options

A change that makes `check` fail until adopters rerun `install` is expected on upgrade and is not breaking by itself.

## Choosing a version

Versions follow [SemVer 2.0.0](https://semver.org/). The first release is v0.1.0.

Before 1.0.0:

- Minor: any breaking change or new feature
- Patch: bug fixes, documentation, and internal changes

From 1.0.0:

- Major: any breaking change
- Minor: new features that existing users can ignore
- Patch: bug fixes, documentation, and internal changes

## Who reads the release notes

Readers:

- Maintainers of repositories that use Release Planner, deciding whether to bump `version`

They want to know what they get from upgrading and what they have to change. Lead each entry with what a maintainer can now do or no longer has to worry about, then explain how it works. Keep command, flag, and JSON details short, and label details that matter only to people building on Release Planner, such as "For tool authors".

## Order of the release notes

Start with the first section that has something to say. Don't put a summary or introduction above it.

The notes present changes in this order, leaving out any with nothing to say:

1. New features: something maintainers can do or get that they couldn't before
2. Improvements: existing behavior that works better, including new warnings and documentation. Give significant ones a `###` heading, and end the section with small ones in a bulleted `### Also in this release`.
3. Bug fixes: the trigger, the previous wrong behavior, and the corrected result
4. Breaking changes: one `###` entry per change, saying who is affected and what happens if they don't act, followed by numbered `### Upgrade steps`

Describe each change once, in the section it fits best.

## Always and never

- Always show the upgrade command: in the upgrade steps when a release has breaking changes, and otherwise in a `## Upgrade` section after the last change section.
- Never mention refactors or test-only changes.
- Never link to headings within the notes. Headings start with emoji, so their anchors are fragile; refer to a section by its name instead.
