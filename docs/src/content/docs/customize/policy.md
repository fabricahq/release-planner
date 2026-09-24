---
title: Release policy
description: Tell the agent what your users depend on and how you choose versions.
---

Your release policy lives in `.release-planner/policy.md`. The agent reads it before every release to choose the version and decide what the notes need to say. It's yours: Release Planner creates a starter once and never changes it.

## What to write

The starter has four sections, each with a `TODO:` prompt. Until you replace the prompts, the agent stops and asks you instead of guessing.

- **What users depend on.** What must stay compatible: commands and flags, configuration, APIs, file formats, or identifiers other projects reference.
- **Choosing a version.** What makes a release major, minor, or patch, in terms of that list.
- **Who reads the release notes.** Who they are and what they need to know first.
- **Always and never.** Anything the notes must always include or leave out.

Write it the way you'd brief a new maintainer. Where your policy and the default guidance disagree, your policy wins.

## Examples

### A library that other projects import

```markdown
## What users depend on
Group IDs and rule IDs. Projects list group IDs to import and rule IDs to
exclude or replace, and an unknown ID is an error.

## Choosing a version
- Major: removing or renaming a group or rule, or reversing what a rule requires.
- Minor: new rules or groups, or extending a rule to new situations.
- Patch: corrections, clearer wording, and better examples.

## Who reads the release notes
Engineers deciding whether to adopt the new version. Name every rule ID that
was added, renamed, or removed.
```

### A command-line tool

```markdown
## What users depend on
Command names, flags, exit codes, configuration keys, and JSON output.

## Choosing a version
- Major: removing or renaming a command, flag, or key, or changing output that scripts parse.
- Minor: new commands, flags, or keys.
- Patch: bug fixes and help-text changes.

## Who reads the release notes
Developers upgrading the tool. Lead with anything that requires them to change
configuration or scripts.

## Always and never
Always show the upgrade command. Leave out dependency updates unless they fix a
security issue.
```

### A service with an HTTP API

```markdown
## What users depend on
The public API: endpoints, request and response fields, status codes, and
authentication.

## Choosing a version
- Major: removing an endpoint or field, or changing its meaning.
- Minor: new endpoints or optional fields.
- Patch: fixes and performance work with no API change.

## Who reads the release notes
API consumers and our support team. Link every change to its API reference page.
```
