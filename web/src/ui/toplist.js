// The "who used the most" list, drawn as bars: containers of the history and,
// where the host measures them, the processes eating the machine right now.
import { html } from "../html.js";
import { bytes, pct, ago } from "../format.js";
import { mergeTop, PROCS_OK, useProcs } from "../procs.js";
import { SORT, coverageText, useTop } from "../top.js";

const LIMIT = 12;

// Processes are collected for the two metrics the host measures per process;
// disk growth is written per container only.
function withProcs(metric) {
    return metric === "cpu" || metric === "mem";
}

function amount(row) {
    return Number.isFinite(row.value) ? row.value : 0;
}

function label(row, metric) {
    if (metric === "disk") return row.value === undefined ? "—" : `+${bytes(row.value)}`;
    return metric === "cpu" ? pct(amount(row)) : bytes(amount(row));
}

function note(row) {
    if (row.kind === "process") return row.note;
    return [
        coverageText(row.coverage),
        !row.alive && row.lastSeen
            ? `last seen ${ago(new Date(row.lastSeen * 1000).toISOString())}`
            : "",
    ].filter(Boolean).join(" · ");
}

// footer says what the numbers of a mixed list mean, because one row is an
// average over the period and the next one is this second.
function footer(top, metric, procs, rows) {
    const parts = [];
    const fromHistory = rows.some((row) => row.kind === "container");
    if (top && fromHistory) {
        parts.push([SORT[top.sortBy] || top.sortBy, top.resolution].filter(Boolean).join(" · "));
    }
    if (metric !== "disk" && fromHistory) parts.push("the thin bar is the average");
    if (withProcs(metric)) {
        parts.push(procs && procs.state === PROCS_OK
            ? "processes show what the machine is doing right now: no history is written for them"
            : "host processes are not here: the agent does not collect them");
    }
    return parts.filter(Boolean).join(" · ");
}

export function TopList({ metric, period }) {
    const state = useTop(metric, period);
    const { procs } = useProcs(withProcs(metric));

    const top = state.kind === "ready" ? state.top : null;
    const rows = mergeTop(top, withProcs(metric) ? procs : null, metric, {
        field: metric === "disk" ? "delta" : "max",
        limit: LIMIT,
    });

    const most = rows.reduce((best, row) => Math.max(best, amount(row)), 0);

    return html`
        <div class="toplist">
            ${state.kind === "loading" && html`<p class="hint">Loading…</p>`}
            ${state.kind === "unavailable" && html`<p class="hint warn">${state.error}</p>`}
            ${state.kind === "failed" && html`<p class="hint crit">${state.error}</p>`}
            ${top && rows.length === 0 && html`<p class="hint">There is no data for this period.</p>`}

            ${rows.map((row) => {
                const share = most > 0 ? (amount(row) / most) * 100 : 0;
                const avg = metric !== "disk" && row.avg != null && most > 0
                    ? (row.avg / most) * 100
                    : null;
                const sub = note(row);

                return html`
                    <div class="toprow" key=${row.key} title=${row.hint || row.name}>
                        <div class="topline">
                            <span class="nm">
                                <span class="who">${row.name}</span>
                                ${row.kind === "process" && html`<span class="tag proc">process</span>`}
                                ${!row.alive && html`<span class="tag dead">down</span>`}
                            </span>
                            <span class="topval">${label(row, metric)}</span>
                        </div>
                        <div class="topbar ${row.alive ? "" : "dead"} ${row.kind === "process" ? "proc" : ""}">
                            <i class="mx" style=${`width:${Math.max(1, Math.min(100, share))}%`}></i>
                            ${avg !== null && html`<i class="av" style=${`width:${Math.max(0, Math.min(100, avg))}%`}></i>`}
                        </div>
                        ${sub && html`<p class="sub">${sub}</p>`}
                    </div>
                `;
            })}

            ${rows.length > 0 && html`<p class="hint">${footer(top, metric, procs, rows)}</p>`}
        </div>
    `;
}
