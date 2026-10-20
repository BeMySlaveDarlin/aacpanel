"""The pin of a session group, shared by devices and kept under the role's key."""
import json
import os

import roles
import sessionfile


def of(config_dir, cwd, name):
    """Returns the time the session was pinned, or None when it has no pin."""
    file = roles.file_name(config_dir, cwd, name)
    if not file or not name:
        return None
    try:
        with open(os.path.join(sessionfile.state_dir("pins"), file), encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        return None
    if not isinstance(data, dict):
        return None
    if (data.get("configDir") != sessionfile.clean(config_dir) or data.get("repo") != roles.repo(cwd)
            or data.get("name") != name):
        return None
    at = data.get("at")
    return at if isinstance(at, str) and at else None
