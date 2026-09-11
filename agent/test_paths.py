import ast
import os
import pathlib
import unittest

import test_barrier  # noqa: F401
import paths


class StateDir(unittest.TestCase):
    def setUp(self):
        was = os.environ.get("AACP_STATE_DIR")
        self.addCleanup(self.restore, was)

    @staticmethod
    def restore(was):
        if was is None:
            os.environ.pop("AACP_STATE_DIR", None)
        else:
            os.environ["AACP_STATE_DIR"] = was

    def test_default_matches_what_was_hardcoded(self):
        os.environ.pop("AACP_STATE_DIR", None)
        self.assertEqual(paths.state("state.json"), "/var/lib/aacpanel/state.json")

    def test_env_moves_every_file(self):
        os.environ["AACP_STATE_DIR"] = "/srv/state/aacpanel"
        self.assertEqual(paths.state("asked.json"), "/srv/state/aacpanel/asked.json")
        self.assertEqual(paths.state("chat"), "/srv/state/aacpanel/chat")

    def test_nobody_keeps_own_copy_of_the_path(self):
        here = pathlib.Path(__file__).parent
        bad = []
        for path in sorted(here.glob("*.py")):
            if path.name == "paths.py" or path.name.startswith("test_"):
                continue
            tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
            for node in ast.walk(tree):
                if isinstance(node, ast.Constant) and isinstance(node.value, str):
                    if node.value.startswith(paths.DEFAULT_STATE_DIR):
                        bad.append("%s:%d %r" % (path.name, node.lineno, node.value))
        self.assertEqual(bad, [], "the state directory path is hardcoded around paths.state()")


if __name__ == "__main__":
    unittest.main()
