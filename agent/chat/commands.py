"""What a slash command answered, as data for a card rather than as text.

The answer of a command is written for the screen it was typed at: a grid of
coloured glyphs for a terminal, a markdown document for everything else. Laid
into the feed as text, the first is a paragraph of glyphs and the second a
table as long as the list of skills. So a command the feed knows is read into
numbers, and the screen draws them its own way.
"""
import re

from .harness import ANSI_RE, COMMAND_OUT_RE

# How many entries of one list a card carries. The lists are the tools, agents
# and skills that take room in the context, and a setup with hundreds of them
# still reads as its largest few: the rest are a count in the header.
MAX_ENTRIES = 400

# A category is one of four things, and only the used ones fill the context.
# Deferred ones are loaded on demand and take no room until then; the buffer is
# held back for compaction; the free space is what is left. The record names
# the kind; the markdown does not, and its names say the same.
KINDS = ("used", "deferred", "buffer", "free")

CONTEXT_HEAD = "## Context Usage"
TERMINAL_CONTEXT_HEAD = "Context Usage"

MODEL_RE = re.compile(r"^\*\*Model:\*\*\s*(\S+)", re.M)
TOTAL_RE = re.compile(r"^\*\*Tokens:\*\*\s*(\S+)\s*/\s*(\S+)\s*\((\d+(?:\.\d+)?)%\)", re.M)
SECTION_RE = re.compile(r"^###\s+(.+?)\s*$", re.M)
COUNT_RE = re.compile(r"^(<\s*)?~?\s*(\d+(?:\.\d+)?)\s*([km]?)$", re.I)
SCALE = {"": 1, "k": 1000, "m": 1000000}


def count(text):
    """Returns a token count as written in the markdown, as (tokens, under).

    The markdown rounds: 3.8k, 1m, ~260. A count below its precision is
    written as "< 20", which is not a number, and it is kept as the bound it
    is rather than turned into one.
    """
    found = COUNT_RE.match((text or "").strip())
    if not found:
        return 0, 0
    value = round(float(found.group(2)) * SCALE[found.group(3).lower()])
    if found.group(1):
        return 0, value
    return value, 0


def kind_of(name, said=""):
    """Returns the kind of a category, from the record when it says one."""
    if said in KINDS:
        return said
    low = name.lower()
    if low.endswith("(deferred)"):
        return "deferred"
    if low.startswith("free space"):
        return "free"
    if low.startswith("autocompact buffer"):
        return "buffer"
    return "used"


def tables(text):
    """Returns the tables of a markdown document by the heading above each, as lists of rows."""
    out = {}
    marks = list(SECTION_RE.finditer(text))
    for i, mark in enumerate(marks):
        end = marks[i + 1].start() if i + 1 < len(marks) else len(text)
        rows = []
        for line in text[mark.end():end].splitlines():
            line = line.strip()
            if not line.startswith("|"):
                continue
            cells = [c.strip() for c in line.strip("|").split("|")]
            if all(set(c) <= set("-: ") for c in cells):
                continue
            rows.append(cells)
        # The first row of a table is its header.
        out[mark.group(1).strip().lower()] = rows[1:]
    return out


def entry(fields, tokens_text):
    tokens, under = count(tokens_text)
    item = dict(fields, tokens=tokens)
    if under:
        item["under"] = under
    return item


def usage_from_markdown(text):
    """Returns the context usage read off the markdown answer of /context, or None."""
    if not text.startswith(CONTEXT_HEAD):
        return None
    total = TOTAL_RE.search(text)
    if not total:
        return None
    model = MODEL_RE.search(text)
    used, _ = count(total.group(1))
    most, _ = count(total.group(2))
    parts = tables(text)
    categories = []
    for row in parts.get("estimated usage by category", []):
        if len(row) < 2:
            continue
        categories.append(entry({"name": row[0], "kind": kind_of(row[0])}, row[1]))
    usage = {
        "model": model.group(1) if model else "",
        "used": used,
        "max": most,
        "percent": float(total.group(3)),
        "categories": categories,
        "mcp": servers([entry({"name": r[0], "server": r[1]}, r[2])
                        for r in parts.get("mcp tools", []) if len(r) >= 3]),
        "agents": [entry({"name": r[0], "source": r[1]}, r[2])
                   for r in parts.get("custom agents", []) if len(r) >= 3],
        "memory": [entry({"type": r[0], "path": r[1]}, r[2])
                   for r in parts.get("memory files", []) if len(r) >= 3],
        "skills": [entry({"name": r[0], "source": r[1]}, r[2])
                   for r in parts.get("skills", []) if len(r) >= 3],
    }
    return capped(usage)


