#!/usr/bin/env python3
"""Reading a git repository for the panel: the agent runs git, nothing else does.

The service lives in a container with no rights on the host, and reading a
repository is reading — so it is the agent's work. What goes back is raw: a list
of changed files, the listing of one directory, one blob, the diff of one file.
Cutting a diff into hunks and colouring it happens in the service, where a
mistake costs a redraw rather than a shell command.

Every window carries the revision it was read at. A repository under an agent
that keeps writing moves between two requests, and a window answered from the
new state while the list came from the old one is a diff nobody can trust: the
answer is "stale" and the screen asks again.
"""

import hashlib
import os
import subprocess

from sesstate import inside

# What one reply may carry. A file past the ceiling is not cut silently: the
# reply says how much there is and the screen says it out loud.
MAX_BLOB = 2 * 1024 * 1024
MAX_DIFF = 2 * 1024 * 1024
MAX_FILES = 3000
MAX_ENTRIES = 2000
MAX_LINES = 20000

GIT_TIMEOUT = 20

# The trailer a session leaves in the commits it writes. It is what ties a line
# of code to the conversation it was written in.
SESSION_TRAILER = "Claude-Session:"


class RepoError(Exception):
    """A refusal a person can act on: a bad directory, a name git will not take."""


def _run(cwd, *args, limit=MAX_DIFF):
    """Runs one git command in cwd and returns its output, or raises RepoError."""
    try:
        done = subprocess.run(
            ("git", "-C", cwd, "--no-pager", *args),
            capture_output=True,
            timeout=GIT_TIMEOUT,
        )
    except subprocess.TimeoutExpired:
        raise RepoError(f"git {args[0]} did not finish in {GIT_TIMEOUT} seconds")
    except OSError as e:
        raise RepoError(f"git could not be run: {e}")
    if done.returncode != 0:
        why = done.stderr.decode("utf-8", "replace").strip().splitlines()
        raise RepoError(why[0] if why else f"git {args[0]} failed")
    out = done.stdout
    return out[:limit], len(out) > limit


def _text(raw):
    return raw.decode("utf-8", "replace")


def _repo_dir(cwd):
    """Returns the working tree cwd belongs to, or raises when it is not one."""
    if not isinstance(cwd, str) or not cwd:
        raise RepoError("no directory was named")
    real = os.path.realpath(os.path.expanduser(cwd))
    if not os.path.isdir(real):
        raise RepoError("there is no such directory on the host")
    top, _ = _run(real, "rev-parse", "--show-toplevel")
    return _text(top).strip() or real


def _file(cwd, path):
    """Returns the absolute path of a file inside the working tree, or raises."""
    real = inside(path, cwd)
    if not real:
        raise RepoError("the path lies outside the working tree of this project")
    return real


# ---------------------------------------------------------------- the branch

def _upstream(cwd, branch):
    try:
        out, _ = _run(cwd, "rev-parse", "--abbrev-ref", f"{branch}@{{upstream}}")
    except RepoError:
        return ""
    return _text(out).strip()


def _created_from(cwd, branch):
    """Reads the branch this one was created from out of the reflog.

    Git keeps no record of where a branch came from. Its own reflog holds a
    line "branch: Created from ...", but that line names HEAD whenever the
    branch was cut from wherever the person already stood — which is nearly
    always. The name is in the reflog of HEAD instead, in the move that created
    it: "checkout: moving from <parent> to <branch>". The oldest such move is
    the one that made the branch; the later ones are comings and goings.
    """
    try:
        out, _ = _run(cwd, "reflog", "show", "--no-abbrev", branch)
    except RepoError:
        out = b""
    for line in _text(out).splitlines():
        marker = "branch: Created from "
        if marker in line:
            name = line.split(marker, 1)[1].strip()
            if name and name != "HEAD":
                return name

    try:
        out, _ = _run(cwd, "reflog", "show", "--no-abbrev", "HEAD")
    except RepoError:
        return ""
    moves = []
    for line in _text(out).splitlines():
        marker = "checkout: moving from "
        if marker not in line:
            continue
        move = line.split(marker, 1)[1].strip()
        parent, _, went = move.partition(" to ")
        if went == branch and parent and parent != branch:
            moves.append(parent)
    return moves[-1] if moves else ""


