"""Search of a conversation: the words its feed shows, over the whole transcript.

The feed holds a window of a conversation, and the words a person looks for
may lie anywhere in it. So the search reads the transcript from its head,
record by record, through the same parser the feed is folded from, and keeps
no more of it than the matches it answers with: a match is a row the feed
draws, under the position the feed knows that row by, and the screen opens
the window around it by that position.

What is searched is what the feed shows as the conversation: the person's
messages, the model's answers, letters and the words of a few cards. A call,
what it was given and what it returned, thinking, and what the harness and
the hooks say are not: a search that found its words in the output of every
grep would bury the one place the person remembers.
"""
import heapq

from .records import MARKS
from .tail import Stream


MIN_QUESTION = 2
MAX_QUESTION = 200

# The most matches one answer carries. A question found more often than this
# keeps the newest: the screen walks the matches from the end of the
# conversation up, and the oldest are the ones it would reach last.
MAX_MATCHES = 300

# The length of a snippet, counted the way the screen counts a string: in
# UTF-16 units, so the place of the hit inside it is a place in that string.
SNIPPET = 160


def flat(text):
    """Returns the text with every run of whitespace folded into one space.

    A phrase the person types on one line may stand across a line break in
    the conversation, and the screen draws the break as a space anyway.
    """
    return " ".join(text.split())


def refusal(question):
    """Returns why a question is not searched for, or an empty string when it is."""
    if not isinstance(question, str):
        return "the question is not a string"
    size = len(question.strip())
    if size < MIN_QUESTION:
        return f"the question is too short: at least {MIN_QUESTION} characters are searched for"
    if size > MAX_QUESTION:
        return f"the question is too long: at most {MAX_QUESTION} characters are searched for"
    return ""


def said(item):
    """Returns what a row of the feed says as the conversation: the role of its match and its words.

    A letter from a hook is the harness speaking, and a card is searched by
    the words it shows as its name, not by what it holds. The task of a thread
    another drives is drawn as a letter, and found as one.
    """
    role = item.get("role")
    if role == "me":
        return "me", (item.get("text"),)
    if role == "ai":
        return "assistant", (item.get("text"),)
    if (role == "mail" and item.get("source") != "hook") or role == "task":
        return "letter", (item.get("text"),)
    if role in ("brief", "artifact"):
        return "card", (item.get("title"),)
    if role == "secret":
        return "card", (item.get("title"), item.get("name"))
    return None, ()


def spans(text, want):
    """Yields where a casefolded question stands in a text, as places in the text.

    Folding the case may turn one character into several — ß into ss — and
    a place in the folded text is then not a place in the text: each
    character of the folded text is traced back to the one it came from.
    Hits do not overlap, the way a find on a page walks them.
    """
    low = text.casefold()
    at = low.find(want)
    if at < 0:
        return
    back = None
    if len(low) != len(text):
        back = [i for i, ch in enumerate(text) for _ in ch.casefold()]
    last = 0
    while at >= 0:
        end = at + len(want)
        start, stop = (at, end) if back is None else (back[at], back[end - 1] + 1)
        if start >= last:
            yield start, stop
            last = stop
        at = low.find(want, end)


def width(ch):
    """Returns how many UTF-16 units a character takes."""
    return 2 if ord(ch) > 0xFFFF else 1


def units(text):
    """Returns the length of a text in UTF-16 units."""
    return len(text.encode("utf-16-le")) // 2


def snippet(text, start, stop):
    """Returns the text around a hit, at most SNIPPET units long, and the hit inside it.

    The hit is [start, length] in UTF-16 units of the snippet. The text
    around it is taken a character at a time on either side in turn, so the
    hit stands in the middle unless the text ends first. A hit longer than
    the snippet keeps its head.
    """
    room = SNIPPET
    end = start
    while end < stop and width(text[end]) <= room:
        room -= width(text[end])
        end += 1
    left, right = start, end
    grew = True
    while grew:
        grew = False
        if left > 0 and width(text[left - 1]) <= room:
            left -= 1
            room -= width(text[left])
            grew = True
        if right < len(text) and width(text[right]) <= room:
            room -= width(text[right])
            right += 1
            grew = True
    return text[left:right], [units(text[left:start]), units(text[start:end])]


