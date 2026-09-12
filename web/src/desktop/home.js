// The desktop home screen: a grid of widgets.
import { useState } from "preact/hooks";

import { html } from "../html.js";
import { Icon } from "../ui/icons.js";
import { pct, plural, uptime } from "../format.js";
import * as usage from "../data/usage.js";
import { UsageLayer, nextStep } from "./usage/breakdown.js";
import { Hours } from "./usage/hours.js";
import { ScanLayer } from "./usage/scan.js";
import { Cache, Today } from "./usage/today.js";
import { Models, Projects, Tools } from "./usage/tops.js";

const TOPS_PERIOD = "30d";

const TODAY = { period: "today" };

function bucketLabel(range) {
    const at = new Date(range.from * 1000);
    const day = at.toLocaleDateString("ru-RU", { day: "2-digit", month: "2-digit" });
    if (range.to - range.from > 3600) return day;
    return `${at.toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit" })} · ${day}`;
}

export function Home({ snapshot, tree, onSection }) {
    const h = (snapshot && snapshot.host) || {};
    const sessions = (snapshot && snapshot.sessions) || [];
    const waiting = sessions.filter((x) => x.status === "waiting").length;
    const stacks = (tree && tree.stacks) || [];

    const [layer, setLayer] = useState(null);
    const scan = usage.useUsageScan();
    const today = usage.useUsageSummary(TODAY, scan.rounds);
    const todayHours = usage.useUsageSeries(TODAY, "hour", scan.rounds);

    const jumps = [
        {
            id: "sessions",
            label: "Sessions",
            icon: Icon.sessions,
            value: sessions.length,
            sub: waiting
                ? `${waiting} waiting for an answer`
                : (sessions.length ? "all busy with work" : "there are no live sessions"),
            hot: waiting > 0,
        },
        {
            id: "containers",
            label: "Containers",
            icon: Icon.containers,
            value: tree ? `${tree.running}/${tree.total}` : "—",
            sub: tree ? `${stacks.length} ${plural(stacks.length, "stack", "stacks")}` : "the tree is unavailable",
        },
        {
            id: "machine",
            label: "Machine",
            icon: Icon.cpu,
            value: pct(h.cpuPct),
            sub: `memory ${pct(h.mem && h.mem.pct)}${uptime(h.uptime) && ` · ${uptime(h.uptime)}`}`,
        },
    ];

    const openBreakdown = (period, by, row) => {
        const base = { by, label: by === usage.BY_SESSION ? "sessions" : "projects", filter: {} };
        const down = row ? nextStep(base, row) : null;
        setLayer({ kind: "breakdown", period, trail: down ? [base, down] : [base] });
    };

    const openBucket = (range) => setLayer({
        kind: "breakdown",
        pinned: bucketLabel(range),
        trail: [{ by: usage.BY_SESSION, label: "sessions", filter: { from: range.from, to: range.to } }],
    });

    return html`
        <section class="dkcenter">
            <div class="dkhead">
                <div class="dkheadtop">
                    <span class="dkheadname">home</span>
                    <span class="dkheadpath">${(snapshot && snapshot.hostName) || "host"} · overview</span>
                </div>
            </div>
            <div class="dkgrid">
                <div class="dkwidget w6 h1 dkjumps">
                    ${jumps.map((it) => html`
                        <button
                            key=${it.id}
                            class=${`dkjump${it.hot ? " hot" : ""}`}
                            type="button"
                            onClick=${() => onSection(it.id)}
                        >
                            <span class="dkjumpicon"><${it.icon} /></span>
                            <span class="dkjumpvalue">${it.value}</span>
                            <span class="dkjumplabel">${it.label}</span>
                            <span class="dkjumpsub">${it.sub}</span>
                        </button>
                    `)}
                </div>
                <${Today}
                    summary=${today}
                    series=${todayHours}
                    scan=${scan}
                    onScan=${() => setLayer({ kind: "scan" })}
                    onOpen=${() => openBreakdown("today", usage.BY_PROJECT, null)}
                />
                <${Cache}
                    summary=${today}
                    scan=${scan}
                    onOpen=${() => setLayer({
                        kind: "breakdown",
                        period: "today",
                        trail: [{ by: usage.BY_SESSION, label: "worst cache first", filter: {}, sort: "hit" }],
                    })}
                />
                <${Hours}
                    rounds=${scan.rounds}
                    scan=${scan}
                    onOpen=${(period, range) => (range ? openBucket(range) : openBreakdown(period, usage.BY_PROJECT, null))}
                />
                <${Projects}
                    rounds=${scan.rounds}
                    scan=${scan}
                    onOpen=${(row) => openBreakdown(TOPS_PERIOD, usage.BY_PROJECT, row)}
                />
                <${Models}
                    rounds=${scan.rounds}
                    scan=${scan}
                    onOpen=${() => setLayer({ kind: "models", period: TOPS_PERIOD })}
                />
                <${Tools}
                    rounds=${scan.rounds}
                    scan=${scan}
                    onOpen=${() => setLayer({ kind: "tools", period: TOPS_PERIOD })}
                />
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
