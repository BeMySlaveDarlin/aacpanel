"""Main loop: sampling, snapshot assembly and an atomic write."""
import json
import os
import sys
import threading
import time

import asked
import briefs
import chat
import contours
import notes
import projects
import seen
import usage_link

import agent

from .limits import limits
from .metrics import cpu_jiffies, disks, mem_info, net_counters, net_link, read
from .procs import proc_sample, proc_top
from .thermal import temperatures
from .workers import (models_snapshot, models_worker, probe_worker,
                      probes_snapshot, sessions_worker)


def write_state(state):
    os.makedirs(os.path.dirname(agent.STATE_PATH), exist_ok=True)
    tmp = agent.STATE_PATH + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(state, f, ensure_ascii=False)
    os.replace(tmp, agent.STATE_PATH)
    os.chmod(agent.STATE_PATH, 0o644)


def main():
    prev_cpu = cpu_jiffies()
    prev_net = net_counters()
    prev_procs = proc_sample()
    prev_at = time.monotonic()

    disk = projects.scan()
    disk_at = time.monotonic()

    sess = {"at": 0, "sessions": [], "notes": []}
    sess_lock = threading.Lock()
    threading.Thread(target=sessions_worker, args=(sess, sess_lock), daemon=True).start()

    probes = {"at": 0, "items": []}
    probes_lock = threading.Lock()
    threading.Thread(target=probe_worker, args=(probes, probes_lock), daemon=True).start()

    catalog = {"catalog": None}
    catalog_lock = threading.Lock()
    threading.Thread(target=models_worker, args=(catalog, catalog_lock), daemon=True).start()

    threading.Thread(target=chat.worker, daemon=True).start()

    threading.Thread(target=asked.worker, daemon=True).start()

    threading.Thread(target=notes.worker, daemon=True).start()

    threading.Thread(target=briefs.worker, daemon=True).start()

    threading.Thread(target=seen.worker, daemon=True).start()

    threading.Thread(target=usage_link.worker, daemon=True).start()

    while True:
        time.sleep(agent.INTERVAL)
        now = time.monotonic()
        elapsed = max(now - prev_at, 0.001)

        cur_cpu = cpu_jiffies()
        busy_delta = cur_cpu[0] - prev_cpu[0]
        total_delta = cur_cpu[1] - prev_cpu[1]
        cpu_pct = round(busy_delta / total_delta * 100, 1) if total_delta > 0 else 0.0
        prev_cpu = cur_cpu

        cur_net = net_counters()
        nets = []
        for name, (rx, tx) in cur_net.items():
            prx, ptx = prev_net.get(name, (rx, tx))
            nets.append({
                "name": name,
                "rx": rx,
                "tx": tx,
                "rxRate": max(0, int((rx - prx) / elapsed)),
                "txRate": max(0, int((tx - ptx) / elapsed)),
                **net_link(name),
            })
        prev_net = cur_net

        cur_procs = proc_sample()
        procs = proc_top(prev_procs, cur_procs, elapsed)
        prev_procs = cur_procs

        prev_at = now

        if now - disk_at >= agent.DISK_EVERY:
            disk = projects.scan()
            disk_at = now

        with sess_lock:
            sess_snapshot = dict(sess)

        load1, load5, load15 = read("/proc/loadavg").split()[:3]
        state = {
            "at": int(time.time()),
            "host": {
                "cpuPct": cpu_pct,
                "cpus": os.cpu_count() or 1,
                "home": os.path.expanduser("~"),
                "load": [float(load1), float(load5), float(load15)],
                "uptime": int(float(read("/proc/uptime").split()[0])),
                **temperatures(),
                "mem": mem_info(),
                "disks": disks(),
                "net": sorted(nets, key=lambda n: -(n["rxRate"] + n["txRate"])),
            },
            "sessions": sess_snapshot["sessions"],
            "sessionNotes": sess_snapshot["notes"],
            "sessionsAt": sess_snapshot["at"],
            "projects": disk,
            "procs": procs,
            "limits": limits(),
            "profiles": contours.described(),
            "models": models_snapshot(catalog, catalog_lock),
            "probes": probes_snapshot(probes, probes_lock),
        }
        try:
            write_state(state)
        except OSError as e:
            print(f"aacpanel-agent: could not write {agent.STATE_PATH}: {e}", file=sys.stderr, flush=True)
