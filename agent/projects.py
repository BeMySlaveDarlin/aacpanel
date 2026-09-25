#!/usr/bin/env python3
"""Project directories on disk: what a project in the map can be made of."""

import os
import re
import time

import contours


def _scan_roots():
    named = os.environ.get("AACP_PROJECT_SCAN", "").strip()
    if named:
        return named

    home = os.path.expanduser("~")
    allowed = os.environ.get("AACP_PROJECT_ROOTS", "").strip()
    if allowed:
        outside = [r for r in allowed.split(":")
                   if r.strip() and os.path.normpath(os.path.expanduser(r.strip())) != os.path.normpath(home)]
        if outside:
            return ":".join(outside)
    return home


SCAN_ROOTS = _scan_roots()

DEPTH = 4

SKIP = frozenset({"node_modules", "vendor", "dist", "build", "target", "__pycache__"})

MARKERS = (".git", ".claude", "CLAUDE.md")


def roots():
    """Returns the walk roots, without duplicates and without the filesystem root."""
    out = []
    for raw in SCAN_ROOTS.split(":") + contours.prefixes():
        raw = raw.strip()
        if not raw:
            continue
        r = os.path.normpath(os.path.expanduser(raw))
        if r != "/" and os.path.isabs(r) and r not in out:
            out.append(r)
    return [r for r in out if not any(o != r and r.startswith(o + os.sep) for o in out)]


def slug(path):
    """Returns the name of the claude transcript directory for a working directory."""
    return re.sub(r"[^A-Za-z0-9]", "-", path)


def known_slugs():
    """Returns the slugs of directories claude has already been run in, across contours."""
    out = set()
    for d in contours.config_dirs():
        try:
            out.update(os.listdir(os.path.join(d, "projects")))
        except OSError:
            continue
    return out


def worktree_of(path):
    """Returns the main checkout of a git worktree, or "" for anything else.

    A worktree keeps a file named .git that points at its own directory inside
    the main repository; that directory names the repository's git directory in
    its commondir. A session started in a worktree belongs to the project of
    the main checkout, which is what the map knows.
    """
    try:
        with open(os.path.join(path, ".git"), encoding="utf-8") as f:
            line = f.readline().strip()
    except (OSError, UnicodeDecodeError):
        return ""
    if not line.startswith("gitdir:"):
        return ""
    gitdir = os.path.normpath(os.path.join(path, line[len("gitdir:"):].strip()))
    try:
        with open(os.path.join(gitdir, "commondir"), encoding="utf-8") as f:
            common = os.path.normpath(os.path.join(gitdir, f.readline().strip()))
    except (OSError, UnicodeDecodeError):
        return ""
    if os.path.basename(common) != ".git":
        return ""
    main = os.path.dirname(common)
    return main if main != path else ""


def checkout_of(cwd):
    """Returns the main checkout of the git worktree a directory lies in, or "".

    The directory may be deep inside the worktree: the climb stops at the
    nearest directory holding a .git, which is a file in a worktree and a
    directory in the repository itself.
    """
    if not cwd or not os.path.isabs(cwd):
        return ""
    d = os.path.normpath(cwd)
    while d != os.path.dirname(d):
        if os.path.lexists(os.path.join(d, ".git")):
            return worktree_of(d)
        d = os.path.dirname(d)
    return ""


def scan():
    """Returns a snapshot of directories: roots, depth and everything the walk saw."""
    seen = known_slugs()
    found = roots()
    dirs = []
    for root in found:
        if not os.path.isdir(root):
            continue
        _walk(root, 0, seen, dirs)
    return {"at": int(time.time()), "roots": found, "depth": DEPTH, "dirs": dirs}


def _walk(path, depth, seen, dirs):
    try:
        names = os.listdir(path)
    except OSError:
        return
    if depth > 0:
        git = ".git" in names
        claude = slug(path) in seen
        if git or claude or any(m in names for m in MARKERS):
            row = {"path": path, "kind": "project", "git": git, "claude": claude}
            main = worktree_of(path) if git else ""
            if main:
                row["worktreeOf"] = main
            dirs.append(row)
            return
        if depth >= DEPTH:
            return
        dirs.append({"path": path, "kind": "folder"})
    for name in sorted(names):
        if name.startswith(".") or name in SKIP:
            continue
        child = os.path.join(path, name)
        if os.path.islink(child):
            if os.path.isdir(child):
                dirs.append({"path": child, "kind": "link"})
            continue
        if os.path.isdir(child):
            _walk(child, depth + 1, seen, dirs)
