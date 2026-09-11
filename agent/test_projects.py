import importlib
import os
import shutil
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import contours  # noqa: E402
import projects  # noqa: E402


class Scan(unittest.TestCase):
    def setUp(self):
        self.tmp = test_barrier.tmp_path(prefix="projects-")
        self.addCleanup(shutil.rmtree, self.tmp, True)
        self.root = os.path.join(self.tmp, "proj")
        os.makedirs(self.root)
        self.home = os.path.join(self.tmp, ".claude")
        os.makedirs(os.path.join(self.home, "projects"))
        self.reg = os.path.join(self.tmp, "registry.conf")
        for mod, name in ((contours, "HOME"), (contours, "REGISTRY"),
                          (projects, "SCAN_ROOTS")):
            self.addCleanup(setattr, mod, name, getattr(mod, name))
        contours.HOME = self.home
        contours.REGISTRY = self.reg
        projects.SCAN_ROOTS = self.root

    def mk(self, *parts, file=None):
        path = os.path.join(self.root, *parts)
        os.makedirs(path, exist_ok=True)
        if file:
            with open(os.path.join(path, file), "w", encoding="utf-8") as f:
                f.write("")
        return path

    def kinds(self):
        return {d["path"]: d["kind"] for d in projects.scan()["dirs"]}

    def test_a_marker_on_disk_makes_a_directory_a_project(self):
        git = self.mk("Labs", "site", ".git")
        site = os.path.dirname(git)
        wt = self.mk("Beta", "wt", file=".git")
        mon = self.mk("Beta", "service", "aacpanel", file="CLAUDE.md")
        dot = os.path.dirname(self.mk("Beta", "dot", ".claude"))
        self.mk("Labs", "site", "sub", ".git")

        kinds = self.kinds()
        for path in (site, wt, mon, dot):
            self.assertEqual(kinds.get(path), "project", path)
        self.assertNotIn(os.path.join(site, "sub"), kinds)
        self.assertEqual(kinds.get(os.path.join(self.root, "Beta", "service")), "folder")

    def test_the_flags_say_what_the_directory_was_recognised_by(self):
        git = os.path.dirname(self.mk("Labs", "site", ".git"))
        site = self.mk("Demo", "site.example")
        os.makedirs(os.path.join(self.home, "projects", projects.slug(site)))

        rows = {d["path"]: d for d in projects.scan()["dirs"]}
        self.assertTrue(rows[git]["git"])
        self.assertFalse(rows[git]["claude"])
        self.assertEqual(rows[site]["kind"], "project",
                         "a transcripts directory is no worse a marker than .git: claude has already been run there")
        self.assertTrue(rows[site]["claude"])
        self.assertFalse(rows[site]["git"])

    def test_slug_repeats_the_rule_of_claude(self):
        self.assertEqual(projects.slug("/srv/proj/web-shop.example"), "-srv-proj-web-shop-example")
        self.assertEqual(projects.slug("/home/u/.cache"), "-home-u--cache")

    def test_hidden_directories_and_dependencies_are_skipped(self):
        self.mk("Beta", ".hidden", "x", ".git")
        self.mk("Beta", "node_modules", "dep", ".git")
        self.mk("Beta", "vendor", "lib", ".git")
        kinds = self.kinds()
        for name in (".hidden", "node_modules", "vendor"):
            self.assertFalse(any(f"/{name}/" in p or p.endswith(f"/{name}") for p in kinds),
                             f"{name} got into the walk: {sorted(kinds)}")

    def test_a_symlink_is_visible_but_not_followed(self):
        target = os.path.dirname(self.mk("Labs", "site", ".git"))
        link = os.path.join(self.root, "Link")
        os.symlink(target, link)
        kinds = self.kinds()
        self.assertEqual(kinds.get(link), "link")
        self.assertFalse(any(p.startswith(link + "/") for p in kinds))

    def test_the_depth_is_limited(self):
        self.mk("a", "b", "c", "d", "e", ".git")
        self.mk("a", "b", "c", "p", ".git")
        kinds = self.kinds()
        self.assertEqual(kinds.get(os.path.join(self.root, "a", "b", "c")), "folder")
        self.assertEqual(kinds.get(os.path.join(self.root, "a", "b", "c", "p")), "project")
        self.assertNotIn(os.path.join(self.root, "a", "b", "c", "d"), kinds)
        self.assertNotIn(os.path.join(self.root, "a", "b", "c", "d", "e"), kinds)

    def test_roots_from_the_environment_and_from_the_registry(self):
        other = os.path.join(self.tmp, "work")
        os.makedirs(os.path.join(other, "site", ".git"))
        gone = os.path.join(self.tmp, "gone")
        with open(self.reg, "w", encoding="utf-8") as f:
            f.write("# profile | prefix | config | token\n"
                    f"work | {other}/ | {self.home} | -\n"
                    f"old | {gone}/ | {self.home} | -\n"
                    f"personal | * | {self.home} | -\n")

        snap = projects.scan()
        self.assertEqual(snap["roots"], [self.root, other, gone],
                         "the registry prefixes come after their own roots, `*` is not a root")
        self.assertEqual({d["path"] for d in snap["dirs"]}, {os.path.join(other, "site")},
                         "a root that does not exist is skipped silently instead of breaking the walk")

    def test_a_root_inside_a_root_is_not_walked_twice(self):
        inner = os.path.join(self.root, "Labs")
        os.makedirs(os.path.join(inner, "site", ".git"))
        with open(self.reg, "w", encoding="utf-8") as f:
            f.write(f"work | {inner}/ | {self.home} | -\n")
        snap = projects.scan()
        self.assertEqual(snap["roots"], [self.root])
        paths = [d["path"] for d in snap["dirs"]]
        self.assertEqual(paths, sorted(set(paths)), "the directories arrived twice")

    def test_the_precedence_of_the_scan_roots(self):
        env = dict(os.environ)
        self.addCleanup(lambda: (os.environ.clear(), os.environ.update(env),
                                 importlib.reload(projects)))
        os.environ.pop("AACP_PROJECT_SCAN", None)
        os.environ.pop("AACP_PROJECT_ROOTS", None)
        self.assertEqual(importlib.reload(projects).SCAN_ROOTS,
                         os.path.expanduser("~"), "without the environment the walk goes over the home directory")

        os.environ["AACP_PROJECT_ROOTS"] = "/srv/proj:/srv/work"
        self.assertEqual(importlib.reload(projects).SCAN_ROOTS,
                         "/srv/proj:/srv/work", "the project roots beat the home directory")

        os.environ["AACP_PROJECT_SCAN"] = "/srv/scan"
        self.assertEqual(importlib.reload(projects).SCAN_ROOTS, "/srv/scan",
                         "its own variable beats the project roots: the walk and the "
                         "permission to open sessions are different questions")

        os.environ["AACP_PROJECT_SCAN"] = "   "
        self.assertEqual(importlib.reload(projects).SCAN_ROOTS, "/srv/proj:/srv/work",
                         "an empty value means 'no answer', not 'nothing to walk'")

    def test_the_home_directory_is_dropped_from_the_project_roots(self):
        env = dict(os.environ)
        self.addCleanup(lambda: (os.environ.clear(), os.environ.update(env),
                                 importlib.reload(projects)))
        os.environ.pop("AACP_PROJECT_SCAN", None)
        os.environ["HOME"] = "/home/u"

        os.environ["AACP_PROJECT_ROOTS"] = "/srv/proj:/home/u"
        self.assertEqual(importlib.reload(projects).SCAN_ROOTS, "/srv/proj",
                         "the home directory stayed among the scan roots — everything ever downloaded goes into the list")

        os.environ["AACP_PROJECT_ROOTS"] = "/home/u/"
        self.assertEqual(importlib.reload(projects).SCAN_ROOTS, "/home/u",
                         "there are no roots besides the home directory — an empty list "
                         "reads as 'I have no projects', and that is worse than a long one")

    def test_an_empty_variable_does_not_mean_walk_everything(self):
        projects.SCAN_ROOTS = ""
        self.assertEqual(projects.roots(), [])
        projects.SCAN_ROOTS = "/"
        self.assertEqual(projects.roots(), [], "the filesystem root is never a scan root")

    def test_the_snapshot_names_the_time_the_roots_and_the_depth(self):
        snap = projects.scan()
        self.assertGreater(snap["at"], 0)
        self.assertEqual(snap["roots"], [self.root])
        self.assertEqual(snap["depth"], projects.DEPTH)
        self.assertEqual(snap["dirs"], [])


if __name__ == "__main__":
    unittest.main()
