"""Exercise release selection against committed notes, real tags, and retry histories.

Ported from Code Rules' `internal/release/plan_test.go`. Run from the repository root:
python3 -m unittest discover -s src
"""

import os
import subprocess
import tempfile
import unittest

import release_planner
from release_planner import PlanError, parse

FIRST = "v1.0.0"


class Repository:
    def __init__(self, directory):
        self.directory = directory
        self.git("init", "-q", "-b", "main")
        self.write("README.md", "Fixture")
        self.commit("Initial")
        self.initial = self.head()

    def git(self, *args):
        env = {**os.environ, "GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@example.com",
               "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@example.com"}
        result = subprocess.run(["git", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false", *args],
                                cwd=self.directory, capture_output=True, text=True, env=env, check=True)
        return result.stdout.strip()

    def write(self, name, body):
        path = os.path.join(self.directory, name)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w") as file:
            file.write(body)

    def commit(self, message):
        self.git("add", "-A")
        self.git("commit", "-q", "-m", message)
        return self.head()

    def head(self):
        return self.git("rev-parse", "HEAD")

    def plan(self, base, head="HEAD"):
        return release_planner.plan(release_planner.Repository(self.directory), base, head, FIRST)

    def inventory(self, head="HEAD"):
        return release_planner.inventory(release_planner.Repository(self.directory), head, FIRST)


class ReleaseTest(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.repo = Repository(temp.name)

    def test_release_requests(self):
        cases = [
            ("first", "v1.0.0", "## ✨ New Features\nFirst release.\n", "", ""),
            ("wrong-first", "v0.1.0", "Notes", "", "first release"),
            ("empty", "v1.0.0", " \n", "", "empty"),
            ("invalid", "v01.1.0", "Notes", "", "name the file"),
            ("partial", "v1.0", "Notes", "", "name the file"),
            ("metadata", "v1.0.0+build.1", "Notes", "", "build metadata"),
            ("patch", "v1.0.1", "Notes", "v1.0.0", ""),
            ("minor", "v1.1.0", "Notes", "v1.0.0", ""),
            ("major", "v2.0.0", "Notes", "v1.9.0", ""),
            ("prerelease", "v1.1.0-rc.1", "Notes", "v1.0.0", ""),
            ("backwards", "v1.0.0", "Notes", "v1.1.0", "newer"),
            ("prerelease-after-release", "v1.1.0-rc.1", "Notes", "v1.1.0", "newer"),
            ("reused", "v1.0.0", "Notes", "v1.0.0", "another commit"),
        ]
        for name, tag, notes, existing, want in cases:
            with self.subTest(name):
                self.setUp()
                repo = self.repo
                if existing:
                    repo.git("tag", existing)
                base = repo.head()
                repo.write(f"releases/{tag}.md", notes)
                head = repo.commit("Release")
                if want:
                    with self.assertRaisesRegex(PlanError, want):
                        repo.plan(base, head)
                    continue
                plan = repo.plan(base, head)
                self.assertEqual(
                    (plan["tag"], plan["notes"], plan["commit"], plan["previous"], plan["prerelease"]),
                    (tag, notes, head, existing, "-" in tag),
                )
                repo.git("tag", tag)
                self.assertEqual(repo.plan(base, head), plan, "retry at the tagged commit")

    def test_no_request(self):
        self.repo.write("practices/rule.md", "Rule")
        self.assertEqual(self.repo.plan(self.repo.initial, self.repo.commit("Rule"))["tag"], "")

    def test_notes_ownership(self):
        repo = self.repo
        repo.write("releases/v1.0.0.md", "Original notes")
        first = repo.commit("Notes")
        repo.git("tag", "v1.0.0")
        repo.write("releases/v1.0.0.md", "Uncommitted edit")
        self.assertEqual(repo.plan(repo.initial, first)["notes"], "Original notes")
        repo.commit("Edit")
        with self.assertRaisesRegex(PlanError, "immutable"):
            repo.plan(first)
        repo.git("tag", "-d", "v1.0.0")
        base = repo.head()
        repo.write("releases/v1.0.0.md", "Edited again")
        repo.write("releases/v1.1.0.md", "Second")
        repo.commit("More notes")
        with self.assertRaisesRegex(PlanError, "one release"):
            repo.plan(base)

    def test_correct_or_withdraw_untagged_request(self):
        repo = self.repo
        repo.write("releases/v1.0.0.md", "Original")
        original = repo.commit("Request")
        repo.write("releases/v1.0.0.md", "Corrected")
        corrected = repo.commit("Correct")
        plan = repo.plan(original, corrected)
        self.assertEqual((plan["tag"], plan["notes"]), ("v1.0.0", "Corrected"))
        repo.git("rm", "-q", "releases/v1.0.0.md")
        repo.commit("Withdraw")
        self.assertEqual(repo.plan(corrected)["tag"], "")

    def test_ignores_malformed_version_tags(self):
        repo = self.repo
        repo.git("tag", "v-preview")
        repo.write("releases/v1.0.0.md", "Approved notes")
        repo.commit("Release")
        self.assertEqual(repo.plan(repo.initial)["tags"], [])

    def test_pending_request_cannot_be_skipped(self):
        repo = self.repo
        repo.git("tag", "v1.0.0")
        repo.write("releases/v1.1.0.md", "Pending release")
        base = repo.commit("Pending request")
        repo.write("releases/v1.2.0.md", "Later release")
        repo.commit("Later request")
        with self.assertRaisesRegex(PlanError, "untagged release request"):
            repo.plan(base)
        repo.git("tag", "v1.1.0", base)
        plan = repo.plan(base)
        self.assertEqual((plan["tag"], plan["previous"]), ("v1.2.0", "v1.1.0"))
        repo.git("tag", "-d", "v1.1.0")
        repo.git("rm", "-q", "releases/v1.1.0.md")
        repo.commit("Withdraw pending request")
        plan = repo.plan(base)
        self.assertEqual((plan["tag"], plan["previous"]), ("v1.2.0", "v1.0.0"))

    def test_inventory_first_release(self):
        repo = self.repo
        repo.write("a.md", "a")
        repo.commit("Add a (#1)")
        result = repo.inventory()
        self.assertEqual(result["previous"], "")
        self.assertEqual(result["candidates"], {"first": FIRST})
        self.assertEqual([c["subject"] for c in result["commits"]], ["Initial", "Add a (#1)"])
        self.assertEqual(result["pullRequests"], [1])

    def test_inventory_since_previous_release(self):
        repo = self.repo
        repo.write("releases/v1.0.0.md", "First")
        repo.commit("Release v1.0.0")
        repo.git("tag", "v1.0.0")
        repo.write("b.md", "b")
        repo.commit("Merge pull request #7 from fabricahq/feature")
        repo.write("releases/v1.1.0.md", "Pending")
        repo.commit("Request v1.1.0")
        result = repo.inventory()
        self.assertEqual(result["previous"], "v1.0.0")
        self.assertEqual(result["candidates"], {"patch": "v1.0.1", "minor": "v1.1.0", "major": "v2.0.0"})
        self.assertEqual(result["pullRequests"], [7])
        self.assertEqual(result["pendingRequests"], ["releases/v1.1.0.md"])
        self.assertEqual(len(result["commits"]), 2)

    def test_inventory_after_prerelease(self):
        repo = self.repo
        repo.git("tag", "v1.1.0-rc.1")
        self.assertEqual(repo.inventory()["candidates"], {"patch": "v1.1.0", "minor": "v1.1.0", "major": "v2.0.0"})

    def test_custom_notes_directory(self):
        repo = self.repo
        repo.write("docs/releases/v1.0.0.md", "Notes")
        repo.write("releases/v9.0.0.md", "Ignored: outside the configured directory")
        repo.commit("Release")
        plan = release_planner.plan(release_planner.Repository(repo.directory, "docs/releases"), repo.initial, "HEAD", FIRST)
        self.assertEqual(plan["tag"], "v1.0.0")

    def test_semver_precedence(self):
        ordered = ["v1.0.0-alpha", "v1.0.0-alpha.1", "v1.0.0-alpha.beta", "v1.0.0-beta",
                   "v1.0.0-beta.2", "v1.0.0-beta.11", "v1.0.0-rc.1", "v1.0.0", "v1.0.1", "v1.10.0", "v2.0.0"]
        self.assertEqual(sorted(ordered, key=parse), ordered)


if __name__ == "__main__":
    unittest.main()
