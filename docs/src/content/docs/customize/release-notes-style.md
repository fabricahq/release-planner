---
title: Release notes style
description: Change how the agent writes release notes.
---

The release notes style tells the agent how to write the notes: how to open, which headings to use, and how to write each entry.

## The default style

Unless you change it, the agent:

- opens with one or two sentences on what the release means for its readers
- organizes changes under these headings, in the order your [policy](/customize/policy/) gives, leaving out any that are empty:
  - ✨ New Features
  - ⬆️ Improvements
  - 🐛 Squashed Bugs
  - ⛓️‍💥 Breaking Changes, also mentioned in the opening sentences
- gives significant changes their own subheading, and uses bullets for small ones
- ends every entry with the pull requests it covers, such as (#7) or (#7, #9)
- groups related commits into one entry

To see the exact text the agent follows, run:

```sh
release-planner guide --default-style
```

## Change the style

Write your style in a markdown file, such as `.release-planner/release-notes-style.md`. Then name that file in `.release-planner/config.yml`, and choose one of two modes.

**Add to the default style:**

```yaml
release-notes-style:
  file: .release-planner/release-notes-style.md
  mode: append
```

The agent follows the default style, then yours. Use this to add rules. When you upgrade Release Planner, you still get improvements to the default style.

**Replace the default style:**

```yaml
release-notes-style:
  file: .release-planner/release-notes-style.md
  mode: replace
```

The agent follows only your style. Use this when you want different headings or a different format altogether. Your style stays exactly as you wrote it when you upgrade.

Release Planner reads only the file your config names. If that file is missing or empty, every command fails and says so, rather than quietly using the default style. Without a `release-notes-style` setting, the agent uses the default style.

### Append example

Keep the default headings, and add rules specific to your project:

```markdown
- Under New Features, name each new rule by its ID, such as
  `techs/react/server-auth-actions`, and link to the rule file at the new tag.
- Under Breaking Changes, list every removed or renamed rule ID with the
  `exclude` or `replace` entry importers must update.
```

### Replace example

Use [Keep a Changelog](https://keepachangelog.com) headings instead of the defaults:

```markdown
- Open with one sentence naming the most important change.
- Use only these headings, leaving out empty ones:
  `## Added`, `## Changed`, `## Deprecated`, `## Removed`, `## Fixed`, `## Security`.
- Write one bullet per change, in the past tense, ending with the pull request link.
```

List the same headings, in the order you want, under **Order of the release notes** in your policy.

To start a replacement from the default text:

```sh
release-planner guide --default-style > .release-planner/release-notes-style.md
```

## What every release keeps

Whatever your style says, the notes end with a **Pull Requests** section listing every pull request with its author, grouped into Features, Bug Fixes, Documentation, CI, and Chores, followed by a **Full Changelog** link (or, for a first release, a link to the released source). The release workflow also rejects notes that still contain the draft's placeholder line or have empty headings.
