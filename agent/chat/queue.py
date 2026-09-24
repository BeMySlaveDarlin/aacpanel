"""Queue of prompts typed while the model was answering."""


class Pending:
    """Prompts typed while the model was answering: the queue and leaving it."""

    def __init__(self):
        # The text of the prompt the queue has just handed over: a slash command
        # comes back after it as a record of its own, and that is the same prompt.
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

    def shown_at(self, text):
        """Returns the position where this text is already shown, or None."""
        shown = self.texts.get(text)
        return shown[0] if shown else None

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

    def head(self):
        """Returns the item the queue has just handed to the model."""
        item = self.waiting.pop(0) if self.waiting else None
        self.last = item["text"] if item else None
        return item

    def by_text(self, text):
        """Returns the waiting item with this text."""
        for i, item in enumerate(self.waiting):
            if item["text"] == text:
                self.last = item["text"]
                return self.waiting.pop(i)
        return None

    def handed(self, text):
        """Reports, once, whether this is the prompt the queue has just handed over."""
        if self.last is not None and self.last == text:
            self.last = None
            return True
        return False


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
