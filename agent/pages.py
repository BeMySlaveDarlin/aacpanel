"""Pages: a copy of what a session published, kept so the panel can show it.

An artifact is published into the account the session works under. A person
reading the panel from a phone is signed into one account, and the contours
work under others, so the link on the card opens for whoever happens to match
and refuses everyone else. The page itself is a file on this machine at the
moment it goes out, and a copy of that file costs nothing to keep.

The copy is the page and nothing more. Its supporting files and its uploaded
assets stay where they were published: a page built from several files shows
here without them, and says so rather than pretending to be whole.

Nothing here renders anything, and nothing here reaches out to the network.
"""

import hashlib
import json
import os
import re
import socket
import threading
import time

import paths

SOCKET_DIR = os.environ.get("AACP_PAGE_DIR", os.environ.get("AACP_BRIEF_DIR", "/run/aacpanel-agent"))
SOCKET_NAME = "page.sock"

STORE = os.environ.get("AACP_PAGE_STORE", paths.state("pages"))

# A page is html and grows with what it draws. The ceiling is the point past
# which a phone gains nothing: a document that large is read on a desk, through
# the link, and the copy would cost the shelf its sweep.
MAX_REQUEST = 4 * 1024 * 1024
MAX_HTML = 2 * 1024 * 1024

MAX_ID = 64
MAX_TITLE = 200
MAX_LINE = 400
MAX_FILE = 200

# How much of this the machine keeps. A copy exists to be opened from a phone
# in the days after it was made, not to become an archive of everything every
# session ever drew.
MAX_PAGES = 40
MAX_STORE = 64 * 1024 * 1024
MAX_AGE = 30 * 24 * 3600

ID_RE = re.compile(r"[^a-z0-9-]+")


class Refused(Exception):
    """The page was not taken, and the reason is for the session to read."""


def _text(value, limit):
    if value is None:
        return ""
    return " ".join(str(value).split())[:limit]


def _slug(value):
    out = ID_RE.sub("-", str(value or "").strip().lower()).strip("-")
    return out[:MAX_ID]


def page_id(url, session, path):
    """Returns the name a page is kept under.

    The published address is the same across republishing, so it names the page
    and a reissue lands on the copy it replaces. Without an address the session
    and the file name stand in, which keeps two pages of one session apart.
    """
    tail = ""
    if url:
        tail = _slug(str(url).rstrip("/").rsplit("/", 1)[-1])
    if tail:
        return tail
    seed = f"{session}\n{path}".encode("utf-8")
    return "f-" + hashlib.sha256(seed).hexdigest()[:16]


