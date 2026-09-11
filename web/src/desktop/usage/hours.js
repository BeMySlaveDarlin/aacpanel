// The main usage chart: spend by hour.
import { useEffect, useRef, useState } from "preact/hooks";

import { html } from "../../html.js";
import { area, Chart } from "../../chart.js";
import { tokens } from "../../format.js";
import * as usage from "../../data/usage.js";
import { Blank, Widget, stop, troubleOf } from "./parts.js";

const SERIES = [
    {},
    area("--accent", "#63a8ff"),
    area("--accent-2", "#7fe3d4", { opacity: 0.28, scale: "y2" }),
];

const RANGE = [0, null];
const Y2 = { range: RANGE, format: tokens };

const CHIPS = usage.PERIODS;

function pick(points, idx, step, onOpen, period) {
    const point = points[idx];
    if (!point) return;
    const from = Math.floor(Date.parse(point.at) / 1000);
    onOpen(period, { from, to: from + (step === "day" ? 86400 : 3600) });
}

function useBoxHeight(ref, initial) {
    const [height, setHeight] = useState(initial);
    useEffect(() => {
        const el = ref.current;
        if (!el) return undefined;
        const observer = new ResizeObserver(() => {
            const next = Math.round(el.clientHeight);
            setHeight((prev) => (next > 80 && Math.abs(prev - next) > 4 ? next : prev));
        });
        observer.observe(el);
        return () => observer.disconnect();
    }, [ref]);
    return height;
}

export function Hours({ rounds, scan, onOpen }) {
    const box = useRef(null);
    const height = useBoxHeight(box, 220);
    const [period, setPeriod] = useState("14d");
    const step = usage.stepFor(period);
    const state = usage.useUsageSeries({ period }, step, rounds);

    const points = (state.kind === "ready" && state.data.points) || [];
    const plot = [
        points.map((p) => Date.parse(p.at) / 1000),
        points.map(usage.inboundOf),
        points.map((p) => p.output || 0),
    ];

    return html`
        <${Widget}
            span="w8 h2"
            title=${step === "hour" ? "Usage by hour" : "Usage by day"}
            note=${html`
                <span class="ukey"><i style="background:var(--accent)"></i>inbound</span>
                <span class="ukey"><i style="background:var(--accent-2)"></i>output, right axis</span>
            `}
            actions=${html`
                <span class="uchips" onClick=${stop}>
                    ${CHIPS.map((p) => html`
                        <button
                            key=${p.id}
                            class=${`uchip${p.id === period ? " on" : ""}`}
                            type="button"
                            onClick=${() => setPeriod(p.id)}
                        >${p.label}</button>
                    `)}
                </span>
            `}
            onOpen=${scan.collected ? () => onOpen(period, null) : null}
        >
            <div class="uwgchart" ref=${box} onClick=${stop}>
                ${!scan.collected
                    ? html`<${Blank}>Nothing collected yet.<//>`
                    : troubleOf(state) || (points.length === 0
                        ? html`<${Blank}>No usage in this period.<//>`
                        : html`
                        <${Chart}
                            data=${plot}
                            series=${SERIES}
                            range=${RANGE}
                            y2=${Y2}
                            yFormat=${tokens}
                            height=${height}
                            onPick=${(idx) => pick(points, idx, step, onOpen, period)}
                        />
                    `)}
            </div>
        <//>
    `;
}
