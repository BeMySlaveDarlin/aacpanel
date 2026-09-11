"""List limits and the two operations over them: cut the text, forget the old."""

MAX_ITEMS = 40
MAX_TEXT = 200


def _short(text):
    text = " ".join(str(text or "").split())
    return text[:MAX_TEXT]


def _prune(store):
    if len(store) <= MAX_ITEMS:
        return
    old = sorted(store.items(), key=lambda kv: kv[1].get("at") or "")
    for key, _ in old[:len(store) - MAX_ITEMS]:
        store.pop(key, None)
