"""Queue of prompts typed while the model was answering."""


class Pending:
    """Prompts typed while the model was answering: the queue and leaving it."""

    def __init__(self):
        # The row of the prompt the queue has just handed over: a command comes
        # back after it as a record of its own, and that is the same prompt.
        self.last = None
        self.waiting = []
        self.texts = {}
        self.wake = set()
        self.service_texts = set()

    def clone(self):
        """Returns a copy of the queue that a throwaway read may spoil."""
        twin = Pending()
        twin.last = self.last
        twin.waiting = list(self.waiting)
        twin.texts = dict(self.texts)
        twin.wake = set(self.wake)
        twin.service_texts = set(self.service_texts)
        return twin

    def seen(self, text):
        return text in self.texts

    def service_seen(self, text):
        """Reports whether the feed has already shown this harness insert."""
        return text in self.service_texts

    def service_remember(self, text):
        self.service_texts.add(text)

    def shown_as_wake(self, text):
        """Reports whether the text is already shown as an alarm card."""
        shown = self.texts.get(text)
        return bool(shown and shown[1])

    def schedule(self, prompt):
        if isinstance(prompt, str) and prompt.strip():
            self.wake.add(prompt.strip())

    def is_wake(self, text):
        return text in self.wake

    def remember(self, text, pos=None, wake=False):
        self.texts.setdefault(text, (pos, wake))

    def enqueue(self, item):
        self.waiting.append(item)

    def drain(self):
        """Returns every waiting item at once."""
        out, self.waiting = self.waiting, []
        return out

    def read(self):
        """Returns the waiting messages at once: the model reads them in the middle of its turn.

        A command waits in the queue for the turn to end, and claude hands it
        over then.
        """
        out = [item for item in self.waiting if item["role"] != "command"]
        self.waiting = [item for item in self.waiting if item["role"] == "command"]
        return out

    def head(self):
        """Returns the item the queue has just handed to the model."""
        item = self.waiting.pop(0) if self.waiting else None
        self.last = item
        return item

    def by_text(self, text):
        """Returns the waiting item with this text."""
        for i, item in enumerate(self.waiting):
            if item["text"] == text:
                self.last = item
                return self.waiting.pop(i)
        return None

    def drawn(self, text, waiting=True):
        """Returns, once, the row the queue drew for this prompt, or None.

        The row still waits in the queue or was the last to leave it. A text
        seen anywhere else is not asked: the same words come again — the next
        tick of a loop, the next reset of the limit — and each time the queue
        draws them anew.
        """
        item = self.by_text(text) if waiting else None
        if item is None and self.last is not None and self.last["text"] == text:
            item = self.last
        if item is not None:
            self.last = None
        return item


def delivered(item):
    """Returns the same item without the queued mark."""
    fixed = dict(item)
    fixed.pop("state", None)
    return fixed


def withdrawn(item):
    """Returns the same item marked as taken back before the model read it."""
    fixed = dict(item)
    fixed["state"] = "withdrawn"
    return fixed
