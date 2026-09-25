#!/usr/bin/env python3
"""Reading a git repository for the panel: the agent runs git, nothing else does.

The service lives in a container with no rights on the host, and reading a
repository is reading — so it is the agent's work. What goes back is raw: a list
of changed files, the listing of one directory, one blob, the diff of one file.
Cutting a diff into hunks and colouring it happens in the service, where a
mistake costs a redraw rather than a shell command.

The files of a project are there whether git keeps them or not. The tree, a
file and a search by name are read off the disk when there is no repository;
what belongs to a branch — its changes, a diff, a base, a commit — has nothing
to be read from there and says so.

Every window carries the revision it was read at. A repository under an agent
that keeps writing moves between two requests, and a window answered from the
new state while the list came from the old one is a diff nobody can trust: the
answer is "stale" and the screen asks again.
"""

import hashlib
import os
import re
import subprocess
import time
from collections import deque

from sesstate import inside

from .locate import conversation_of

# What one reply may carry. A file past the ceiling is not cut silently: the
# reply says how much there is and the screen says it out loud.
MAX_BLOB = 2 * 1024 * 1024
MAX_DIFF = 2 * 1024 * 1024
MAX_FILES = 3000
MAX_ENTRIES = 2000
MAX_FOUND = 60
MAX_LINES = 20000

GIT_TIMEOUT = 20

# How far a search walks a directory git does not keep. A repository names its
# files in one command; a plain directory is walked, and a project can be a
# home directory. The service stops waiting at ten seconds, so the walk stops
# well before that, and at a count of names that a local disk reads in a
# fraction of a second — a slow mount is what the clock is for.
MAX_WALKED = 50000
WALK_SECONDS = 3.0

# Directories a search does not walk into. A tool fills them, a person does not
# write them: tens of thousands of names nobody types, which would spend the
# ceiling above before the walk reached the files a person wrote. A repository
# answers the same question with its ignore list; a plain directory has none to
# read. The tree still lists them and opens them by hand.
UNWALKED = frozenset((".git", "node_modules", ".venv", "venv", "__pycache__"))

# The trailer a session leaves in the commits it writes. It is what ties a line
# of code to the conversation it was written in.
SESSION_TRAILER = "Claude-Session:"


class RepoError(Exception):
    """A refusal a person can act on: a bad directory, a name git will not take."""


