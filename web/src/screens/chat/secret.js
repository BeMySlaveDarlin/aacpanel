// The notepad of a secret a session asked for. The session prepared the text —
// the keys and where each value is found — and the person fills it in here;
// the host writes it into a file of its own and tells the session the path.
// The values never enter the conversation, and the text never rests anywhere
// in the browser: it lives in the state of this sheet, not in the drafts of
// the composer and not in the storage of the page. Closing the sheet takes
// the state down with it, and the next opening starts from the template.

import { useState } from "preact/hooks";

import { html } from "../../html.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";
import { useToast } from "../../ui/toasts.js";
import { named, pathOf } from "../../data/secrets.js";
import { stampText } from "./labels.js";

// The field is as tall as the template asks, within what a phone can show
// beside its keyboard.
const ROWS_MIN = 4;
const ROWS_MAX = 12;

function rowsFor(template) {
    const lines = String(template || "").split("\n").length + 1;
    return Math.min(Math.max(lines, ROWS_MIN), ROWS_MAX);
}

export function SecretPad({ item, session, exec, shelf, onSaved, onDone }) {
    const run = useAction();
    const toast = useToast();
    const [text, setText] = useState(item.template || "");
    const [hidden, setHidden] = useState(false);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    const can = knows(exec, "secret.put");
    const ready = can && !busy && text.trim() !== "";
    const known = Boolean(shelf && shelf.dir);
    const path = pathOf(shelf, item.name);
    const was = named(shelf, item.name);

    const save = async () => {
        if (!ready) return;
        setBusy(true);
        setError("");
        const result = await run("secret.put", session, { name: item.name, text });
        setBusy(false);
        if (!result || !result.ok) {
            setError((result && result.error) || "the host did not save it");
            return;
        }
        toast(known ? `Saved to ${path}` : `Saved as ${item.name}`, result.data && result.data.detail);
        if (onSaved) onSaved();
        onDone();
    };

    return html`
        <div class="cmdsheet">
            <div class="shead cmdtitle"><span class="cmdhead">${item.title || "A secret for the session"}</span></div>
            <div class="skwhere">
                <code class="skpath">${known ? path : item.name}</code>
                ${!known && html`<span class="cmdnote">in the secrets directory of the host</span>`}
                ${was && html`<p class="cmdnote stpwarn skwas">Replaces the secret saved ${stampText(was.at)}</p>`}
            </div>
            <div class="skfield">
                <div class="skbar">
                    <span class="cmdnote">Fill in the values; the session gets the path and the names of the keys.</span>
                    <button class="btn skeye" type="button" aria-pressed=${hidden ? "true" : "false"}
                            onClick=${() => setHidden((h) => !h)}>${hidden ? "Show" : "Hide"}</button>
                </div>
                <textarea class=${`skpad${hidden ? " masked" : ""}`} aria-label=${`the text of the secret ${item.name}`}
                          rows=${rowsFor(item.template)} value=${text}
                          autocomplete="off" autocorrect=${false} autocapitalize="off" spellcheck=${false}
                          onInput=${(e) => setText(e.currentTarget.value)}></textarea>
            </div>
            <p class="cmdnote">
                The host writes the text into a file only its user can read, and nothing of it stays in this
                browser or in the conversation. Saving under the same name replaces the file.
            </p>
            ${!can && html`<p class="cmdnote">${whyNot(exec, "secret.put")}</p>`}
            ${error && html`<p class="cmdnote stpwarn skerr">${error}</p>`}
            <div class="btnrow">
                <button class="btn" type="button" onClick=${onDone}>Cancel</button>
                <button class="btn primary" type="button" disabled=${!ready} onClick=${save}>
                    ${busy ? "Saving…" : "Save"}
                </button>
            </div>
        </div>
    `;
}
