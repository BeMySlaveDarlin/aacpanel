#!/usr/bin/env python3
"""Builds the landing: index.src.html -> index.html.

The boards are data (BOARDS below): each names the asset on its stage and the
tiles of its two lanes, in a hand and at a desk. An asset is one of three kinds:

  scene  a live DOM scene, scenes/<id>.html, pasted raw;
  shot   a picture: shots/<id>.webp cut to the tile, and shots/<full>-full.webp,
         the whole frame, which the page loads only when the shot is taken
         onto the stage;
  drawn  drawn/<name>.html, a screen no stand can show: a lock screen, an ssh
         terminal, a scheme.

A scene with one closing tag too many climbs out of its frame and eats the rest
of the page, so every pasted piece is checked for balance first. A missing or
broken file still leaves a plain box naming it, and the build names every such
slot and ends with 1: a page with a hole in it is not one to publish.

    python3 site/build.py                   the page, strict, into site/index.html
    python3 site/build.py --stand-ins DIR   a preview: what site/ lacks is taken
                                            from DIR/scenes and DIR/shots, and the
                                            page goes to DIR/preview/, never into
                                            site/, so a page on stand-ins is never
                                            the one committed
"""
import argparse
import hashlib
import html
import pathlib
import re
import sys
from html.parser import HTMLParser

HERE = pathlib.Path(__file__).resolve().parent

VOID = {"area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta",
        "param", "source", "track", "wbr"}

# The stand's home directory is nobody's business on a public page.
STAND_HOME = ("/home/demo", "~")

# What a public page must not carry even after the stand's home is replaced: a
# path of some other machine, or an address that only works on it.
FOREIGN = ("/home/", "localhost")


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


def webp_size(path):
    """The pixel size a WebP file declares in its first chunk, or None."""
    head = path.read_bytes()[:30]
    if len(head) < 30 or head[:4] != b"RIFF" or head[8:12] != b"WEBP":
        return None
    chunk = head[12:16]
    if chunk == b"VP8X":
        return 1 + int.from_bytes(head[24:27], "little"), 1 + int.from_bytes(head[27:30], "little")
    if chunk == b"VP8L":
        bits = int.from_bytes(head[21:25], "little")
        return (bits & 0x3FFF) + 1, ((bits >> 14) & 0x3FFF) + 1
    if chunk == b"VP8 ":
        return int.from_bytes(head[26:28], "little") & 0x3FFF, int.from_bytes(head[28:30], "little") & 0x3FFF
    return None


# ── the assets ─────────────────────────────────────────────────────────────
PHONE, DESK = "phone", "desk"

# The pixels of a picture: a tile shows a crop cut ahead, square for a phone
# and 16:10 for a desk; the stage shows the whole frame, a 393x852 phone at
# twice its size or the 1600x1000 desk.
TILE = {PHONE: (480, 480), DESK: (760, 475)}
FULL = {PHONE: (786, 1704), DESK: (1600, 1000)}

# A scene or a drawn screen is laid out at its own size, 393 px wide for a
# phone and 1600 for the desk, and a frame shows a window into it, in those
# pixels: crop=(x, y, size) is the square (phone) or 16:10 (desk) a tile shows,
# stage=(x, y, size) the 4:3 part of a desk a phone-wide page shows on the
# stage. A shot's tile is cut ahead, so a shot has only the stage window.
DESK_STAGE = (133, 0, 1333)

# The picture a link to the page unfurls into; nothing on the page shows it.
OG = "shots/og.webp"

A = {}


def asset(key, kind, dev, cap, **kw):
    a = dict(key=key, kind=kind, dev=dev, cap=cap, src=key)
    if kind == "shot":
        a.update(full=key, tile=TILE[dev], full_size=FULL[dev])
    if dev == DESK:
        a.update(stage=DESK_STAGE)
    a.update(kw)
    A[key] = a


