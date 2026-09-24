---
title: Release notes style
description: Change how the agent writes release notes.
---

The release notes style tells the agent how to write the notes: how to open, which headings to use, and how to write each entry.

## The default style

Unless you change it, the agent:

- opens with one or two sentences on what the release means for its readers
- organizes changes under these headings, leaving out any that are empty:
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

Create `.release-planner/release-notes-style.md` with your guidance, and choose how it applies in `.release-planner/config.yml`:

```yaml
release-notes-style: append   # or replace
```

- **`append`**, the default, keeps the default style and adds yours after it. Use it to add rules. When you upgrade Release Planner, you still get improvements to the default style.
- **`replace`** uses only your style. Use it when you want different headings or a different format altogether. Your style stays exactly as you wrote it when you upgrade.

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
- Use only these headings, in this order, leaving out empty ones:
  `## Added`, `## Changed`, `## Deprecated`, `## Removed`, `## Fixed`, `## Security`.
- Write one bullet per change, in the past tense, ending with the pull request link.
```

To start a replacement from the default text:

```sh
release-planner guide --default-style > .release-planner/release-notes-style.md
```

## What every release keeps

Whatever your style says, the notes end with a **What's Changed** list of the pull requests, followed by a **Full Changelog** link (or, for a first release, a link to the released source). The release workflow also rejects notes that still contain the draft's placeholder line or have empty headings.
