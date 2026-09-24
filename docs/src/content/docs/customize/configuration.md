---
title: Configuration
description: Every setting in .release-planner/config.yml, and how to upgrade.
---

Release Planner reads `.release-planner/config.yml`. After changing it, run `release-planner install` and commit the result.

## Settings

| Key | Default | Meaning |
| --- | --- | --- |
| `schema-version` | required | The format of the file. Currently `1`. |
| `version` | required | The Release Planner version to use: a release tag such as `v0.1.0`. A full commit SHA also works; your workflow then builds Release Planner from source with Go instead of downloading a release. |
| `first-version` | `v0.1.0` | The version your first release must use. |
| `validate` | none | Optional [release checks](/customize/release-checks/). |
| `release-notes-style` | `append` | How `.release-planner/release-notes-style.md` applies, if it exists. See [Release notes style](/customize/release-notes-style/). |
| `notes-dir` | `releases` | Where the release notes files live. |
| `branch` | `main` | The branch that releases are published from. |

A complete example:

```yaml
schema-version: 1
version: v0.1.0
first-version: v1.0.0
release-notes-style: append
validate:
  run: make smoke-test
```

## Upgrade Release Planner

Install the new version, change `version` in `config.yml` to match, then regenerate your files:

```sh
curl -fsSL https://raw.githubusercontent.com/fabricahq/release-planner/main/install.sh | sh -s -- --version v0.2.0
release-planner install
```

One version pin covers everything: the workflow, the agent's instructions, and the release checks change together.

## Generated files

`install` writes the workflow, the agent skill, and the **Releases** section of `AGENTS.md`. Don't edit them. Change the config and run `install` instead.

- `install` is safe to run any number of times. It updates files that are out of date and leaves everything else alone.
- Your own text in `AGENTS.md` is never touched. Release Planner only changes the text between its `release-planner:begin` and `release-planner:end` markers.
- If a generated file was edited by hand, `install` refuses to overwrite it and says why. Move your change into the config, or run `install --force` to discard it.
- The release workflow runs `release-planner check` on every release pull request, so an out-of-date or edited file fails before it can cause a bad release.

To stop using Release Planner, run `release-planner uninstall`. It removes the generated files and the `AGENTS.md` section, and leaves `.release-planner/` and your release notes in place.