def clean(payload):
    """Returns the page in the form the shelf keeps, or raises Refused."""
    if not isinstance(payload, dict):
        raise Refused("a page is an object")

    html = payload.get("html")
    if not isinstance(html, str) or not html.strip():
        raise Refused("a page carries the html it was published as")
    if len(html.encode("utf-8")) > MAX_HTML:
        raise Refused(f"the page is larger than {MAX_HTML // (1024 * 1024)} MB and is left to its link")

    session = _text(payload.get("session"), MAX_ID)
    if not session:
        raise Refused("a page carries the session that published it")

    path = _text(payload.get("path"), MAX_LINE)
    url = _text(payload.get("url"), MAX_LINE)
    if url and not url.startswith(("http://", "https://")):
        url = ""

    title = _text(payload.get("title"), MAX_TITLE)
    name = os.path.basename(path)[:MAX_FILE]
    if not title and not name:
        raise Refused("a page carries a title or a file name: it is what the person sees on the card")

    return {
        "id": page_id(url, session, path),
        "session": session,
        "cwd": _text(payload.get("cwd"), MAX_LINE),
        "path": path,
        "file": name,
        "title": title or name,
        "desc": _text(payload.get("desc"), MAX_LINE),
        "icon": _text(payload.get("icon"), 8),
        "url": url,
        "html": html,
        "bytes": len(html.encode("utf-8")),
        "at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


def card(page):
    """Returns what the shelf shows about a page without opening it."""
    return {
        "id": page.get("id", ""),
        "session": page.get("session", ""),
        "cwd": page.get("cwd", ""),
        "file": page.get("file", ""),
        "title": page.get("title", ""),
        "desc": page.get("desc", ""),
        "icon": page.get("icon", ""),
        "url": page.get("url", ""),
        "bytes": int(page.get("bytes") or 0),
        "versions": int(page.get("versions") or 1),
        "at": page.get("at", ""),
    }


class Shelf:
    """Pages on disk, one file each, named by the page.

    One file each for the same reason briefs are: a page runs to megabytes, and
    reading them all to show a list of titles would cost the shelf its speed.
    """

    def __init__(self, path=None):
        self.path = path or STORE
        self.lock = threading.Lock()

    def _file(self, page_name):
        return os.path.join(self.path, page_name + ".json")

    def _read(self, page_name):
        try:
            with open(self._file(page_name), encoding="utf-8") as f:
                data = json.load(f)
        except (OSError, ValueError):
            return None
        return data if isinstance(data, dict) else None

    def put(self, page):
        """Writes a page, keeping the count of how many times it was published."""
        with self.lock:
            try:
                os.makedirs(self.path, exist_ok=True)
            except OSError as e:
                return False, f"the shelf of pages is not writable: {e}"
            was = self._read(page["id"]) or {}
            page = dict(page)
            page["versions"] = int(was.get("versions") or 0) + 1
            if was.get("firstAt"):
                page["firstAt"] = was["firstAt"]
            elif was.get("at"):
                page["firstAt"] = was["at"]
            tmp = self._file(page["id"]) + ".new"
            try:
                with open(tmp, "w", encoding="utf-8") as f:
                    json.dump(page, f, ensure_ascii=False)
                os.replace(tmp, self._file(page["id"]))
            except OSError as e:
                return False, f"the page was not written: {e}"
        return True, ""

    def of(self, page_name):
        """Returns one page whole, or None."""
        name = _slug(page_name) or _text(page_name, MAX_ID)
        if not name or name != str(page_name):
            # A name that had to be cleaned is not the name anything was
            # written under: reading on would open a neighbouring file.
            return None
        return self._read(name)

    def cards(self, session=None):
        """Returns the card of every page, newest first."""
        try:
            names = os.listdir(self.path)
        except OSError:
            return []
        out = []
        for name in names:
            if not name.endswith(".json"):
                continue
            page = self._read(name[: -len(".json")])
            if not page:
                continue
            if session and page.get("session") != session:
                continue
            out.append(card(page))
        out.sort(key=lambda c: str(c.get("at") or ""), reverse=True)
        return out

    def sweep(self):
        """Drops pages by age, by count and by the room they take together."""
        gone = []
        with self.lock:
            try:
                names = [n for n in os.listdir(self.path) if n.endswith(".json")]
            except OSError:
                return gone
            now = time.time()
            kept = []
            for name in names:
                full = os.path.join(self.path, name)
                try:
                    stat = os.stat(full)
                except OSError:
                    continue
                if now - stat.st_mtime > MAX_AGE:
                    gone.append(name)
                    continue
                kept.append((stat.st_mtime, stat.st_size, name))
            kept.sort(reverse=True)
            room = 0
            for i, (_, size, name) in enumerate(kept):
                room += size
                if i >= MAX_PAGES or room > MAX_STORE:
                    gone.append(name)
            for name in gone:
                try:
                    os.unlink(os.path.join(self.path, name))
                except OSError:
                    pass
        return [n[: -len(".json")] for n in gone]


SHELF = Shelf()


def _recv(conn):
    """Reads one request whole: a page does not fit in a single packet."""
    chunks = []
    size = 0
    while True:
        chunk = conn.recv(64 * 1024)
        if not chunk:
            break
        size += len(chunk)
        if size > MAX_REQUEST:
            raise Refused(f"the page is longer than {MAX_REQUEST // (1024 * 1024)} MB")
        chunks.append(chunk)
    return b"".join(chunks)


def handle(conn, shelf=None):
    """Serves one copy and closes the connection."""
    shelf = shelf or SHELF
    with conn:
        try:
            conn.settimeout(20)
            payload = json.loads(_recv(conn).decode("utf-8"))
            page = clean(payload)
            ok, why = shelf.put(page)
            if ok:
                shelf.sweep()
            reply = {"ok": True, "id": page["id"]} if ok else {"ok": False, "error": why}
        except Refused as e:
            reply = {"ok": False, "error": str(e)}
        except (OSError, ValueError, UnicodeDecodeError) as e:
            reply = {"ok": False, "error": f"the page was not parsed: {e}"}
        try:
            conn.sendall(json.dumps(reply, ensure_ascii=False).encode("utf-8"))
        except OSError:
            pass


def listen():
    """Opens a 0600 socket in its own directory."""
    os.makedirs(SOCKET_DIR, exist_ok=True)
    path = os.path.join(SOCKET_DIR, SOCKET_NAME)
    try:
        os.unlink(path)
    except FileNotFoundError:
        pass
    sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    sock.bind(path)
    os.chmod(path, 0o600)
    sock.listen(8)
    return sock


def worker():
    """Runs the thread that takes the copies of published pages."""
    try:
        sock = listen()
    except OSError as e:
        print(f"aacpanel-agent: published pages will not be kept, the socket did not come up: {e}", flush=True)
        return
    print(f"aacpanel-agent: listening for pages on {os.path.join(SOCKET_DIR, SOCKET_NAME)}", flush=True)
    while True:
        try:
            conn, _ = sock.accept()
        except OSError:
            return
        threading.Thread(target=handle, args=(conn,), daemon=True).start()
