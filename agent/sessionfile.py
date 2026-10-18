"""The names of the files the panel keeps of a session on the host: its checklist, its role.

A session is told by the account it runs in — the config directory of claude —
by where it works and by its name. The name of the file is a hash of the two
paths and the name of the session, or of the paths alone for a session without
a name: the one the executor computes in internal/sessionfile. What "where" is
belongs to the file: a checklist is kept by the directory, a role by the
repository.
"""
import hashlib
import os
import posixpath


def state_dir(kind):
    """Returns where the executor keeps the files of one kind: the owner's state."""
    base = os.environ.get("XDG_STATE_HOME") or os.path.expanduser("~/.local/state")
    return os.path.join(base, "aacpanel", kind)


def clean(path):
    """Returns a path in the form the executor names the file by, or None when it is not absolute.

    The form is Go's filepath.Clean: posixpath keeps a leading double slash,
    Go does not.
    """
    if not isinstance(path, str) or not path.startswith("/"):
        return None
    path = posixpath.normpath(path)
    return "/" + path.lstrip("/") if path.startswith("//") else path


def name(value):
    """Returns the name of a session, empty for one without a name."""
    return value if isinstance(value, str) and value else ""


def file_name(config_dir, where, session=None):
    """Returns the name of the file kept of a session, or None for a place that is not one."""
    config_dir, where = clean(config_dir), clean(where)
    if config_dir is None or where is None:
        return None
    key = os.fsencode(config_dir) + b"\0" + os.fsencode(where)
    if name(session):
        key += b"\0" + os.fsencode(session)
    return hashlib.sha256(key).hexdigest()[:32] + ".json"
