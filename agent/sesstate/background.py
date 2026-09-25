"""Background agents: an agent the session sent off to work while it goes on.

The session lists one among its agents, not among its commands: what it is
doing is read in its conversation, not in an output file. One that is over
keeps its place with how it ended, the way a teammate that reported does — the
reading of it is the point.
"""

from .limits import MAX_ITEMS
from .subagents import KIND_BACKGROUND

ACTIVE = "active"

# How a background agent ends when the session stops it by hand, and when the
# process that ran it is gone.
STOPPED = "stopped"
KILLED = "killed"


def launched(state, use, agent_id, started, text):
    """Adds an agent the session sent to the background."""
    if not agent_id:
        return
    state.task_ids[use] = agent_id
    state.bg[agent_id] = {
        "id": agent_id, "name": started["name"], "text": started["text"] or text,
        "at": started["at"], "status": ACTIVE, "kind": KIND_BACKGROUND,
    }
    _prune(state)


def ended(state, agent_id, status, at):
    """Marks an agent as over; reports whether the id was one of them.

    An agent notifies each time it stops, and a letter wakes it again: the
    last end heard is the one it keeps.
    """
    agent = state.bg.get(agent_id)
    if agent is None:
        return False
    agent["status"] = status or "completed"
    agent["doneAt"] = at or agent.get("doneAt") or ""
    _prune(state)
    return True


def woken(state, to):
    """Brings an agent back to work: a letter to one that is over resumes it."""
    agent = state.bg.get(to)
    if agent is None or agent["status"] == ACTIVE:
        return
    agent["status"] = ACTIVE
    agent.pop("doneAt", None)


def older_than(state, born):
    """Ends every agent still at work that was sent off before the process was born."""
    if not born:
        return
    for agent in list(state.bg.values()):
        if agent["status"] == ACTIVE and agent.get("at") and agent["at"][:19] < born[:19]:
            ended(state, agent["id"], KILLED, born)


def fill(state, metas):
    """Fills the agents with what their files say: the model, the context, the last word."""
    for agent_id, agent in state.bg.items():
        known = metas.get(agent_id)
        if not known:
            continue
        for key in ("model", "last", "tokens", "limit", "limitKnown"):
            agent[key] = known[key]


def _prune(state):
    if len(state.bg) <= MAX_ITEMS:
        return
    over = sorted((a for a in state.bg.values() if a["status"] != ACTIVE),
                  key=lambda a: a.get("doneAt") or "")
    for agent in over:
        if len(state.bg) <= MAX_ITEMS:
            break
        state.bg.pop(agent["id"], None)