# the stages: live scenes
asset("container", "scene", PHONE, "A restart names its cost", crop=(0, 459, 393))
asset("feed", "scene", PHONE, "The feed, as it happens", crop=(0, 380, 393))
asset("P2", "scene", PHONE, "Slash commands", crop=(0, 380, 393))
asset("P1", "scene", PHONE, "One tap: after the backup", crop=(0, 340, 393))
asset("terminal", "scene", PHONE, "Its own tmux", crop=(0, 95, 393))
asset("P19", "scene", PHONE, "Panel tools in a turn", crop=(0, 330, 393))
asset("D4", "scene", DESK, "Code, diff, who wrote it", crop=(560, 40, 1040))
asset("D1", "scene", DESK, "Home at a desk", crop=(0, 40, 1280))
asset("D11", "scene", DESK, "The map in three columns", crop=(0, 40, 1280))
asset("P13", "scene", PHONE, "Passkey sign-in", crop=(0, 150, 393))

# the hero: the question on the desk, a window 900 px wide on a phone-wide page
asset("hero-desk", "scene", DESK, "At a desk", src="D2", stage=(300, 120, 900))

# drawn
asset("P28", "drawn", PHONE, "Over ssh", src="p28")
asset("X1-ask", "drawn", PHONE, "The push, on a locked phone", src="x1-ask", crop=(0, 100, 393))
asset("X1-call", "drawn", PHONE, "“Wake me”, as a push", src="x1-call", crop=(0, 100, 393))
asset("X4", "drawn", PHONE, "When the panel is down", src="x4", crop=(0, 20, 393))
asset("scheme", "drawn", DESK, "Three processes", src="scheme")

# shots: phone
for key, cap in [
    ("containers", "Stacks, and a live log"),
    ("machine", "The host's load"),
    ("P21", "Top processes"),
    ("P22", "Probes"),
    ("P11", "Alerts"),
    ("sessions", "Who needs you"),
    ("feed-team", "A subagent argues back"),
    ("feed-mail", "Letters and files"),
    ("calls", "The calls behind a turn"),
    ("subagents", "Its subagents"),
    ("new-work", "Background work, an alarm"),
    ("P25", "A subagent's own feed"),
    ("new-model", "Pick the model"),
    ("new-mode", "The permission mode"),
    ("P3", "Photos and files"),
    ("P4", "Dictation"),
    ("P5", "/btw, on the side"),
    ("permit", "Permission, from the page"),
    ("new-permitted", "Answered, in the feed"),
    ("P10", "A brief for later"),
    ("P12", "Which pushes"),
    ("new-session", "Where it lives"),
    ("P18", "MCP servers"),
    ("P20", "Close, then kill"),
    ("P29", "A letter from billing-api"),
    ("new-checklist", "The checklist it keeps"),
    ("P7", "What the branch changed"),
    ("P8", "A diff in two layers"),
    ("P9", "Who wrote this line"),
    ("P26", "A page it made"),
    ("archive", "The archive, resume"),
    ("usage", "Where the tokens went"),
    ("P27", "/context, broken down"),
    ("P17", "The map"),
    ("P6", "A project's page"),
    ("new-contour", "A contour's defaults"),
    ("new-project", "The next launch, previewed"),
    ("P14", "Ways in"),
    ("P15", "Devices"),
    ("P16", "The journal"),
]:
    asset(key, "shot", PHONE, cap)

# shots: desk. desk-work and desk-rings are two windows into one desk frame,
# so both take that frame whole onto the stage.
for key, cap, kw in [
    ("D8", "The log beside the list", {}),
    ("D9", "Processes on the right", {}),
    ("D16", "Alerts, “silent N”", {}),
    ("desk-work", "The work column", dict(full="desk", stage=(360, 52, 1240))),
    ("D17", "The Files tab", {}),
    ("D5", "Commands, described", {}),
    ("D2", "The question, as a window", {}),
    ("D3", "Permission under the talk", {}),
    ("D13", "A brief, full width", {}),
    ("D6", "The session popover", {}),
    ("D7", "The Terminal tab", {}),
    ("D14", "The archive panel", {}),
    ("D10", "Usage, crumb by crumb", {}),
    ("desk-rings", "Limit rings by contour", dict(full="desk", stage=(0, 52, 700))),
    ("D12", "Launch settings", {}),
    ("D15", "The journal panel", {}),
]:
    asset(key, "shot", DESK, cap, **kw)

