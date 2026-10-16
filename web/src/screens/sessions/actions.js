// What is done with an archived conversation: return to it, or start a new one
// in the same project.

import { knows } from "../../exec.js";
import { codexResume } from "../../agent.js";

// resume brings an archived conversation back by its id rather than its
// name — two contours may hold a project of the same name — and a thread of
// codex in the daemon of its contour. A host that does not know the action is
// not asked.
export function resume(run, row, exec) {
    if (!knows(exec, "session.resume")) return undefined;
    return run("session.resume", row.name, { session: row.sessionId, ...codexResume(row) });
}

// startIn starts a new session in the project an archived conversation ran
// in: by the entry of the map the row carries, since a name is not an address
// when two contours hold a project called the same. A host that does not know
// the action is not asked.
export function startIn(run, row, exec) {
    if (!knows(exec, "session.open")) return undefined;
    return run("session.open", sessionOf(row), row.project ? { project: row.project.id } : {});
}

// sessionOf returns the name a new session of the row's project comes up
// under. The map names a project for people and its session for the host —
// "Admin" and "admin" — and the panel waits for the session by the second.
export function sessionOf(row) {
    return (row && row.project && row.project.session) || projectOf(row);
}

// projectOf returns which project an archived session ran in.
export function projectOf(row) {
    const cwd = ((row && row.cwd) || "").replace(/\/+$/, "");
    if (!cwd.startsWith("/")) return "";
    return cwd.slice(cwd.lastIndexOf("/") + 1);
}
