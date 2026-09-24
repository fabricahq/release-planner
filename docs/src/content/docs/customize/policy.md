---
title: Release policy
description: Tell the agent what counts as a breaking change, how you choose versions, and who reads your notes.
---

Your release policy lives in `.release-planner/policy.md`. The agent reads it before every release to choose the version and decide what the notes need to say. It's yours: Release Planner creates a starter once and never changes it.

## What's in the starter

The starter has five sections, each a short list for you to edit:

- **Breaking changes.** A list of common things users depend on, such as commands, configuration keys, and APIs. Keep the ones that apply, delete the rest, and add your own.
- **Choosing a version.** What makes a release major, minor, or patch. If your first release is before 1.0.0, it also covers 0.x releases: SemVer lets anything change before 1.0, so the starter uses the common convention of a minor bump for breaking changes and a patch bump for everything else.
- **Who reads the release notes.** Your readers, which default to developers. The agent writes for them.
- **Order of the release notes.** The order changes appear in: new features, then improvements, then bug fixes, then breaking changes. Reorder it to taste; some projects put breaking changes first.
- **Always and never.** Anything the notes must always include or leave out.

Each section ends with a `TODO:` line. Until you delete every one, the agent stops and asks you instead of guessing a version.

## Have your agent fill it in

The starter opens with a prompt, in a comment, that you can copy and give to your agent:

```text
Fill in .release-planner/policy.md for this repository. Read the README, the docs,
the public interfaces, and the release history to work out what counts as a breaking
change and who reads our release notes. Keep the bulleted lists, delete items that
don't apply, add ones that do, and remove every TODO line. Ask me about anything you
can't tell from the repository.
```

Review what it writes before your first release. Where your policy and the default guidance disagree, your policy wins.

## Examples

### A library that other projects import

```markdown
## Breaking changes
- Removing or renaming a group or rule ID
- Reversing what a rule requires

## Choosing a version
- Major: any breaking change
- Minor: new rules or groups, or extending a rule to new situations
- Patch: corrections, clearer wording, and better examples

## Who reads the release notes
Readers:
- Engineers deciding whether to adopt the new version

## Order of the release notes
1. Breaking changes, listing every rule ID that was removed or renamed
2. New features: new rules and groups
3. Bug fixes: corrections to existing rules
```

### A command-line tool

```markdown
## Breaking changes
- Command names, flags, and exit codes
- Configuration keys
- JSON output that scripts parse

## Who reads the release notes
Readers:
- Developers upgrading the tool

## Order of the release notes
1. New features: new commands and flags
2. Improvements
3. Bug fixes
4. Breaking changes that require them to update configuration or scripts

## Always and never
- Always show the upgrade command.
- Never mention dependency updates unless they fix a security issue.
```

### A service with an HTTP API

```markdown
## Breaking changes
- Removing an endpoint or field
- Changing the meaning of a field or status code
- Changing authentication

## Who reads the release notes
Readers:
- Developers who call the API
- Our support team

## Order of the release notes
1. Breaking changes and their deadlines
2. New features: new endpoints and fields, linked to the API reference
3. Improvements
4. Bug fixes
```