def base_of(cwd, branch, named=""):
    """Returns the branch a review is measured against, and how it was chosen.

    The order is the one the panel promises: what the project says, then what
    the branch itself points at, then what the reflog remembers, then a merge
    base with the usual trunk. The choice is named out loud because it cannot
    be guessed right every time — the screen shows it and lets it be changed.
    """
    if named:
        return named, "project"
    up = _upstream(cwd, branch)
    if up:
        return up, "upstream"
    born = _created_from(cwd, branch)
    if born:
        return born, "reflog"
    for trunk in ("main", "master"):
        try:
            _run(cwd, "rev-parse", "--verify", f"{trunk}^{{commit}}")
        except RepoError:
            continue
        if trunk != branch:
            return trunk, "trunk"
    return "", "none"


# ----------------------------------------------------------------- the state

def _head(cwd):
    out, _ = _run(cwd, "rev-parse", "HEAD")
    return _text(out).strip()


def _merge_base(cwd, base, head):
    if not base:
        return ""
    try:
        out, _ = _run(cwd, "merge-base", base, head)
    except RepoError:
        return ""
    return _text(out).strip()


def _oids(cwd):
    """Returns the object id of every tracked file, the working tree included.

    A file changed in place keeps its path and its size; the id is what says it
    is a different file now. It is what the revision below is made of.
    """
    out, _ = _run(cwd, "ls-files", "-s", "-z", limit=8 * 1024 * 1024)
    ids = {}
    for row in _text(out).split("\0"):
        if not row:
            continue
        meta, _, path = row.partition("\t")
        parts = meta.split()
        if len(parts) >= 2 and path:
            ids[path] = parts[1]
    return ids


def revision(cwd, base, head):
    """Returns a short name for the state a window was read at.

    It holds the head, the merge base and the ids of the tracked files. Two
    reads with the same revision saw the same repository; a window asked for
    under an old one is refused rather than stitched onto a newer list.
    """
    digest = hashlib.sha256()
    digest.update(head.encode())
    digest.update(b"\0")
    digest.update(_merge_base(cwd, base, head).encode())
    for path, oid in sorted(_oids(cwd).items()):
        digest.update(b"\0")
        digest.update(path.encode())
        digest.update(b" ")
        digest.update(oid.encode())
    return digest.hexdigest()[:16]


# ------------------------------------------------------------- the operations

def refs(cwd):
    """Returns the branches of the repository, its worktrees and where it stands."""
    top = _repo_dir(cwd)
    out, _ = _run(top, "for-each-ref", "--format=%(refname:short)%09%(upstream:short)", "refs/heads")
    branches = []
    for line in _text(out).splitlines():
        name, _, up = line.partition("\t")
        if name:
            branches.append({"name": name, "upstream": up})

    out, _ = _run(top, "worktree", "list", "--porcelain")
    trees, current = [], {}
    for line in _text(out).splitlines():
        if line.startswith("worktree "):
            if current:
                trees.append(current)
            current = {"path": line[len("worktree "):]}
        elif line.startswith("branch "):
            current["branch"] = line[len("branch "):].replace("refs/heads/", "")
        elif line.strip() == "detached":
            current["branch"] = ""
    if current:
        trees.append(current)

    out, _ = _run(top, "rev-parse", "--abbrev-ref", "HEAD")
    branch = _text(out).strip()
    return {"root": top, "branch": branch, "branches": branches, "worktrees": trees}


