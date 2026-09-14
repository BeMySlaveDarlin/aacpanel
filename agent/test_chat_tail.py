"""What opening a feed costs: the window is folded from the end of the file."""
import json
import os
import unittest

import test_barrier

import chat


CWD = "/srv/proj"
AT = "2026-09-01T10:00:00Z"

# A record of a live transcript is mostly the output of a call: the file
# grows by tens of kilobytes while the feed gains a row or two.
FILLER = "f" * (48 * 1024)

# Two transcripts of the same shape, one ten times longer than the other.
SHORT = 100
LONG = 1000


def line(record):
    return json.dumps(record, ensure_ascii=False) + "\n"


def chunk(n, filler=FILLER):
    """Returns one turn of a conversation: a long answer and a short prompt."""
    return (line({"type": "assistant", "cwd": CWD, "timestamp": AT,
                  "message": {"content": [{"type": "text", "text": f"answer {n} {filler}"}]}})
            + line({"type": "user", "cwd": CWD, "timestamp": AT,
                    "message": {"content": f"prompt {n}"}}))


def write(path, numbers, filler=FILLER, mode="a"):
    """Adds the turns with these numbers to the end of a transcript."""
    with open(path, mode, encoding="utf-8") as f:
        f.write("".join(chunk(n, filler) for n in numbers))


def counting(numbers):
    """Returns the numbers of a conversation that ends at zero."""
    return range(numbers - 1, -1, -1)


def page(window):
    """Returns what the reader sees of a window: the rows and their text."""
    return [(item["role"], item.get("text", "")[:40]) for item in window["items"]]


def turn(n, filler=FILLER):
    """Returns the two rows one turn draws in the feed."""
    return [("ai", f"answer {n} {filler}"[:40]), ("me", f"prompt {n}")]


class Counted:
    """A file that tells how many bytes were read through it."""

    def __init__(self, f, tally):
        self.f = f
        self.tally = tally

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return self.f.__exit__(*exc)

    def __iter__(self):
        for raw in self.f:
            self.tally.append(len(raw))
            yield raw

    def read(self, n=-1):
        raw = self.f.read(n)
        self.tally.append(len(raw))
        return raw

    def readline(self):
        raw = self.f.readline()
        self.tally.append(len(raw))
        return raw

    def seek(self, *args):
        return self.f.seek(*args)


class Reads(unittest.TestCase):
    """Tests that count what the fold reads from the disk."""

    def setUp(self):
        chat.tail.PIECES.forget()
        self.tally = []
        chat.tail.open = lambda *args, **kw: Counted(open(*args, **kw), self.tally)
        self.addCleanup(delattr, chat.tail, "open")

    def bytes_read(self, path, **kw):
        """Returns how many bytes a window cost and the window itself."""
        del self.tally[:]
        window = chat.feed(path, **kw)
        return sum(self.tally), window


class Cost(Reads):
    """The first page of a feed costs the same however long the conversation is."""

    @classmethod
    def setUpClass(cls):
        cls.dir = test_barrier.tmp_dir(prefix="aacpanel-feed-cost-")
        cls.short = os.path.join(cls.dir.name, "short.jsonl")
        cls.long = os.path.join(cls.dir.name, "long.jsonl")
        write(cls.short, counting(SHORT))
        write(cls.long, counting(LONG))

    @classmethod
    def tearDownClass(cls):
        cls.dir.cleanup()

    taken = 0

    def own(self, source):
        """Returns the same transcript under a name no other read has opened."""
        Cost.taken += 1
        path = os.path.join(self.dir.name, f"own-{Cost.taken}.jsonl")
        os.link(source, path)
        self.addCleanup(os.unlink, path)
        return path

    def test_the_first_page_of_a_long_conversation_reads_what_a_short_one_reads(self):
        quick, short = self.bytes_read(self.own(self.short), limit=40)
        slow, long = self.bytes_read(self.own(self.long), limit=40)
        self.assertEqual(page(short), page(long),
                         "the two transcripts end alike and their first pages must match")
        self.assertLessEqual(abs(slow - quick), len(chunk(0)),
                             f"the longer transcript costs {slow} bytes against "
                             f"{quick} of the shorter one")
        self.assertLess(slow, os.path.getsize(self.long) // 4,
                        "the first page read a good part of the file")

    def test_opening_the_same_conversation_again_reads_nothing_of_it(self):
        path = self.own(self.long)
        self.bytes_read(path, limit=40)
        again, _ = self.bytes_read(path, limit=40)
        self.assertLessEqual(again, chat.tail.STAMP,
                             f"the second opening read {again} bytes of an unchanged file")

    def test_the_second_opening_parses_only_what_was_written_since(self):
        path = self.own(self.long)
        read = []
        whole = chat.tail.record_of

        def counted(raw):
            read.append(1)
            return whole(raw)

        chat.tail.record_of = counted
        self.addCleanup(setattr, chat.tail, "record_of", whole)
        chat.feed(path, limit=40)
        first = len(read)
        chat.feed(path, limit=40)
        self.assertGreater(first, 20, "the first opening parses the end of the file")
        self.assertLess(len(read) - first, 2,
                        "the second opening parses the file again")

    def test_the_end_of_the_file_gives_the_page_the_whole_file_gives(self):
        path = self.own(self.long)
        piece = chat.feed(path, limit=40)
        span = chat.tail.FIRST_SPAN
        chat.tail.FIRST_SPAN = os.path.getsize(path) * 2
        self.addCleanup(setattr, chat.tail, "FIRST_SPAN", span)
        whole = chat.feed(path, limit=40)
        self.assertEqual(piece["items"], whole["items"])
        self.assertEqual(piece["moreBefore"], whole["moreBefore"])
        self.assertEqual(piece["last"], whole["last"])

    def test_a_window_after_a_position_the_piece_has_let_go_is_read_from_there(self):
        path = self.own(self.long)
        first = chat.feed(path, limit=40)
        read, later = self.bytes_read(path, limit=8, after=0)
        self.assertEqual(page(later)[:3], turn(LONG - 1)[1:] + turn(LONG - 2),
                         "the window after the first record starts at the second")
        self.assertLess(read, os.path.getsize(path) // 4,
                        "a window after a position near the head read on to the end")
        self.assertEqual(page(chat.feed(path, limit=40)), page(first),
                         "the read spoiled the piece of the end")