def number(value):
    return value if isinstance(value, (int, float)) and not isinstance(value, bool) else 0


def usage_from_record(data):
    """Returns the context usage from the field the record carries it in, or None."""
    if not isinstance(data, dict):
        return None

    def rows(key):
        return [r for r in (data.get(key) or []) if isinstance(r, dict)]

    usage = {
        "model": str(data.get("model") or ""),
        "used": number(data.get("total_tokens")),
        "max": number(data.get("raw_max_tokens")),
        "percent": number(data.get("percentage")),
        "categories": [{"name": str(r.get("name") or ""), "tokens": number(r.get("tokens")),
                        "kind": kind_of(str(r.get("name") or ""), r.get("kind") or "")}
                       for r in rows("categories")],
        "mcp": servers([{"name": str(r.get("name") or ""), "server": str(r.get("server_name") or ""),
                         "tokens": number(r.get("tokens"))} for r in rows("mcp_tools")]),
        "agents": [{"name": str(r.get("agent_type") or r.get("name") or ""),
                    "source": str(r.get("source") or ""), "tokens": number(r.get("tokens"))}
                   for r in rows("agents")],
        "memory": [{"type": str(r.get("type") or ""), "path": str(r.get("path") or ""),
                    "tokens": number(r.get("tokens"))} for r in rows("memory_files")],
        "skills": [{"name": str(r.get("name") or ""), "source": str(r.get("source") or ""),
                    "tokens": number(r.get("tokens"))} for r in rows("skills")],
    }
    if not usage["max"]:
        return None
    return capped(usage)


def servers(tools):
    """Returns the tools of MCP folded into their servers, largest first.

    A setup carries tools by the hundred, and what a person asks of them is
    which server costs what: one line per server says that, a line per tool
    buries it.
    """
    out = {}
    for tool in tools:
        name = tool.get("server") or "?"
        row = out.setdefault(name, {"name": name, "tools": 0, "tokens": 0})
        row["tools"] += 1
        row["tokens"] += tool["tokens"]
    return sorted(out.values(), key=lambda r: -r["tokens"])


def capped(usage):
    """Returns the usage with each list cut to what a card carries, and says how long it was."""
    for key in ("mcp", "agents", "memory", "skills"):
        rows = usage[key]
        if len(rows) > MAX_ENTRIES:
            usage[key + "Total"] = len(rows)
            usage[key] = sorted(rows, key=lambda r: -r["tokens"])[:MAX_ENTRIES]
    return usage


def answer(record, text, at, pos):
    """Returns the feed items for a command answer the feed draws as a card.

    None is any other text, and it goes on to be read as a prompt or a note.
    An empty list is an answer that is not shown at all: a terminal writes
    /context twice, as a coloured grid and as markdown after it, and the card
    is drawn from the markdown.
    """
    usage = usage_from_record(record.get("contextUsage"))
    if usage:
        return [card(usage, at, pos)]
    found = COMMAND_OUT_RE.search(text)
    said = (found.group(2) if found else text).strip()
    if ANSI_RE.sub("", said).strip().startswith(TERMINAL_CONTEXT_HEAD) and "\x1b[" in said:
        return []
    usage = usage_from_markdown(said)
    if usage:
        return [card(usage, at, pos)]
    return None


def card(usage, at, pos):
    return {"role": "command", "name": "context", "data": usage, "at": at, "pos": pos}