# ── the boards, in the order of the night ──────────────────────────────────
BOARDS = [
    dict(id="machine", phase="evening", title="Keep the machine up",
         line="After the deploy, billing-api went unhealthy. A restart says what it costs before it runs.",
         hand=["container", "containers", "machine", "P21", "P22", "P11"],
         desk=["D8", "D9", "D16"], stage="container"),
    dict(id="work", phase="evening", title="Watch it work",
         line="shopfront reads the log, argues with a subagent, and writes a plan.",
         hand=["feed", "sessions", "feed-team", "feed-mail", "calls", "subagents", "new-work", "P25"],
         desk=["desk-work", "D17"], stage="feed"),
    dict(id="tell", phase="night", title="Tell it what to do",
         line="“Run the migration tonight, wake me if it fails.” Typed, dictated or picked from a list.",
         hand=["P2", "new-model", "new-mode", "P3", "P4", "P5"],
         desk=["D5"], stage="P2"),
    dict(id="asks", phase="night", title="It asks. You answer.",
         line="Now, or after the backup? One tap, from bed.",
         hand=["P1", "permit", "new-permitted", "P10", "X1-ask", "P12"],
         desk=["D2", "D3", "D13"], stage="P1"),
    dict(id="wheel", phase="night", title="Take the wheel, or hand it back",
         line="Its own terminal, where it lives, and the way to close it.",
         hand=["terminal", "new-session", "P18", "P20"],
         desk=["D6", "D7"], stage="terminal"),
    dict(id="itself", phase="night", title="Sessions use the panel too",
         line="It keeps a checklist, writes to billing-api, and calls you when it is done.",
         hand=["P19", "P29", "new-checklist", "X1-call"],
         desk=[], stage="P19"),
    dict(id="wrote", phase="morning", title="Read what it wrote",
         line="Every changed line leads back to the conversation that wrote it.",
         hand=["P7", "P8", "P9", "P26", "archive"],
         desk=["D4", "D14"], stage="D4"),
    dict(id="costs", phase="morning", title="Know what it costs",
         line="What the night cost, by project, model and hour, and what is left before the limit.",
         hand=["usage", "P27"],
         desk=["D1", "D10", "desk-rings"], stage="D1"),
    dict(id="start", phase="setup", title="Decide how sessions start",
         line="Accounts, groups and projects, and what each one starts with.",
         hand=["P17", "P6", "new-contour", "new-project"],
         desk=["D11", "D12"], stage="D11"),
    dict(id="trust", phase="setup", title="Only you get in",
         line=None,
         hand=["P13", "P14", "P15", "P16", "X4"],
         desk=["D15", "scheme"], stage="P13"),
]

TRUST = [
    "A passkey, enrolled with a one-time code printed on the machine.",
    "The part facing the internet only asks; a process on the host decides.",
    "Your conversations never enter that container.",
    "Every action is written down before it runs.",
]

ICON = {
    PHONE: ('<svg viewBox="0 0 16 16" aria-hidden="true"><rect x="4.5" y="1.5" width="7" height="13" rx="1.6"/>'
            '<path d="M7 12.4h2"/></svg>'),
    DESK: ('<svg viewBox="0 0 16 16" aria-hidden="true"><rect x="1.5" y="2.5" width="13" height="8.5" rx="1.2"/>'
           '<path d="M6 14h4M8 11v3"/></svg>'),
}
ICON_OPEN = ('<svg viewBox="0 0 16 16" aria-hidden="true">'
             '<path d="M9.5 2.5h4v4M13.5 2.5 9 7M6.5 13.5h-4v-4M2.5 13.5 7 9"/></svg>')
LANE = {PHONE: "in a hand", DESK: "at a desk"}


def esc(s):
    return html.escape(s, quote=True)


