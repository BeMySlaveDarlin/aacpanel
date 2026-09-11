"""Host processes: who eats processor and memory right now."""
import os
import pwd
import time

from .metrics import mem_info, read

TOP = 20

CMD_MAX = 160

HZ = os.sysconf("SC_CLK_TCK")
PAGE = os.sysconf("SC_PAGE_SIZE")

_users = {}


def parse_proc_stat(raw):
    """Returns (comm, ticks, starttime, memory pages) from /proc/<pid>/stat."""
    end = raw.rfind(")")
    if end < 0:
        return None
    fields = raw[end + 2:].split()
    if len(fields) < 22:
        return None
    try:
        return (raw[raw.find("(") + 1:end],
                int(fields[11]) + int(fields[12]),
                int(fields[19]),
                int(fields[21]))
    except ValueError:
        return None


def proc_sample():
    """Returns a counter snapshot of every process as {pid: (comm, ticks, starttime, pages)}."""
    out = {}
    for name in os.listdir("/proc"):
        if not name.isdigit():
            continue
        try:
            raw = read(f"/proc/{name}/stat")
        except OSError:
            continue
        parsed = parse_proc_stat(raw)
        if parsed:
            out[int(name)] = parsed
    return out


def proc_rank(prev, cur, elapsed, uptime, total_mem, limit=TOP):
    """Returns the top processes by processor and by memory in one list."""
    rows = []
    for pid, (comm, ticks, start, pages) in cur.items():
        was = prev.get(pid)
        if was and was[2] == start:
            delta, span = ticks - was[1], elapsed
        else:
            delta, span = ticks, uptime - start / HZ
        rss = pages * PAGE
        rows.append({
            "pid": pid,
            "comm": comm,
            "cpuPct": round(max(0, delta) / HZ / max(span, 0.001) * 100, 1),
            "rss": rss,
            "memPct": round(rss / total_mem * 100, 1) if total_mem else 0.0,
        })

    chosen = {}
    for key in ("cpuPct", "rss"):
        for row in sorted(rows, key=lambda r: -r[key])[:limit]:
            chosen[row["pid"]] = row
    return sorted(chosen.values(), key=lambda r: -r["cpuPct"])


def proc_describe(pid, comm):
    """Returns the command and the user of a process."""
    try:
        cmd = read(f"/proc/{pid}/cmdline").replace("\0", " ").strip()
    except OSError:
        cmd = ""
    try:
        uid = os.stat(f"/proc/{pid}").st_uid
    except OSError:
        uid = None

    if uid is not None and uid not in _users:
        try:
            _users[uid] = pwd.getpwuid(uid).pw_name
        except KeyError:
            _users[uid] = str(uid)

    return {
        "cmd": (cmd or f"[{comm}]")[:CMD_MAX],
        "user": _users.get(uid, ""),
    }


def proc_top(prev, cur, elapsed):
    """Returns the procs block of the snapshot: the top processes and how many there are."""
    uptime = float(read("/proc/uptime").split()[0])
    rows = proc_rank(prev, cur, elapsed, uptime, mem_info()["total"])
    for row in rows:
        row.update(proc_describe(row["pid"], row.pop("comm")))
    return {"at": int(time.time()), "total": len(cur), "items": rows}
