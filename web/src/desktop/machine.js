// The Machine section: categories on the left, the picked one in the centre.
import { html } from "../html.js";
import { Icon } from "../ui/icons.js";
import { area, Chart } from "../chart.js";
import { ago, bytes, degrees, level, pct, plural, rate, uptime, withDegrees } from "../format.js";
import { OUTCOME } from "../alerts.js";
import { every, KIND_TITLE, probeState } from "../screens/probes.js";
import { mergeTop, PROCS_OK, useProcs } from "../procs.js";

export const CATS = [
    { id: "cpu", label: "cpu", icon: Icon.cpu },
    { id: "mem", label: "memory", icon: Icon.memory },
    { id: "disk", label: "disks", icon: Icon.disk },
    { id: "net", label: "network", icon: Icon.globe },
    { id: "probes", label: "probes", icon: Icon.probes },
];

export function MachineCats({ snapshot, probes: fromDB, current, onPick }) {
    const h = (snapshot && snapshot.host) || {};
    const disks = h.disks || [];
    const nets = h.net || [];
    const probes = fromDB || [];
    const ok = probes.filter((p) => probeState(p) === "ok").length;

    const value = {
        cpu: pct(h.cpuPct),
        mem: pct(h.mem && h.mem.pct),
        disk: disks.length ? pct(Math.max(...disks.map((d) => d.pct || 0))) : "—",
        net: rate(nets.reduce((a, n) => a + (n.rxRate || 0), 0)),
        probes: probes.length ? `${ok}/${probes.length}` : "0/0",
    };
    const sub = {
        cpu: withDegrees(`${h.cpus || "?"} cores · load ${(h.load || [])[0] !== undefined ? h.load[0].toFixed(2) : "—"}`, h.cpuTemp),
        mem: withDegrees(`${bytes(h.mem && h.mem.used)} of ${bytes(h.mem && h.mem.total)}`, h.memTemp),
        disk: withDegrees(`${disks.length} ${plural(disks.length, "partition", "partitions")}`, h.diskTemp),
        net: `${nets.length} ${plural(nets.length, "interface", "interfaces")}`,
        probes: probes.length ? `${probes.length - ok} silent` : "no probes",
    };
    const load = {
        cpu: h.cpuPct,
        mem: h.mem && h.mem.pct,
        disk: disks.length ? Math.max(...disks.map((d) => d.pct || 0)) : 0,
    };

    return html`
        <aside class="dkleft">
            <div class="dkcontour">
                <span class="dkcontourname">machine</span>
                <span class="dkcontournum">${uptime(h.uptime)}</span>
            </div>
            <div class="dkscroll">
                ${CATS.map((c) => html`
                    <button
                        key=${c.id}
                        class=${`dkcat${current === c.id ? " on" : ""}`}
                        type="button"
                        style=${`--fill:${load[c.id] === undefined ? 0 : Math.min(100, load[c.id] || 0)}%`}
                        onClick=${() => onPick(c.id)}
                    >
                        <span class="dkcaticon"><${c.icon} /></span>
                        <span class="dkcatbody">
                            <span class="dkcatname">${c.label}</span>
                            <span class="dkcatsub">${sub[c.id]}</span>
                        </span>
                        <span class="dkcatvalue">${value[c.id]}</span>
                        ${load[c.id] !== undefined && html`
                            <span class=${`dkcatbar dk${level(load[c.id] || 0)}`}>
                                <i style=${`width:${Math.min(100, load[c.id] || 0)}%`}></i>
                            </span>
                        `}
                    </button>
                `)}
            </div>
        </aside>
    `;
}

function Plot({ values, stamps, token, fallback, fmt, height = 190 }) {
    if (!values || values.length < 2) {
        return html`<div class="dkplot dkplotempty">no samples yet: the history builds up since the tab was opened</div>`;
    }
    return html`
        <div class="dkplot">
            <${Chart} data=${[stamps, values]} series=${[{}, area(token, fallback)]} height=${height} yFormat=${fmt} />
        </div>
    `;
}

