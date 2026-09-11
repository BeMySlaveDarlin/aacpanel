// The "who used the most" list, drawn as bars.
import { html } from "../html.js";
import { bytes, pct, ago } from "../format.js";
import { SORT, coverageText, useTop } from "../top.js";

function value(row, metric) {
    if (metric === "disk") return row.delta === undefined ? 0 : row.delta;
    return row.max === undefined || row.max === null ? 0 : row.max;
}

function label(row, metric) {
    if (metric === "disk") return row.delta === undefined ? "—" : `+${bytes(row.delta)}`;
    return metric === "cpu" ? pct(row.max) : bytes(row.max);
}

export function TopList({ metric, period }) {
    const state = useTop(metric, period);
    const top = state.kind === "ready" ? state.top : null;
    const rows = (top && top.rows) || [];

    const most = rows.reduce((best, row) => Math.max(best, value(row, metric)), 0);

    return html`
        <div class="toplist">
            ${state.kind === "loading" && html`<p class="hint">Loading…</p>`}
            ${state.kind === "unavailable" && html`<p class="hint warn">${state.error}</p>`}
            ${state.kind === "failed" && html`<p class="hint crit">${state.error}</p>`}
            ${top && rows.length === 0 && html`<p class="hint">There is no data for this period.</p>`}

            ${rows.map((row) => {
                const share = most > 0 ? (value(row, metric) / most) * 100 : 0;
                const avg = metric !== "disk" && row.avg != null && most > 0
                    ? (row.avg / most) * 100
                    : null;
                const note = [
                    coverageText(row.coverage),
                    !row.alive && row.lastSeen
                        ? `last seen ${ago(new Date(row.lastSeen * 1000).toISOString())}`
                        : "",
                ].filter(Boolean).join(" · ");

                return html`
                    <div class="toprow" key=${row.container}>
                        <div class="topline">
                            <span class="nm">
                                ${row.container}
                                ${!row.alive && html`<span class="tag dead">down</span>`}
                            </span>
                            <span class="topval">${label(row, metric)}</span>
                        </div>
                        <div class="topbar ${row.alive ? "" : "dead"}">
                            <i class="mx" style=${`width:${Math.max(1, Math.min(100, share))}%`}></i>
                            ${avg !== null && html`<i class="av" style=${`width:${Math.max(0, Math.min(100, avg))}%`}></i>`}
                        </div>
                        ${note && html`<p class="sub">${note}</p>`}
                    </div>
                `;
            })}

            ${top && rows.length > 0 && html`
                <p class="hint">${SORT[top.sortBy] || top.sortBy} · ${top.resolution}${metric !== "disk" ? " · the thin bar is the average" : ""}</p>
            `}
        </div>
    `;
}
