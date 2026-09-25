// The sheet that renames a session on the stream, as the client's own does: a
// field with the name the session has, saved when the person says so. The
// name is the key the panel and the host find the session by, so it keeps to
// what those take, and a name another live session answers to is refused
// before it is sent. The open conversation follows the session to the name.

import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";

const NAME = /^[A-Za-z0-9_][A-Za-z0-9._-]{0,63}$/;

// nameTrouble says what keeps a typed name from being saved, or nothing.
export function nameTrouble(value, current, taken) {
    const name = String(value || "").trim();
    if (!name) return "Enter a name.";
    if (name === current) return "";
    if (!NAME.test(name)) return "A name keeps to latin letters, digits, - _ and . and does not start with - or .";
    if ((taken || []).includes(name)) return "A live session is called so already.";
    return "";
}

export function RenameSheet({ name, exec, taken, onDone }) {
    const run = useAction();
    const [value, setValue] = useState(name);
    const [busy, setBusy] = useState(false);
    const can = knows(exec, "session.rename");
    const next = value.trim();
    const trouble = nameTrouble(value, name, (taken || []).filter((n) => n !== name));
    const ready = can && !busy && !trouble && next !== name;

    const save = async () => {
        if (!ready) return;
        setBusy(true);
        const result = await run("session.rename", name, { name: next });
        setBusy(false);
        if (result && result.ok) onDone();
    };

    return html`
        <div class="cmdsheet">
            <div class="shead cmdtitle"><span class="cmdhead">Rename the session</span></div>
            <p class="cmdnote">Enter a new name for this session. Its conversation stays as it is; the panel and the host
                find the session by the new name.</p>
            <input class="rnname" type="text" aria-label="the new name of the session" value=${value}
                   autocomplete="off" autocapitalize="off" spellcheck="false"
                   onInput=${(e) => setValue(e.currentTarget.value)}
                   onKeyDown=${(e) => { if (e.key === "Enter") { e.preventDefault(); save(); } }} />
            ${trouble && html`<p class="cmdnote stpwarn">${trouble}</p>`}
            ${!can && html`<p class="cmdnote">${whyNot(exec, "session.rename")}</p>`}
            <div class="btnrow">
                <button class="btn" type="button" onClick=${onDone}>Cancel</button>
                <button class="btn primary" type="button" disabled=${!ready} onClick=${save}>
                    ${busy ? "Saving…" : "Save"}
                </button>
            </div>
        </div>
    `;
}
