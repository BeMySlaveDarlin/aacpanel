"""An index of actionable cards, without keeping the rest of a conversation."""
import os
import threading
from collections import OrderedDict

from .tail import Piece


# The same parser finds the card and its position for both the feed and the
# queue. Only new records are read after the first listing of a conversation.
_lock = threading.Lock()
_indexes = OrderedDict()
MAX_INDEXES = 64


class Index:
    def __init__(self, path):
        self.piece = Piece(path, 0, 0)
        self.items = {}

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
        index.piece.read_on(size, index.take)
        _indexes[path] = index
        while len(_indexes) > MAX_INDEXES:
            _indexes.popitem(last=False)
        return [dict(item) for item in index.items.values()]
