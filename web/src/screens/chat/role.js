// The sheet that gives a session its role in its project: a sentence or two
// that tell it from the other sessions working in the same repository. The
// panel keeps it by the session's name and its repository and tells the
// session at every start, so it outlives a restart and a reopen under the
// same name. Saved and cleared with the sheet's own buttons; a role past its
// length is not sent.

import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";

// ROLE_MAX is the length of a role the host keeps, in characters.
export const ROLE_MAX = 500;

// roleLength counts a role as the host does: by characters, a letter outside
// the basic plane one, not two halves of one.
export function roleLength(text) {
    return [...String(text || "").trim()].length;
}

// roleWhy says why the role of the session cannot be set from the panel, or
// nothing when it can.
export function roleWhy(exec) {
    return knows(exec, "session.role") ? "" : whyNot(exec, "session.role");
}

export function RoleSheet({ name, live, exec, onDone }) {
    const run = useAction();
    const current = (live && live.role) || "";
    const [value, setValue] = useState(current);
    const [busy, setBusy] = useState(false);
    const why = roleWhy(exec);
    const next = value.trim();
    const length = roleLength(value);
    const over = length > ROLE_MAX;
    const ready = !why && !busy && !over && next !== current;

    const send = async (role) => {
        setBusy(true);
        const result = await run("session.role", name, { role });
        setBusy(false);
        if (result && result.ok) onDone();
    };

    return html`
        <div class="cmdsheet">
            <div class="shead cmdtitle"><span class="cmdhead">Role of the session</span></div>
            <p class="cmdnote">What this session does in its project, in a sentence or two: it tells it from the other
                sessions working in the same repository. The session is told its role at every start, the next one
                included; it keeps it across a restart and a reopen under the same name.</p>
            <textarea class="rnname cxfield rolefield" rows="4" aria-label="the role of the session" value=${value}
                      placeholder="the lead of the database split: the migrations and their review"
                      onInput=${(e) => setValue(e.currentTarget.value)}></textarea>
            <p class=${`cmdnote rolecount${over ? " stpwarn" : ""}`}>${over
                ? `${length} of ${ROLE_MAX} characters: say it shorter`
                : `${length} / ${ROLE_MAX}`}</p>
            ${why && html`<p class="cmdnote">${why}</p>`}
            <div class="btnrow">
                ${current && html`<button class="btn danger" type="button" disabled=${Boolean(why) || busy}
                                          onClick=${() => send("")}>Clear</button>`}
                <button class="btn" type="button" onClick=${onDone}>Cancel</button>
                <button class="btn primary" type="button" disabled=${!ready} onClick=${() => send(next)}>
                    ${busy ? "Saving…" : "Save"}
                </button>
            </div>
        </div>
    `;
}
