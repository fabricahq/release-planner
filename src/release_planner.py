#!/usr/bin/env python3
"""Plan agent-drafted, maintainer-approved GitHub releases.

`inventory` tells an agent what a new release would contain: the previous release,
every commit since it, and the candidate next versions.

`plan` validates a committed release request and prints the exact publication inputs.
A release request is one new or edited `releases/v<semver>.md` file between two commits.
The plan binds that file's version and notes to the head commit, which the release tags.
An empty `tag` means the range requests no release.

Ported from Code Rules' `plan-release`. Uses only the Python standard library and git.
"""

import argparse
import json
import re
import subprocess
import sys

SEMVER = re.compile(
    r"^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)"
    r"(?:-((?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*))?$"
)
PULL_REQUEST = re.compile(r"^Merge pull request #(\d+)|\(#(\d+)\)$")


class PlanError(Exception):
    pass


def parse(tag):
    """Return a sortable SemVer 2.0.0 precedence key, or None for a non-version tag."""
    match = SEMVER.match(tag)
    if not match:
        return None
    core = tuple(int(part) for part in match.groups()[:3])
    pre = match.group(4)
    if pre is None:
        # A release without a prerelease suffix sorts after all of its prereleases.
        return core, (1,)
    # Numeric identifiers sort before alphanumeric ones and compare numerically.
    ids = tuple((0, int(i), "") if i.isdigit() else (1, 0, i) for i in pre.split("."))
    return core, (0,) + ids


class Repository:
    def __init__(self, source, notes_dir="releases"):
        self.source = source
        self.notes_dir = notes_dir.strip("/")

    def git(self, *args):
        result = subprocess.run(["git", *args], cwd=self.source, capture_output=True, text=True)
        if result.returncode != 0:
            raise PlanError(f"git {' '.join(args)}: {result.stderr.strip()}")
        return result.stdout

    def resolve(self, ref):
        return self.git("rev-parse", "--verify", "--end-of-options", ref + "^{commit}").strip()

    def is_ancestor(self, ancestor, descendant):
        return subprocess.run(
            ["git", "merge-base", "--is-ancestor", ancestor, descendant], cwd=self.source, capture_output=True
        ).returncode == 0

    def version_tags(self):
        return self.git("tag", "--list", "v*").split()

    def notes_tag(self, name):
        """Return the tag a notes path requests, or None for other files."""
        directory, _, base = name.rpartition("/")
        if directory == self.notes_dir and base.startswith("v") and base.endswith(".md"):
            return base[: -len(".md")]
        return None

    def notes_files(self, commit):
        names = self.git("ls-tree", "-r", "--name-only", "-z", commit, "--", self.notes_dir + "/").split("\0")
        return [name for name in names if name and self.notes_tag(name)]


def plan(repo, base, head, first):
    empty = {"tags": [], "tag": "", "version": "", "commit": "", "previous": "", "notes": "", "prerelease": False}
    base, head = repo.resolve(base), repo.resolve(head)
    if not repo.is_ancestor(base, head):
        raise PlanError("release base must be an ancestor of head")
    tags = repo.version_tags()

    fields = repo.git("diff", "--name-status", "-z", "--no-renames", base, head, "--", repo.notes_dir + "/").split("\0")
    requested = None
    for status, name in zip(fields[0::2], fields[1::2]):
        tag = repo.notes_tag(name)
        if not tag:
            continue
        if status != "A":
            # Published notes are history; correct later behavior in a new release.
            if tag in tags:
                raise PlanError(f"tagged release notes are immutable: {name}")
            if status == "D":
                continue
            if status != "M":
                raise PlanError(f"unsupported release note change: {name}")
        if requested:
            raise PlanError("submit one release notes file per change")
        requested = name
    if not requested:
        return empty

    tag = repo.notes_tag(requested)
    current = parse(tag)
    if current is None:
        raise PlanError(f"{requested}: name the file v<MAJOR>.<MINOR>.<PATCH>[-prerelease].md, without build metadata")
    notes = repo.git("show", f"{head}:{requested}")
    if not notes.strip():
        raise PlanError("release notes must not be empty")

    # A new request must not strand an earlier one that never reached tagging.
    for name in repo.notes_files(head):
        pending = repo.notes_tag(name)
        if name != requested and pending not in tags:
            raise PlanError(f"resolve untagged release request {name} before requesting {tag}")

    observed, previous, latest = [], "", None
    for existing in tags:
        other = parse(existing)
        if other is None:
            continue
        if existing == tag:
            # Retrying a publication is allowed only when the tag already marks this exact commit.
            if repo.resolve("refs/tags/" + tag) != head:
                raise PlanError(f"tag {tag} already points to another commit")
            continue
        observed.append(existing)
        if current <= other:
            raise PlanError(f"{tag} must be newer than existing tag {existing}")
        if latest is None or other > latest:
            previous, latest = existing, other
    if not previous and tag != first:
        raise PlanError(f"the first release must be {first}")
    if previous and not repo.is_ancestor("refs/tags/" + previous, head):
        raise PlanError(f"previous release {previous} must be an ancestor of {head}")

    return {
        "tags": sorted(observed),
        "tag": tag,
        "version": tag[1:],
        "commit": head,
        "previous": previous,
        "notes": notes,
        "prerelease": "-" in tag,
    }


