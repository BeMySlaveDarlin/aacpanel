#!/usr/bin/env python3
"""Claude contours: several config directories instead of one."""

import json
import os

REGISTRY = os.environ.get("AACP_CLAUDE_REGISTRY", "")
HOME_ENV = "AACP_CLAUDE_HOME"
PERSONAL = "personal"
HOME = os.path.expanduser(
    (os.environ.get(HOME_ENV) or "~/.claude").split(os.pathsep)[0].strip()
    or "~/.claude")


def _env_dirs():
    raw = os.environ.get(HOME_ENV)
    if raw is None or not raw.strip():
        return [HOME]
    out = []
    for part in raw.split(os.pathsep):
        d = os.path.expanduser(part.strip())
        if d and d not in out:
            out.append(d)
    if HOME not in out:
        out.insert(0, HOME)
    return out


def _name_of(config_dir):
    name = os.path.basename(config_dir.rstrip("/")) or config_dir
    return PERSONAL if name == ".claude" else name.lstrip(".")


def config_dirs():
    """Returns the config directories of every contour, the personal one first."""
    return [d for _, d in profiles()]


def _rows():
    reg, order = {}, []
    for parts in _entries():
        d = os.path.expanduser(parts[2])
        if d in reg:
            continue
        reg[d] = (parts[0], parts[3] if len(parts) > 3 else "")
        order.append(d)

    rows, seen = [], set()

    def put(d, name, token):
        if d in seen:
            return
        seen.add(d)
        rows.append((name, d, token))

    for d in _env_dirs():
        name, token = reg.get(d, (_name_of(d), "-"))
        put(d, name, token)
    for d in order:
        name, token = reg[d]
        put(d, name, token)
    return rows


def _entries():
    try:
        with open(REGISTRY, encoding="utf-8") as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith("#"):
                    continue
                parts = [x.strip() for x in line.split("|")]
                if len(parts) >= 3:
                    yield parts
    except OSError:
        return


def prefixes():
    """Returns the directory prefixes of work contours, without * and without duplicates."""
    out = []
    for parts in _entries():
        prefix = parts[1]
        if prefix in ("", "*"):
            continue
        p = os.path.expanduser(prefix)
        if p not in out:
            out.append(p)
    return out


def profiles():
    """Returns contours as pairs of profile name and config directory, the personal one first."""
    return [(name, d) for name, d, _ in _rows() if os.path.isdir(d)]


def _hooks_of(config_dir):
    path = os.path.join(config_dir, "settings.json")
    try:
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        return None
    return data.get("hooks") if isinstance(data, dict) else None


def _hooks_state(base, config_dir):
    if base is None:
        return "unknown"
    hooks = _hooks_of(config_dir)
    if hooks is None:
        return "unknown"
    return "same" if hooks == base else "diverged"


def described():
    """Returns the profiles with the state of their authorization and hooks."""
    base = _hooks_of(HOME)
    out = []
    for name, conf, token in _rows():
        if not os.path.isdir(conf):
            continue
        if not token or token == "-":
            auth = "builtin"
        else:
            auth = "token" if os.path.isfile(os.path.expanduser(token)) else "missing"
        row = {"name": name, "configDir": conf, "auth": auth}
        if conf != HOME:
            row["hooks"] = _hooks_state(base, conf)
        out.append(row)
    return out


def dirs(name, only=None):
    """Returns the named directory of every contour."""
    if only:
        return [only]
    return [os.path.join(d, name) for d in config_dirs()]


def files(name, only=None):
    """Returns the named file in every contour, the personal one first."""
    if only:
        return [only]
    return [os.path.join(d, name) for d in config_dirs()]
