"""The context guard of a place, as the panel's executor keeps it for the host."""

import os

CAP_DEFAULT = 80

# How far under the cap, in points of the window, a session of a project that
# restarts its sessions is restarted at the end of a turn. A session wraps its
# work up for the restart it sees coming and stops a little short of the cap,
# and a session stopped under it does not stop again until someone writes to
# it. The stamp names the cap itself, never the margin: told of the margin, a
# session would stop short of that instead.
RESTART_MARGIN = 1.0


def home():
    """Returns the directory the executor keeps what the hooks of the host read in."""
    base = os.environ.get("XDG_STATE_HOME") or os.path.join(os.path.expanduser("~"), ".local", "state")
    return os.path.join(base, "aacpanel")


def path():
    """Returns the file the executor keeps the guards in."""
    return os.path.join(home(), "guards.tsv")


def where(payload):
    """Returns the directory a session belongs to.

    The project directory claude names for its hooks, not the working directory:
    that one follows every cd the agent makes, and the cap would jump to whatever
    project the agent is looking into.
    """
    return (os.environ.get("CLAUDE_PROJECT_DIR")
            or (payload.get("cwd") if isinstance(payload, dict) else "")
            or os.getcwd())


def of(cwd, file=None):
    """Returns (cap, restart) of the place closest above cwd, or None where the file names none.

    A line a place: the directory, the cap in percent of the model's window, and 1
    where a session past it restarts itself. The closest place is the longest path
    that is the directory itself or holds it — a project inside its contour's tree.
    """
    try:
        with open(file or path(), encoding="utf-8") as f:
            lines = f.read().splitlines()
    except OSError:
        return None
    best = None
    for line in lines:
        parts = line.split("\t")
        if len(parts) != 3:
            continue
        place, cap, restart = parts
        if not cwd or not (cwd == place or cwd.startswith(place.rstrip("/") + "/")):
            continue
        if best is not None and len(place) <= len(best[0]):
            continue
        try:
            best = (place, int(cap), restart == "1")
        except ValueError:
            continue
    return None if best is None else best[1:]