class Found:
    """The matches of one search: how many there are in all and the newest of them.

    The newest are kept in a heap by place, so a search through a long
    conversation holds no more than the answer it gives. A row is known by
    its place, its role and its number among the rows of its role in its
    record, as the fold knows it. Counted holds the rows a later record may
    draw again in their place, with how many hits each gave; letters, every
    letter seen, the way the fold shows a letter that arrives twice once.
    """

    def __init__(self, want, limit):
        self.want = want
        self.limit = limit
        self.total = 0
        self.kept = []
        self.seq = 0
        self.counted = {}
        self.letters = set()

    def take(self, item, line_pos):
        """Counts the hits of one row of the feed, read from the record at line_pos.

        A row the queue drew comes again from a later record under the same
        position — delivered, taken back, or found to be an alarm or a
        command — and the fold puts it in the place of the first: the hits
        of the first go, and the hits of the row as it is now count.
        """
        pos = item.get("pos", line_pos)
        nth = item.get("nth", 0)
        key = (pos, item["role"], nth)
        again = pos != line_pos or bool(item.get("fixes"))
        if again:
            for gone in dict.fromkeys(((pos, item.get("fixes") or item["role"], nth), key)):
                hits = self.counted.pop(gone, 0)
                if hits:
                    self.drop(gone, hits)
                    break
        hits = self.count(item, key)
        if hits and (again or item.get("state") == "queued"):
            self.counted[key] = hits

    def drop(self, key, hits):
        """Takes back the hits of a row another one has taken the place of."""
        self.total -= hits
        kept = [entry for entry in self.kept if entry[2] != key]
        if len(kept) != len(self.kept):
            heapq.heapify(kept)
            self.kept = kept

    def count(self, item, key):
        """Counts the hits of a row and keeps them among the newest, returning how many there were."""
        if item["role"] == "mail":
            letter = hash((item.get("from"), item.get("text"), item.get("use")))
            if letter in self.letters:
                return 0
            self.letters.add(letter)
        role, texts = said(item)
        if role is None:
            return 0
        hits = 0
        for text in texts:
            if not isinstance(text, str) or not text:
                continue
            text = flat(text)
            for start, stop in spans(text, self.want):
                hits += 1
                self.seq += 1
                entry = (key[0], self.seq, key, role, item.get("at") or "", text, start, stop)
                if len(self.kept) < self.limit:
                    heapq.heappush(self.kept, entry)
                else:
                    heapq.heappushpop(self.kept, entry)
        self.total += hits
        return hits

    def answer(self):
        matches = []
        for pos, _, key, role, at, text, start, stop in sorted(self.kept):
            piece, hit = snippet(text, start, stop)
            match = {"pos": pos, "at": at, "role": role, "snippet": piece, "hit": hit}
            if key[2]:
                match["nth"] = key[2]
            matches.append(match)
        return {"matches": matches, "total": self.total, "cut": self.total > len(matches)}


def search(path, question, limit=MAX_MATCHES, sidechain=False):
    """Returns the places in a transcript that say the question, oldest first.

    The question is taken as refusal() lets it through. A match names its
    row as the feed does: by its place, and among several rows of one role
    in one record by their number, nth, absent for the first.
    """
    limit = max(1, min(int(limit or MAX_MATCHES), MAX_MATCHES))
    found = Found(flat(question).casefold(), limit)
    for line_pos, items in Stream(path, 0, sidechain):
        for item in items:
            if item.get("role") not in MARKS:
                found.take(item, line_pos)
    return found.answer()