class Page:
    """One build: where files are looked for, and what was missing or broken."""

    def __init__(self, stand_ins=None):
        self.stand_ins = stand_ins
        self.missing, self.broken = set(), {}
        self.scenes, self.shots, self.copies = set(), {}, 0

    def find(self, rel):
        """A file of the page: site/ first, then the stand-ins when there are any."""
        for root in (HERE, self.stand_ins):
            if root is not None and (root / rel).is_file():
                return root / rel
        return None

    def fragment(self, rel):
        """A piece of markup to paste: read and checked, or None with the fault noted."""
        path = self.find(rel)
        if path is None:
            self.missing.add(rel)
            return None
        text = path.read_text(encoding="utf-8").strip().replace(*STAND_HOME)
        check = Balance()
        check.feed(text)
        faults = check.verdict() + [f"carries {word}" for word in FOREIGN if word in text]
        if faults:
            self.broken[rel] = faults
            return None
        return text

    def scene(self, name):
        """A scene's markup, with its ids made unique to this copy: the same
        scene can stand on the page twice, and a second id="x" breaks url(#x)."""
        rel = f"scenes/{name}.html"
        self.scenes.add(rel)
        text = self.fragment(rel)
        if text is None:
            return None
        self.copies += 1
        n = self.copies
        text = re.sub(r'id="([\w-]+)"', lambda m: f'id="{m.group(1)}-{n}"', text)
        return re.sub(r"url\(#([\w-]+)\)", lambda m: f"url(#{m.group(1)}-{n})", text)

    def picture(self, rel, size=None):
        """A picture file: found and of the size the table gives, or noted as not."""
        path = self.find(rel)
        if path is None:
            self.missing.add(rel)
            return False
        self.shots[rel] = path
        got = webp_size(path)
        if got is None:
            self.broken[rel] = ["is not a WebP picture"]
        elif size and got != size:
            self.broken[rel] = [f"is {got[0]}x{got[1]}, the table says {size[0]}x{size[1]}"]
        return True

    def shot(self, a):
        """A shot as its tile shows it; the full frame waits for a tap."""
        tile, full = f'shots/{a["src"]}.webp', f'shots/{a["full"]}-full.webp'
        (tw, th), (fw, fh) = a["tile"], a["full_size"]
        has_full = self.picture(full, a["full_size"])
        if not self.picture(tile, a["tile"]):
            return None
        tile, full = versioned(tile), versioned(full)
        return (f'<img src="{tile}" width="{tw}" height="{th}" loading="lazy" decoding="async" alt=""'
                f' data-tile="{tile} {tw} {th}"' + (f' data-full="{full} {fw} {fh}"' if has_full else "") + ">")

    def screen(self, a):
        """The .screen of an asset: what moves between a tile and the stage. A
        missing or broken one moves too, as a box that names its file."""
        attrs = [f'data-asset="{esc(a["key"])}"', f'data-cap="{esc(a["cap"])}"', f'data-dev="{a["dev"]}"']
        style = []
        for names, window in ((("--cx", "--cy", "--cs"), a.get("crop")), (("--sx", "--sy", "--ss"), a.get("stage"))):
            if window:
                style += [f"{name}:{v}" for name, v in zip(names, window)]
        if style:
            attrs.append(f'style="{";".join(style)}"')
        attrs = " ".join(attrs)
        if a["kind"] == "scene":
            body, what = self.scene(a["src"]), f'scenes/{a["src"]}.html'
        elif a["kind"] == "drawn":
            body, what = self.fragment(f'drawn/{a["src"]}.html'), f'drawn/{a["src"]}.html'
        else:
            body, what = self.shot(a), f'shots/{a["src"]}.webp'
        if body is None:
            return f'<div class="screen missing" inert {attrs}><span>{esc(what)}</span></div>'
        return f'<div class="screen {a["kind"]} still" inert {attrs}>{body}</div>'

    def figcap(self, a):
        return (f'<figcaption><span class="dev">{ICON[a["dev"]]}</span>'
                f'<span class="cap">{esc(a["cap"])}</span></figcaption>')

    def tile(self, a, hidden):
        """A tile. The one whose asset stands on the stage is kept, empty and
        hidden, so the stage's asset has a home to go back to."""
        h = " hidden" if hidden else ""
        body = "" if hidden else self.screen(a)
        return (f'<div class="thumb" role="button" tabindex="0" data-dev="{a["dev"]}" data-home="{esc(a["key"])}"'
                f' aria-label="Show on the big screen: {esc(a["cap"])}"{h}>'
                f'<div class="crop">{body}<span class="enlarge">{ICON_OPEN}</span></div>'
                f'<span class="tcap">{esc(a["cap"])}</span></div>')

    def board(self, b):
        st = A[b["stage"]]
        dev = st["dev"]
        stage = (f'<figure class="stage" data-dev="{dev}"><div class="bezel"><div class="crop">{self.screen(st)}</div></div>'
                 f'{self.figcap(st)}</figure>')
        lanes = []
        for lane_dev, keys in ((PHONE, b["hand"]), (DESK, b["desk"])):
            if not keys:
                continue
            tiles = "".join(self.tile(A[k], hidden=(k == b["stage"])) for k in keys)
            head = f'<div class="lanehead">{ICON[lane_dev]}<span>{LANE[lane_dev]}</span></div>'
            inner = head + (stage if lane_dev == dev else "") + f'<div class="thumbs">{tiles}</div>'
            lanes.append(f'<div class="lane" data-dev="{lane_dev}">{inner}</div>')
        single = " single" if not b["desk"] else ""
        if b["id"] == "trust":
            items = "".join(f"<li>{esc(t)}</li>" for t in TRUST)
            text = (f'<div class="trustline"><div class="n31"><b>31</b><span>named actions, and no “run this command”</span></div>'
                    f'<ul class="trust">{items}</ul></div>')
        else:
            text = f'<p>{esc(b["line"])}</p>'
        return (f'<section class="board" id="{b["id"]}" data-phase="{b["phase"]}" data-stage="{dev}">'
                f'<div class="bhead"><h2>{esc(b["title"])}</h2>{text}</div>'
                f'<div class="lanes{single}">{"".join(lanes)}</div></section>')

    def build(self):
        src = (HERE / "index.src.html").read_text(encoding="utf-8")
        self.picture(OG)

        def stamp(m):
            kind, arg = m.group(1), m.group(2)
            if kind == "boards":
                return "".join(self.board(b) for b in BOARDS if b["phase"] == arg)
            return self.screen(A[arg])

        text = re.sub(r"\{\{(boards|screen):([\w-]+)\}\}", stamp, src)
        return re.sub(r'(href|src)="(style\.css|scenes\.css|page\.js)"',
                      lambda m: f'{m.group(1)}="{versioned(m.group(2))}"', text)


