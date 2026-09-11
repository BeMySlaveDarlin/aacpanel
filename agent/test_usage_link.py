import json
import os
import socket
import sys
import tempfile
import threading
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import contours  # noqa: E402
import usage_link  # noqa: E402


def fake_scan_list(pairs):
    out = []
    for contour, root in pairs:
        for base, _, names in os.walk(root):
            for name in sorted(names):
                if not name.endswith(".jsonl"):
                    continue
                path = os.path.join(base, name)
                st = os.stat(path)
                out.append({"path": path, "contour": contour,
                            "inode": st.st_ino, "size": st.st_size})
    return out


class Boundary(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.addCleanup(self.dir.cleanup)
        self.was_home = contours.HOME
        contours.HOME = os.path.join(self.dir.name, "home")
        os.environ["AACP_CLAUDE_HOME"] = contours.HOME
        os.environ["AACP_CLAUDE_REGISTRY"] = os.path.join(self.dir.name, "missing.conf")
        contours.REGISTRY = os.environ["AACP_CLAUDE_REGISTRY"]
        self.addCleanup(self.restore)

        self.root = os.path.join(contours.HOME, "projects", "-p")
        os.makedirs(self.root)
        self.talk = os.path.join(self.root, "talk.jsonl")
        with open(self.talk, "w", encoding="utf-8") as f:
            f.write("{}\n")
        usage_link.reset_roots()

    def restore(self):
        contours.HOME = self.was_home
        os.environ.pop("AACP_CLAUDE_HOME", None)
        os.environ.pop("AACP_CLAUDE_REGISTRY", None)
        usage_link.OVERRIDE.clear()
        usage_link.reset_roots()

    def test_a_path_outside_the_roots_is_not_read(self):
        outside = os.path.join(self.dir.name, "secret.jsonl")
        with open(outside, "w", encoding="utf-8") as f:
            f.write("{}\n")

        reply = usage_link.parse_one({"path": outside, "offset": 0})
        self.assertIn("error", reply)
        self.assertNotIn("rows", reply)

    def test_a_symlink_from_the_root_does_not_lead_out(self):
        outside = os.path.join(self.dir.name, "secret.jsonl")
        with open(outside, "w", encoding="utf-8") as f:
            f.write("{}\n")
        link = os.path.join(self.root, "link.jsonl")
        os.symlink(outside, link)

        self.assertIn("error", usage_link.parse_one({"path": link, "offset": 0}))

    def test_walking_up_the_tree_does_not_work(self):
        path = os.path.join(self.root, "..", "..", "..", "secret.jsonl")
        self.assertIn("error", usage_link.parse_one({"path": path, "offset": 0}))

    def test_a_file_that_is_not_jsonl_is_not_read(self):
        other = os.path.join(self.root, "note.txt")
        with open(other, "w", encoding="utf-8") as f:
            f.write("hello\n")
        self.assertIn("error", usage_link.parse_one({"path": other, "offset": 0}))

    def test_the_rewind_flag_reaches_the_caller(self):
        class Chunk(tuple):
            rewind = True

        usage_link.OVERRIDE["parse_file"] = lambda path, offset, contour="": Chunk(
            ([], [], [], {"sessionId": "s"}, 7))

        self.assertTrue(usage_link.parse_one({"path": self.talk, "offset": 0})["rewind"])

    def test_a_path_inside_the_roots_is_read(self):
        usage_link.OVERRIDE["parse_file"] = lambda path, offset, contour="": (
            [], [], [], {"sessionId": "s", "contour": "invented"}, 7)

        reply = usage_link.parse_one({"path": self.talk, "offset": 0})
        self.assertNotIn("error", reply)
        self.assertEqual(reply["offset"], 7)
        self.assertEqual(reply["session"]["contour"], "home")


class WhoseRows(unittest.TestCase):
    def test_the_root_file_keeps_itself_and_the_sidechain(self):
        self.assertEqual(usage_link.agents_of("/p/talk.jsonl", [], []),
                         ["", "sidechain"])

    def test_a_subagent_file_keeps_only_its_own(self):
        path = os.path.join("/p", "uuid", "subagents", "agent-1.jsonl")
        self.assertEqual(
            usage_link.agents_of(path, [{"agent": "agent-01J"}], [{"agent": "agent-01J"}]),
            ["agent-01J"])

    def test_an_empty_subagent_file_wipes_nothing(self):
        path = os.path.join("/p", "uuid", "subagents", "agent-1.jsonl")
        self.assertEqual(usage_link.agents_of(path, [], []), [])


class Stream(unittest.TestCase):
    def setUp(self):
        self.dir = test_barrier.tmp_dir()
        self.addCleanup(self.dir.cleanup)
        self.was = usage_link.SOCKET_DIR
        usage_link.SOCKET_DIR = os.path.join(self.dir.name, "run")
        self.addCleanup(self.restore)

    def restore(self):
        usage_link.SOCKET_DIR = self.was
        usage_link.OVERRIDE.clear()

    def ask(self, request):
        sock = usage_link.listen()
        self.addCleanup(sock.close)
        threading.Thread(target=lambda: usage_link.handle(sock.accept()[0]),
                         daemon=True).start()

        client = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        client.settimeout(5)
        client.connect(usage_link.socket_path())
        client.sendall(json.dumps(request).encode("utf-8"))
        client.shutdown(socket.SHUT_WR)
        chunks = []
        while True:
            part = client.recv(65536)
            if not part:
                break
            chunks.append(part)
        client.close()
        return [json.loads(x) for x in b"".join(chunks).decode("utf-8").splitlines()]

    def test_the_end_of_the_stream_is_named(self):
        reply = self.ask({"op": "ping"})
        self.assertTrue(reply[-1].get("end"))

    def test_an_unknown_request_explains_why(self):
        reply = self.ask({"op": "dance"})
        self.assertIn("error", reply[0])
        self.assertTrue(reply[-1].get("end"))

    def test_a_job_without_a_list_is_rejected(self):
        reply = self.ask({"op": "parse"})
        self.assertIn("error", reply[0])

    def test_garbage_instead_of_a_request_does_not_crash_the_agent(self):
        sock = usage_link.listen()
        self.addCleanup(sock.close)
        threading.Thread(target=lambda: usage_link.handle(sock.accept()[0]),
                         daemon=True).start()
        client = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        client.settimeout(5)
        client.connect(usage_link.socket_path())
        client.sendall("this is not json".encode("utf-8"))
        client.shutdown(socket.SHUT_WR)
        lines = client.recv(65536).decode("utf-8").splitlines()
        client.close()
        self.assertIn("error", json.loads(lines[0]))
        self.assertTrue(json.loads(lines[-1]).get("end"))


class Parsing(unittest.TestCase):
    def setUp(self):
        self.addCleanup(usage_link.OVERRIDE.clear)

    def test_the_order_of_replies_repeats_the_order_of_the_job(self):
        usage_link.OVERRIDE["parse_file"] = lambda path, offset, contour="": (
            [], [], [], {"sessionId": path}, 0)
        job = [{"path": f"/p/{i}.jsonl", "offset": 0} for i in range(6)]

        was = usage_link.POOL_FROM
        usage_link.POOL_FROM = 1000
        self.addCleanup(setattr, usage_link, "POOL_FROM", was)

        replies = [r["path"] for r in usage_link.parse_all(job)]
        self.assertEqual(replies, [t["path"] for t in job])

    def test_the_worker_count_is_taken_from_the_environment(self):
        os.environ[usage_link.WORKERS_ENV] = "8"
        self.addCleanup(os.environ.pop, usage_link.WORKERS_ENV, None)
        self.assertEqual(usage_link.workers(), 8)

    def test_garbage_in_the_worker_count_does_not_break_the_collection(self):
        os.environ[usage_link.WORKERS_ENV] = "eight"
        self.addCleanup(os.environ.pop, usage_link.WORKERS_ENV, None)
        self.assertEqual(usage_link.workers(), usage_link.WORKERS_DEFAULT)

    def test_the_pool_parse_does_not_go_to_the_system_temp_directory(self):
        import paths

        job = [{"path": f"/p/{i}.jsonl", "offset_bytes": 0} for i in range(2)]
        was = usage_link.POOL_FROM
        usage_link.POOL_FROM = 1
        self.addCleanup(setattr, usage_link, "POOL_FROM", was)

        broken = os.path.join(paths.state_dir(), "no-such-directory")
        was_dir, was_env = tempfile.tempdir, os.environ.get("TMPDIR")
        tempfile.tempdir, os.environ["TMPDIR"] = broken, broken

        def restore():
            tempfile.tempdir = was_dir
            if was_env is None:
                os.environ.pop("TMPDIR", None)
            else:
                os.environ["TMPDIR"] = was_env
        self.addCleanup(restore)

        replies = list(usage_link.parse_all(job))
        self.assertEqual(len(replies), len(job))
        self.assertTrue(
            tempfile.gettempdir().startswith(paths.state_dir()),
            "the parse temp directory is outside the state directory: %s" % tempfile.gettempdir())

    def test_the_pool_parse_runs_the_way_it_runs_in_production(self):
        job = [{"path": f"/p/{i}.jsonl", "offset_bytes": 0} for i in range(2)]
        was = usage_link.POOL_FROM
        usage_link.POOL_FROM = 1
        self.addCleanup(setattr, usage_link, "POOL_FROM", was)

        was_dir, was_env = tempfile.tempdir, os.environ.get("TMPDIR")

        def restore():
            tempfile.tempdir = was_dir
            if was_env is None:
                os.environ.pop("TMPDIR", None)
            else:
                os.environ["TMPDIR"] = was_env
        self.addCleanup(restore)

        replies = [r["path"] for r in usage_link.parse_all(job)]
        self.assertEqual(replies, [t["path"] for t in job])


if __name__ == "__main__":
    unittest.main()
