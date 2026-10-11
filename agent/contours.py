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


def _name_of(config_dir, own=".claude"):
    name = os.path.basename(config_dir.rstrip("/")) or config_dir
    return PERSONAL if name == own else name.lstrip(".")


CODEX_ENV = "AACP_CODEX_HOMES"
CODEX_HOME = "~/.codex"


def codex_homes():
    """Returns the codex homes as pairs of contour name and directory, in the order named.

    A home is named the way a claude config directory is: ~/.codex is the
    personal one, any other by its last part without the leading dot. It pairs
    with the claude contour whose account bears that name —
    ~/.codex-profiles/claude-work with the account of ~/.claude-work — and,
    failing that, with the contour the installer puts on the map under that
    name: ~/.codex-profiles/work with the same account, which the installer
    offers to call work. Either way the home stands under the account's name,
    so the archive, the runs and New of the contour find it the same.

    A contour pairs with one home: the one named after the account wins over
    the one named after the map, and of two of one name the first listed. A
    home left over, like one that claims no contour, stands as a contour of
    its own name. The executor pairs them by the same rule.
    """
    raw = os.environ.get(CODEX_ENV) or ""
    out = []
    for part in (raw.split(os.pathsep) if raw.strip() else [CODEX_HOME]):
        d = os.path.expanduser(part.strip())
        if d and d not in out:
            out.append(d)
    return list(zip(_paired([_name_of(d, ".codex") for d in out]), out))


def _suggested_name(config_dir):
    """Returns the name the installer offers for the contour of a claude config
    directory on the map: personal for ~/.claude, any other its last part
    without a leading .claude- or dot, in lower case — ~/.claude-work is work."""
    name = os.path.basename(config_dir.rstrip("/")) or config_dir
    if name == ".claude":
        return PERSONAL
    if name.startswith(".claude-"):
        name = name[len(".claude-"):]
    if name.startswith("."):
        name = name[1:]
    return name.lower()


def _paired(names):
    """Returns the contour each codex home of these names stands under, in their
    order: every home named after an account first, wherever it is listed,
    then the homes named after the map."""
    accounts = [(name, _suggested_name(d)) for name, d, _ in _rows()]
    own = {name for name, _ in accounts}
    out, claimed = [None] * len(names), set()
    for i, name in enumerate(names):
        if name in own and name not in claimed:
            out[i] = name
            claimed.add(name)
    for i, name in enumerate(names):
        if out[i] is not None:
            continue
        account = next((a for a, suggested in accounts if suggested == name), None)
        if account is not None and account not in claimed:
            out[i] = account
            claimed.add(account)
        else:
            out[i] = name
    return out


def codex_contour(home):
    """Returns the contour a codex home belongs to: the one it is listed under,
    else the name a listed home of that directory would have."""
    home = os.path.normpath(home)
    for contour, listed in codex_homes():
        if os.path.normpath(listed) == home:
            return contour
    return _name_of(home, ".codex")


def config_dirs():
    """Returns the config directories of every contour, the personal one first."""
    return [d for _, d in profiles()]


def _rows():
    return [(name, d, token) for name, d, token, _ in _routed_rows()]


def _routed_rows():
    """Returns the contours with the registry's word on each: the directory
    prefix it routes by, or None for one the registry does not hold."""
    reg, order = {}, []
    for parts in _entries():
        d = os.path.expanduser(parts[2])
        if d in reg:
            continue
        reg[d] = (parts[0], parts[3] if len(parts) > 3 else "", parts[1])
        order.append(d)

    rows, seen = [], set()

    def put(d, name, token, prefix):
        if d in seen:
            return
        seen.add(d)
        rows.append((name, d, token, prefix))

    for d in _env_dirs():
        name, token, prefix = reg.get(d, (_name_of(d), "-", None))
        put(d, name, token, prefix)
    for d in order:
        name, token, prefix = reg[d]
        put(d, name, token, prefix)
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


def _settings_of(config_dir):
    path = os.path.join(config_dir, "settings.json")
    try:
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        return None
    return data if isinstance(data, dict) else None


def _hooks_of(config_dir):
    data = _settings_of(config_dir)
    return data.get("hooks") if data is not None else None


# The context guard, as the installer writes it or wrote it before: the
# executor's hook of the name, from wherever the executor lies, or the script
# of a clone.
GUARD_HOOK = "context-guard"
GUARD_SCRIPT = GUARD_HOOK + ".py"


def _runs_guard(command):
    """Says whether a hook's command runs the context guard, in either form."""
    words = command.split()
    if any(w.endswith(GUARD_SCRIPT) for w in words):
        return True
    return any(os.path.basename(w) == "aacpanel-exec" and words[i + 1:i + 3] == ["hook", GUARD_HOOK]
               for i, w in enumerate(words))


def _account_of(settings):
    """Returns what the account starts a session with, in the map's words.

    Three keys and nothing else: the settings hold the environment of the account,
    tokens among it, and the panel has no business carrying that anywhere.
    """
    out = {}
    for key, value in (("model", settings.get("model")),
                       ("effort", settings.get("effortLevel")),
                       ("permissionMode", (settings.get("permissions") or {}).get("defaultMode"))):
        if isinstance(value, str) and value:
            out[key] = value
    return out


def _guarded(settings):
    """Says whether the account runs the context guard hook at the end of a turn."""
    for entry in ((settings.get("hooks") or {}).get("Stop") or []):
        for hook in (entry.get("hooks") or []) if isinstance(entry, dict) else []:
            if isinstance(hook, dict) and _runs_guard(str(hook.get("command") or "")):
                return True
    return False


def _hooks_state(base, config_dir):
    if base is None:
        return "unknown"
    hooks = _hooks_of(config_dir)
    if hooks is None:
        return "unknown"
    return "same" if hooks == base else "diverged"


def _codex_home_of(name):
    """Returns the codex home that pairs with a contour, as the executor pairs it,
    or None where none does or its directory is not there."""
    for contour, d in codex_homes():
        if contour == name:
            return d if os.path.isdir(d) else None
    return None


def described():
    """Returns the profiles with the state of their authorization and hooks."""
    base = _hooks_of(HOME)
    out = []
    for name, conf, token, prefix in _routed_rows():
        if not os.path.isdir(conf):
            continue
        if not token or token == "-":
            auth = "builtin"
        else:
            auth = "token" if os.path.isfile(os.path.expanduser(token)) else "missing"
        row = {"name": name, "configDir": conf, "auth": auth}
        # The registry of the router holds the account: the directory a
        # session starts in picks it, by this prefix ("*" for the rest).
        if prefix is not None:
            row["routed"] = True
            row["prefix"] = prefix
        if conf != HOME:
            row["hooks"] = _hooks_state(base, conf)
        settings = _settings_of(conf)
        if settings is not None:
            row["account"] = _account_of(settings)
            row["contextGuard"] = _guarded(settings)
        # New offers codex only in a contour with a home of its own: without
        # one the executor has no daemon to start the thread in.
        codex = _codex_home_of(name)
        if codex is not None:
            row["codexHome"] = codex
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
