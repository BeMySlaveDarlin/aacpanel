#!/usr/bin/env python3
"""Host collector for aacpanel: host load and live claude sessions."""
import os
import sys

import paths
import sesstate

import ctx  # noqa: F401

STATE_PATH = os.environ.get("AACP_STATE", paths.state("state.json"))
CLAUDE_SESSIONS = os.environ.get("AACP_CLAUDE_SESSIONS")
LIMITS_PATH = os.environ.get("AACP_LIMITS")
SESSION_STATE = sesstate.SHARED
INTERVAL = float(os.environ.get("AACP_INTERVAL", "5"))
SESSIONS_EVERY = float(os.environ.get("AACP_SESSIONS_INTERVAL", "6"))

DISK_EVERY = float(os.environ.get("AACP_DISK_SCAN_INTERVAL", "60"))

PROBE_PORTS = os.environ.get("AACP_PROBE_PORTS", "")
PROBES_EVERY = float(os.environ.get("AACP_PROBES_INTERVAL", "60"))
PROBE_TIMEOUT = float(os.environ.get("AACP_PROBE_TIMEOUT", "3"))

MODELS_EVERY = float(os.environ.get("AACP_MODELS_INTERVAL", "3600"))

sys.modules.setdefault("agent", sys.modules[__name__])

from collect.limits import limits, limits_paths, limits_sources  # noqa: E402,F401
from collect.live import (claude_config_dirs, live_session_files,  # noqa: E402,F401
                          live_session_remote, live_session_status, live_session_status_at,
                          live_session_waits, session_files, session_profiles,
                          sessions, sessions_dirs, sessions_sources)
from collect.loop import main, write_state  # noqa: E402,F401
from collect.metrics import (SKIP_FSTYPES, SKIP_MOUNT_PREFIXES,  # noqa: E402,F401
                             cpu_jiffies, disks, ipv4_of, mem_info, net_counters,
                             net_link, read)
from collect.probes import port_probes  # noqa: E402,F401
from collect.procs import (parse_proc_stat, proc_describe,  # noqa: E402,F401
                           proc_rank, proc_sample, proc_top)
from collect.workers import (models_snapshot, models_worker,  # noqa: E402,F401
                             probe_worker, probes_snapshot, sessions_worker)

if __name__ == "__main__":
    main()
