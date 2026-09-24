"""Exercise publish_release.sh against a fake gh that stores tags and releases in a JSON file.

Run from the repository root: python3 -m unittest discover -s src
"""

import json
import os
import stat
import subprocess
import tempfile
import textwrap
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
COMMIT = "a" * 40
OTHER = "b" * 40

# A minimal stand-in for the gh commands the publisher uses.
FAKE_GH = textwrap.dedent('''\
    #!/usr/bin/env python3
    import json, os, sys
    path = os.environ["FAKE_GH_STATE"]
    state = json.load(open(path))
    args = sys.argv[1:]
    def save():
        json.dump(state, open(path, "w"))
    def option(name):
        for arg in args:
            if arg.startswith(name + "="):
                return arg.split("=", 1)[1]
        return args[args.index(name) + 1] if name in args else None
    def out(value):
        query = option("-q")
        if query:
            if query == ".[].ref":
                value = "\\n".join(r["ref"] for r in value)
            else:
                for key in query.lstrip(".").split("."):
                    value = value[key]
            if isinstance(value, bool):
                value = str(value).lower()
            print(value)
        else:
            print(json.dumps(value))
    if args[0] == "api":
        endpoint = [a for a in args[1:] if not a.startswith("-") and a != option("-q")][0]
        repo = os.environ["GH_REPO"]
        if endpoint == f"repos/{repo}/git/matching-refs/tags/v":
            out([{"ref": "refs/tags/" + t} for t in state["tags"] if t.startswith("v")])
        elif endpoint.startswith(f"repos/{repo}/git/ref/tags/"):
            tag = endpoint.rsplit("/", 1)[1]
            if tag not in state["tags"]:
                sys.exit(1)
            out({"object": state["tags"][tag]})
        elif endpoint.startswith(f"repos/{repo}/git/tags/"):
            out({"object": {"sha": state["annotated"][endpoint.rsplit("/", 1)[1]]}})
        else:
            sys.exit(f"unexpected endpoint {endpoint}")
    elif args[:2] == ["release", "view"]:
        release = state["releases"].get(args[2])
        if not release:
            sys.exit(1)
        out({**release, "url": "https://example.com/" + args[2]})
    elif args[:2] == ["release", "create"]:
        tag = args[2]
        state["writes"].append("create")
        state["releases"][tag] = {"name": option("--title"), "body": open(option("--notes-file")).read(),
                                  "isDraft": False, "isPrerelease": "--prerelease" in args}
        state["tags"].setdefault(tag, {"type": "commit", "sha": option("--target")})
        save()
    elif args[:2] == ["release", "edit"]:
        state["writes"].append("edit")
        state["releases"][args[2]]["isDraft"] = False
        save()
    else:
        sys.exit(f"unexpected gh {args}")
''')


class PublishTest(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.dir = temp.name
        gh = os.path.join(self.dir, "gh")
        with open(gh, "w") as file:
            file.write(FAKE_GH)
        os.chmod(gh, os.stat(gh).st_mode | stat.S_IEXEC)
        self.state = {"tags": {}, "annotated": {}, "releases": {}, "writes": []}

    def publish(self, tag="v1.1.0", tags=("v1.0.0",), previous="v1.0.0", notes="## Notes\n"):
        if previous:
            self.state["tags"].setdefault(previous, {"type": "commit", "sha": OTHER})
            self.state["releases"].setdefault(previous, {"name": previous, "body": "", "isDraft": False, "isPrerelease": False})
        state_file = os.path.join(self.dir, "state.json")
        with open(state_file, "w") as file:
            json.dump(self.state, file)
        plan = os.path.join(self.dir, "plan.json")
        with open(plan, "w") as file:
            json.dump({"tags": list(tags), "tag": tag, "version": tag[1:], "commit": COMMIT, "previous": previous,
                       "notes": notes, "prerelease": "-" in tag}, file)
        env = {**os.environ, "PATH": self.dir + os.pathsep + os.environ["PATH"],
               "FAKE_GH_STATE": state_file, "GH_REPO": "fabricahq/example"}
        result = subprocess.run(["bash", os.path.join(HERE, "publish_release.sh"), plan],
                                capture_output=True, text=True, env=env)
        with open(state_file) as file:
            self.state = json.load(file)
        return result

    def test_creates_tag_and_release(self):
        result = self.publish()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.state["writes"], ["create"])
        self.assertEqual(self.state["tags"]["v1.1.0"]["sha"], COMMIT)
        self.assertEqual(self.state["releases"]["v1.1.0"]["body"], "## Notes\n")

    def test_first_release(self):
        result = self.publish(tag="v0.1.0", tags=(), previous="")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.state["writes"], ["create"])

    def test_prerelease(self):
        result = self.publish(tag="v1.1.0-rc.1")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(self.state["releases"]["v1.1.0-rc.1"]["isPrerelease"])

    def test_retry_after_publication_makes_no_writes(self):
        self.assertEqual(self.publish().returncode, 0)
        self.state["writes"] = []
        result = self.publish()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("already published", result.stdout)
        self.assertEqual(self.state["writes"], [])

    def test_publishes_matching_draft(self):
        self.state["releases"]["v1.1.0"] = {"name": "v1.1.0", "body": "## Notes", "isDraft": True, "isPrerelease": False}
        self.state["tags"]["v1.1.0"] = {"type": "commit", "sha": COMMIT}
        result = self.publish()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.state["writes"], ["edit"])

    def test_refuses_tag_on_another_commit(self):
        self.state["tags"]["v1.1.0"] = {"type": "tag", "sha": "tagobject"}
        self.state["annotated"]["tagobject"] = OTHER
        result = self.publish()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("already points to", result.stdout)
        self.assertEqual(self.state["writes"], [])

    def test_accepts_annotated_tag_on_approved_commit(self):
        self.state["tags"]["v1.1.0"] = {"type": "tag", "sha": "tagobject"}
        self.state["annotated"]["tagobject"] = COMMIT
        result = self.publish()
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_refuses_different_existing_release(self):
        self.state["releases"]["v1.1.0"] = {"name": "v1.1.0", "body": "Other", "isDraft": True, "isPrerelease": False}
        result = self.publish()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.state["writes"], [])

    def test_refuses_tags_added_after_planning(self):
        self.state["tags"]["v1.2.0"] = {"type": "commit", "sha": OTHER}
        result = self.publish()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("changed since planning", result.stdout)

    def test_refuses_unpublished_previous_release(self):
        self.state["tags"]["v1.0.0"] = {"type": "commit", "sha": OTHER}
        self.state["releases"]["v1.0.0"] = {"name": "v1.0.0", "body": "", "isDraft": True, "isPrerelease": False}
        result = self.publish()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Publish v1.0.0 first", result.stdout)


if __name__ == "__main__":
    unittest.main()
