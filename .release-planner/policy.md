# Release policy

Release Planner's own release policy. The agent reads this file before every release.

## Breaking changes

A change is breaking when it changes any of these in a way that forces adopting repositories to change their setup or workflow:

- Commands, their flags, exit codes, and JSON output
- The keys in `.release-planner/config.yml` and what they mean
- The generated files: their paths, their markers, and what the Release workflow does
- The checks `plan` and `publish` enforce, which decide whether a release goes out
- The release archive names and the install script's options

A change that makes `check` fail until adopters rerun `install` is expected on upgrade and is not breaking by itself.

## Choosing a version

Versions follow [SemVer 2.0.0](https://semver.org/). The first release is v0.1.0.

Before 1.0.0:

- Minor: any breaking change, or a significant new feature
- Patch: everything else

From 1.0.0:

- Major: any breaking change
- Minor: new features that existing users can ignore
- Patch: bug fixes, documentation, and internal changes

## Who reads the release notes

Readers:

- Maintainers of repositories that use Release Planner, deciding whether to bump `version`

They want to know what changes for them when they upgrade, including whether their generated files change.

## Order of the release notes

The notes present changes in this order, leaving out any with nothing to say:

1. New features
2. Improvements
3. Bug fixes
4. Breaking changes, with the upgrade steps

## Always and never

- Always show the upgrade command.
- Never mention refactors or test-only changes.
