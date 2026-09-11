import os
import unittest

import test_barrier  # noqa: F401

AGENT_DIR = os.path.dirname(os.path.abspath(__file__))
OWNER = "contours.py"


class PersonalNameHasOneOwner(unittest.TestCase):
    def test_no_literal_outside_contours(self):
        found, seen = [], False
        for root, dirs, files in os.walk(AGENT_DIR):
            dirs[:] = [d for d in dirs if d != "__pycache__"]
            for name in sorted(files):
                if not name.endswith(".py") or name.startswith("test_"):
                    continue
                path = os.path.join(root, name)
                rel = os.path.relpath(path, AGENT_DIR)
                with open(path, encoding="utf-8") as f:
                    body = f.read()
                if '"personal"' not in body and "'personal'" not in body:
                    continue
                if rel == OWNER:
                    seen = True
                    continue
                found.append(rel)

        self.assertTrue(seen, f"{OWNER} does not name the literal — the constant has moved, "
                              "and the test guards an empty spot")
        self.assertEqual(found, [], "the name of the personal contour is decided by contours.PERSONAL: "
                                    "a literal of its own in these files drifts away from it silently")


if __name__ == "__main__":
    unittest.main()