def inventory(repo, head, first):
    """Describe what a release cut at head would contain, for the agent drafting notes."""
    head = repo.resolve(head)
    tags = repo.version_tags()
    released = [t for t in tags if parse(t) and repo.is_ancestor("refs/tags/" + t, head)]
    previous = max(released, key=parse, default="")
    if previous:
        major, minor, patch = parse(previous)[0]
        pre = "-" in previous
        # A prerelease's own core version is the next stable release in its line.
        candidates = {
            "patch": f"v{major}.{minor}.{patch if pre else patch + 1}",
            "minor": f"v{major}.{minor if pre and patch == 0 else minor + 1}.0",
            "major": f"v{major if pre and minor == patch == 0 else major + 1}.0.0",
        }
        log_range = f"{repo.resolve('refs/tags/' + previous)}..{head}"
    else:
        candidates = {"first": first}
        log_range = head
    commits = []
    for record in repo.git("log", "--reverse", "--format=%H%x1f%an%x1f%s%x1e", log_range).split("\x1e"):
        if not record.strip():
            continue
        sha, author, subject = record.strip().split("\x1f")
        match = PULL_REQUEST.search(subject)
        commits.append({
            "sha": sha,
            "author": author,
            "subject": subject,
            "pullRequest": int(match.group(1) or match.group(2)) if match else None,
        })
    pending = [name for name in repo.notes_files(head) if repo.notes_tag(name) not in tags]
    newer = [t for t in tags if parse(t) and previous and parse(t) > parse(previous)]
    return {
        "head": head,
        "previous": previous,
        "candidates": candidates,
        "pendingRequests": pending,
        "unmergedNewerTags": sorted(newer, key=parse),
        "pullRequests": sorted({c["pullRequest"] for c in commits if c["pullRequest"]}),
        "commits": commits,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--source", default=".", help="Repository to inspect")
    parser.add_argument("--notes-dir", default="releases", help="Directory holding v<semver>.md release requests")
    parser.add_argument("--first", default="v0.1.0", help="Version required when no release exists yet")
    commands = parser.add_subparsers(dest="command", required=True)
    plan_parser = commands.add_parser("plan", help="Validate a committed release request")
    plan_parser.add_argument("--base", required=True, help="Commit before the release request")
    plan_parser.add_argument("--head", default="HEAD", help="Commit containing the approved notes")
    inventory_parser = commands.add_parser("inventory", help="List changes since the previous release")
    inventory_parser.add_argument("--head", default="HEAD", help="Commit the release would tag")
    args = parser.parse_args()
    repo = Repository(args.source, args.notes_dir)
    try:
        if args.command == "plan":
            result = plan(repo, args.base, args.head, args.first)
        else:
            result = inventory(repo, args.head, args.first)
    except PlanError as error:
        print(error, file=sys.stderr)
        return 1
    json.dump(result, sys.stdout, indent=2 if args.command == "inventory" else None)
    print()
    return 0


if __name__ == "__main__":
    sys.exit(main())
