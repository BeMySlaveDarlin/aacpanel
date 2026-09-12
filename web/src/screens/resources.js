// The “Resources” tab: cpu, memory, disks, network.
import { useMemo, useState } from "preact/hooks";

import { html } from "../html.js";
import { NotRecorded, Stale, Trouble } from "../ui/trouble.js";
import { bytes, degrees, level, pct, plural, rate, withDegrees } from "../format.js";
import { area, Chart, spanRange } from "../chart.js";
import { Metric } from "../ui/metric.js";
import { TopList } from "../ui/toplist.js";
import { Icon } from "../ui/icons.js";
import { netPlot, PERIODS, resolutionText, useNetHistory } from "../history.js";
import { Chips } from "../ui/chips.js";
import { bytes as fmtBytes } from "../format.js";

const MIN_POINTS = 3;

const SPARK = [{}, area("--accent", "#63a8ff")];
const SPARK_TEAL = [{}, area("--accent-2", "#7fe3d4")];

const CPU_RANGE = spanRange(10);
const MEM_RANGE = spanRange(5);
const NET_RANGE = spanRange(64 * 1024);

export function resourceChips(snapshot) {
    const host = (snapshot && snapshot.host) || {};
    return [
        { id: "all", label: "All" },
        { id: "cpu", label: "CPU" },
        { id: "mem", label: "Memory" },
        { id: "net", label: "Network", count: (host.net || []).filter((n) => n.rxRate || n.txRate).length },
        { id: "disks", label: "Disks", count: (host.disks || []).length },
    ];
}

export function Resources({ snapshot, error, ageSec, filter = "all", history = [], faults = [] }) {
    const host = snapshot && snapshot.host;

    const cpuData = useSeries(history, "cpu");
    const memData = useSeries(history, "mem");
    const rxData = useSeries(history, "rx");

    if (error || !host) {
        return html`
            <${Trouble} what="host load" error=${error || "there is no agent snapshot"} hint="the numbers are collected by aacpanel-agent on the host." />
            <${Metric} title="cpu" subject="host" metric="cpu" top="cpu"
                yFormat=${(v) => `${Math.round(v)}%`}
                format=${(s) => `peak over the period: ${peak(s).toFixed(1)}%`} />
            <${Metric} title="memory" subject="host" metric="mem" top="mem"
                yFormat=${(v) => bytes(v)}
                format=${(s) => `peak over the period: ${bytes(peak(s))}`} />
            <${Traffic} />
        `;
    }

    const show = (id) => filter === "all" || filter === id;
    const enough = history.length >= MIN_POINTS;
    const cpuLine = withDegrees(`load ${host.load.map((n) => n.toFixed(2)).join(" · ")}`, host.cpuTemp);
    const memLine = withDegrees(`${bytes(host.mem.used)} of ${bytes(host.mem.total)}`, host.memTemp);

    return html`
        <${Stale} ageSec=${ageSec} />
        <${NotRecorded} faults=${faults} blocks=${["host", "disks", "net"]} />

        <div class="metrics">
        ${show("cpu") && html`
            <${Metric}
                title="CPU"
                icon=${Icon.cpu()}
                value=${Math.round(host.cpuPct)}
                unit="%"
                kind=${level(host.cpuPct)}
                brief=${cpuLine}
                hint=${`${host.cpus} ${plural(host.cpus, "core", "cores")}`}
                subject="host"
                metric="cpu"
                top="cpu"
                yFormat=${(v) => `${Math.round(v)}%`}
                summary=${html`
                    <p class="sub">${cpuLine}</p>
                `}
                format=${(s) => `peak over the period: ${peak(s).toFixed(1)}%`}
                fallback=${enough && html`
                    <p class="hint">showing the tab buffer — it builds up since the tab was opened and does not survive a reload</p>
                    <${Chart} data=${cpuData} series=${SPARK} height=${44} compact range=${CPU_RANGE} />
                `}
            />
        `}

        ${show("mem") && html`
            <${Metric}
                title="Memory"
                icon=${Icon.memory()}
                value=${Math.round(host.mem.pct)}
                unit="%"
                kind=${level(host.mem.pct)}
                brief=${memLine}
                subject="host"
                metric="mem"
                top="mem"
                yFormat=${(v) => fmtBytes(v)}
                summary=${html`
                    <p class="numbers">${memLine}</p>
                    ${host.mem.swapUsed > 0 && html`<p class="sub">swap ${bytes(host.mem.swapUsed)} of ${bytes(host.mem.swapTotal)}</p>`}
                `}
                format=${(s) => `peak over the period: ${fmtBytes(peak(s))}`}
                fallback=${enough && html`
                    <p class="hint">showing the tab buffer</p>
                    <${Chart} data=${memData} series=${SPARK_TEAL} height=${44} compact range=${MEM_RANGE} />
                `}
            />
        `}

        ${show("disks") && html`
            <${Disks} disks=${host.disks} temps=${host.diskTemps} hottest=${host.diskTemp} />
        `}
        </div>

        ${show("net") && html`
            <section class="card">
                <h2>Network</h2>
                ${host.net.map((n) => html`
                    <div class="netrow" key=${n.name}>
                        <div class="peak">
                            <span class="nm">${n.name}${n.addr ? ` · ${n.addr}` : ""}</span>
                            <span class="v">↓ ${rate(n.rxRate)} · ↑ ${rate(n.txRate)}</span>
                        </div>
                        <p class="sub">
                            <span class=${n.state === "up" ? "" : "warn"}>${linkState(n.state)}</span>
                            ${n.speed > 0 && ` · ${n.speed} Mbit/s`}
                        </p>
                    </div>
                `)}
                ${enough && html`<${Chart} data=${rxData} series=${SPARK} height=${44} compact range=${NET_RANGE} />`}
            </section>
            <${Traffic} />
        `}

    `;
}