function TopTable({ top, procs, kind, fmt }) {
    const rows = mergeTop(top, procs, kind, { field: "avg", limit: 12 });
    if (rows.length === 0) return null;
    const noProcs = !procs || procs.state !== PROCS_OK;
    return html`
        <div class="dktable">
            <div class="dktr dkth dkttr">
                <span>kind</span><span>who</span>
                <span class="dkc-num">avg</span><span class="dkc-num">peak</span>
            </div>
            ${rows.map((r) => html`
                <div class="dktr dkttr" key=${r.key} title=${r.hint || r.name}>
                    <span class="dkkind">${r.kind}</span>
                    <span class="dkc-name">${r.name}</span>
                    <span class="dkc-num">${fmt(r.value)}</span>
                    <span class="dkc-num">${r.max === undefined ? "—" : fmt(r.max)}</span>
                </div>
            `)}
            ${noProcs
                ? html`<p class="dkempty">host processes do not get here: the agent does not collect them</p>`
                : html`<p class="dktopnote">containers show the hourly average and peak, processes show right now: no history is written for them</p>`}
        </div>
    `;
}

const ALERT_AFTER = 3;

function ProbeRow({ probe }) {
    const last = probe.last;
    const st = probeState(probe);
    const tone = { ok: "", warn: "dkwarn", crit: "dkbad", off: "dkoff" }[st];

    const answer = !probe.enabled ? "disabled"
        : !last ? "not checked yet"
        : st === "ok" ? "answers"
        : OUTCOME[last.outcome] || last.outcome;

    const why = [];
    if (last && last.error) why.push(last.error);
    if (probe.failStreak > 0) {
        why.push(probe.failStreak >= ALERT_AFTER
            ? `${probe.failStreak} ${plural(probe.failStreak, "failure", "failures")} in a row — the alert is raised`
            : `${probe.failStreak} of ${ALERT_AFTER} to the alert`);
    }
    if (st === "crit" || st === "warn") {
        why.push(probe.lastOk
            ? `last answered ${ago(new Date(probe.lastOk * 1000).toISOString())}`
            : "has not answered once in a day");
    }

    return html`
        <div class="dkprow">
            <div class="dktr dkptr">
                <span class="dkc-state">
                    <i class=${`dkdot ${{ ok: "dkok", warn: "dkwaiting", crit: "dkbad", off: "dkoff" }[st]}`}></i>
                </span>
                <span class="dkc-name" title=${probe.name}>${probe.name}</span>
                <span class="dkc-status" title=${`${probe.target} · ${KIND_TITLE[probe.kind] || probe.kind}`}>${probe.target}</span>
                <span class=${`dkpanswer ${tone}`}>${answer}</span>
                <span class="dkc-num">${last && last.latencyMs != null ? `${last.latencyMs} ms` : "—"}</span>
                <span class="dkc-num">${last ? ago(new Date(last.ts * 1000).toISOString()) : "—"}</span>
                <span class="dkc-num">${every(probe.intervalSec)}</span>
            </div>
            ${why.length > 0 && html`<div class=${`dkpwhy ${tone}`}>${why.join(" · ")}</div>`}
        </div>
    `;
}

