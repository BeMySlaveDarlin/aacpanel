#!/usr/bin/env python3
"""Checks incremental parsing: two passes against one over live transcripts."""

import argparse
import os
import random
import sys
import tempfile
import time

if __name__ == "__main__" and __package__ is None:
    sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
    __package__ = "usage"

from .parse import parse_file  # noqa: E402
from .scan import roots, scan_list  # noqa: E402

KEY_ROW = lambda r: (r["bucket"], r["model"], r["agent"], r["speed"], r["serviceTier"])  # noqa: E731
KEY_TOOL = lambda r: (r["bucket"], r["agent"], r["tool"])  # noqa: E731
KEY_EVENT = lambda r: (r["bucket"], r["agent"])  # noqa: E731
KEYS = (("rows", KEY_ROW, 2), ("tools", KEY_TOOL, 1), ("events", KEY_EVENT, 1))

COLD_SECONDS = 36 * 3600
MIN_SIZE = 1_000_000


def apply(db, parsed, rewrite):
    """Writes a batch the way the store does it: drop its own rows, then replace."""
    agents = set()
    for name, _, _ in KEYS:
        agents |= {r["agent"] for r in getattr(parsed, name)}
    for name, key, pos in KEYS:
        for k in list(db[name]):
            if k[pos] in agents and (rewrite or (parsed.since and k[0] >= parsed.since)):
                del db[name][k]
        for r in getattr(parsed, name):
            db[name][key(r)] = r


def replay(path, cut, tmpdir):
    """Returns the batches that differ for one file and one cut, or an empty list."""
    full = parse_file(path)
    tmp = os.path.join(tmpdir, "cut.jsonl")
    with open(path, "rb") as src, open(tmp, "wb") as dst:
        dst.write(src.read(cut))
    before = parse_file(tmp)
    after = parse_file(path, before.offset)

    db = {name: {} for name, _, _ in KEYS}
    apply(db, before, True)
    if after.rewind:
        apply(db, parse_file(path), True)
    else:
        apply(db, after, before.offset == 0)

    bad = []
    for name, key, _ in KEYS:
        if db[name] != {key(r): r for r in getattr(full, name)}:
            bad.append(name)
    return bad


def cuts_of(path, count):
    """Returns cut points inside the file, always on a line boundary."""
    with open(path, "rb") as f:
        data = f.read()
    out = []
    for i in range(1, count + 1):
        nl = data.find(b"\n", len(data) * i // (count + 1))
        if 0 <= nl < len(data) - 1:
            out.append(nl + 1)
    return out


def pick(limit, seed):
    """Returns live transcripts fit for the check, the largest ones first."""
    edge = time.time() - COLD_SECONDS
    fit = []
    for it in scan_list(roots()):
        if it["size"] < MIN_SIZE:
            continue
        try:
            if os.stat(it["path"]).st_mtime > edge:
                continue
        except OSError:
            continue
        fit.append(it)
    if seed:
        random.Random(seed).shuffle(fit)
        return fit[:limit]
    fit.sort(key=lambda it: -it["size"])
    if limit >= len(fit):
        return fit
    step = len(fit) / limit
    return [fit[int(i * step)] for i in range(limit)]


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--files", type=int, default=40, help="how many files to take")
    parser.add_argument("--cuts", type=int, default=3, help="how many cut points per file")
    parser.add_argument("--seed", type=int, default=0,
                        help="a random pick instead of the largest")
    args = parser.parse_args(argv)

    files = pick(args.files, args.seed)
    if not files:
        print("SKIPPED: no live transcripts were found on this machine "
              "(looked in %s)." % ", ".join(d for _, d in roots()))
        print("         The incremental pass and the read boundary are NOT checked: "
              "a cut in live records is reproduced by nothing else.")
        return 0

    started = time.time()
    parses = 0
    broken = []
    with tempfile.TemporaryDirectory(prefix="aacpanel-replay-") as tmpdir:
        for it in files:
            for cut in cuts_of(it["path"], args.cuts):
                parses += 1
                trouble = replay(it["path"], cut, tmpdir)
                if trouble:
                    broken.append((it["path"], cut, trouble))

    took = time.time() - started
    if broken:
        print("DIVERGENCES: %d parses out of %d" % (len(broken), parses))
        for path, cut, trouble in broken[:10]:
            print("  %s cut at %d: %s" % (path, cut, ", ".join(trouble)))
        if len(broken) > 10:
            print("  … and %d more" % (len(broken) - 10))
        return 1
    print("the incremental pass agrees with the straight one: %d parses over %d files, %.1f s"
          % (parses, len(files), took))
    return 0


if __name__ == "__main__":
    sys.exit(main())