def versioned(rel):
    """A file's address with its content in it: a browser that kept the previous
    style sheet or picture asks again the moment the file changes, and never before."""
    path = HERE / rel
    if not path.is_file():
        return rel
    return f"{rel}?v={hashlib.sha256(path.read_bytes()).hexdigest()[:10]}"


def link(target, name):
    """A link in the preview, in place of the one a previous build left."""
    if name.is_symlink() or name.exists():
        name.unlink()
    name.symlink_to(target)


def preview(text, shots, stand_ins):
    """The preview directory: the page, and links to the files it loads."""
    out = stand_ins / "preview"
    out.mkdir(exist_ok=True)
    (out / "index.html").write_text(text, encoding="utf-8")
    for name in ("style.css", "page.js", "fonts", "icons"):
        link(HERE / name, out / name)
    own = stand_ins / "scenes.css"
    link(own if own.is_file() else HERE / "scenes.css", out / "scenes.css")
    (out / "shots").mkdir(exist_ok=True)
    for old in (out / "shots").iterdir():
        if old.is_symlink():
            old.unlink()
    for rel, path in shots.items():
        link(path, out / rel)
    return out / "index.html"


def main():
    ap = argparse.ArgumentParser(description="Builds the landing page.")
    ap.add_argument("--stand-ins", type=pathlib.Path, metavar="DIR",
                    help="take what site/ lacks from DIR/scenes and DIR/shots, write DIR/preview/")
    args = ap.parse_args()
    stand_ins = args.stand_ins.resolve() if args.stand_ins else None

    page = Page(stand_ins)
    text = page.build()
    if stand_ins:
        built = preview(text, page.shots, stand_ins)
    else:
        built = HERE / "index.html"
        built.write_text(text, encoding="utf-8")
    print(f"built {built} — {len(text)} bytes, {len(BOARDS)} boards, "
          f"{page.copies} scene copies, {len(page.shots)} picture files", flush=True)

    unused = sorted({f"scenes/{p.name}" for p in (HERE / "scenes").glob("*.html")} - page.scenes)
    unused += sorted({f"shots/{p.name}" for p in (HERE / "shots").glob("*.webp")} - set(page.shots))
    if unused:
        print("in site/ and not on the page:", " ".join(unused), flush=True)
    for rel in sorted(page.missing):
        print(f"MISSING {rel}", file=sys.stderr)
    for rel, faults in sorted(page.broken.items()):
        print(f"BROKEN {rel}: {'; '.join(faults)}", file=sys.stderr)
    if page.missing or page.broken:
        print(f"{len(page.missing)} missing, {len(page.broken)} broken", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
