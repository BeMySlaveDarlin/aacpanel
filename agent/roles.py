"""The role of a session in its project: a short text that tells it from the other sessions of its repository.

The executor keeps one file a session — written by the session's role tool,
by session_open and by the person in the panel — kept by the config
directory of the account, by the repository the session works in and by its
name, so a session moving between the main checkout and a worktree of it keeps
its role. The collector reads it into the row of the session.
"""
import json
import os
import posixpath
import stat

import sessionfile


def roles_dir():
    """Returns where the executor keeps the roles: the owner's state."""
    return sessionfile.state_dir("roles")


def repo(cwd):
    """Returns the repository a directory works in, as the key of a role names it, or None for a path that is not one.

    It is the common git directory of the checkout the directory lies in —
    which a worktree shares with its main checkout — or the directory itself
    where no checkout holds it or its git files do not read: the one the
    executor finds in internal/role, from the files git leaves and without
    running git. A .git directory with no HEAD in it is no repository, and the
    walk goes on above it.
    """
    start = sessionfile.clean(cwd)
    if start is None:
        return None
    at = start
    while True:
        dotgit = posixpath.join(at, ".git")
        try:
            mode = os.stat(dotgit).st_mode
        except FileNotFoundError:
            mode = None
        except OSError:
            return start
        if mode is not None and stat.S_ISDIR(mode):
            if os.path.exists(posixpath.join(dotgit, "HEAD")):
                return dotgit
        elif mode is not None:
            return _common_dir(at, dotgit) or start
        parent = posixpath.dirname(at)
        if parent == at:
            return start
        at = parent


def _read(path):
    with open(path, "rb") as f:
        return os.fsdecode(f.read().strip())


def _common_dir(at, dotgit):
    """Follows the .git file of a worktree to the common git directory, or returns None when it does not read.

    A git directory without a commondir is the common one itself, as git takes it.
    """
    try:
        gitdir = _read(dotgit)
    except OSError:
        return None
    if not gitdir.startswith("gitdir:"):
        return None
    gitdir = gitdir[len("gitdir:"):].strip()
    if not gitdir:
        return None
    gitdir = sessionfile.clean(gitdir if gitdir.startswith("/") else posixpath.join(at, gitdir))
    try:
        common = _read(posixpath.join(gitdir, "commondir"))
    except FileNotFoundError:
        return gitdir
    except OSError:
        return None
    if not common:
        return None
    return sessionfile.clean(common if common.startswith("/") else posixpath.join(gitdir, common))


def file_name(config_dir, cwd, name=None):
    """Returns the file name of the role of a session, or None for a place that is not one."""
    where = repo(cwd)
    if where is None:
        return None
    return sessionfile.file_name(config_dir, where, name)


def of(config_dir, cwd, name=None):
    """Returns the role of a live session, or None when it has none."""
    file = file_name(config_dir, cwd, name)
    if file is None:
        return None
    try:
        with open(os.path.join(roles_dir(), file), encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        return None
    if not isinstance(data, dict):
        return None
    if (data.get("configDir") != sessionfile.clean(config_dir) or data.get("repo") != repo(cwd)
            or sessionfile.name(data.get("name")) != sessionfile.name(name)):
        return None
    text = data.get("role")
    return text.strip() if isinstance(text, str) and text.strip() else None
