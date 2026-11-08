"""Resource readings of sessions that have a cgroup of their own."""
import os
import re
import time

import ctx
from .metrics import read

CGROUP = "/sys/fs/cgroup"

# A slice includes other sessions, and the codex daemon's service includes
# every thread it hosts. Neither is a reading of one session.
SESSION_UNIT = re.compile(r".*/aacp-agents\.slice/(?:run-[^/]+\.service|[^/]+\.scope)")

_previous = {}


def session_group(pid):
    """Returns the unified cgroup path only for a session's own unit."""
    try:
        for line in read(f"{ctx.PROC}/{pid}/cgroup").splitlines():
            if line.startswith("0::") and SESSION_UNIT.fullmatch(line[3:]):
                return line[3:]
    except OSError:
        pass
    return None


def of_sessions(files):
    """Returns memory bytes and one core's CPU percentage by conversation."""
    global _previous
    now = time.monotonic()
    current, out = {}, {}
    for data in files:
        sid = data.get("sessionId")
        group = session_group(data["pid"]) if sid else None
        if not group:
            continue
        root = os.path.join(CGROUP, group.lstrip("/"))
        try:
            mem = int(read(os.path.join(root, "memory.current")))
            pages = dict(line.split() for line in read(os.path.join(root, "memory.stat")).splitlines())
            files, shared = int(pages["file"]), int(pages["shmem"])
            stat = dict(line.split() for line in read(os.path.join(root, "cpu.stat")).splitlines())
            used = int(stat["usage_usec"])
        except (OSError, ValueError, KeyError):
            continue
        if mem < 0 or used < 0 or files < 0 or shared < 0:
            continue
        # The unit is charged for the files it read and wrote, and that cache
        # is what the kernel drops first. The reading is what the session
        # holds, as the host's heavy queue counts it: shared memory stays,
        # since only swap takes it.
        cache = min(files - min(shared, files), mem)
        res = {"mem": mem - cache}
        if cache:
            res["cache"] = cache
        was = _previous.get(group)
        if was and now > was[0] and used >= was[1]:
            res["cpu"] = round((used - was[1]) / (now - was[0]) / 10000, 1)
        current[group] = (now, used)
        out[sid] = res
    # A unit that disappeared starts with a baseline if it is seen again.
    _previous = current
    return out