def changes(cwd, base_named=""):
    """Returns every file this branch changed, in one list.

    Three questions make it: what the commits of the branch changed against the
    base, what the working tree changed against the commits, and what is not
    tracked at all. They are one list because the tree on the screen and the
    run of diffs under it are the same set of files — counted twice they come
    apart, and the file a person taps is not the file they were shown.
    """
    top = _repo_dir(cwd)
    head = _head(top)
    out, _ = _run(top, "rev-parse", "--abbrev-ref", "HEAD")
    branch = _text(out).strip()
    base, how = base_of(top, branch, base_named)

    files = {}

    def put(path, layer, add=0, delete=0, status=""):
        row = files.setdefault(path, {"path": path, "layer": layer, "add": 0, "delete": 0, "status": status})
        # The working tree wins the label: a file in a commit and changed again
        # since is not yet in a commit, and that is what the mark has to say.
        if layer == "worktree":
            row["layer"] = "worktree"
        row["add"] += add
        row["delete"] += delete
        if status:
            row["status"] = status

    def numstat(layer, *args):
        out, _ = _run(top, "diff", "--numstat", "-z", "--no-renames", *args, limit=4 * 1024 * 1024)
        fields = _text(out).split("\0")
        i = 0
        while i + 2 < len(fields) + 1:
            row = fields[i]
            i += 1
            if not row:
                continue
            add, _, rest = row.partition("\t")
            delete, _, path = rest.partition("\t")
            if not path:
                # A rename arrives as three fields; --no-renames keeps them
                # apart, so a path missing here is the end of the list.
                continue
            put(path, layer,
                add=int(add) if add.isdigit() else 0,
                delete=int(delete) if delete.isdigit() else 0,
                status="M")

    if base:
        numstat("committed", f"{base}...HEAD")
    numstat("worktree", "HEAD")

    out, _ = _run(top, "ls-files", "--others", "--exclude-standard", "-z")
    for path in _text(out).split("\0"):
        if path:
            put(path, "worktree", status="A")

    rows = sorted(files.values(), key=lambda r: r["path"])
    cut = len(rows) > MAX_FILES
    return {
        "root": top,
        "branch": branch,
        "base": base,
        "baseFrom": how,
        "head": head,
        "rev": revision(top, base, head),
        "files": rows[:MAX_FILES],
        "total": len(rows),
        "cut": cut,
    }


def tree(cwd, path=""):
    """Returns the entries of one directory of the working tree.

    One directory at a time, not the repository flattened into a window: a tree
    is walked by opening what is asked for, and a listing of everything is a
    number nobody reads and a reply nobody needs.
    """
    top = _repo_dir(cwd)
    where = path.strip("/")
    real = _file(top, where) if where else top
    if not os.path.isdir(real):
        raise RepoError("that path is not a directory")

    spec = f"{where}/" if where else ""
    args = ("ls-tree", "--name-only", "-z", "HEAD") + ((spec,) if spec else ())
    out, _ = _run(top, *args)
    entries = {}
    for name in _text(out).split("\0"):
        if not name:
            continue
        short = name[len(spec):] if spec and name.startswith(spec) else name
        if not short:
            continue
        full = os.path.join(real, short)
        entries[short] = {"name": short, "dir": os.path.isdir(full)}

    # What git does not know about yet is still on the screen: a new file is
    # the most interesting thing in a review, and a tree that hides it lies.
    try:
        for name in sorted(os.listdir(real)):
            if name == ".git" or name in entries:
                continue
            full = os.path.join(real, name)
            entries[name] = {"name": name, "dir": os.path.isdir(full), "untracked": True}
    except OSError:
        pass

    rows = sorted(entries.values(), key=lambda r: (not r["dir"], r["name"]))
    return {"root": top, "path": where, "entries": rows[:MAX_ENTRIES], "total": len(rows),
            "cut": len(rows) > MAX_ENTRIES}


def blob(cwd, path, rev="", first=1, lines=MAX_LINES):
    """Returns a window of one file of the working tree.

    The window is lines, not bytes: the screen counts in lines, and a window
    that ends mid-line has to be stitched by whoever draws it.
    """
    top = _repo_dir(cwd)
    real = _file(top, path)
    if not os.path.isfile(real):
        raise RepoError("there is no such file in the working tree")
    head = _head(top)
    branch_out, _ = _run(top, "rev-parse", "--abbrev-ref", "HEAD")
    base, _ = base_of(top, _text(branch_out).strip())
    now = revision(top, base, head)
    if rev and rev != now:
        return {"stale": True, "rev": now}

    # The id of the blob travels with it: it is what a coloured copy is kept
    # under in the service, and it changes with the content and nothing else.
    # A name, a size and a timestamp all stay the same across an edit that
    # changes every line.
    try:
        oid_out, _ = _run(top, "hash-object", "--", real)
        oid = _text(oid_out).strip()
    except RepoError:
        oid = ""

    size = os.path.getsize(real)
    if size > MAX_BLOB:
        return {"rev": now, "path": path, "size": size, "oid": oid, "tooBig": True}
    with open(real, "rb") as f:
        raw = f.read(MAX_BLOB)
    if b"\0" in raw[:8000]:
        return {"rev": now, "path": path, "oid": oid, "size": size, "binary": True}

    text = raw.decode("utf-8", "replace").split("\n")
    if text and text[-1] == "":
        text.pop()
    start = max(1, int(first or 1))
    want = max(1, min(int(lines or MAX_LINES), MAX_LINES))
    window = text[start - 1:start - 1 + want]
    return {
        "rev": now,
        "path": path,
        "oid": oid,
        "size": size,
        "first": start,
        "lines": window,
        "total": len(text),
        "more": start - 1 + len(window) < len(text),
    }


