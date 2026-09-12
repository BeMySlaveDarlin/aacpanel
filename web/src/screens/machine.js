// The “Machine” page: everything about the host in one place.
import { useState } from "preact/hooks";

import { html } from "../html.js";
import { BackHead } from "../ui/back.js";
import { Icon } from "../ui/icons.js";
import { level, uptime } from "../format.js";
import { useProbes } from "../alerts.js";
import { Resources } from "./resources.js";
import { Probes, probeState } from "./probes.js";

function Block({ title, icon, value, unit, brief, fill, kind, open, onToggle, children }) {
    return html`
        <section class="card tile" data-open=${open ? "1" : "0"}>
            <button class="cardhead" type="button" onClick=${onToggle}>
                <span class="tname">${icon}<span>${title}</span></span>
                <span class="chev">${Icon.chevron()}</span>
            </button>
            ${!open && value != null && html`
                <div class=${`tval ${kind || ""}`}>${value}${unit && html`<s>${unit}</s>`}</div>
            `}
            ${!open && brief && html`<p class="tbrief">${brief}</p>`}
            ${!open && fill != null && html`
                <div class="bar"><i class=${kind || ""} style=${`width:${Math.min(100, fill)}%`}></i></div>
            `}
            ${open && children}
        </section>
    `;
}

function probeBrief(probes, watched) {
    if (probes.kind !== "ready") return probes.kind === "loading" ? "loading…" : "state unknown";
    if (probes.probes.length === 0) return "none set up";
    if (watched === 0) return "all disabled";
    return "answering";
}

export function Machine({ snapshot, error, ageSec, history = [], faults = [], onBack }) {
    const [open, setOpen] = useState("");
    const toggle = (id) => setOpen((cur) => (cur === id ? "" : id));

    const probes = useProbes();
    const seen = probes.probes.map(probeState);
    const watched = seen.filter((state) => state !== "off").length;
    const ok = seen.filter((state) => state === "ok").length;
    const up = uptime(snapshot && snapshot.host && snapshot.host.uptime);

    return html`
        <${BackHead} onBack=${onBack} label="to the containers">
            <h2>Machine</h2>
            ${up && html`<span class="hint">${up}</span>`}
        <//>

        <div class="mgrid">
            <${Resources} snapshot=${snapshot} error=${error} ageSec=${ageSec} filter="all"
                history=${history} faults=${faults} />

            <${Block} title="Probes" icon=${Icon.probes()}
                value=${probes.kind === "ready" && watched > 0 ? `${ok}/${watched}` : null}
                brief=${probeBrief(probes, watched)}
                fill=${probes.kind === "ready" && watched > 0 ? (ok / watched) * 100 : null}
                kind=${probes.kind === "ready" && watched > 0 ? level(((watched - ok) / watched) * 100) : ""}
                open=${open === "probes"} onToggle=${() => toggle("probes")}>
                <${Probes} faults=${faults} state=${probes} />
            <//>
        </div>
    `;
}

// machineStats returns the three host figures for the containers header.
export function machineStats(snapshot) {
    const host = snapshot && snapshot.host;
    if (!host) return null;
    const net = (host.net || []).reduce((sum, n) => sum + (n.rxRate || 0) + (n.txRate || 0), 0);
    return {
        cpuPct: host.cpuPct || 0,
        memPct: (host.mem && host.mem.pct) || 0,
        net,
    };
}
