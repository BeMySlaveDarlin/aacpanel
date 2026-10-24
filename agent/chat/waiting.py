"""An index of actionable cards, without keeping the rest of a conversation."""
import os
import re
import threading
from collections import OrderedDict

from .tail import Piece
from .limits import MAX_TEXT


# The same parser finds the card and its position for both the feed and the
# queue. Only new records are read after the first listing of a conversation.
_lock = threading.Lock()
_indexes = OrderedDict()
MAX_INDEXES = 64


def question_in(text):
    """Code and link destinations do not ask the person anything."""
    prose, fence = [], None
    for line in text.splitlines():
        marker = re.match(r"^ {0,3}(`{3,}|~{3,})", line)
        if fence:
            if marker and marker[1][0] == fence[0] and len(marker[1]) >= len(fence):
                fence = None
        elif marker:
            fence = marker[1]
        elif not line.startswith(("    ", "\t")):
            prose.append(line)
    text = "\n".join(prose)
    text = re.sub(r"(?<!`)(`+)(?!`).*?(?<!`)\1(?!`)", "", text, flags=re.S)
    text = re.sub(r"\[([^\]]*)\]\([^\s)]*(?:\s+\"[^\"]*\")?\)", r"\1", text)
    text = re.sub(r"(?m)^ {0,3}\[[^\]]+\]:\s*\S+.*$", "", text)
    text = re.sub(r"\b(?:[a-zA-Z][a-zA-Z0-9+.-]*://|www\.)[^\s<>]+", "", text)
    return "?" in text


class Index:
    def __init__(self, path):
        self.piece = Piece(path, 0, 0)
        self.items = {}
        self.question = None

    def inspect(self, record, items):
        # The feed already distinguishes a person's input from tool results,
        # letters and harness inserts. Those must not settle a question.
        if any(item.get("role") == "me" for item in items):
            self.question = None
        if record.get("type") != "assistant" or record.get("isSidechain") or record.get("isMeta"):
            return
        self.question = None
        message = record.get("message") or {}
        text = "\n\n".join(b.get("text", "") for b in message.get("content", [])
                           if isinstance(b, dict) and b.get("type") == "text")
        anchor = next((i for i in items if i.get("role") == "ai"), None)
        if not anchor or not question_in(text):
            return
        self.question = dict(role="question", id=record.get("uuid") or str(anchor["pos"]),
                             text=text[:MAX_TEXT], cut=len(text) > MAX_TEXT, title="Asks in text", at=anchor.get("at", ""),
                             pos=anchor["pos"], nth=anchor.get("nth", 0))

    def take(self, items):
        for item in items:
            role = item.get("role")
            if role not in ("request", "call", "brief", "secret"):
                continue
            key = (role, item.get("id") or item.get("name") or "call")
            self.items[key] = {k: item[k] for k in
                               ("role", "id", "name", "title", "text", "at", "pos", "nth", "blocking", "questions")
                               if k in item}


def cards(path):
    """Returns the latest card of each task, brief and secret, and the last call."""
    with _lock:
        size = os.path.getsize(path)
        index = _indexes.pop(path, None)
        if index is None or index.piece.pos > size or not index.piece.intact():
            index = Index(path)
        index.piece.read_on(size, index.take, index.inspect)
        _indexes[path] = index
        while len(_indexes) > MAX_INDEXES:
            _indexes.popitem(last=False)
        return [dict(item) for item in index.items.values()] + ([dict(index.question)] if index.question else [])
