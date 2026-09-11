#!/usr/bin/env python3
"""State directory defaults shared by the agent modules."""

import os

DEFAULT_STATE_DIR = "/var/lib/aacpanel"


def state_dir():
    """Returns the state directory of this machine."""
    return os.environ.get("AACP_STATE_DIR", DEFAULT_STATE_DIR)


def state(name):
    """Returns the path to a state file by its name."""
    return os.path.join(state_dir(), name)
