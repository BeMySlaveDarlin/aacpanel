"""Import every module of the collector, the way the service starts it.

The collector runs from the working tree, so a restart is a release: a name
undefined at module level does not break one screen, it keeps the service from
starting at all. Tests do not catch that — they import what they need. make
check runs this, and so does the installer before it restarts the collector.

    python3 agent/importcheck.py [DIR]

DIR is the directory of the modules, the one this file lies in by default.
Exits 1 and names each module that does not import.
"""

import importlib
import importlib.util
import pathlib
import sys
import traceback


def failures(here):
    """The modules of here that do not import, a line each."""
    here = here.resolve()
    sys.path.insert(0, str(here))
    try:
        return _failures(here)
    finally:
        sys.path.remove(str(here))


def _failures(here):
    bad = []
    for path in sorted(here.glob("*.py")):
        if path.name.startswith("test_") or path.name == "importcheck.py":
            continue
        try:
            if "-" in path.stem:
                spec = importlib.util.spec_from_file_location(path.stem.replace("-", "_"), path)
                spec.loader.exec_module(importlib.util.module_from_spec(spec))
            else:
                importlib.import_module(path.stem)
        except BaseException as err:  # a module may fail any way at all, exit included
            where = ""
            if not getattr(err, "filename", None):
                frames = [f for f in traceback.extract_tb(err.__traceback__)
                          if here in pathlib.Path(f.filename).resolve().parents]
                if frames:
                    where = " (%s:%s)" % (pathlib.Path(frames[-1].filename).resolve().relative_to(here),
                                          frames[-1].lineno)
            bad.append("%s: %s: %s%s" % (path.name, type(err).__name__, err, where))
    return bad


def main(argv):
    here = pathlib.Path(argv[1]) if len(argv) > 1 else pathlib.Path(__file__).parent
    bad = failures(here)
    if bad:
        print("!! the collector will not come up - these modules do not import:")
        for line in bad:
            print("   " + line)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