def diff(cwd, path, base_named="", rev="", layer=""):
    """Returns the unified diff of one file, as text, for the service to cut up.

    Two diffs make the picture: what the commits of the branch did, and what
    the working tree has done since. They are asked for separately because a
    person reviewing needs to know which is which — "already in a commit" and
    "not yet" are different things to answer for.
    """
    top = _repo_dir(cwd)
    _file(top, path)
    head = _head(top)
    branch_out, _ = _run(top, "rev-parse", "--abbrev-ref", "HEAD")
    branch = _text(branch_out).strip()
    base, how = base_of(top, branch, base_named)
    now = revision(top, base, head)
    if rev and rev != now:
        return {"stale": True, "rev": now}

    out = {"rev": now, "path": path, "base": base, "baseFrom": how}
    if layer in ("", "committed") and base:
        raw, cut = _run(top, "diff", "--full-index", f"{base}...HEAD", "--", path)
        out["committed"] = _text(raw)
        out["committedCut"] = cut
    if layer in ("", "worktree"):
        raw, cut = _run(top, "diff", "--full-index", "HEAD", "--", path)
        text = _text(raw)
        if not text.strip():
            # An untracked file has nothing to diff against; git shows it only
            # when told to treat it as added.
            try:
                raw, cut = _run(top, "diff", "--full-index", "--no-index", "/dev/null", os.path.join(top, path))
            except RepoError:
                cut = False
            else:
                text = _text(raw)
        out["worktree"] = text
        out["worktreeCut"] = cut
    return out


def commit(cwd, sha):
    """Returns who wrote a commit and which conversation it was written in.

    The tie is not a guess: a session leaves its own name in a trailer of the
    commits it writes, and that name is the address of the conversation.
    """
    top = _repo_dir(cwd)
    if not isinstance(sha, str) or not sha or any(c in sha for c in " \t\n:;|&"):
        raise RepoError("that is not a commit name")
    raw, _ = _run(top, "show", "-s", "--format=%H%n%an%n%aI%n%s%n%b", sha)
    text = _text(raw).split("\n")
    if len(text) < 4:
        raise RepoError("there is no such commit")
    session = ""
    for line in text[4:]:
        if line.startswith(SESSION_TRAILER):
            session = line[len(SESSION_TRAILER):].strip()
            break
    return {"hash": text[0], "author": text[1], "at": text[2], "subject": text[3], "session": session}


def answer(request):
    """Dispatches one repo request. Every reply says which operation it answers."""
    if not isinstance(request, dict):
        return {"ok": False, "error": "the repo request is not an object"}
    op = request.get("op")
    cwd = request.get("cwd")
    try:
        if op == "refs":
            out = refs(cwd)
        elif op == "changes":
            out = changes(cwd, str(request.get("base") or ""))
        elif op == "tree":
            out = tree(cwd, str(request.get("path") or ""))
        elif op == "blob":
            out = blob(cwd, str(request.get("path") or ""), str(request.get("rev") or ""),
                       request.get("first") or 1, request.get("lines") or MAX_LINES)
        elif op == "diff":
            out = diff(cwd, str(request.get("path") or ""), str(request.get("base") or ""),
                       str(request.get("rev") or ""), str(request.get("layer") or ""))
        elif op == "commit":
            out = commit(cwd, str(request.get("hash") or ""))
        else:
            return {"ok": False, "error": f"there is no such repo operation: {op!r}"}
    except RepoError as e:
        return {"ok": False, "error": str(e)}
    except (OSError, ValueError) as e:
        return {"ok": False, "error": f"the repository was not read: {e}"}
    return {"ok": True, "op": op, "repo": out}
