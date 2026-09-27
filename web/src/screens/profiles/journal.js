// The journal of the map: who changed what and when — a contour, a group, a
// project, key by key — the newest first; a deleted project is taken back
// from its entry.
import { useEffect, useState } from "preact/hooks";

import { html } from "../../html.js";
import { BackHead, useBackClose } from "../../ui/back.js";
import { useAction } from "../../actions/gate.js";
import { ago } from "../../format.js";
import { Layer } from "./kit.js";

const WHAT = { profile: "contour", group: "group", project: "project" };

function shown(value) {
    if (value === null || value === undefined) return "—";
    if (typeof value === "string") return value === "" ? "“”" : value;
    return JSON.stringify(value);
}

export function Journal({ onClose, onDone }) {
    useBackClose(true, onClose);
    const run = useAction();
    const [entries, setEntries] = useState(null);
    const [error, setError] = useState("");

    const load = async () => {
        try {
            const response = await fetch("/api/profiles/journal?limit=100", { credentials: "same-origin" });
            if (!response.ok) throw new Error((await response.text()).trim() || `the server answered ${response.status}`);
            const got = await response.json();
            setEntries(got.journal || []);
            setError("");
        } catch (err) {
            setError(err.message);
        }
    };
    useEffect(() => {
        load();
    }, []);

    const undo = async (entry) => {
        const result = await run("project.restore", entry.name, { entry: entry.id });
        if (result && result.ok) {
            onDone(result.data);
            load();
        }
    };

    return html`
        <${BackHead} onBack=${onClose} label="back">
            <h2>Changes of the map</h2>
            <span class="where">the newest first</span>
        <//>
        ${error && html`<p class="hint crit">${error}</p>`}
        ${entries === null && !error && html`<p class="empty">Loading…</p>`}
        ${entries && entries.length === 0 && html`<p class="empty">Nothing has changed on the map since the journal was started.</p>`}
        ${(entries || []).map((e) => html`
            <div class="pzentry" key=${e.id}>
                <div class="pzentryhead">
                    <span class="pzentrywhat">${e.op} ${WHAT[e.entity] || e.entity} <b>${e.name}</b></span>
                    <span class="pzhelp">${e.actor || "unknown"} · ${ago(new Date(e.at * 1000).toISOString())}</span>
                </div>
                ${(e.changes || []).map((c) => html`
                    <div class="pzchange" key=${c.key}>
                        <code>${c.key}</code> <span>${shown(c.from)}</span> <span class="pzarrow">→</span> <span>${shown(c.to)}</span>
                    </div>
                `)}
                ${e.undoneAt && html`<span class="pzhelp">taken back</span>`}
                ${e.undoable && html`<button class="btn" type="button" onClick=${() => undo(e)}>Undo</button>`}
            </div>
        `)}
    `;
}

export function JournalLayer(props) {
    return html`<${Layer} label="changes of the map"><${Journal} ...${props} /><//>`;
}
