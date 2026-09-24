# Release requests

Each release of Release Planner starts as one `v<semver>.md` file in this directory. Edit the notes in the release PR, then merge the PR to publish. The Release workflow runs the tests on the merged commit, tags it, and publishes the notes verbatim.

The agent procedure is in [the release skill](../skills/release/SKILL.md).

## Version policy

Use [SemVer 2.0.0](https://semver.org/) with tags `vMAJOR.MINOR.PATCH`. The first release is `v0.1.0`.

The public contract is what consuming repositories rely on: the `plan` and `publish` action inputs and outputs, the `release_planner.py` command-line interface and JSON output, the release-request rules the planner enforces, and the documented workflow template.

- Before `1.0.0`, increment minor for new features or breaking changes, and patch for compatible fixes. Always document breaking changes.
- From `1.0.0`, increment major for breaking changes, minor for compatible features, and patch for compatible fixes.

## Validation

Run `python3 -m unittest discover -s src` from the repository root.
