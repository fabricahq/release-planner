# Release Planner agent guide

The [README](README.md) explains what Release Planner does and how consuming repositories set it up.

Keep the planner and publisher free of third-party dependencies: the Python standard library, Bash, `jq`, and the GitHub CLI. Run `python3 -m unittest discover -s src` before you push.

When asked to make a release, draft or revise release notes, or retry a failed release, follow [the release skill](skills/release/SKILL.md). The agent prepares the release PR; the maintainer approves publication by merging it.
