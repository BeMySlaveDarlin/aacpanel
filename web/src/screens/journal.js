// The action journal: who, what, when and how it ended.
import { useCallback, useEffect, useState } from "preact/hooks";

import { html } from "../html.js";
import { BackHead } from "../ui/back.js";
import { Chips } from "../ui/chips.js";
import { ago } from "../format.js";
import { actionName } from "../actions/registry.js";

const RESULT = {
    ok: { word: "done", kind: "ok" },
    failed: { word: "did not work", kind: "crit" },
    timeout: { word: "no answer — it may still have gone through", kind: "warn" },
    rejected: { word: "rejected", kind: "crit" },
    "": { word: "running", kind: "" },
};

const FILTERS = [
    { id: "", label: "all" },
    { id: "failed", label: "failures" },
    { id: "pending", label: "in flight" },
];

// Journal renders the action journal.
export function Journal({ onBack }) {
    const [state, setState] = useState({ kind: "loading", actions: [] });
    const [older, setOlder] = useState([]);
    const [filter, setFilter] = useState("");

    const load = useCallback(async () => {
        try {
            const query = filter ? `?limit=50&result=${filter}` : "?limit=50";
            const response = await fetch("/api/actions" + query, { credentials: "same-origin" });
            if (response.status === 503 || response.status === 500) {
                setState({ kind: "unavailable", actions: [], error: "the journal is unavailable: the database is not answering" });
                return;
            }
            if (!response.ok) {
                setState({ kind: "failed", actions: [], error: `the server answered ${response.status}` });
                return;
            }
            const body = await response.json();
            setState({ kind: "ready", actions: body.actions || [] });
        } catch (err) {
            setState({ kind: "failed", actions: [], error: "the network is unavailable" });
        }
    }, [filter]);

    useEffect(() => {
        setOlder([]);
        load();
    }, [load]);

    const more = useCallback(async () => {
        const all = [...state.actions, ...older];
        const last = all[all.length - 1];
        if (!last) return;
        try {
            const tail = filter ? `&result=${filter}` : "";
            const response = await fetch(`/api/actions?limit=50&before=${last.id}` + tail,
                { credentials: "same-origin" });
            if (!response.ok) return;
            const body = await response.json();
            setOlder((prev) => [...prev, ...(body.actions || [])]);
        } catch (err) {
        }
    }, [state.actions, older, filter]);

    const rows = [...state.actions, ...older];

    return html`
        ${onBack && html`
            <${BackHead} onBack=${onBack} label="back">
                <h2>Journal</h2>
            <//>
        `}

        <${Chips} items=${FILTERS} current=${filter} onSelect=${setFilter} />

        ${state.kind === "loading" && html`<p class="hint">Loading…</p>`}
        ${state.kind === "unavailable" && html`<p class="hint warn">${state.error}</p>`}
        ${state.kind === "failed" && html`<p class="hint crit">${state.error}</p>`}
        ${state.kind === "ready" && rows.length === 0 && html`
            <p class="empty">${filter === "failed"
                ? "There were no failures — not one action ended with an error."
                : filter === "pending"
                ? "Every action reached an answer: none are left open."
                : "Nothing has been done yet. Records appear when the action buttons start working."}</p>
        `}

        ${rows.map((row) => {
            const result = RESULT[row.result || ""] || { word: row.result, kind: "" };
            const pending = !row.result;
            return html`
                <section class="card entry" key=${row.id}>
                    <div class="entry-head">
                        <span class="dot ${result.kind}"></span>
                        <span class="entry-what">${actionName(row.kind)} ${row.target}</span>
                    </div>
                    <div class="entry-meta">
                        <span class=${result.kind}>${result.word}</span>
                        ${pending && html`<span class="pulse connecting"></span>`}
                        ${row.error && html`<span class="crit">${row.error}</span>`}
                    </div>
                    ${row.detail && html`<div class="entry-meta detail">${row.detail}</div>`}
                    <div class="entry-meta">
                        <span>${row.device_name || "the device was deleted"}</span>
                        <span>${ago(row.ts)}</span>
                        ${row.duration_ms > 0 && html`<span>${duration(row.duration_ms)}</span>`}
                    </div>
                </section>
            `;
        })}

        ${state.kind === "ready" && rows.length > 0 && html`
            <button class="ghost wide" type="button" onClick=${more}>show more</button>
        `}
    `;
}

function duration(ms) {
    if (ms < 1000) return `${ms} ms`;
    return `${(ms / 1000).toFixed(1)} s`;
}
