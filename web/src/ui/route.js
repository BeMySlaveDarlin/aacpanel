// Panel addresses: every router leg with a ping from where the panel is open.
import { useEffect, useState } from "preact/hooks";

import { html } from "../html.js";
import { LABELS, measure } from "../router.js";
import { Sheet } from "./sheet.js";

function routeWords(route) {
    const here = route && route.here ? LABELS[route.here] || route.here : "";
    const via = route && route.via ? LABELS[route.via] || route.via : "";
    if (!here) return { value: "—", brief: "the address map is unavailable" };
    const page = `page ${here}`;
    if (via) return { value: via, brief: `${page} · requests go through ${via}` };
    const why = {
        alone: `${page} · there is no closer address`,
        here: `${page} · requests from here too, nobody closer answered`,
        silent: `${page} · not one address answers`,
        "no-token": `${page} · the closer address answered, but no bearer was issued for it`,
        "no-map": "the address map is unavailable",
    }[route.why];
    return { value: here, brief: why || `${page} · requests from here too` };
}

export function RouteTable({ route }) {
    const [rows, setRows] = useState(null);
    const here = (route && route.here) || "";
    const via = (route && route.via) || "";

    const check = async () => {
        setRows(null);
        const got = await measure();
        setRows(got.rows);
        if (route && route.onRecheck) route.onRecheck();
    };
    useEffect(() => {
        let alive = true;
        measure().then((got) => alive && setRows(got.rows));
        return () => { alive = false; };
    }, []);

    return html`
        <div class="routetable">
            ${rows === null
                ? html`<p class="tbrief">measuring…</p>`
                : rows.length === 0
                ? html`<p class="tbrief">the address map is unavailable</p>`
                : rows.map((row) => html`
                    <div class=${`routerow ${row.ok ? "ok" : "bad"}${row.kind === here ? " here" : ""}${row.kind === via ? " via" : ""}`} key=${row.kind}>
                        <span class="routekind">${LABELS[row.kind] || row.kind}</span>
                        <span class="routeurl" title=${row.url}>${row.url.replace(/^https?:\/\//, "")}</span>
                        <span class="routems">${row.ok ? `${row.ms} ms` : "unavailable"}</span>
                        <span class="routehere">${row.kind === via ? "requests" : row.kind === here ? "page" : ""}</span>
                    </div>
                `)}
            <button class="ghost" type="button" disabled=${rows === null} onClick=${check}>check again</button>
        </div>
    `;
}

// routeChip returns the label and tooltip of the address button in the header.
export function routeChip(route) {
    const words = routeWords(route);
    const via = route && route.via ? LABELS[route.via] || route.via : "";
    return { text: via || words.value, tip: words.brief, near: Boolean(via) };
}

// RouteSheet is the address table behind that button.
export function RouteSheet({ open, onClose, route }) {
    const words = routeWords(route);
    return html`
        <${Sheet} open=${open} onClose=${onClose} label="panel addresses">
            <div class="shead">
                <div>
                    <div class="stitle">Connection</div>
                    <div class="ssub">${words.brief}</div>
                </div>
            </div>
            <${RouteTable} route=${route} />
        <//>
    `;
}
