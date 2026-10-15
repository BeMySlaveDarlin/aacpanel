// The desktop home screen, the live part on top. Now: the graph of the
// sessions, and beside it the containers, the machine and today's spend.
// Period below: the four tiles that answer what the spend went on, under one
// switch of the period, so the four always speak of the same days.
import { useState } from "preact/hooks";

import { html } from "../html.js";
import { Icon } from "../ui/icons.js";
import { pct, plural, uptime } from "../format.js";
import * as usage from "../data/usage.js";
import { Orbits } from "./orbits/orbits.js";
import { UsageLayer, nextStep } from "./usage/breakdown.js";
import { Hours } from "./usage/hours.js";
import { ScanLayer } from "./usage/scan.js";
import { Today } from "./usage/today.js";
import { Models, Projects, Tools } from "./usage/tops.js";

// The period the four tiles open on: two weeks is the step by the hour still
// reads, and the span a working habit shows in.
export const PERIOD = "14d";

const TODAY = { period: "today" };

function bucketLabel(range) {
    const at = new Date(range.from * 1000);
    const day = at.toLocaleDateString("ru-RU", { day: "2-digit", month: "2-digit" });
    if (range.to - range.from > 3600) return day;
    return `${at.toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit" })} · ${day}`;
}

// Jump is a line that leads to a section: its figure, what it is, and the
// way there.
function Jump({ icon, value, label, sub, onOpen }) {
    return html`
        <button class="dkhmjump" type="button" onClick=${onOpen}>
            <span class="dkhmjumpicon"><${icon} /></span>
            <span class="dkhmjumpvalue">${value}</span>
            <span class="dkhmjumplabel">
                <b>${label}</b>
                <span>${sub}</span>
            </span>
            <span class="dkhmjumpgo">open<${Icon.chevron} /></span>
        </button>
    `;
}

// Group is the quiet heading over the tiles that answer one question.
function Group({ name, note, children }) {
    return html`
        <div class="dkhmgroup">
            <h2>${name}</h2>
            ${note && html`<p>${note}</p>`}
            ${children}
        </div>
    `;
}

// PeriodSwitch is the one switch of the period of the four tiles.
function PeriodSwitch({ period, onPick }) {
    return html`
        <span class="uchips dkhmperiod" role="group" aria-label="Period">
            ${usage.PERIODS.map((p) => html`
                <button
                    key=${p.id}
                    class=${`uchip${p.id === period ? " on" : ""}`}
                    type="button"
                    aria-pressed=${p.id === period ? "true" : "false"}
                    onClick=${() => onPick(p.id)}
                >${p.label}</button>
            `)}
        </span>
    `;
}

export function Home({ snapshot, tree, exec, wait, theme, onSection, onSession, onTerm }) {
    const h = (snapshot && snapshot.host) || {};
    const stacks = (tree && tree.stacks) || [];

    const [layer, setLayer] = useState(null);
    const [period, setPeriod] = useState(PERIOD);
    const scan = usage.useUsageScan();
    const today = usage.useUsageSummary(TODAY, scan.rounds);
    const todayHours = usage.useUsageSeries(TODAY, "hour", scan.rounds);

    const openBreakdown = (at, by, row) => {
        const base = { by, label: by === usage.BY_SESSION ? "sessions" : "projects", filter: {} };
        const down = row ? nextStep(base, row) : null;
        setLayer({ kind: "breakdown", period: at, trail: down ? [base, down] : [base] });
    };

    const openBucket = (range) => setLayer({
        kind: "breakdown",
        pinned: bucketLabel(range),
        trail: [{ by: usage.BY_SESSION, label: "sessions", filter: { from: range.from, to: range.to } }],
    });

    const stopped = tree ? tree.total - tree.running : 0;

    return html`
        <section class="dkcenter">
            <div class="dkhead">
                <div class="dkheadtop">
                    <span class="dkheadname">home</span>
                    <span class="dkheadpath">${(snapshot && snapshot.hostName) || "host"} · overview</span>
                </div>
            </div>
            <div class="dkhm">
                <${Group} name="Now" />
                <div class="dkhmnow">
                    <${Orbits}
                        snapshot=${snapshot}
                        exec=${exec}
                        wait=${wait}
                        theme=${theme}
                        onSession=${onSession}
                        onTerm=${onTerm}
                    />
                    <div class="dkhmside">
                        <div class="dkwidget dkhmjumps">
                            <${Jump}
                                icon=${Icon.containers}
                                value=${tree ? `${tree.running}/${tree.total}` : "—"}
                                label="Containers"
                                sub=${tree
                                    ? `${stacks.length} ${plural(stacks.length, "stack", "stacks")} · ${stopped} stopped`
                                    : "the tree is unavailable"}
                                onOpen=${() => onSection("containers")}
                            />
                            <${Jump}
                                icon=${Icon.cpu}
                                value=${pct(h.cpuPct)}
                                label="Machine"
                                sub=${`memory ${pct(h.mem && h.mem.pct)}${uptime(h.uptime) && ` · ${uptime(h.uptime)}`}`}
                                onOpen=${() => onSection("machine")}
                            />
                        </div>
                        <${Group} name="Today" />
                        <${Today}
                            summary=${today}
                            series=${todayHours}
                            scan=${scan}
                            onScan=${() => setLayer({ kind: "scan" })}
                            onOpen=${() => openBreakdown("today", usage.BY_PROJECT, null)}
                            onCache=${() => setLayer({
                                kind: "breakdown",
                                period: "today",
                                trail: [{ by: usage.BY_SESSION, label: "worst cache first", filter: {}, sort: "hit" }],
                            })}
                        />
                    </div>
                </div>
                <${Group} name="Period" note="one switch for all four">
                    <${PeriodSwitch} period=${period} onPick=${setPeriod} />
                <//>
                <div class="dkhmgrid">
                    <${Hours}
                        period=${period}
                        rounds=${scan.rounds}
                        scan=${scan}
                        onOpen=${(range) => (range ? openBucket(range) : openBreakdown(period, usage.BY_PROJECT, null))}
                    />
                    <${Projects}
                        period=${period}
                        rounds=${scan.rounds}
                        scan=${scan}
                        onOpen=${(row) => openBreakdown(period, usage.BY_PROJECT, row)}
                    />
                    <${Models}
                        period=${period}
                        rounds=${scan.rounds}
                        scan=${scan}
                        onOpen=${() => setLayer({ kind: "models", period })}
                    />
                    <${Tools}
                        period=${period}
                        rounds=${scan.rounds}
                        scan=${scan}
                        onOpen=${() => setLayer({ kind: "tools", period })}
                    />
                </div>
            </div>
            ${layer && layer.kind === "scan" && html`
                <${ScanLayer} scan=${scan} onClose=${() => setLayer(null)} />
            `}
            ${layer && layer.kind !== "scan" && html`
                <${UsageLayer} view=${layer} onClose=${() => setLayer(null)} />
            `}
        </section>
    `;
}
