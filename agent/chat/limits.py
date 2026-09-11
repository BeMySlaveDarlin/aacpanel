"""Feed limits and honest truncation."""


MAX_TEXT = 16 * 1024
MAX_ARG = 200
MAX_NOTE = 1024
MAX_ARGS = 8 * 1024
MAX_RESULT = 24 * 1024
DEFAULT_LIMIT = 40
MAX_LIMIT = 200


def cut(text, limit):
    """Truncates the text to the limit and reports whether it was cut."""
    if len(text) <= limit:
        return text, False
    return text[:limit], True
