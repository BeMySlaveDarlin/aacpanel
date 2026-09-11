// The probes screen: tunnels, ports and external APIs.
import { html } from "../html.js";
import { ago, plural } from "../format.js";
import { OUTCOME } from "../alerts.js";
import { NotRecorded, Trouble } from "../ui/trouble.js";

const ALERT_AFTER = 3;

export const KIND_TITLE = {
    tcp: "port",
    http: "http",
    status: "status page",
    wg: "tunnel",
};

// every renders how often the probe runs.
export function every(sec) {
    if (!sec) return "—";
    if (sec < 60) return `every ${sec} s`;
    const min = Math.round(sec / 60);
    if (min < 60) return `every ${min} min`;
    return `every ${Math.round(min / 60)} h`;
}

const RUNNERS = [
    { id: "agent", title: "from the host", hint: "run by aacpanel-agent: tunnels and local ports" },
    { id: "service", title: "from the service", hint: "run by aacpanel: external APIs" },
];

// Probes renders the list of availability probes.
export function Probes({ faults = [], state }) {

    return html`
        <${NotRecorded} faults=${faults} blocks=${["probes"]} />

        ${state.kind === "loading" && html`<p class="hint">Loading…</p>`}
        ${state.kind === "unavailable" && html`
            <${Trouble} what="probes" error=${state.error}
                hint="their state is kept in the database; the probes themselves keep running." />
        `}
        ${state.kind === "failed" && html`<p class="hint crit">${state.error}</p>`}
        ${state.kind === "ready" && state.probes.length === 0 && html`
            <p class="empty">No probes are set up.</p>
        `}

        ${state.kind === "ready" && RUNNERS.map((runner) => {
            const own = state.probes.filter((p) => p.runner === runner.id);
            if (own.length === 0) return null;
            return html`
                <div class="grouphead" key=${runner.id}>${runner.title}</div>
                <p class="hint">${runner.hint}</p>
                ${own.map((probe) => html`<${Row} key=${probe.id} probe=${probe} />`)}
            `;
        })}
    `;
}

function Row({ probe }) {
    const last = probe.last;
    const state = probeState(probe);

    return html`
        <section class="card probe-card">
            <div class="entry-head">
                <span class="dot ${state}"></span>
                <span class="entry-what">${probe.name}</span>
                ${!probe.enabled && html`<span class="tag">disabled</span>`}
            </div>

            <div class="entry-meta">
                <span class=${state === "ok" ? "ok" : state}>
                    ${last ? OUTCOME[last.outcome] || last.outcome : "not checked yet"}
                </span>
                ${last && last.ok && last.latencyMs != null && html`<span>${last.latencyMs} ms</span>`}
                ${last && last.error && html`<span class=${state === "crit" ? "crit" : "warn"}>${last.error}</span>`}
            </div>

            ${probe.failStreak > 0 && html`
                <div class="entry-meta">
                    <span class=${probe.failStreak >= ALERT_AFTER ? "crit" : "warn"}>
                        ${probe.failStreak >= ALERT_AFTER
                            ? `${probe.failStreak} ${plural(probe.failStreak, "failure", "failures")} in a row — the alert is raised`
                            : `${probe.failStreak} of ${ALERT_AFTER} to the alert`}
                    </span>
                </div>
            `}

            ${last && !last.ok && html`
                <div class="entry-meta">
                    <span class="warn">
                        ${probe.lastOk
                            ? `last answered ${ago(new Date(probe.lastOk * 1000).toISOString())}`
                            : "has not answered once in a day"}
                    </span>
                </div>
            `}

            <div class="entry-meta">
                <span>${probe.target}</span>
                <span>${KIND_TITLE[probe.kind] || probe.kind}</span>
                <span>${every(probe.intervalSec)}</span>
                ${last && html`<span>checked ${ago(new Date(last.ts * 1000).toISOString())}</span>`}
            </div>
        </section>
    `;
}

export function probeState(probe) {
    if (!probe.enabled) return "off";
    if (!probe.last) return "off";
    if (probe.last.ok) return "ok";
    return probe.last.outcome === "config" || probe.last.outcome === "degraded" ? "warn" : "crit";
}