export function MachineCenter({ snapshot, probes: fromDB, history, topCpu, topMem, cat }) {
    const { procs } = useProcs(cat === "cpu" || cat === "mem");
    const h = (snapshot && snapshot.host) || {};
    const stamps = (history || []).map((p) => p.t);
    const disks = h.disks || [];
    const nets = h.net || [];
    const probes = fromDB || [];
    const title = (CATS.find((c) => c.id === cat) || CATS[0]).label;

    const body = () => {
        if (cat === "mem") {
            const m = h.mem || {};
            return html`
                <div class="dkmbody">
                    <${Plot} values=${(history || []).map((p) => p.mem)} stamps=${stamps}
                        token="--ok" fallback="#6b9c83" fmt=${(v) => `${Math.round(v)}%`} />
                    <div class="dkfacts">
                        <div class="dkcard"><span>used</span><b>${bytes(m.used)}</b></div>
                        <div class="dkcard"><span>free</span><b>${bytes(m.available)}</b></div>
                        <div class="dkcard"><span>total</span><b>${bytes(m.total)}</b></div>
                        <div class="dkcard"><span>swap</span><b>${bytes(m.swapUsed)} of ${bytes(m.swapTotal)}</b></div>
                        ${h.memTemp != null && html`<div class="dkcard"><span>temperature</span><b>${degrees(h.memTemp)}</b></div>`}
                    </div>
                    <${TopTable} top=${topMem} procs=${procs} kind="mem" fmt=${bytes} />
                </div>
            `;
        }
        if (cat === "disk") {
            return html`
                <div class="dkmbody">
                    <div class="dktable">
                        <div class="dktr dkth dkdtr">
                            <span>mount point</span><span>device</span><span>fs</span>
                            <span class="dkc-num">used</span><span class="dkc-num">free</span><span class="dkc-num">total</span>
                        </div>
                        ${disks.map((d) => html`
                            <div class="dktr dkdtr" key=${d.mount}>
                                <span class="dkc-name" title=${d.mount}>${d.mount}</span>
                                <span class="dkc-status">${d.device}</span>
                                <span class="dkc-status">${d.fstype}</span>
                                <span class="dkc-num">${pct(d.pct)}</span>
                                <span class="dkc-num">${bytes(d.free)}</span>
                                <span class="dkc-num">${bytes(d.total)}</span>
                            </div>
                        `)}
                    </div>
                    <div class="dkfacts">
                        ${disks.map((d) => html`
                            <div class="dkcard" key=${d.mount}>
                                <span>${d.mount}</span>
                                <b>${pct(d.pct)}</b>
                                <span class="dkdbar"><i class=${(d.pct || 0) > 85 ? "hot" : ""} style=${`width:${Math.min(100, d.pct || 0)}%`}></i></span>
                            </div>
                        `)}
                        ${(h.diskTemps || []).map((d) => html`
                            <div class="dkcard" key=${`temp/${d.name}`} title=${d.model || d.name}>
                                <span>${d.name}</span>
                                <b>${degrees(d.temp)}</b>
                            </div>
                        `)}
                    </div>
                </div>
            `;
        }
        if (cat === "net") {
            return html`
                <div class="dkmbody">
                    <${Plot} values=${(history || []).map((p) => p.rx)} stamps=${stamps}
                        token="--accent" fallback="#7d99bd" fmt=${(v) => rate(v)} />
                    <div class="dktable">
                        <div class="dktr dkth dkntr">
                            <span>interface</span><span>address</span>
                            <span class="dkc-num">rx</span><span class="dkc-num">tx</span><span class="dkc-num">link</span>
                        </div>
                        ${nets.map((n) => html`
                            <div class="dktr dkntr" key=${n.name}>
                                <span class="dkc-name">${n.name}</span>
                                <span class="dkc-status">${n.addr || "—"}</span>
                                <span class="dkc-num">${rate(n.rxRate)}</span>
                                <span class="dkc-num">${rate(n.txRate)}</span>
                                <span class="dkc-num">${n.state === "up" ? `${n.speed || "?"} Mbit` : "no"}</span>
                            </div>
                        `)}
                    </div>
                </div>
            `;
        }
        if (cat === "probes") {
            return html`
                <div class="dkmbody">
                    ${probes.length === 0
                        ? html`<p class="dkempty">there are no probes</p>`
                        : html`
                            <div class="dktable">
                                <div class="dktr dkptr dkth">
                                    <span></span><span>probe</span><span>target</span><span>answer</span>
                                    <span class="dkc-num">latency</span>
                                    <span class="dkc-num">polled</span>
                                    <span class="dkc-num">interval</span>
                                </div>
                                ${probes.map((p) => html`<${ProbeRow} key=${p.id || p.name} probe=${p} />`)}
                            </div>
                            <p class="dktopnote">
                                status pages are read by their content, not by their code: they answer
                                200 during an outage too
                            </p>
                        `}
                </div>
            `;
        }
        return html`
            <div class="dkmbody">
                <${Plot} values=${(history || []).map((p) => p.cpu)} stamps=${stamps}
                    token="--accent" fallback="#7d99bd" fmt=${(v) => `${Math.round(v)}%`} />
                <div class="dkfacts">
                    <div class="dkcard"><span>now</span><b>${pct(h.cpuPct)}</b></div>
                    <div class="dkcard"><span>cores</span><b>${h.cpus || "—"}</b></div>
                    <div class="dkcard"><span>load</span><b>${(h.load || []).map((x) => x.toFixed(2)).join(" · ") || "—"}</b></div>
                    ${h.cpuTemp != null && html`<div class="dkcard"><span>temperature</span><b>${degrees(h.cpuTemp)}</b></div>`}
                </div>
                <${TopTable} top=${topCpu} procs=${procs} kind="cpu" fmt=${pct} />
            </div>
        `;
    };

    return html`
        <section class="dkcenter">
            <div class="dkhead">
                <div class="dkheadtop">
                    <span class="dkheadname">${title}</span>
                    <span class="dkheadpath">
                        ${h.cpus || "—"} cores · memory ${bytes(h.mem && h.mem.total)}${uptime(h.uptime) && ` · ${uptime(h.uptime)}`}
                    </span>
                </div>
            </div>
            ${body()}
        </section>
    `;
}
