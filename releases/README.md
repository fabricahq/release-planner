# Release policy

Release Planner's own release policy. The agent reads this file before every release.

## What users depend on

Adopting repositories rely on:

- The commands, their flags, exit codes, and JSON output.
- The keys in `release-planner.yml` and what they mean.
- The generated files: their paths, their markers, and what the Release workflow does.
- The checks `plan` and `publish` enforce, which decide whether a release goes out.

## Choosing a version

Versions follow [SemVer 2.0.0](https://semver.org/). The first release is v0.1.0.

- Before 1.0.0, increment minor for new features or anything that breaks the list above, and patch for compatible fixes. Always document breaking changes.
- From 1.0.0, increment major for breaking changes, minor for compatible features, and patch for compatible fixes.
- A change that makes `check` fail in adopting repositories until they rerun `install` is expected on upgrade and is not breaking by itself. Say so in the notes.

## Who reads the release notes

Maintainers of repositories that use Release Planner, deciding whether to bump `version`. Lead with anything that changes what they must configure or how releases behave, and show the upgrade command.

## Always and never

Always say whether upgrading changes the generated files. Leave out refactors and test-only changes.
