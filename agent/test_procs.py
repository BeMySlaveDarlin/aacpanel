import os
import pwd
import sys
import unittest
from unittest import mock

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import test_barrier  # noqa: E402,F401
import agent  # noqa: E402,F401
from collect import procs  # noqa: E402

HZ = procs.HZ
PAGE = procs.PAGE
GB = 8 * 1024**3


def stat_line(pid, comm, utime, stime, starttime, rss):
    tail = [0] * 22
    tail[0] = 0
    tail[11] = utime
    tail[12] = stime
    tail[19] = starttime
    tail[21] = rss
    return f"{pid} ({comm}) S " + " ".join(str(v) for v in tail[1:]) + "\n"


class ParseStat(unittest.TestCase):
    def test_a_name_with_spaces_does_not_shift_the_columns(self):
        raw = stat_line(42, "Isolated Web Co", utime=300, stime=100, starttime=7, rss=5)
        self.assertEqual(procs.parse_proc_stat(raw), ("Isolated Web Co", 400, 7, 5))

    def test_a_parenthesis_inside_a_name_is_cut_at_the_last_one(self):
        raw = stat_line(42, "kworker (u32:1)", utime=1, stime=2, starttime=9, rss=3)
        self.assertEqual(procs.parse_proc_stat(raw), ("kworker (u32:1)", 3, 9, 3))

    def test_a_truncated_line_does_not_break_the_walk(self):
        self.assertIsNone(procs.parse_proc_stat("42 (truncated) S 1 2 3\n"))
        self.assertIsNone(procs.parse_proc_stat("garbage with no parentheses at all\n"))


class Rank(unittest.TestCase):
    def rank(self, prev, cur, elapsed=10.0, uptime=1000.0, limit=procs.TOP):
        return {r["pid"]: r for r in procs.proc_rank(prev, cur, elapsed, uptime, GB, limit)}

    def test_the_delta_is_taken_from_the_previous_sample(self):
        prev = {7: ("worker", 100, 500, 0)}
        cur = {7: ("worker", 100 + HZ * 5, 500, 0)}
        self.assertEqual(self.rank(prev, cur)[7]["cpuPct"], 50.0)

    def test_a_process_that_took_over_a_pid_counts_as_new(self):
        prev = {7: ("old", HZ * 3600, 500, 0)}
        cur = {7: ("new", HZ * 2, HZ * 900, 0)}
        self.assertEqual(self.rank(prev, cur)[7]["cpuPct"], 2.0)

    def test_one_born_between_samples_is_counted_from_its_birth(self):
        cur = {9: ("build", HZ * 4, HZ * 995, 0)}
        self.assertEqual(self.rank({}, cur)[9]["cpuPct"], 80.0)

    def test_memory_is_counted_as_a_gauge_not_as_a_delta(self):
        cur = {3: ("memory", 0, 1, 4096)}
        row = self.rank({}, cur)[3]
        self.assertEqual(row["rss"], 4096 * PAGE)
        self.assertEqual(row["memPct"], round(4096 * PAGE / GB * 100, 1))

    def test_the_list_takes_the_top_by_cpu_and_by_memory_both(self):
        prev, cur = {}, {}
        for pid in range(1, 6):
            cur[pid] = (f"cpu{pid}", HZ * pid, HZ * 999, 0)
        for pid in range(11, 16):
            cur[pid] = (f"mem{pid}", 0, HZ * 999, 1000 * pid)
        got = self.rank(prev, cur, limit=2)
        self.assertEqual(sorted(got), [4, 5, 14, 15])

    def test_the_list_is_sorted_by_cpu(self):
        cur = {1: ("quiet", 0, HZ * 999, 900), 2: ("noisy", HZ, HZ * 999, 1)}
        rows = procs.proc_rank({}, cur, 10.0, 1000.0, GB)
        self.assertEqual([r["pid"] for r in rows], [2, 1])


class SampleAndDescribe(unittest.TestCase):
    def test_our_own_process_is_visible_in_the_sample(self):
        me = procs.proc_sample()[os.getpid()]
        with open(f"/proc/{os.getpid()}/comm", encoding="utf-8") as f:
            self.assertEqual(me[0], f.read().strip())
        self.assertGreater(me[3], 0, "the resident memory of our own process cannot be zero")

    def test_the_command_and_the_user_are_read_from_a_live_process(self):
        got = procs.proc_describe(os.getpid(), "python3")
        self.assertIn("python3", got["cmd"])
        self.assertEqual(got["user"], pwd.getpwuid(os.getuid()).pw_name)

    def test_a_process_of_the_host_names_no_container(self):
        self.assertNotIn("container", procs.proc_describe(os.getpid(), "python3"))

    def test_the_container_is_read_from_the_cgroup(self):
        ids = "4426af4f45ae09720a1e50f220d19db5b71cfe897480d860f0bc761d7f4be69d"
        for raw in (f"0::/system.slice/docker-{ids}.scope\n",
                    f"12:memory:/docker/{ids}\n11:cpu:/docker/{ids}\n"):
            with mock.patch.object(procs, "read", return_value=raw):
                self.assertEqual(procs.proc_container(1), ids, raw)
        with mock.patch.object(procs, "read", return_value="0::/user.slice/user-1000.slice\n"):
            self.assertEqual(procs.proc_container(1), "")

    def test_a_long_command_is_truncated(self):
        got = procs.proc_describe(os.getpid(), "python3")
        self.assertLessEqual(len(got["cmd"]), procs.CMD_MAX)

    def test_a_kernel_thread_gets_its_name_in_brackets_instead_of_a_command(self):
        got = procs.proc_describe(0, "kthreadd")
        self.assertEqual(got["cmd"], "[kthreadd]")

    def test_the_snapshot_block_names_the_total_count_too(self):
        prev = procs.proc_sample()
        block = procs.proc_top(prev, procs.proc_sample(), 1.0)
        self.assertGreaterEqual(block["total"], len(block["items"]))
        self.assertLessEqual(len(block["items"]), procs.TOP * 2)
        self.assertTrue(all(r["cmd"] for r in block["items"]), "a row without a command means nothing in the table")


if __name__ == "__main__":
    unittest.main()
