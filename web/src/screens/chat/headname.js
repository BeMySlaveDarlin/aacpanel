// The name edits in place only where the host can rename the session. The
// sheet and the header share the validation and the action gate. A codex
// thread edits its title instead: the panel goes on finding the session by
// its address, and the title is any line of text.

import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { isCodex, shownName } from "../../agent.js";
import { Icon } from "../../ui/icons.js";
import { MidName } from "./head.js";
import { renameTrouble } from "./codexthread.js";
import { useRename } from "./rename.js";
import { renameWhy } from "./sessiontools.js";

export function HeadName({ name, live, exec, taken }) {
    const codex = Boolean(live) && isCodex(live);
    const title = codex ? shownName(live, name) : name;
    const [editing, setEditing] = useState(false);
    const [saved, setSaved] = useState(title);
    // A title given on another device reaches the header with the snapshot.
    useEffect(() => setSaved(title), [title]);
    const button = useRef(null);
    const close = () => {
        setEditing(false);
        // The input leaves before focus returns to the name's button.
        queueMicrotask(() => { if (button.current) button.current.focus(); });
    };
    if (!live || renameWhy(live, exec)) return html`<${MidName} text=${title} />`;
    return html`
        <span class="headrename">
            ${editing
                ? html`<${HeadRename} name=${codex ? live.session : saved} current=${saved}
                                     check=${codex ? renameTrouble : undefined} exec=${exec} taken=${taken}
                                     onCancel=${close} onDone=${(next) => { setSaved(next); close(); }} />`
                : html`<button class="headrename-open" type="button" ref=${button} aria-label="rename the session"
                               onClick=${() => setEditing(true)}><${MidName} text=${saved} /></button>`}
        </span>
    `;
}

function HeadRename({ name, current, check, exec, taken, onCancel, onDone }) {
    const { value, change, busy, trouble, error, ready, save } = useRename({ name, current, check, exec, taken, onDone });
    const input = useRef(null);
    useLayoutEffect(() => {
        input.current.focus();
        input.current.select();
    }, []);
    const key = (e) => {
        if (e.isComposing) return;
        if (e.key !== "Enter" && e.key !== "Escape") return;
        if (e.key === "Enter" && e.target !== input.current) return;
        e.preventDefault();
        e.stopPropagation();
        if (busy) return;
        if (e.key === "Enter") save();
        else onCancel();
    };
    const note = trouble || error;
    return html`
        <span class="headrename-edit" aria-busy=${busy} onKeyDown=${key}>
            <input class="headrename-field" ref=${input} type="text" aria-label="the new name of the session"
                   value=${value} disabled=${busy} autocomplete="off" autocapitalize="off" spellcheck=${false}
                   aria-invalid=${Boolean(note)} onInput=${(e) => change(e.currentTarget.value)} />
            <button class="headrename-action" type="button" aria-label=${busy ? "saving the session name" : "save the session name"}
                    title=${busy ? "Saving…" : "Save"} disabled=${!ready} onClick=${save}>${Icon.check()}</button>
            <button class="headrename-action" type="button" aria-label="cancel renaming" title="Cancel"
                    disabled=${busy} onClick=${onCancel}>${Icon.close()}</button>
            ${note && html`<span class="headrename-note cmdnote stpwarn" role="alert">${note}</span>`}
        </span>
    `;
}
