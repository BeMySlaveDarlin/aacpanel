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
    ["quick", "Quick phrases"],
];

function normalized(key, value) {
    const text = value || "";
    return key === "quick" ? text.split(/\r?\n/).map((line) => line.trim()).filter(Boolean).join("\n") : text.trim();
}

function problemOf(key, value) {
    const text = normalized(key, value);
    if (key !== "quick") return [...text].length > INTENT_MAX ? `Longer than ${INTENT_MAX} characters` : "";
    const phrases = text ? text.split("\n") : [];
    if (phrases.length > 5) return "Use up to 5 phrases";
    if (phrases.some((phrase) => [...phrase].length > 200)) return "Each phrase may have up to 200 characters";
    return "";
}

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
    const changes = Object.fromEntries(fields.map(([key]) => [key, normalized(key, values[key])])
        .filter(([key, value]) => value !== ((data && data.own && data.own[key]) || "")));
    const over = fields.some(([key]) => problemOf(key, values[key]));
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
                message the session wrote for it first. Quick phrases go as your messages from the command ring.</p>
            ${error && html`<p class="cmdnote stpwarn">${error}</p>`}
            ${!data && !error && html`<p class="cmdnote">Loading messages…</p>`}
            ${data && fields.map(([key, label]) => {
                const inherited = data.inherited.find((v) => v.key === key);
                const from = inherited && ["project", "contour", "panel"].includes(inherited.layer) ? inherited.layer : "";
                const inheritedText = (inherited && inherited.value) || "opens without a message";
                const value = values[key] || "";
                const text = normalized(key, value);
                const length = [...text].length;
                const problem = problemOf(key, value);
                return html`<div class="intentfield" key=${key}>
                    <label class="cmdnote" for=${`session-${key}`}>${label} · ${text ? "own" : "inherited"}</label>
                    <textarea id=${`session-${key}`} class="rnname cxfield rolefield" rows=${key === "quick" ? 5 : 3} aria-label=${label}
                              value=${value} placeholder="Empty inherits" disabled=${busy || Boolean(why)}
                              onInput=${(e) => setValues({ ...values, [key]: e.currentTarget.value })}></textarea>
                    <p class=${`cmdnote${problem ? " stpwarn" : ""}`}>${problem || (key === "quick"
                        ? `${text ? text.split("\n").length : 0} / 5 phrases · up to 200 characters each`
                        : `${length} / ${INTENT_MAX}`)}</p>
                    <p class="cmdnote inherited">${from === "panel" ? `Default: ${inheritedText}`
                        : from ? `From ${from}: ${inheritedText}` : `No inherited message: ${inheritedText}`}</p>
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
