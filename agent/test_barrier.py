import ast
import os
import pathlib
import shutil
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import paths  # noqa: E402

ADVICE = "run the agent tests through `make agent-test`"

TMPDIR_VAR = "AACP_TEST_TMPDIR"

CONFINED_VAR = "AACP_TEST_CONFINED"


def tmp_root():
    return os.environ.get(TMPDIR_VAR) or None


def tmp_dir(prefix=None):
    return tempfile.TemporaryDirectory(prefix=prefix, dir=tmp_root())


def tmp_path(prefix=None):
    return tempfile.mkdtemp(prefix=prefix, dir=tmp_root())


def tmp_file(suffix=None, prefix=None):
    return tempfile.NamedTemporaryFile("w", suffix=suffix, prefix=prefix,
                                       delete=False, encoding="utf-8",
                                       dir=tmp_root())


def inside(path, directory):
    path = os.path.normpath(path)
    directory = os.path.normpath(directory)
    return path == directory or path.startswith(directory + os.sep)


def reason(environment):
    directory = environment.get("AACP_STATE_DIR", paths.DEFAULT_STATE_DIR)

    if directory == paths.DEFAULT_STATE_DIR:
        return ("AACP_STATE_DIR is not set, and the run looks into the live "
                "state directory %s" % paths.DEFAULT_STATE_DIR)

    if os.path.isdir(directory) and os.listdir(directory):
        return ("the state directory %s is not empty: the run has its own, "
                "empty one, and a non-empty one is someone's live one" % directory)

    store = environment.get("AACP_ASK_STORE")
    if store and not inside(store, directory):
        return ("AACP_ASK_STORE points outside the state directory of the "
                "run: %s" % store)

    return None


_reason = reason(os.environ)
if _reason:
    sys.stderr.write("\n!! the agent tests are stopped at import: %s.\n   %s\n\n"
                     % (_reason, ADVICE))
    sys.stderr.flush()
    os._exit(1)


class Reason(unittest.TestCase):
    def setUp(self):
        self.directory = tmp_path(prefix="aacpanel-barrier-")
        self.addCleanup(shutil.rmtree, self.directory, True)

    def test_without_a_substituted_directory_the_run_does_not_start(self):
        said = reason({})
        self.assertIsNotNone(said)
        self.assertIn(paths.DEFAULT_STATE_DIR, said)

    def test_a_substituted_empty_directory_lets_the_run_through(self):
        self.assertIsNone(reason({"AACP_STATE_DIR": self.directory}))

    def test_a_non_empty_state_directory_counts_as_live(self):
        with open(os.path.join(self.directory, "asked.json"), "w") as f:
            f.write("{}")
        said = reason({"AACP_STATE_DIR": self.directory})
        self.assertIsNotNone(said)
        self.assertIn(self.directory, said)

    def test_the_question_store_from_the_environment_does_not_survive_the_substitution(self):
        said = reason({"AACP_STATE_DIR": self.directory,
                       "AACP_ASK_STORE": os.path.join(
                           paths.DEFAULT_STATE_DIR, "asked.json")})
        self.assertIsNotNone(said)
        self.assertIn("AACP_ASK_STORE", said)

    def test_a_store_inside_the_substituted_directory_is_allowed(self):
        self.assertIsNone(reason({
            "AACP_STATE_DIR": self.directory,
            "AACP_ASK_STORE": os.path.join(self.directory, "asked.json")}))

    def test_a_directory_next_to_the_substituted_one_does_not_excuse_the_store(self):
        said = reason({"AACP_STATE_DIR": self.directory,
                       "AACP_ASK_STORE": self.directory + "-x/asked.json"})
        self.assertIsNotNone(said)


class Confinement(unittest.TestCase):
    def setUp(self):
        if not os.environ.get(CONFINED_VAR):
            self.skipTest("an ordinary run; the confinement of the unit is "
                          "checked by `make agent-confined`")

    def test_writing_outside_the_directories_of_the_run_is_impossible(self):
        places = ["/tmp", "/var/tmp", os.path.expanduser("~")]
        writable = []
        for place in places:
            probe = os.path.join(place, "aacpanel-confined-probe")
            try:
                with open(probe, "w"):
                    pass
            except OSError:
                continue
            os.unlink(probe)
            writable.append(place)
        self.assertEqual(writable, [],
                         "the run calls itself confined but writes here like "
                         "an ordinary one: the confinement of the unit never "
                         "reached it")


class Coverage(unittest.TestCase):
    PICK_THE_PLACE = ("TemporaryDirectory", "NamedTemporaryFile",
                      "mkdtemp", "mkstemp")

    def test_the_fixtures_name_the_place_of_the_temporary_files(self):
        here = pathlib.Path(__file__).parent
        unnamed = []
        for path in sorted(here.rglob("test_*.py")):
            tree = ast.parse(path.read_text(encoding="utf-8"),
                             filename=str(path))
            for node in ast.walk(tree):
                if not isinstance(node, ast.Call):
                    continue
                name = (node.func.attr if isinstance(node.func, ast.Attribute)
                        else getattr(node.func, "id", ""))
                if name not in self.PICK_THE_PLACE:
                    continue
                if any(kw.arg == "dir" for kw in node.keywords):
                    continue
                unnamed.append("%s:%d — %s"
                               % (path.relative_to(here), node.lineno, name))
        self.assertEqual(unnamed, [],
                         "these calls take the temporary directory from the "
                         "system: call `test_barrier.tmp_dir()`, `tmp_path()` "
                         "or `tmp_file()`, or name `dir=` yourself")

    def test_every_file_of_the_agent_tests_imports_the_barrier(self):
        here = pathlib.Path(__file__).parent
        name = pathlib.Path(__file__).stem
        without_barrier = []
        for path in sorted(here.rglob("test_*.py")):
            if path.stem == name:
                continue
            tree = ast.parse(path.read_text(encoding="utf-8"),
                             filename=str(path))
            found = any(
                isinstance(node, ast.Import)
                and any(alias.name == name for alias in node.names)
                for node in ast.walk(tree))
            if not found:
                without_barrier.append(str(path.relative_to(here)))
        self.assertEqual(without_barrier, [],
                         "these test files do not import the barrier against "
                         "the live state: `import %s`" % name)


if __name__ == "__main__":
    unittest.main()
