#!/usr/bin/env python3
"""Builds index.html out of index.src.html: {{scene:name}} -> scenes/name.html.

A scene is pasted raw, so a scene with one closing tag too many climbs out of
its frame and eats the rest of the page. Every scene is checked before it is
stamped: one that has no file yet, or does not close what it opens, becomes a
marked placeholder instead, and the build says so and ends unhappy.
"""
import html
import pathlib
import re
import sys
from html.parser import HTMLParser

VOID = {"area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta",
        "param", "source", "track", "wbr"}


class Balance(HTMLParser):
    """Says what a fragment leaves open, and what it closes that it never opened."""

    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.open, self.faults = [], []

    def handle_starttag(self, tag, attrs):
        if tag not in VOID:
            self.open.append(tag)

    def handle_startendtag(self, tag, attrs):
        pass                                    # <path/>, <stop/>: opened and closed at once

    def handle_endtag(self, tag):
        if tag in VOID:
            return
        if tag not in self.open:
            self.faults.append(f"</{tag}> closes what was never opened")
        else:
            while self.open.pop() != tag:
                pass

    def verdict(self):
        if self.open:
            self.faults.append("left open: " + " ".join(f"<{t}>" for t in self.open))
        return self.faults


here = pathlib.Path(__file__).parent
src = (here / "index.src.html").read_text(encoding="utf-8")
used, missing, broken = set(), set(), {}


def stamp(m):
    name = m.group(1)
    used.add(name)
    scene = here / "scenes" / f"{name}.html"
    if not scene.is_file():
        missing.add(name)
        return f'<div class="placeholder">scene: {html.escape(name)}</div>'
    text = scene.read_text(encoding="utf-8").strip()
    check = Balance()
    check.feed(text)
    faults = check.verdict()
    if faults:
        broken[name] = faults
        return f'<div class="placeholder broken">scene: {html.escape(name)} — tags</div>'
    return text


out = re.sub(r"\{\{scene:([\w-]+)\}\}", stamp, src)
(here / "index.html").write_text(out, encoding="utf-8")

have = {p.stem for p in (here / "scenes").glob("*.html")}
print(f"built index.html — {len(out)} bytes, {len(used)} slots")
if missing:
    print("no file yet:", " ".join(sorted(missing)))
if have - used:
    print("scenes nobody asks for:", " ".join(sorted(have - used)))
for name, faults in sorted(broken.items()):
    print(f"BROKEN {name}.html: {'; '.join(faults)}", file=sys.stderr)
sys.exit(1 if broken else 0)
