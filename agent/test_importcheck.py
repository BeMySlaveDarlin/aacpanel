import contextlib
import io
import os
import pathlib
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import importcheck  # noqa: E402
import test_barrier  # noqa: E402


class ImportCheck(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_dir(prefix="importcheck-")
        self.here = pathlib.Path(self.dir.name)
        self.addCleanup(self.dir.cleanup)
        # The modules of a test directory must not outlive it in the cache.
        self.addCleanup(lambda: [sys.modules.pop(n, None) for n in ("fine_mod", "broken_mod", "dash_mod")])

    def write(self, name, body):
        (self.here / name).write_text(body, encoding="utf-8")

    def test_a_module_with_a_name_undefined_at_its_top_is_named_with_its_line(self):
        self.write("fine_mod.py", "X = 1\n")
        self.write("broken_mod.py", "import os\n\nY = undefined_name\n")
        self.write("test_skipped.py", "raise SystemExit('tests are not the collector')\n")
        bad = importcheck.failures(self.here)
        self.assertEqual(len(bad), 1, bad)
        self.assertIn("broken_mod.py: NameError", bad[0])
        self.assertIn("(broken_mod.py:3)", bad[0])

    def test_a_module_with_a_dash_is_loaded_from_its_file(self):
        self.write("dash-mod.py", "raise RuntimeError('at import')\n")
        bad = importcheck.failures(self.here)
        self.assertEqual(len(bad), 1, bad)
        self.assertTrue(bad[0].startswith("dash-mod.py: RuntimeError: at import"), bad[0])

    def test_the_exit_status_and_the_lines_say_it(self):
        self.write("broken_mod.py", "raise SystemExit(3)\n")
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            status = importcheck.main(["importcheck.py", str(self.here)])
        self.assertEqual(status, 1)
        self.assertIn("the collector will not come up", out.getvalue())
        self.assertIn("broken_mod.py: SystemExit", out.getvalue())

    def test_a_clean_directory_passes(self):
        self.write("fine_mod.py", "X = 1\n")
        self.assertEqual(importcheck.main(["importcheck.py", str(self.here)]), 0)


if __name__ == "__main__":
    unittest.main()