const LINK = {
    up: "link is up",
    down: "link is down",
    dormant: "waiting for a connection",
    lowerlayerdown: "the lower layer is down",
    testing: "testing",
    notpresent: "no adapter",
    unknown: "state unknown",
};

function linkState(state) {
    return LINK[state] || state || "state unknown";
}

function Traffic() {
    const [period, setPeriod] = useState("1h");
    const state = useNetHistory(period);
    const net = state.kind === "ready" ? state.net : null;
    const ifaces = (net && net.ifaces) || [];

    return html`
        <section class="card">
            <h2>Traffic over the period</h2>
            <${Chips}
                items=${PERIODS.filter((p) => p.id !== "30m")}
                current=${period}
                onSelect=${setPeriod}
            />

            ${state.kind === "loading" && html`<p class="hint">Loading…</p>`}
            ${state.kind === "unavailable" && html`<p class="hint warn">${state.error}</p>`}
            ${state.kind === "failed" && html`<p class="hint crit">${state.error}</p>`}
            ${net && ifaces.length === 0 && html`<p class="hint">There are no network records for this period.</p>`}

            ${ifaces.map((iface) => {
                const data = netPlot(net, iface);
                return html`
                    <div class="netrow" key=${iface.name}>
                        <div class="peak">
                            <span class="nm">${iface.name}</span>
                            <span class="v">↓ ${bytes(iface.rxTotal)} · ↑ ${bytes(iface.txTotal)}</span>
                        </div>
                        ${data
                            ? html`<${Chart} data=${data} series=${TRAFFIC} height=${64} yFormat=${(v) => rate(v)} />`
                            : html`<p class="sub">there was not a single sample over the period</p>`}
                    </div>
                `;
            })}

            ${ifaces.length > 0 && html`
                <p class="hint">
                    ${resolutionText(net.resolution, net.stepSec)} · the chart shows the average speed in a slice,
                    the volume comes from the interface counters
                </p>
            `}
        </section>
    `;
}

const TRAFFIC = [{}, area("--accent", "#63a8ff"), area("--accent-2", "#7fe3d4")];

function Disks({ disks, temps, hottest }) {
    const [open, setOpen] = useState(false);
    const [period, setPeriod] = useState("1h");

    const root = (disks || []).find((d) => d.mount === "/") || (disks || [])[0];
    const devices = temps || [];

    return html`
        <section class="card tile" data-open=${open ? "1" : "0"}>
            <button class="cardhead" type="button" onClick=${() => setOpen((v) => !v)}>
                <span class="tname">${Icon.disk()}<span>${open ? "Disks" : "Disk /"}</span></span>
                <span class="chev">${Icon.chevron()}</span>
            </button>
            ${!open && root && html`
                <div class=${`tval ${level(root.pct)}`}>${Math.round(root.pct)}<s>%</s></div>
                <p class="tbrief">${withDegrees(`${bytes(root.free)} free`, hottest)}</p>
                <div class="bar"><i class=${level(root.pct)} style=${`width:${Math.min(100, root.pct)}%`}></i></div>
            `}
            ${open && (disks || []).map((disk) => html`
                <div class="kv" key=${disk.mount}>
                    <span class="k">${disk.mount}</span>
                    <span class="v ${level(disk.pct)}">${pct(disk.pct)} · ${bytes(disk.free)} free</span>
                </div>
            `)}
            ${open && devices.map((d) => html`
                <div class="kv" key=${d.name}>
                    <span class="k">${d.name}${d.model ? ` · ${d.model}` : ""}</span>
                    <span class="v">${degrees(d.temp)}</span>
                </div>
            `)}

            ${open && html`
                <div class="periods">
                    ${PERIODS.map((p) => html`
                        <button
                            key=${p.id}
                            class="chip"
                            type="button"
                            aria-pressed=${period === p.id ? "true" : "false"}
                            onClick=${() => setPeriod(p.id)}
                        >${p.label}</button>
                    `)}
                </div>
                <div class="subhead">who wrote the most</div>
                <${TopList} metric="disk" period=${period} />
            `}
        </section>
    `;
}

function peak(series) {
    let best = 0;
    for (const value of series.max || []) {
        if (value !== null && value > best) best = value;
    }
    return best;
}

function useSeries(history, key) {
    return useMemo(
        () => [history.map((p) => p.t), history.map((p) => (p[key] === undefined ? null : p[key]))],
        [history, key],
    );
}