class Sparse(Reads):
    """A conversation whose end is sparse needs a wide piece, and needs it read once."""

    def setUp(self):
        super().setUp()
        self.dir = test_barrier.tmp_dir(prefix="aacpanel-feed-sparse-")
        self.addCleanup(self.dir.cleanup)
        self.path = os.path.join(self.dir.name, "talk.jsonl")
        # One turn per 300 KB: the first span folds into far fewer rows than
        # a window of eight has to let go of.
        write(self.path, counting(40), filler="s" * (300 * 1024))

    def test_the_piece_grows_once_and_the_next_opening_reads_nothing(self):
        first, window = self.bytes_read(self.path, limit=8)
        self.assertGreater(first, chat.tail.FIRST_SPAN,
                           "the first span held rows enough, the fixture is not sparse")
        self.assertLess(first, os.path.getsize(self.path),
                        "the piece grew into the whole file, the fixture is too short")
        self.assertEqual(page(window)[-2:], turn(0, "s" * (300 * 1024)))
        again, _ = self.bytes_read(self.path, limit=8)
        self.assertLessEqual(again, chat.tail.STAMP,
                             f"the second opening read {again} bytes of an unchanged file")


class Growth(Reads):
    """A window opened again sees what the session has written since."""

    def setUp(self):
        super().setUp()
        self.dir = test_barrier.tmp_dir(prefix="aacpanel-feed-growth-")
        self.addCleanup(self.dir.cleanup)
        self.path = os.path.join(self.dir.name, "talk.jsonl")
        write(self.path, counting(SHORT))

    def test_the_window_sees_the_turns_written_after_the_last_opening(self):
        chat.feed(self.path, limit=8)
        write(self.path, (-1, -2))
        read, again = self.bytes_read(self.path, limit=8)
        self.assertEqual(page(again)[-4:], turn(-1) + turn(-2))
        self.assertLessEqual(read, 2 * len(chunk(-1)) + chat.tail.STAMP,
                             "the opening read more than what was written since the last")

    def test_a_window_after_a_position_sees_the_new_turns(self):
        first = chat.feed(self.path, limit=8)
        write(self.path, (-1,))
        after = chat.feed(self.path, limit=8, after=first["last"])
        self.assertEqual(page(after), turn(-1))

    def test_a_window_before_a_position_holds_the_earlier_turns(self):
        whole = chat.feed(self.path, limit=200)
        edge = whole["items"][-9]["pos"]
        before = chat.feed(self.path, limit=4, before=edge)
        self.assertEqual(page(before), page({"items": whole["items"][-13:-9]}))
        self.assertTrue(before["moreBefore"])

    def test_a_transcript_cut_short_is_read_anew(self):
        chat.feed(self.path, limit=8)
        write(self.path, range(50, 0, -1), mode="w")
        again = chat.feed(self.path, limit=8)
        self.assertEqual(page(again)[-2:], turn(1))

    def test_a_transcript_written_anew_under_the_same_name_is_read_anew(self):
        chat.feed(self.path, limit=8)
        other = "g" * len(FILLER)
        write(self.path, counting(SHORT), filler=other, mode="w")
        self.assertEqual(os.path.getsize(self.path), sum(len(chunk(n)) for n in counting(SHORT)),
                         "the file written anew is not the size of the old one, the check is moot")
        again = chat.feed(self.path, limit=8)
        self.assertEqual(page(again)[-2:], turn(0, other))

    def test_a_record_still_being_written_waits_for_its_end(self):
        chat.feed(self.path, limit=8)
        whole = chunk(-1)
        with open(self.path, "a", encoding="utf-8") as f:
            f.write(whole[:-10])
        half = chat.feed(self.path, limit=8)
        self.assertEqual(page(half)[-3:], turn(0) + turn(-1)[:1],
                         "the cut record is not the answer, and the answer is whole")
        with open(self.path, "a", encoding="utf-8") as f:
            f.write(whole[-10:])
        done = chat.feed(self.path, limit=8)
        self.assertEqual(page(done)[-2:], turn(-1))


if __name__ == "__main__":
    unittest.main()
