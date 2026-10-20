// Session messages outlive a conversation. The inherited text stays visible
// while editing, so clearing an override says exactly what will take its place.
import { useEffect, useState } from "preact/hooks";

import { html } from "../../html.js";
import { useAction } from "../../actions/gate.js";
import { knows, whyNot } from "../../exec.js";
import { ROLE_MAX as INTENT_MAX } from "./role.js";

const fields = [
    ["intent", "First message"],
    ["restartIntent", "Message after a restart"],
];

export function intentWhy(exec) {
    return knows(exec, "session.intent") ? "" : whyNot(exec, "session.intent");
}

export function IntentSheet({ name, exec, onDone }) {
    const run = useAction();
    const [data, setData] = useState(null);
    const [values, setValues] = useState({});
    const [error, setError] = useState("");
    const [busy, setBusy] = useState(false);
    useEffect(() => {
        let alive = true;
        fetch(`/api/session/intent?name=${encodeURIComponent(name)}`, { credentials: "same-origin" })
            .then(async (r) => {
                if (!r.ok) throw new Error((await r.text()).trim() || `the server answered ${r.status}`);
                return r.json();
            })
            .then((body) => {
                if (alive) { setData(body); setValues(body.own || {}); }
            })
            .catch((err) => { if (alive) setError(String(err.message || err)); });
        return () => { alive = false; };
    }, [name]);
    const why = intentWhy(exec);
    const changes = Object.fromEntries(fields.map(([key]) => [key, (values[key] || "").trim()])
        .filter(([key, value]) => value !== ((data && data.own && data.own[key]) || "")));
    const over = fields.some(([key]) => [...(values[key] || "").trim()].length > INTENT_MAX);
    const ready = data && !why && !busy && !over && Object.keys(changes).length > 0;
    const save = async () => {
        setBusy(true);
        const result = await run("session.intent", name, changes);
        setBusy(false);
        if (result && result.ok) onDone();
    };
    return html`
        <div class="cmdsheet intentsheet">
            <div class="shead cmdtitle"><span class="cmdhead">Session messages</span></div>
            <p class="cmdnote">These messages stay with this session's name and repository across a restart and a reopen.
                Leave a field empty to inherit the project, then the contour. A continued conversation uses the
                message the session wrote for it first.</p>
            ${error && html`<p class="cmdnote stpwarn">${error}</p>`}
            ${!data && !error && html`<p class="cmdnote">Loading messages…</p>`}
            ${data && fields.map(([key, label]) => {
                const inherited = data.inherited.find((v) => v.key === key);
                const from = inherited && ["project", "contour"].includes(inherited.layer) ? inherited.layer : "";
                const inheritedText = (inherited && inherited.value) || "opens without a message";
                const value = values[key] || "";
                const length = [...value.trim()].length;
                return html`<div class="intentfield" key=${key}>
                    <label class="cmdnote" for=${`session-${key}`}>${label} · ${value.trim() ? "own" : "inherited"}</label>
                    <textarea id=${`session-${key}`} class="rnname cxfield rolefield" rows="3" aria-label=${label}
                              value=${value} placeholder="Empty inherits" disabled=${busy || Boolean(why)}
                              onInput=${(e) => setValues({ ...values, [key]: e.currentTarget.value })}></textarea>
                    <p class=${`cmdnote${length > INTENT_MAX ? " stpwarn" : ""}`}>${length} / ${INTENT_MAX}</p>
                    <p class="cmdnote inherited">${from ? `From ${from}: ${inheritedText}` : `No inherited message: ${inheritedText}`}</p>
                    ${value && html`<button class="btn" type="button" disabled=${busy || Boolean(why)}
                        onClick=${() => setValues({ ...values, [key]: "" })}>Use inherited</button>`}
                </div>`;
            })}
            ${why && html`<p class="cmdnote">${why}</p>`}
            <div class="btnrow">
                <button class="btn" type="button" onClick=${onDone}>Cancel</button>
                <button class="btn primary" type="button" disabled=${!ready} onClick=${save}>${busy ? "Saving…" : "Save"}</button>
            </div>
        </div>
    `;
}
