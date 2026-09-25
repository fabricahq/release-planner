---
title: Configuration
description: Every setting in .release-planner/config.yml, and how to upgrade.
---

Release Planner reads its settings from `.release-planner/config.yml`. After changing it, run `release-planner install` and commit the result.

## The minimum

Only two settings are required:

```yaml
schema-version: 1   # the format of this file; always 1 for now
version: v0.2.0     # the Release Planner version this repository uses
```

Everything else has a sensible default, so many repositories never add more.

## Every setting

This example sets everything. The comments say what each setting does and what you get if you leave it out.

```yaml
# Required. The format of this file. Always 1 for now.
schema-version: 1

# Required. The Release Planner version to use, as a release tag.
# A full commit SHA also works; your workflow then builds Release Planner
# from source with Go instead of downloading a release. Bumping it upgrades the
# GitHub Actions workflow (.github/workflows/release-planner.yml) and the agent
# guide together; run release-planner install after changing it.
version: v0.2.0

# The version your first release must use.
# Default: v0.1.0
first-version: v1.0.0

# Where the release notes files live, one per release.
# Default: _releases
release-notes-dir: _releases

# The branch that release pull requests merge into, and releases are published from.
# Default: main
release-branch: main

# Your own release notes style: the markdown file that holds it, and whether
# it adds to the default style (append) or replaces it (replace).
# Default: none; the agent uses the default style.
release-notes-style:
  file: .release-planner/release-notes-style.md
  mode: append

# Checks to run on the exact commit being released, before it's tagged.
# Default: none.
release-checks:
  run: make smoke-test

# Release notes rules to skip. List them with: release-planner validate --rules
# Default: none; every rule is checked.
exclude-rules: [no-long-heading]
```

For the details of the last three, see [Release notes style](/customize/release-notes-style/), [Release checks](/customize/release-checks/), and [Release notes rules](/customize/release-notes-style/#release-notes-rules).

## Upgrade Release Planner

Install the new version, change `version` in `config.yml` to match, then regenerate your files:

```sh
curl -fsSL https://raw.githubusercontent.com/fabricahq/release-planner/main/install.sh | sh -s -- --version v0.2.0
release-planner install
```

One version pin covers everything: the workflow and the agent's instructions change together.

## Generated files

`install` writes the workflow, the agent skill, and the **Releases** section of `AGENTS.md`. Don't edit them. Change the config and run `install` instead.

- `install` is safe to run any number of times. It updates files that are out of date and leaves everything else alone.
- Your own text in `AGENTS.md` is never touched. Release Planner only changes the text between its `release-planner:begin` and `release-planner:end` markers.
- If a generated file was edited by hand, `install` refuses to overwrite it and says why. Move your change into the config, or run `install --force` to discard it.
- The release workflow runs `release-planner check` on every release pull request, so an out-of-date or edited file fails before it can cause a bad release.

To stop using Release Planner, run `release-planner uninstall`. It removes the generated files and the `AGENTS.md` section, and leaves `.release-planner/` and your release notes in place.
