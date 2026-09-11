"""Machine load: processor, memory, disks, network."""
import fcntl
import shutil
import socket
import struct

SKIP_FSTYPES = {
    "tmpfs", "devtmpfs", "squashfs", "overlay", "proc", "sysfs", "cgroup", "cgroup2",
    "devpts", "mqueue", "hugetlbfs", "debugfs", "tracefs", "securityfs", "pstore",
    "bpf", "configfs", "fusectl", "autofs", "binfmt_misc", "nsfs", "ramfs", "efivarfs",
}
SKIP_MOUNT_PREFIXES = ("/snap", "/var/lib/docker", "/run", "/sys", "/proc", "/dev")


def read(path):
    with open(path, encoding="utf-8", errors="replace") as f:
        return f.read()


def cpu_jiffies():
    """Returns (busy, total) from the first line of /proc/stat."""
    parts = read("/proc/stat").split("\n", 1)[0].split()[1:]
    vals = [int(v) for v in parts]
    total = sum(vals)
    idle = vals[3] + (vals[4] if len(vals) > 4 else 0)
    return total - idle, total


def mem_info():
    info = {}
    for line in read("/proc/meminfo").splitlines():
        k, _, v = line.partition(":")
        info[k] = int(v.strip().split()[0]) * 1024
    total = info.get("MemTotal", 0)
    avail = info.get("MemAvailable", 0)
    swap_total = info.get("SwapTotal", 0)
    return {
        "total": total,
        "used": total - avail,
        "available": avail,
        "pct": round((total - avail) / total * 100, 1) if total else 0,
        "swapTotal": swap_total,
        "swapUsed": swap_total - info.get("SwapFree", 0),
    }


def disks():
    seen = set()
    out = []
    for line in read("/proc/mounts").splitlines():
        parts = line.split()
        if len(parts) < 3:
            continue
        dev, mount, fstype = parts[0], parts[1].replace("\\040", " "), parts[2]
        if fstype in SKIP_FSTYPES or not dev.startswith("/"):
            continue
        if any(mount.startswith(p) for p in SKIP_MOUNT_PREFIXES) and mount != "/":
            continue
        if dev in seen:
            continue
        seen.add(dev)
        try:
            usage = shutil.disk_usage(mount)
        except OSError:
            continue
        out.append({
            "mount": mount,
            "device": dev,
            "fstype": fstype,
            "total": usage.total,
            "used": usage.used,
            "free": usage.free,
            "pct": round(usage.used / usage.total * 100, 1) if usage.total else 0,
        })
    return sorted(out, key=lambda d: d["mount"])


def net_counters():
    out = {}
    for line in read("/proc/net/dev").splitlines()[2:]:
        name, _, rest = line.partition(":")
        name = name.strip()
        if name == "lo" or name.startswith(("veth", "docker", "br-", "vpn-", "wg-")):
            continue
        f = rest.split()
        if len(f) < 9:
            continue
        out[name] = (int(f[0]), int(f[8]))
    return out


def net_link(name):
    """Returns the state of an interface and its address."""
    def sysfs(field, default=""):
        try:
            return read(f"/sys/class/net/{name}/{field}").strip()
        except OSError:
            return default

    state = sysfs("operstate", "unknown")
    speed = 0
    if raw := sysfs("speed"):
        try:
            speed = max(0, int(raw))
        except ValueError:
            speed = 0

    return {"state": state, "speed": speed, "addr": ipv4_of(name)}


def ipv4_of(name):
    """Returns the IPv4 address of an interface, asking the kernel directly."""
    try:
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
            packed = struct.pack("256s", name.encode()[:15])
            return socket.inet_ntoa(fcntl.ioctl(sock.fileno(), 0x8915, packed)[20:24])
    except OSError:
        return ""
