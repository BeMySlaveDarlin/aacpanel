import os
import subprocess
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
from chat import repo  # noqa: E402


def git(cwd, *args):
    done = subprocess.run(("git", "-C", cwd, *args), capture_output=True)
    if done.returncode != 0:
        raise AssertionError(f"git {args}: {done.stderr.decode()}")
    return done.stdout.decode()


def write(cwd, path, text):
    full = os.path.join(cwd, path)
    os.makedirs(os.path.dirname(full), exist_ok=True)
    with open(full, "w") as f:
        f.write(text)
    return full


class Repo(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_path(prefix="repo")
        self.addCleanup(subprocess.run, ("rm", "-rf", self.dir))
        git(self.dir, "init", "-q", "-b", "main")
        git(self.dir, "config", "user.email", "t@example.org")
        git(self.dir, "config", "user.name", "Test")
        write(self.dir, "keep.txt", "one\ntwo\nthree\n")
        write(self.dir, "pkg/mod.go", "package pkg\n")
        git(self.dir, "add", "-A")
        git(self.dir, "commit", "-qm", "first")


class Changes(Repo):
    def test_one_list_holds_the_commits_and_the_working_tree(self):
        # The tree on the screen and the run of diffs under it are the same set
        # of files. Counted twice they come apart, and the file a person taps is
        # not the file they were shown.
        git(self.dir, "checkout", "-qb", "work")
        write(self.dir, "keep.txt", "one\ntwo\nthree\nfour\n")
        git(self.dir, "commit", "-qam", "in a commit")
        write(self.dir, "pkg/mod.go", "package pkg\n\nfunc New() {}\n")
        write(self.dir, "fresh.md", "not tracked at all\n")

        out = repo.changes(self.dir, "main")
        by = {f["path"]: f for f in out["files"]}

        self.assertEqual(out["base"], "main")
        self.assertEqual(out["baseFrom"], "project")
        self.assertEqual(sorted(by), ["fresh.md", "keep.txt", "pkg/mod.go"])
        self.assertEqual(by["keep.txt"]["layer"], "committed",
                         "a file changed only by a commit is marked as not yet committed")
        self.assertEqual(by["pkg/mod.go"]["layer"], "worktree")
        self.assertEqual(by["fresh.md"]["layer"], "worktree")
        self.assertEqual(by["fresh.md"]["status"], "A",
                         "a file git does not know about yet is the most interesting thing in a review")

    def test_a_file_changed_again_after_its_commit_is_not_yet_committed(self):
        # Two layers meet on one file, and the mark has to say what is still
        # unanswered for rather than what happened first.
        git(self.dir, "checkout", "-qb", "work")
        write(self.dir, "keep.txt", "one\ntwo\nthree\nfour\n")
        git(self.dir, "commit", "-qam", "in a commit")
        write(self.dir, "keep.txt", "one\ntwo\nthree\nfour\nfive\n")

        by = {f["path"]: f for f in repo.changes(self.dir, "main")["files"]}
        self.assertEqual(by["keep.txt"]["layer"], "worktree")


class Base(Repo):
    def test_the_project_setting_wins_over_everything_git_knows(self):
        git(self.dir, "checkout", "-qb", "work")
        base, how = repo.base_of(self.dir, "work", "release/2026")
        self.assertEqual((base, how), ("release/2026", "project"))

    def test_without_a_setting_the_branch_says_where_it_points(self):
        git(self.dir, "checkout", "-qb", "work")
        git(self.dir, "branch", "--set-upstream-to=main", "work")
        base, how = repo.base_of(self.dir, "work")
        self.assertEqual((base, how), ("main", "upstream"))

    def test_the_reflog_remembers_what_the_branch_was_created_from(self):
        # Git keeps no record of where a branch came from; this line of the
        # reflog is the closest there is, until it is trimmed.
        git(self.dir, "checkout", "-qb", "feature")
        base, how = repo.base_of(self.dir, "feature")
        self.assertEqual(how, "reflog", f"the base came from {how} instead of the reflog")
        self.assertEqual(base, "main")

    def test_the_trunk_is_the_last_resort_and_says_so(self):
        # A reflog is trimmed by git itself in time, and then there is nothing
        # left to read the parent out of: both journals go, as they would after
        # ninety days.
        git(self.dir, "checkout", "-qb", "orphan")
        for log in (os.path.join(self.dir, ".git", "logs", "refs", "heads", "orphan"),
                    os.path.join(self.dir, ".git", "logs", "HEAD")):
            if os.path.exists(log):
                os.remove(log)
        base, how = repo.base_of(self.dir, "orphan")
        self.assertEqual((base, how), ("main", "trunk"))


class Windows(Repo):
    def test_a_window_asked_for_under_an_old_revision_is_refused(self):
        # A repository under an agent that keeps writing moves between two
        # requests. A window answered from the new state while the list came
        # from the old one is a diff nobody can trust.
        first = repo.changes(self.dir)["rev"]
        window = repo.blob(self.dir, "keep.txt", rev=first)
        self.assertEqual(window["lines"], ["one", "two", "three"])

        write(self.dir, "keep.txt", "one\ntwo\nthree\nfour\n")
        git(self.dir, "add", "-A")
        second = repo.changes(self.dir)["rev"]
        self.assertNotEqual(first, second, "the revision did not move when a tracked file did")

        stale = repo.blob(self.dir, "keep.txt", rev=first)
        self.assertTrue(stale.get("stale"), "a window from an old revision was answered as if nothing moved")
        self.assertEqual(stale["rev"], second, "the refusal does not say which revision to ask again under")

    def test_a_window_is_counted_in_lines(self):
        write(self.dir, "long.txt", "".join(f"line {i}\n" for i in range(1, 101)))
        out = repo.blob(self.dir, "long.txt", first=10, lines=5)
        self.assertEqual(out["lines"], ["line 10", "line 11", "line 12", "line 13", "line 14"])
        self.assertEqual(out["total"], 100)
        self.assertTrue(out["more"])

    def test_a_path_outside_the_working_tree_is_refused(self):
        with self.assertRaises(repo.RepoError):
            repo.blob(self.dir, "../../etc/passwd")
        with self.assertRaises(repo.RepoError):
            repo.blob(self.dir, "/etc/passwd")

    def test_a_binary_file_is_named_rather_than_sent(self):
        with open(os.path.join(self.dir, "bin.dat"), "wb") as f:
            f.write(b"\x7fELF\x00\x00\x00\x00" + b"x" * 200)
        out = repo.blob(self.dir, "bin.dat")
        self.assertTrue(out.get("binary"))
        self.assertNotIn("lines", out)


class Tree(Repo):
    def test_a_directory_shows_what_git_does_not_know_about(self):
        write(self.dir, "pkg/new.go", "package pkg\n")
        names = {e["name"]: e for e in repo.tree(self.dir, "pkg")["entries"]}
        self.assertIn("mod.go", names)
        self.assertIn("new.go", names)
        self.assertTrue(names["new.go"].get("untracked"))

    def test_the_git_directory_is_not_part_of_the_tree(self):
        names = [e["name"] for e in repo.tree(self.dir)["entries"]]
        self.assertNotIn(".git", names)


class Commits(Repo):
    def test_a_commit_names_the_conversation_it_was_written_in(self):
        # The tie is not a guess: a session leaves its own name in a trailer,
        # and that name is the address of the conversation.
        write(self.dir, "keep.txt", "one\ntwo\nthree\nfour\n")
        git(self.dir, "commit", "-qam",
            "a change\n\nClaude-Session: https://claude.ai/code/session_01abc")
        sha = git(self.dir, "rev-parse", "HEAD").strip()

        out = repo.commit(self.dir, sha)
        self.assertEqual(out["session"], "https://claude.ai/code/session_01abc")
        self.assertEqual(out["subject"], "a change")

    def test_a_commit_written_by_hand_carries_no_conversation(self):
        # Most commits have no trailer, and inventing one would tie a line of
        # code to a conversation that never touched it.
        write(self.dir, "keep.txt", "one\ntwo\n")
        git(self.dir, "commit", "-qam", "by hand")
        out = repo.commit(self.dir, git(self.dir, "rev-parse", "HEAD").strip())
        self.assertEqual(out["session"], "")

    def test_a_name_that_is_not_a_commit_is_turned_away(self):
        with self.assertRaises(repo.RepoError):
            repo.commit(self.dir, "HEAD; rm -rf /")


class Dispatch(Repo):
    def test_every_reply_says_which_operation_it_answers(self):
        out = repo.answer({"op": "refs", "cwd": self.dir})
        self.assertTrue(out["ok"])
        self.assertEqual(out["op"], "refs")
        self.assertEqual(out["repo"]["branch"], "main")

    def test_a_directory_that_is_not_a_repository_is_refused_with_a_reason(self):
        plain = test_barrier.tmp_path(prefix="plain")
        self.addCleanup(subprocess.run, ("rm", "-rf", plain))
        out = repo.answer({"op": "changes", "cwd": plain})
        self.assertFalse(out["ok"])
        self.assertTrue(out["error"], "the refusal is empty, so the screen has nothing to show")

    def test_an_unknown_operation_names_itself(self):
        out = repo.answer({"op": "rm", "cwd": self.dir})
        self.assertFalse(out["ok"])
        self.assertIn("rm", out["error"])


if __name__ == "__main__":
    unittest.main()