def _run(cwd, *args, limit=MAX_DIFF):
    """Runs one git command in cwd and returns its output, or raises RepoError."""
    # The output of git is read, not shown: under the language of the host its
    # messages arrive translated, and a reply parsed by its words comes apart.
    # The panel also speaks one language of its own, and a refusal in another
    # one on its screen is a refusal from somewhere else.
    env = dict(os.environ, LC_ALL="C", LANG="C", GIT_PAGER="cat")
    try:
        done = subprocess.run(
            ("git", "-C", cwd, "--no-pager", *args),
            capture_output=True,
            timeout=GIT_TIMEOUT,
            env=env,
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


class NotARepo(Exception):
    """A directory that simply is not a repository.

    Not a failure: a project can be a shelf of notes or a stand, and a panel
    that answers "fatal: not a git repository" to a directory that never
    claimed to be one is a panel shouting at its own screen.
    """

    def __init__(self, path):
        super().__init__(path)
        self.path = path


def _repo_dir(cwd):
    """Returns the working tree cwd belongs to, or raises when it is not one."""
    if not isinstance(cwd, str) or not cwd:
        raise RepoError("no directory was named")
    real = os.path.realpath(os.path.expanduser(cwd))
    if not os.path.isdir(real):
        raise RepoError("there is no such directory on the host")
    try:
        top, _ = _run(real, "rev-parse", "--show-toplevel")
    except RepoError:
        raise NotARepo(real)
    return _text(top).strip() or real


def _root(cwd):
    """Returns the directory the files are read from, and whether git keeps it.

    The files do not need git; only what is said about them does. A directory
    that is not a repository is read as it lies on the disk.
    """
    try:
        return _repo_dir(cwd), True
    except NotARepo as e:
        return e.path, False


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
    """Returns the commit HEAD stands on, or "" in a repository with no commit yet.

    A repository just made with `git init` names a branch that does not exist
    yet. It is still a repository — its files are all new, and that is what
    the screen shows — not a refusal to open the directory.
    """
    try:
        out, _ = _run(cwd, "rev-parse", "--verify", "--quiet", "HEAD")
    except RepoError:
        return ""
    return _text(out).strip()


def _branch(cwd):
    """Returns the name of the branch HEAD is on, the unborn one included; "HEAD" when detached."""
    try:
        out, _ = _run(cwd, "symbolic-ref", "--quiet", "--short", "HEAD")
    except RepoError:
        return "HEAD"
    return _text(out).strip() or "HEAD"


def _against(cwd, head):
    """Returns what the working tree is measured against: HEAD, or nothing at all before the first commit."""
    if head:
        return head
    out, _ = _run(cwd, "hash-object", "-t", "tree", "/dev/null")
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

    return {"root": top, "branch": _branch(top), "branches": branches, "worktrees": trees}


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
    branch = _branch(top)
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

    # A branch with no commit has committed nothing, whatever the base.
    if base and head:
        numstat("committed", f"{base}...HEAD")
    numstat("worktree", _against(top, head))

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


def find(cwd, query):
    """Returns the paths of the working tree whose names carry what was typed.

    The whole tree by name, where the tree above is one directory at a time: a
    name is what a person has in their head when they know the file and not
    where it sits, and walking down to it by hand is the thing this answers
    instead of.
    """
    top, kept = _root(cwd)
    want = query.strip().lower()
    if not want:
        return {"root": top, "query": "", "paths": [], "total": 0, "cut": False}

    partial = False
    if kept:
        out, _ = _run(top, "ls-files", "-z", limit=8 * 1024 * 1024)
        paths = [p for p in _text(out).split("\0") if p]
        out, _ = _run(top, "ls-files", "--others", "--exclude-standard", "-z")
        paths += [p for p in _text(out).split("\0") if p]
    else:
        paths, partial = _walk(top)

    hits = [p for p in dict.fromkeys(paths) if want in p.lower()]
    # What was typed is a name, so a file whose own name carries it comes
    # before one that only matches somewhere up its directories; after that the
    # shorter path is the likelier answer.
    hits.sort(key=lambda p: (want not in os.path.basename(p).lower(), len(p), p))
    out = {"root": top, "query": query, "paths": hits[:MAX_FOUND],
           "total": len(hits), "cut": len(hits) > MAX_FOUND or partial}
    if partial:
        out["partial"] = True
    return out


def _walk(top):
    """Returns the files under a directory git does not keep, and whether the walk stopped short.

    Breadth first: when a ceiling stops the walk, what is left out is the
    deepest part of the tree rather than whichever directory happened to sort
    last, and the shallow path is the likelier answer anyway. Links to
    directories are not followed — a link can lead out of the project or back
    into itself, and the tree refuses the first one on its own.
    """
    paths = []
    queue = deque([""])
    seen = 0
    deadline = time.monotonic() + WALK_SECONDS
    while queue:
        rel = queue.popleft()
        try:
            with os.scandir(os.path.join(top, rel) if rel else top) as found:
                entries = sorted(found, key=lambda e: e.name)
        except OSError as e:
            if not rel:
                raise RepoError(f"the directory was not read: {e.strerror or e}")
            # A directory the agent may not read is one the tree cannot open
            # either: there is nothing in it a person could be sent to.
            continue
        for entry in entries:
            if seen >= MAX_WALKED:
                return paths, True
            seen += 1
            path = f"{rel}/{entry.name}" if rel else entry.name
            try:
                if entry.is_dir(follow_symlinks=False):
                    if entry.name not in UNWALKED:
                        queue.append(path)
                    continue
                if entry.is_dir():
                    # A link to a directory: not walked, and not a file either
                    # — opened, it would find no file there.
                    continue
            except OSError:
                continue
            paths.append(path)
        if queue and time.monotonic() > deadline:
            return paths, True
    return paths, False


def tree(cwd, path=""):
    """Returns the entries of one directory of the working tree.

    One directory at a time, not the repository flattened into a window: a tree
    is walked by opening what is asked for, and a listing of everything is a
    number nobody reads and a reply nobody needs.
    """
    top, kept = _root(cwd)
    where = path.strip("/")
    real = _file(top, where) if where else top
    if not os.path.isdir(real):
        raise RepoError("that path is not a directory")
    if not kept:
        return _listing(top, where, real)

    spec = f"{where}/" if where else ""
    out = b""
    if _head(top):
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

    return _entries(top, where, entries)


def _listing(top, where, real):
    """Returns one directory of a project git does not keep, as the disk has it.

    Nothing in it is marked untracked: there is nothing here that tracks. And
    a directory that cannot be read is a refusal rather than an empty listing,
    since the disk is the only listing there is.
    """
    try:
        names = os.listdir(real)
    except OSError as e:
        raise RepoError(f"the directory was not read: {e.strerror or e}")
    entries = {}
    for name in names:
        if name == ".git":
            continue
        entries[name] = {"name": name, "dir": os.path.isdir(os.path.join(real, name))}
    return _entries(top, where, entries)


def _entries(top, where, entries):
    """Returns the rows of one directory, directories first, under the ceiling."""
    rows = sorted(entries.values(), key=lambda r: (not r["dir"], r["name"]))
    return {"root": top, "path": where, "entries": rows[:MAX_ENTRIES], "total": len(rows),
            "cut": len(rows) > MAX_ENTRIES}


def _oid(real):
    """Returns the id git gives a file of this content, counted without git.

    The id of the blob travels with it: it is what a coloured copy is kept
    under in the service, and it changes with the content and nothing else. A
    name, a size and a timestamp all stay the same across an edit that changes
    every line. Counted here in the form git uses, a file reads under the same
    key whether a repository keeps it or not.
    """
    digest = hashlib.sha1()
    try:
        with open(real, "rb") as f:
            digest.update(b"blob %d\0" % os.fstat(f.fileno()).st_size)
            for chunk in iter(lambda: f.read(1024 * 1024), b""):
                digest.update(chunk)
    except OSError:
        return ""
    return digest.hexdigest()


def blob(cwd, path, rev="", first=1, lines=MAX_LINES):
    """Returns a window of one file of the working tree.

    The window is lines, not bytes: the screen counts in lines, and a window
    that ends mid-line has to be stitched by whoever draws it.
    """
    top, kept = _root(cwd)
    real = _file(top, path)
    if not os.path.isfile(real):
        raise RepoError("there is no such file in the working tree")
    # A directory git does not keep has no revision: no list of changes was
    # read from it, so there is nothing a window could have fallen behind.
    now = ""
    if kept:
        head = _head(top)
        base, _ = base_of(top, _branch(top))
        now = revision(top, base, head)
        if rev and rev != now:
            return {"stale": True, "rev": now}

    oid = _oid(real)
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
    base, how = base_of(top, _branch(top), base_named)
    now = revision(top, base, head)
    if rev and rev != now:
        return {"stale": True, "rev": now}

    out = {"rev": now, "path": path, "base": base, "baseFrom": how}
    if layer in ("", "committed") and base and head:
        raw, cut = _run(top, "diff", "--full-index", f"{base}...HEAD", "--", path)
        out["committed"] = _text(raw)
        out["committedCut"] = cut
    if layer in ("", "worktree"):
        raw, cut = _run(top, "diff", "--full-index", _against(top, head), "--", path)
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
    commits it writes, and that name is the address of the conversation. A
    commit without the trailer was written by hand, and no conversation is
    made up for it.
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
    out = {"hash": text[0], "author": text[1], "at": text[2], "subject": text[3], "session": session}
    found = conversation_of(session, near=top) if session else None
    if found:
        out["conversation"] = found
    return out


BLAME_HEAD_RE = re.compile(r"^([0-9a-f]{40}) \d+ \d+")
UNCOMMITTED = "0" * 40
# One show for every commit of a window: the unit and record separators keep a
# subject with a newline or a trailer with a comma from splitting a record.
BLAME_FORMAT = "%H%x1f%an%x1f%aI%x1f%s%x1f%(trailers:key=Claude-Session,valueonly,separator=%x2C)%x1e"


def blame(cwd, path, rev="", first=1, lines=MAX_LINES):
    """Returns which commit last wrote each line of a window of one file.

    The window is the one the viewer reads the file by. A line the working
    tree wrote and nobody has committed belongs to no commit. The commits come
    once each, with the conversation they name; which conversation that is, is
    looked up when a line is opened, not for the whole window.
    """
    top = _repo_dir(cwd)
    real = _file(top, path)
    if not os.path.isfile(real):
        raise RepoError("there is no such file in the working tree")
    head = _head(top)
    base, _ = base_of(top, _branch(top))
    now = revision(top, base, head)
    if rev and rev != now:
        return {"stale": True, "rev": now}

    with open(real, "rb") as f:
        raw = f.read(MAX_BLOB)
    total = raw.count(b"\n") + (1 if raw and not raw.endswith(b"\n") else 0)
    start = max(1, int(first or 1))
    want = max(1, min(int(lines or MAX_LINES), MAX_LINES))
    if start > max(total, 1):
        return {"rev": now, "path": path, "first": start, "blame": [], "commits": {}, "total": total}
    last = min(start + want - 1, max(total, 1))

    raw, _ = _run(top, "blame", "--porcelain", "-L", f"{start},{last}", "--", path)
    shas = []
    for line in _text(raw).split("\n"):
        found = BLAME_HEAD_RE.match(line)
        if found:
            shas.append(found.group(1))

    commits = {}
    known = sorted({s for s in shas if s != UNCOMMITTED})
    if known:
        raw, _ = _run(top, "show", "-s", "--no-patch", f"--format={BLAME_FORMAT}", *known)
        for record in _text(raw).split("\x1e"):
            parts = record.strip("\n").split("\x1f")
            if len(parts) != 5:
                continue
            sha, author, at, subject, session = parts
            commits[sha] = {"author": author, "at": at, "subject": subject,
                            "session": session.split(",")[0].strip()}
    return {
        "rev": now,
        "path": path,
        "first": start,
        "blame": ["" if s == UNCOMMITTED else s for s in shas],
        "commits": commits,
        "total": total,
    }


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
        elif op == "find":
            out = find(cwd, str(request.get("query") or ""))
        elif op == "blob":
            out = blob(cwd, str(request.get("path") or ""), str(request.get("rev") or ""),
                       request.get("first") or 1, request.get("lines") or MAX_LINES)
        elif op == "diff":
            out = diff(cwd, str(request.get("path") or ""), str(request.get("base") or ""),
                       str(request.get("rev") or ""), str(request.get("layer") or ""))
        elif op == "commit":
            out = commit(cwd, str(request.get("hash") or ""))
        elif op == "blame":
            out = blame(cwd, str(request.get("path") or ""), str(request.get("rev") or ""),
                        request.get("first") or 1, request.get("lines") or MAX_LINES)
        else:
            return {"ok": False, "error": f"there is no such repo operation: {op!r}"}
    except NotARepo as e:
        return {"ok": True, "op": op, "repo": {"noRepo": True, "root": e.path}}
    except RepoError as e:
        return {"ok": False, "error": str(e)}
    except (OSError, ValueError) as e:
        return {"ok": False, "error": f"the repository was not read: {e}"}
    return {"ok": True, "op": op, "repo": out}
