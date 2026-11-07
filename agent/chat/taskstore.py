"""Read bounded pages of the file registry through the collector's chat socket."""
import json
import os
from pathlib import Path

from .repo import RepoError, _run, _text

PAGE_BYTES = 2 << 20
FILE_BYTES = 2 << 20


def root_of(cwd):
    """Linked worktrees share the store beside the repository's common git dir."""
    if not isinstance(cwd, str) or not os.path.isabs(cwd) or not os.path.isdir(cwd):
        raise ValueError("the task store needs an existing absolute directory")
    real = os.path.realpath(cwd)
    try:
        raw, _ = _run(real, "rev-parse", "--path-format=absolute", "--git-common-dir")
    except RepoError:
        return Path(real)
    return Path(_text(raw).strip()).parent


def answer(request):
    try:
        root = root_of(request.get("cwd"))
        after = request.get("after") or ""
        if not isinstance(after, str):
            raise ValueError("the task store cursor must be text")
        store = root / ".claude" / "session"
        out = {"root": str(root), "present": store.is_dir(), "meta": None,
               "tasksComplete": False, "handoffsComplete": False, "files": [], "errors": []}
        if not out["present"]:
            return {"ok": True, "taskStore": out}
        try:
            out["meta"] = json.loads((store / "meta.json").read_text(encoding="utf-8"))
        except FileNotFoundError:
            pass
        except (OSError, ValueError) as e:
            out["errors"].append(f"meta.json: {type(e).__name__}")
        names = []
        for directory, flag in (("tasks", "tasksComplete"), ("handoff", "handoffsComplete")):
            try:
                entries = list((store / directory).iterdir())
                names.extend(f"{directory}/{p.name}" for p in entries if p.suffix == ".json")
                out[flag] = True
            except FileNotFoundError:
                # A missing directory is not evidence that every source file
                # was deleted: a move or a new store can leave it absent.
                pass
            except OSError as e:
                out["errors"].append(f"{directory}: {type(e).__name__}")
        size = 0
        for name in sorted(names):
            if name <= after:
                continue
            file = {"name": name, "raw": ""}
            try:
                path = store / name
                if path.is_symlink():
                    raise ValueError("a store file must not be a symlink")
                with path.open("rb") as source:
                    raw = source.read(FILE_BYTES + 1)
                if len(raw) > FILE_BYTES:
                    raise ValueError("the store file exceeds the size limit")
                file["raw"] = raw.decode("utf-8")
            except (OSError, ValueError) as e:
                file["error"] = f"{type(e).__name__}: the store file was not read"
            cost = len(json.dumps(file, ensure_ascii=False).encode("utf-8"))
            if cost > PAGE_BYTES:
                file = {"name": name, "raw": "", "error": "the encoded store file exceeds the page limit"}
                cost = len(json.dumps(file).encode("utf-8"))
            if out["files"] and size + cost > PAGE_BYTES:
                out["next"] = out["files"][-1]["name"]
                break
            size += cost
            out["files"].append(file)
        return {"ok": True, "taskStore": out}
    except (OSError, ValueError) as e:
        return {"ok": False, "error": f"the task store was not read: {e}"}
