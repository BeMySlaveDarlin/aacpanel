import { useState } from "preact/hooks";

import { html } from "../html.js";
import { area, Chart, stroke, tint } from "../chart.js";
import { Icon } from "./icons.js";
import { TopList } from "./toplist.js";
import { PERIODS, resolutionText, toPlot, useHistory } from "../history.js";

const SERIES = [
    {},
    area("--accent", "#63a8ff"),
    { stroke: stroke("--warn", "#9fc9f0"), width: 1 },
];

const BANDS = [{ series: [2, 1], fill: tint("--warn", "#9fc9f0") }];

// Metric renders one host value: numbers, a chart and the top consumers.
export function Metric({ title, subject, metric, format, hint, fallback, summary, yFormat, top,
                         icon, value, unit, kind, brief }) {
    const [period, setPeriod] = useState("1h");
    const [open, setOpen] = useState(false);

    const state = useHistory(subject, metric, period);

    const series = state.kind === "ready" ? state.series : null;
    const data = series ? toPlot(series) : null;

    return html`
        <section class="card tile" data-open=${open ? "1" : "0"}>
            <button class="cardhead" type="button" onClick=${() => setOpen((v) => !v)}>
                <span class="tname">${icon}<span>${title}</span>${open && hint && html`<span class="hint">${hint}</span>`}</span>
                <span class="chev">${Icon.chevron()}</span>
            </button>
            ${value != null && html`
                <div class=${`tval ${kind || ""}`}>${value}${unit && html`<s>${unit}</s>`}</div>
            `}
            ${brief && !open && html`<p class="tbrief">${brief}</p>`}
            ${open && summary}

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
            `}

            ${state.kind === "loading" && html`<p class="hint">Loading…</p>`}
            ${state.kind === "unavailable" && html`
                <p class="hint warn">${state.error}</p>
                ${fallback}
            `}
            ${state.kind === "failed" && html`<p class="hint crit">${state.error}</p>`}

            ${state.kind === "ready" && !data && html`
                <p class="hint">There is no data for this period — the history starts when it was turned on.</p>
            `}

            ${data && html`
                <${Chart} data=${data} series=${SERIES} height=${open ? 116 : 38} compact=${!open} bands=${BANDS} yFormat=${yFormat} />
                ${open && html`
                    <div class="legend">
                        <span class="mark avg"></span> average
                        <span class="mark max"></span> maximum
                        <span class="grow"></span>
                        <span class="hint">${resolutionText(series.resolution, series.stepSec)}</span>
                    </div>
                    ${format && html`<p class="sub">${format(series)}</p>`}
                `}
            `}

            ${open && top && html`
                <div class="subhead">who is the biggest</div>
                <${TopList} metric=${top} period=${period} />
            `}
        </section>
    `;
}
