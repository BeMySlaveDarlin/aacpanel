// The buttons on an archived card: return to yesterday's conversation, or start a
// new one in the same project.

import { html } from "../../html.js";
import { Icon } from "../../ui/icons.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";
import { codexResume } from "../../agent.js";

// PastActions renders what can be done with an archived card.
export function PastActions({ row, exec }) {
    return html`
        <${ResumeButton} row=${row} exec=${exec} />
        <${OpenButton} row=${row} exec=${exec} />
    `;
}

function ResumeButton({ row, exec }) {
    const run = useAction();
    const ready = knows(exec, "session.resume");
    const why = whyNot(exec, "session.resume");
    if (!row.sessionId) return null;
    return html`
        <button
            class="iconbtn accent"
            type="button"
            aria-label=${`resume session ${row.name}`}
            disabled=${!ready}
            title=${ready ? "resume the conversation" : why}
            onClick=${async () => {
                // The identifier, not the name: two contours may hold a project
                // of the same name, and a name resumes whichever of them spoke last.
                await run("session.resume", row.name, { session: row.sessionId });
            }}
        >${Icon.resume()}</button>
    `;
}

// resume brings an archived conversation back by its id rather than its
// name — two contours may hold a project of the same name — and a thread of
// codex in the daemon of its contour.
export function resume(run, row) {
    return run("session.resume", row.name, { session: row.sessionId, ...codexResume(row) });
}

// startIn starts a new session in the project an archived conversation ran
// in: by the entry of the map the row carries, since a name is not an address
// when two contours hold a project called the same.
export function startIn(run, row) {
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

function OpenButton({ row, exec }) {
    const run = useAction();
    const ready = knows(exec, "session.open");
    const why = whyNot(exec, "session.open");
    const project = (row.project && row.project.name) || projectOf(row);
    if (!project) return null;
    return html`
        <button
            class="iconbtn accent"
            type="button"
            aria-label=${`new session in project ${project}`}
            disabled=${!ready}
            title=${ready ? `new session in project ${project}` : why}
            onClick=${() => startIn(run, row)}
        >${Icon.plus()}</button>
    `;
}
