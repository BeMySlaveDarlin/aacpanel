"""Tool calls: label, badge kind and the gist in one line."""
from .limits import MAX_ARG, cut


def tool_label(name, data=None):
    """Returns the name of a call for the feed and for the details header."""
    if name.startswith("mcp__"):
        parts = name.split("__", 2)
        if len(parts) == 3 and parts[1] and parts[2]:
            return f"{parts[1]}: {parts[2]}"
        return name
    if name == "Skill" and isinstance(data, dict):
        skill = data.get("skill")
        if isinstance(skill, str) and skill.strip():
            return f"Skill: {one_line(skill)}"
    return name


TOOL_KINDS = (
    ("bash", ("Bash", "BashOutput", "KillShell", "KillBash", "Monitor")),
    ("files", ("Read", "Write", "Edit", "MultiEdit", "NotebookEdit", "NotebookRead",
               "Glob", "Grep", "LS", "SendUserFile")),
    ("web", ("WebFetch", "WebSearch")),
    ("agents", ("Task", "Agent", "SendMessage", "ListAgents", "Workflow",
                "TaskOutput", "TaskStop", "TeamCreate", "TeamDelete")),
    ("skill", ("Skill",)),
    ("ask", ("AskUserQuestion",)),
    ("artifact", ("Artifact",)),
    ("time", ("CronCreate", "CronDelete", "CronList", "ScheduleWakeup")),
)


def tool_kind(name):
    """Returns the kind a call belongs to."""
    if name.startswith("mcp__"):
        parts = name.split("__")
        server = parts[1] if len(parts) > 2 else ""
        if "chrome" in server or "playwright" in server or "browser" in server:
            return "browser"
        return "mcp"
    for kind, names in TOOL_KINDS:
        if name in names:
            return kind
    return "other"


ARG_KEYS = ("command", "file_path", "pattern", "path", "url", "query", "description",
            "action", "text")

# The calls that change a file. What a command changes is known to nobody but
# git, so Bash is not here: the feed shows the command itself instead.
EDITING = ("Write", "Edit", "MultiEdit", "NotebookEdit")


def edited_path(name, data):
    """Returns the file the call changes, empty for a call that changes none."""
    if name not in EDITING or not isinstance(data, dict):
        return ""
    path = data.get("file_path") or data.get("notebook_path")
    return path.strip() if isinstance(path, str) else ""


def tool_arg(data):
    """Returns one line about the call: with what and over what."""
    if not isinstance(data, dict):
        return ""
    for key in ARG_KEYS:
        value = data.get(key)
        if isinstance(value, str) and value.strip():
            return one_line(value)
    return ""


def one_line(text):
    """Returns the text as a single line for the feed, marked and capped."""
    return cut(text.replace("\n", " ⏎ "), MAX_ARG)[0]
