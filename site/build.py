#!/usr/bin/env python3
"""Stamps the landing: index.src.html -> index.html.

A phone keeps a stylesheet or a picture it has seen under the same address,
so every address of a file of the page gets ?v=<the first 10 hex of its
sha256>: a changed file is a new address, an unchanged one stays cached.

The stamp reads the src and href of every tag, the url() of the page's own
<style> (the fonts live there for this reason) and the og:image, which names
the page's own domain. style.css holds no url() of a local file, since
nothing would stamp it there.

The build ends with 1 and names the fault when an address leads to no file,
when style.css carries a local url(), or when the page or its stylesheet
carry a path of some machine.

    python3 site/build.py
"""
import hashlib
import pathlib
import re
import sys

HERE = pathlib.Path(__file__).resolve().parent
SITE = "https://aacpanel.bemyslavedarlin.ru/"

# What a public page must not carry: a path of the machine it was made on.
FOREIGN = ("/home/", "/opt/")

ATTR = re.compile(r'\b(src|href|content)="([^"]*)"')
URL = re.compile(r'url\((["\']?)([^)"\']+)\1\)')
STYLE = re.compile(r"<style>.*?</style>", re.S)


def local(address):
    """The file of the page an address names, or None for one that is not a file of it."""
    path = address.split("?", 1)[0].split("#", 1)[0]
    if path.startswith(SITE):
        path = path[len(SITE):]
    elif re.match(r"^[a-z][a-z0-9+.-]*:", path) or path.startswith("//"):
        return None
    return path.lstrip("/") or None


class Stamp:
    def __init__(self):
        self.faults, self.stamped = [], {}

    def address(self, address):
        path = local(address)
        if path is None:
            return address
        file = HERE / path
        if not file.is_file():
            self.faults.append(f"{address}: there is no site/{path}")
            return address
        digest = hashlib.sha256(file.read_bytes()).hexdigest()[:10]
        self.stamped[path] = digest
        base = address.split("?", 1)[0].split("#", 1)[0]
        return f"{base}?v={digest}"

    def page(self, text):
        # content= holds words as often as addresses; only one naming the
        # page's own domain is taken for an address.
        def attr(m):
            name, value = m.group(1), m.group(2)
            if name == "content" and not value.startswith(SITE):
                return m.group(0)
            return f'{name}="{self.address(value)}"'

        def style(m):
            return URL.sub(lambda u: f"url({u.group(1)}{self.address(u.group(2))}{u.group(1)})", m.group(0))

        return ATTR.sub(attr, STYLE.sub(style, text))


def main():
    stamp = Stamp()
    source = (HERE / "index.src.html").read_text(encoding="utf-8")
    css = (HERE / "style.css").read_text(encoding="utf-8")
    for name, text in (("index.src.html", source), ("style.css", css)):
        for word in FOREIGN:
            if word in text:
                stamp.faults.append(f"{name} carries {word}")
    for m in URL.finditer(css):
        if local(m.group(2)) is not None:
            stamp.faults.append(f"style.css: url({m.group(2)}) is not stamped there; declare it in the page's <style>")
    page = stamp.page(source)
    if stamp.faults:
        for fault in stamp.faults:
            print(f"build: {fault}", file=sys.stderr)
        return 1
    (HERE / "index.html").write_text(page, encoding="utf-8")
    for path, digest in sorted(stamp.stamped.items()):
        print(f"build: {path} ?v={digest}")
    print(f"build: site/index.html — {len(page.encode())} B, {len(stamp.stamped)} files stamped")
    return 0


if __name__ == "__main__":
    sys.exit(main())
